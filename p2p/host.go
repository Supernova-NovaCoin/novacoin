// Package p2p implements the peer-to-peer networking layer for NovaCoin.
package p2p

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

// HostConfig holds P2P host configuration.
type HostConfig struct {
	ListenAddress    string
	NetworkID        uint32
	NetworkMagic     uint32
	MaxInboundPeers  int
	MaxOutboundPeers int
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	PeerStore        *PeerStoreConfig
	Gossip           *GossipConfig
	Latency          *LatencyMonitorConfig
	BootstrapPeers   []string
	UserAgent        string
}

// DefaultHostConfig returns default configuration.
func DefaultHostConfig() *HostConfig {
	return &HostConfig{
		ListenAddress:    "0.0.0.0:30303",
		NetworkID:        1,
		NetworkMagic:     NetworkMagicMainnet,
		MaxInboundPeers:  50,
		MaxOutboundPeers: 50,
		DialTimeout:      10 * time.Second,
		HandshakeTimeout: 5 * time.Second,
		PeerStore:        DefaultPeerStoreConfig(),
		Gossip:           DefaultGossipConfig(),
		Latency:          DefaultLatencyMonitorConfig(),
		UserAgent:        "NovaCoin/1.0",
	}
}

// Host represents the P2P network host.
type Host struct {
	config *HostConfig

	// Identity
	nodeID     PeerID
	privateKey *crypto.BLSSecretKey
	publicKey  *crypto.BLSPublicKey

	// Components
	peerStore      *PeerStore
	gossip         *GossipProtocol
	latencyMonitor *LatencyMonitor

	// Connections
	listener     net.Listener
	connections  map[PeerID]net.Conn
	pendingDials map[string]bool

	// State
	bestBlockHash   dag.Hash
	bestBlockHeight uint64
	validatorIndex  *uint32

	// Message handlers
	messageHandlers map[MessageType]MessageHandler

	// Statistics
	stats HostStats

	ctx    context.Context
	cancel context.CancelFunc

	running bool
	mu      sync.RWMutex
}

// MessageHandler handles incoming messages.
type MessageHandler func(*Message, *Peer) error

// HostStats tracks host statistics.
type HostStats struct {
	StartTime        time.Time
	ConnectionsTotal uint64
	BytesSent        uint64
	BytesReceived    uint64
	MessagesSent     uint64
	MessagesReceived uint64
	HandshakesFailed uint64
}

// NewHost creates a new P2P host.
func NewHost(config *HostConfig) (*Host, error) {
	if config == nil {
		config = DefaultHostConfig()
	}

	// Initialize BLS
	if err := crypto.InitBLS(); err != nil {
		return nil, err
	}

	// Generate identity
	sk, pk, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	// Derive node ID from public key
	pkBytes := pk.Bytes()
	nodeID := PeerID(crypto.Hash(pkBytes))

	ctx, cancel := context.WithCancel(context.Background())

	h := &Host{
		config:          config,
		nodeID:          nodeID,
		privateKey:      sk,
		publicKey:       pk,
		connections:     make(map[PeerID]net.Conn),
		pendingDials:    make(map[string]bool),
		messageHandlers: make(map[MessageType]MessageHandler),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize components
	h.peerStore = NewPeerStore(config.PeerStore)
	h.gossip = NewGossipProtocol(config.Gossip, h.peerStore)
	h.latencyMonitor = NewLatencyMonitor(config.Latency, h.peerStore)

	// Wire up send functions
	h.gossip.SetSendFunc(h.sendMessage)
	h.latencyMonitor.SetSendPingFunc(h.sendPing)

	// Register default message handlers
	h.registerDefaultHandlers()

	return h, nil
}

// Start starts the P2P host.
func (h *Host) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return ErrHostAlreadyRunning
	}

	// Start listener
	listener, err := net.Listen("tcp", h.config.ListenAddress)
	if err != nil {
		return err
	}
	h.listener = listener

	h.running = true
	h.stats.StartTime = time.Now()

	// Start components
	h.gossip.Start()
	h.latencyMonitor.Start()

	// Start background tasks
	go h.acceptLoop()
	go h.maintainConnections()

	// Connect to bootstrap peers
	go h.connectToBootstrap()

	return nil
}

