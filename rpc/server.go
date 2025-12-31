// Package rpc implements the HTTP/WebSocket RPC server for NovaCoin.
package rpc

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ServerConfig contains the RPC server configuration.
type ServerConfig struct {
	// HTTP settings
	HTTPHost     string
	HTTPPort     int
	HTTPEnabled  bool
	HTTPTimeout  time.Duration
	HTTPCors     []string
	HTTPVhosts   []string

	// WebSocket settings
	WSHost     string
	WSPort     int
	WSEnabled  bool
	WSOrigins  []string
	WSTimeout  time.Duration

	// Authentication
	AuthEnabled bool
	AuthUser    string
	AuthPass    string

	// Rate limiting
	RateLimitEnabled bool
	RateLimitRPS     int
	RateLimitBurst   int

	// Request limits
	MaxRequestSize    int64
	MaxBatchSize      int
	MaxConnections    int

	// Enable namespaces
	EnabledNamespaces []string
}

// DefaultServerConfig returns the default server configuration.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		HTTPHost:        "localhost",
		HTTPPort:        8545,
		HTTPEnabled:     true,
		HTTPTimeout:     30 * time.Second,
		HTTPCors:        []string{"*"},
		HTTPVhosts:      []string{"localhost"},

		WSHost:          "localhost",
		WSPort:          8546,
		WSEnabled:       true,
		WSOrigins:       []string{"*"},
		WSTimeout:       60 * time.Second,

		AuthEnabled:     false,
		RateLimitEnabled: false,
		RateLimitRPS:    100,
		RateLimitBurst:  200,

		MaxRequestSize:  5 * 1024 * 1024, // 5MB
		MaxBatchSize:    100,
		MaxConnections:  500,

		EnabledNamespaces: []string{"eth", "net", "web3"},
	}
}

// Server is the RPC server.
type Server struct {
	config  *ServerConfig
	handler *Handler
	subMgr  *SubscriptionManager

	// HTTP servers
	httpServer *http.Server
	wsServer   *http.Server

	// Connection tracking
	connCount    int32
	activeConns  map[net.Conn]struct{}
	connMu       sync.Mutex

	// Rate limiter
	rateLimiter *RateLimiter

	// Lifecycle
	running bool
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// NewServer creates a new RPC server.
func NewServer(config *ServerConfig, backend Backend) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}

	handler := NewHandler()
	subMgr := NewSubscriptionManager(backend, 1000)

	server := &Server{
		config:      config,
		handler:     handler,
		subMgr:      subMgr,
		activeConns: make(map[net.Conn]struct{}),
		done:        make(chan struct{}),
	}

	// Setup rate limiter
	if config.RateLimitEnabled {
		server.rateLimiter = NewRateLimiter(config.RateLimitRPS, config.RateLimitBurst)
	}

	// Register standard APIs
	server.registerAPIs(backend)

	return server
}

// registerAPIs registers the standard API namespaces.
func (s *Server) registerAPIs(backend Backend) {
	// Check if namespace is enabled
	isEnabled := func(ns string) bool {
		for _, enabled := range s.config.EnabledNamespaces {
			if enabled == ns {
				return true
			}
		}
		return false
	}

	// eth namespace
	if isEnabled("eth") {
		ethAPI := NewEthAPI(backend)
		_ = s.handler.RegisterAPI("eth", ethAPI)

		// Subscribe API
		subAPI := NewSubscribeAPI(s.subMgr)
		_ = s.handler.RegisterAPI("eth", subAPI)
	}

	// net namespace
	if isEnabled("net") {
		netAPI := NewNetAPI(1, nil, nil)
		_ = s.handler.RegisterAPI("net", netAPI)
	}

	// web3 namespace
	if isEnabled("web3") {
		web3API := NewWeb3API("1.0.0")
		_ = s.handler.RegisterAPI("web3", web3API)
	}

	// txpool namespace
	if isEnabled("txpool") {
		txpoolAPI := NewTxPoolAPI(nil, nil)
		_ = s.handler.RegisterAPI("txpool", txpoolAPI)
	}

	// debug namespace
	if isEnabled("debug") {
		debugAPI := NewDebugAPI()
		_ = s.handler.RegisterAPI("debug", debugAPI)
	}

	// admin namespace
	if isEnabled("admin") {
		adminAPI := NewAdminAPI(nil, nil, nil, nil, nil)
		_ = s.handler.RegisterAPI("admin", adminAPI)
	}

	// nova namespace (NovaCoin-specific)
	if isEnabled("nova") {
		novaAPI := NewNovaAPI(nil, nil, nil, nil, nil)
		_ = s.handler.RegisterAPI("nova", novaAPI)
	}
}