// Stop stops the P2P host.
func (h *Host) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return ErrHostNotRunning
	}

	h.cancel()
	h.running = false

	// Stop components
	h.gossip.Stop()
	h.latencyMonitor.Stop()
	h.peerStore.Stop()

	// Close listener
	if h.listener != nil {
		h.listener.Close()
	}

	// Close all connections
	for _, conn := range h.connections {
		conn.Close()
	}
	h.connections = make(map[PeerID]net.Conn)

	return nil
}

func (h *Host) acceptLoop() {
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		conn, err := h.listener.Accept()
		if err != nil {
			if h.running {
				continue
			}
			return
		}

		go h.handleInbound(conn)
	}
}

func (h *Host) handleInbound(conn net.Conn) {
	h.stats.ConnectionsTotal++

	// Perform handshake
	peer, err := h.performHandshake(conn, PeerInbound)
	if err != nil {
		h.stats.HandshakesFailed++
		conn.Close()
		return
	}

	h.mu.Lock()
	h.connections[peer.ID] = conn
	h.mu.Unlock()

	// Mark peer as active
	h.peerStore.SetPeerActive(peer.ID)

	// Handle messages
	h.handleConnection(peer, conn)
}

func (h *Host) performHandshake(conn net.Conn, direction PeerDirection) (*Peer, error) {
	conn.SetDeadline(time.Now().Add(h.config.HandshakeTimeout))
	defer conn.SetDeadline(time.Time{})

	// Send our handshake
	handshake := &HandshakeMessage{
		Version:         1,
		NetworkID:       h.config.NetworkID,
		NodeID:          h.nodeID,
		PublicKey:       dag.PublicKey(h.publicKey.To48Bytes()),
		BestBlockHash:   h.bestBlockHash,
		BestBlockHeight: h.bestBlockHeight,
		Timestamp:       time.Now().Unix(),
		UserAgent:       h.config.UserAgent,
		Capabilities:    []string{"gossip", "blocks", "txs"},
		ValidatorIndex:  h.validatorIndex,
	}

	msg := NewMessage(MsgHandshake, handshake.Serialize())
	data, err := msg.Serialize(h.config.NetworkMagic)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	// Read peer's handshake
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	peerMsg, err := DeserializeMessage(buf[:n], h.config.NetworkMagic)
	if err != nil {
		return nil, err
	}

	if peerMsg.Type != MsgHandshake && peerMsg.Type != MsgHandshakeAck {
		return nil, ErrInvalidHandshake
	}

	// Parse handshake (simplified - in production, deserialize properly)
	if len(peerMsg.Payload) < 32+48 {
		return nil, ErrInvalidHandshake
	}

	var peerNodeID PeerID
	copy(peerNodeID[:], peerMsg.Payload[8:40])

	// Create peer
	peer := NewPeer(peerNodeID, conn.RemoteAddr().String(), direction)
	peer.Conn = conn
	peer.State = PeerActive
	peer.ConnectedAt = time.Now()

	// Add to peer store
	if err := h.peerStore.AddPeer(peer); err != nil {
		return nil, err
	}

	return peer, nil
}