// RegisterAPI registers a custom API namespace.
func (s *Server) RegisterAPI(namespace string, api interface{}) error {
	return s.handler.RegisterAPI(namespace, api)
}

// Start starts the RPC server.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New("server already running")
	}

	// Start HTTP server
	if s.config.HTTPEnabled {
		if err := s.startHTTP(); err != nil {
			return fmt.Errorf("failed to start HTTP server: %w", err)
		}
	}

	// Start WebSocket server
	if s.config.WSEnabled {
		if err := s.startWS(); err != nil {
			// Stop HTTP if WS fails
			if s.httpServer != nil {
				_ = s.httpServer.Shutdown(context.Background())
			}
			return fmt.Errorf("failed to start WebSocket server: %w", err)
		}
	}

	s.running = true
	return nil
}

// startHTTP starts the HTTP server.
func (s *Server) startHTTP() error {
	addr := fmt.Sprintf("%s:%d", s.config.HTTPHost, s.config.HTTPPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHTTP)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.wrapMiddleware(mux),
		ReadTimeout:  s.config.HTTPTimeout,
		WriteTimeout: s.config.HTTPTimeout,
		IdleTimeout:  60 * time.Second,
		ConnState:    s.connStateHandler,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error
		}
	}()

	return nil
}

// startWS starts the WebSocket server.
func (s *Server) startWS() error {
	addr := fmt.Sprintf("%s:%d", s.config.WSHost, s.config.WSPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWS)

	s.wsServer = &http.Server{
		Addr:         addr,
		Handler:      s.wrapMiddleware(mux),
		ReadTimeout:  s.config.WSTimeout,
		WriteTimeout: s.config.WSTimeout,
		IdleTimeout:  300 * time.Second,
		ConnState:    s.connStateHandler,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.wsServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error
		}
	}()

	return nil
}

// Stop stops the RPC server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	close(s.done)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	if s.httpServer != nil {
		if e := s.httpServer.Shutdown(ctx); e != nil {
			err = e
		}
	}
	if s.wsServer != nil {
		if e := s.wsServer.Shutdown(ctx); e != nil && err == nil {
			err = e
		}
	}

	// Close all subscriptions
	s.subMgr.CloseAll()

	s.wg.Wait()
	s.running = false

	return err
}

// handleHTTP handles HTTP JSON-RPC requests.
func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow POST
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check content type
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, s.config.MaxRequestSize))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Handle request
	response := s.handler.Handle(r.Context(), body)

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

// handleWS handles WebSocket connections.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket (simplified - real implementation would use gorilla/websocket)
	if !strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") {
		// Fallback to HTTP handling
		s.handleHTTP(w, r)
		return
	}

	// Would implement full WebSocket handshake here
	// For now, return upgrade required
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Upgrade", "websocket")
	w.WriteHeader(http.StatusSwitchingProtocols)
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

// wrapMiddleware wraps handlers with middleware.
func (s *Server) wrapMiddleware(handler http.Handler) http.Handler {
	// Apply middleware in order
	h := handler

	// Rate limiting
	if s.config.RateLimitEnabled {
		h = s.rateLimitMiddleware(h)
	}

	// Authentication
	if s.config.AuthEnabled {
		h = s.authMiddleware(h)
	}

	// CORS
	h = s.corsMiddleware(h)

	// Request logging/metrics
	h = s.loggingMiddleware(h)

	return h
}