func (h *Host) handleConnection(peer *Peer, conn net.Conn) {
	defer func() {
		h.mu.Lock()
		delete(h.connections, peer.ID)
		h.mu.Unlock()
		h.peerStore.SetPeerInactive(peer.ID)
		conn.Close()
	}()

	buf := make([]byte, MaxMessageSize)
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		// Set read deadline
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		peer.Touch()
		peer.mu.Lock()
		peer.BytesReceived += uint64(n)
		peer.MessagesRecv++
		peer.mu.Unlock()

		h.stats.BytesReceived += uint64(n)
		h.stats.MessagesReceived++

		// Parse message
		msg, err := DeserializeMessage(buf[:n], h.config.NetworkMagic)
		if err != nil {
			peer.AdjustScore(-10)
			continue
		}

		msg.Sender = peer.ID

		// Handle message
		h.handleMessage(msg, peer)
	}
}

func (h *Host) handleMessage(msg *Message, peer *Peer) {
	h.mu.RLock()
	handler, exists := h.messageHandlers[msg.Type]
	h.mu.RUnlock()

	if exists {
		if err := handler(msg, peer); err != nil {
			peer.AdjustScore(-5)
		}
	}

	// Forward to gossip if applicable
	switch msg.Type {
	case MsgNewBlock:
		h.gossip.HandleMessage(msg, peer.ID, TopicBlocks)
	case MsgNewTransaction:
		h.gossip.HandleMessage(msg, peer.ID, TopicTransactions)
	case MsgEncryptedTx:
		h.gossip.HandleMessage(msg, peer.ID, TopicEncryptedTx)
	case MsgVote, MsgProposal, MsgCommit, MsgReveal:
		h.gossip.HandleMessage(msg, peer.ID, TopicConsensus)
	}
}

func (h *Host) registerDefaultHandlers() {
	// Ping/Pong
	h.RegisterHandler(MsgPing, h.handlePing)
	h.RegisterHandler(MsgPong, h.handlePong)

	// Peer discovery
	h.RegisterHandler(MsgGetPeers, h.handleGetPeers)
	h.RegisterHandler(MsgPeers, h.handlePeers)
}

func (h *Host) handlePing(msg *Message, peer *Peer) error {
	ping, err := DeserializePing(msg.Payload)
	if err != nil {
		return err
	}

	pong := &PongMessage{
		Nonce:         ping.Nonce,
		OrigTimestamp: ping.Timestamp,
		RecvTimestamp: time.Now().UnixNano(),
	}

	return h.sendMessage(peer.ID, NewMessage(MsgPong, pong.Serialize()))
}

func (h *Host) handlePong(msg *Message, peer *Peer) error {
	pong, err := DeserializePong(msg.Payload)
	if err != nil {
		return err
	}

	h.latencyMonitor.RecordPong(peer.ID, pong.Nonce, pong.OrigTimestamp, pong.RecvTimestamp)
	return nil
}

func (h *Host) handleGetPeers(msg *Message, peer *Peer) error {
	peers := h.peerStore.GetPeersByScore(20)

	// Build peer list
	var peerInfos []PeerInfo
	for _, p := range peers {
		if p.ID != peer.ID {
			peerInfos = append(peerInfos, PeerInfo{
				ID:      p.ID,
				Address: p.Address,
				Score:   p.GetScore(),
			})
		}
	}

	// Serialize and send (simplified)
	// In production, properly serialize the peer list
	return nil
}

func (h *Host) handlePeers(msg *Message, peer *Peer) error {
	// Parse and add new peers
	// In production, deserialize and validate
	return nil
}

func (h *Host) sendMessage(peerID PeerID, msg *Message) error {
	h.mu.RLock()
	conn, exists := h.connections[peerID]
	h.mu.RUnlock()

	if !exists {
		return ErrPeerNotConnected
	}

	data, err := msg.Serialize(h.config.NetworkMagic)
	if err != nil {
		return err
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.Write(data)
	if err != nil {
		return err
	}

	h.stats.BytesSent += uint64(len(data))
	h.stats.MessagesSent++

	if peer := h.peerStore.GetPeer(peerID); peer != nil {
		peer.mu.Lock()
		peer.BytesSent += uint64(len(data))
		peer.MessagesSent++
		peer.mu.Unlock()
	}

	return nil
}

func (h *Host) sendPing(peerID PeerID, nonce uint64) error {
	ping := &PingMessage{
		Nonce:     nonce,
		Timestamp: time.Now().UnixNano(),
	}
	return h.sendMessage(peerID, NewMessage(MsgPing, ping.Serialize()))
}

func (h *Host) maintainConnections() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.dialMorePeers()
		}
	}
}

func (h *Host) dialMorePeers() {
	activePeers := h.peerStore.ActiveCount()
	needed := h.config.MaxOutboundPeers - activePeers

	if needed <= 0 {
		return
	}

	// Get peers to dial
	candidates := h.peerStore.GetPeersByScore(needed * 2)

	for _, peer := range candidates {
		if needed <= 0 {
			break
		}
		if peer.IsActive() {
			continue
		}

		h.mu.Lock()
		if h.pendingDials[peer.Address] {
			h.mu.Unlock()
			continue
		}
		h.pendingDials[peer.Address] = true
		h.mu.Unlock()

		go h.dialPeer(peer.Address)
		needed--
	}
}

func (h *Host) dialPeer(address string) {
	defer func() {
		h.mu.Lock()
		delete(h.pendingDials, address)
		h.mu.Unlock()
	}()

	conn, err := net.DialTimeout("tcp", address, h.config.DialTimeout)
	if err != nil {
		return
	}

	peer, err := h.performHandshake(conn, PeerOutbound)
	if err != nil {
		conn.Close()
		h.stats.HandshakesFailed++
		return
	}

	h.mu.Lock()
	h.connections[peer.ID] = conn
	h.mu.Unlock()

	h.peerStore.SetPeerActive(peer.ID)
	h.handleConnection(peer, conn)
}

func (h *Host) connectToBootstrap() {
	for _, addr := range h.config.BootstrapPeers {
		go h.dialPeer(addr)
	}
}

// RegisterHandler registers a message handler.
func (h *Host) RegisterHandler(msgType MessageType, handler MessageHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messageHandlers[msgType] = handler
}

// Broadcast broadcasts a message to all peers.
func (h *Host) Broadcast(msg *Message) {
	peers := h.peerStore.GetActivePeers()
	for _, peer := range peers {
		h.sendMessage(peer.ID, msg)
	}
}

// Publish publishes a message to a gossip topic.
func (h *Host) Publish(topic Topic, msg *Message) error {
	return h.gossip.Publish(topic, msg)
}

// Subscribe subscribes to a gossip topic.
func (h *Host) Subscribe(topic Topic, handler func(*GossipMessage, PeerID)) {
	h.gossip.Subscribe(topic, handler, 0)
}

// GetPeerStore returns the peer store.
func (h *Host) GetPeerStore() *PeerStore {
	return h.peerStore
}

// GetGossip returns the gossip protocol.
func (h *Host) GetGossip() *GossipProtocol {
	return h.gossip
}

// GetLatencyMonitor returns the latency monitor.
func (h *Host) GetLatencyMonitor() *LatencyMonitor {
	return h.latencyMonitor
}

// GetNodeID returns the node ID.
func (h *Host) GetNodeID() PeerID {
	return h.nodeID
}

// SetBestBlock sets the best block info for handshakes.
func (h *Host) SetBestBlock(hash dag.Hash, height uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bestBlockHash = hash
	h.bestBlockHeight = height
}

// SetValidatorIndex sets the validator index for handshakes.
func (h *Host) SetValidatorIndex(index uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.validatorIndex = &index
}

// GetStats returns host statistics.
func (h *Host) GetStats() HostStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// IsRunning returns true if the host is running.
func (h *Host) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.running
}

// Error types
var (
	ErrHostAlreadyRunning = errors.New("host already running")
	ErrHostNotRunning     = errors.New("host not running")
	ErrPeerNotConnected   = errors.New("peer not connected")
	ErrInvalidHandshake   = errors.New("invalid handshake")
)