// corsMiddleware handles CORS.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check allowed origins
		allowed := false
		for _, o := range s.config.HTTPCors {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware handles authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", "Basic realm=\"RPC\"")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse basic auth
		if !strings.HasPrefix(auth, "Basic ") {
			http.Error(w, "invalid auth method", http.StatusUnauthorized)
			return
		}

		decoded, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			http.Error(w, "invalid auth encoding", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid auth format", http.StatusUnauthorized)
			return
		}

		// Constant-time comparison
		userMatch := subtle.ConstantTimeCompare([]byte(parts[0]), []byte(s.config.AuthUser))
		passMatch := subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.config.AuthPass))

		if userMatch != 1 || passMatch != 1 {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware handles rate limiting.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimiter != nil && !s.rateLimiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Could add logging/metrics here
		next.ServeHTTP(w, r)
	})
}

// connStateHandler tracks connection state.
func (s *Server) connStateHandler(conn net.Conn, state http.ConnState) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	switch state {
	case http.StateNew:
		if int(atomic.LoadInt32(&s.connCount)) >= s.config.MaxConnections {
			conn.Close()
			return
		}
		atomic.AddInt32(&s.connCount, 1)
		s.activeConns[conn] = struct{}{}
	case http.StateClosed, http.StateHijacked:
		atomic.AddInt32(&s.connCount, -1)
		delete(s.activeConns, conn)
	}
}

// GetHandler returns the RPC handler.
func (s *Server) GetHandler() *Handler {
	return s.handler
}

// GetSubscriptionManager returns the subscription manager.
func (s *Server) GetSubscriptionManager() *SubscriptionManager {
	return s.subMgr
}

// Stats returns server statistics.
func (s *Server) Stats() ServerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ServerStats{
		Running:            s.running,
		HTTPEnabled:        s.config.HTTPEnabled,
		WSEnabled:          s.config.WSEnabled,
		ActiveConnections:  int(atomic.LoadInt32(&s.connCount)),
		RegisteredMethods:  len(s.handler.GetMethods()),
		ActiveSubscriptions: s.subMgr.GetStats().Total,
	}
}

// ServerStats contains server statistics.
type ServerStats struct {
	Running             bool
	HTTPEnabled         bool
	WSEnabled           bool
	ActiveConnections   int
	RegisteredMethods   int
	ActiveSubscriptions int
}

// RateLimiter is a simple token bucket rate limiter.
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rps, burst int) *RateLimiter {
	return &RateLimiter{
		tokens:     burst,
		maxTokens:  burst,
		refillRate: rps,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	tokensToAdd := int(elapsed.Seconds() * float64(rl.refillRate))
	if tokensToAdd > 0 {
		rl.tokens += tokensToAdd
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = now
	}

	// Check if we have tokens
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// IPRateLimiter provides per-IP rate limiting.
type IPRateLimiter struct {
	limiters map[string]*RateLimiter
	rps      int
	burst    int
	mu       sync.RWMutex
}

// NewIPRateLimiter creates a new per-IP rate limiter.
func NewIPRateLimiter(rps, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*RateLimiter),
		rps:      rps,
		burst:    burst,
	}
}

// Allow checks if a request from an IP is allowed.
func (rl *IPRateLimiter) Allow(ip string) bool {
	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		limiter = NewRateLimiter(rl.rps, rl.burst)
		rl.limiters[ip] = limiter
		rl.mu.Unlock()
	}

	return limiter.Allow()
}

// Cleanup removes stale limiters.
func (rl *IPRateLimiter) Cleanup(maxAge time.Duration) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for ip, limiter := range rl.limiters {
		if limiter.lastRefill.Before(cutoff) {
			delete(rl.limiters, ip)
			removed++
		}
	}

	return removed
}
