// Package p2p implements the peer-to-peer networking layer for NovaCoin.
package p2p

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// PeerID represents a unique peer identifier (derived from public key).
type PeerID [32]byte

// String returns a hex representation of the peer ID.
func (id PeerID) String() string {
	return string(id[:8]) // Short form for display
}

// PeerState represents the connection state of a peer.
type PeerState int

const (
	PeerDisconnected PeerState = iota
	PeerConnecting
	PeerConnected
	PeerHandshaking
	PeerActive
	PeerBanned
)

func (s PeerState) String() string {
	switch s {
	case PeerDisconnected:
		return "disconnected"
	case PeerConnecting:
		return "connecting"
	case PeerConnected:
		return "connected"
	case PeerHandshaking:
		return "handshaking"
	case PeerActive:
		return "active"
	case PeerBanned:
		return "banned"
	default:
		return "unknown"
	}
}

// PeerDirection indicates if we initiated the connection or received it.
type PeerDirection int

const (
	PeerInbound PeerDirection = iota
	PeerOutbound
)

// Peer represents a connected peer in the network.
type Peer struct {
	ID        PeerID
	PublicKey dag.PublicKey
	Address   string // IP:Port
	State     PeerState
	Direction PeerDirection

	// Connection
	Conn       net.Conn
	LastSeen   time.Time
	ConnectedAt time.Time

	// Latency tracking
	LatencyMs    uint32  // Current RTT in milliseconds
	AvgLatencyMs float64 // Moving average RTT
	MinLatencyMs uint32  // Minimum observed RTT
	MaxLatencyMs uint32  // Maximum observed RTT
	PingCount    uint64  // Number of pings sent

	// Bandwidth tracking
	BytesSent     uint64
	BytesReceived uint64
	MessagesSent  uint64
	MessagesRecv  uint64

	// Reputation
	Score           int32 // -1000 to 1000
	ProtocolVersion uint32
	UserAgent       string
	Capabilities    []string

	// Validator info (if known)
	ValidatorIndex *uint32
	ValidatorStake uint64

	mu sync.RWMutex
}

// NewPeer creates a new peer.
func NewPeer(id PeerID, address string, direction PeerDirection) *Peer {
	return &Peer{
		ID:        id,
		Address:   address,
		State:     PeerDisconnected,
		Direction: direction,
		Score:     100, // Start with positive score
		LastSeen:  time.Now(),
	}
}

// UpdateLatency updates the peer's latency statistics.
func (p *Peer) UpdateLatency(latencyMs uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.LatencyMs = latencyMs
	p.PingCount++

	// Update min/max
	if p.MinLatencyMs == 0 || latencyMs < p.MinLatencyMs {
		p.MinLatencyMs = latencyMs
	}
	if latencyMs > p.MaxLatencyMs {
		p.MaxLatencyMs = latencyMs
	}

	// Exponential moving average (alpha = 0.2)
	if p.AvgLatencyMs == 0 {
		p.AvgLatencyMs = float64(latencyMs)
	} else {
		p.AvgLatencyMs = 0.2*float64(latencyMs) + 0.8*p.AvgLatencyMs
	}
}

// AdjustScore adjusts the peer's reputation score.
func (p *Peer) AdjustScore(delta int32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Score += delta
	if p.Score > 1000 {
		p.Score = 1000
	}
	if p.Score < -1000 {
		p.Score = -1000
	}
}

// IsActive returns true if the peer is active.
func (p *Peer) IsActive() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State == PeerActive
}

// GetLatency returns the current latency.
func (p *Peer) GetLatency() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.LatencyMs
}

// GetScore returns the current reputation score.
func (p *Peer) GetScore() int32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Score
}

// Touch updates the last seen time.
func (p *Peer) Touch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastSeen = time.Now()
}

// PeerStoreConfig holds peer store configuration.
type PeerStoreConfig struct {
	MaxPeers         int           // Maximum peers to track
	MaxActivePeers   int           // Maximum concurrent connections
	BanDuration      time.Duration // How long bans last
	PruneInterval    time.Duration // How often to prune stale peers
	StaleTimeout     time.Duration // When a peer is considered stale
	MinScore         int32         // Minimum score before banning
}

// DefaultPeerStoreConfig returns default configuration.
func DefaultPeerStoreConfig() *PeerStoreConfig {
	return &PeerStoreConfig{
		MaxPeers:       1000,
		MaxActivePeers: 100,
		BanDuration:    24 * time.Hour,
		PruneInterval:  5 * time.Minute,
		StaleTimeout:   30 * time.Minute,
		MinScore:       -500,
	}
}

// PeerStore manages all known peers.
type PeerStore struct {
	config *PeerStoreConfig

	peers       map[PeerID]*Peer
	byAddress   map[string]PeerID
	banned      map[PeerID]time.Time // ID -> unban time
	activeCount int

	stats PeerStoreStats

	stopPrune chan struct{}
	mu        sync.RWMutex
}

// PeerStoreStats tracks peer store statistics.
type PeerStoreStats struct {
	TotalPeers     int
	ActivePeers    int
	BannedPeers    int
	InboundPeers   int
	OutboundPeers  int
	AvgLatencyMs   float64
	TotalBytesSent uint64
	TotalBytesRecv uint64
}

// NewPeerStore creates a new peer store.
func NewPeerStore(config *PeerStoreConfig) *PeerStore {
	if config == nil {
		config = DefaultPeerStoreConfig()
	}

	ps := &PeerStore{
		config:    config,
		peers:     make(map[PeerID]*Peer),
		byAddress: make(map[string]PeerID),
		banned:    make(map[PeerID]time.Time),
		stopPrune: make(chan struct{}),
	}

	go ps.pruneLoop()
	return ps
}

// AddPeer adds or updates a peer.
func (ps *PeerStore) AddPeer(peer *Peer) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if banned
	if unbanTime, banned := ps.banned[peer.ID]; banned {
		if time.Now().Before(unbanTime) {
			return ErrPeerBanned
		}
		delete(ps.banned, peer.ID)
	}

	// Check capacity
	if _, exists := ps.peers[peer.ID]; !exists {
		if len(ps.peers) >= ps.config.MaxPeers {
			return ErrTooManyPeers
		}
	}

	ps.peers[peer.ID] = peer
	ps.byAddress[peer.Address] = peer.ID
	ps.updateStats()

	return nil
}

// RemovePeer removes a peer.
func (ps *PeerStore) RemovePeer(id PeerID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if peer, exists := ps.peers[id]; exists {
		delete(ps.byAddress, peer.Address)
		delete(ps.peers, id)
		ps.updateStats()
	}
}

// GetPeer returns a peer by ID.
func (ps *PeerStore) GetPeer(id PeerID) *Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.peers[id]
}

// GetPeerByAddress returns a peer by address.
func (ps *PeerStore) GetPeerByAddress(address string) *Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if id, exists := ps.byAddress[address]; exists {
		return ps.peers[id]
	}
	return nil
}

// GetActivePeers returns all active peers.
func (ps *PeerStore) GetActivePeers() []*Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var active []*Peer
	for _, peer := range ps.peers {
		if peer.IsActive() {
			active = append(active, peer)
		}
	}
	return active
}

// GetPeersByScore returns peers sorted by score (highest first).
func (ps *PeerStore) GetPeersByScore(limit int) []*Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	peers := make([]*Peer, 0, len(ps.peers))
	for _, peer := range ps.peers {
		if peer.State != PeerBanned {
			peers = append(peers, peer)
		}
	}

	// Sort by score descending
	for i := 0; i < len(peers)-1; i++ {
		for j := i + 1; j < len(peers); j++ {
			if peers[j].GetScore() > peers[i].GetScore() {
				peers[i], peers[j] = peers[j], peers[i]
			}
		}
	}

	if limit > 0 && limit < len(peers) {
		peers = peers[:limit]
	}
	return peers
}

// BanPeer bans a peer.
func (ps *PeerStore) BanPeer(id PeerID, reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.banned[id] = time.Now().Add(ps.config.BanDuration)

	if peer, exists := ps.peers[id]; exists {
		peer.State = PeerBanned
	}
	ps.updateStats()
}

// UnbanPeer unbans a peer.
func (ps *PeerStore) UnbanPeer(id PeerID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	delete(ps.banned, id)
	if peer, exists := ps.peers[id]; exists {
		if peer.State == PeerBanned {
			peer.State = PeerDisconnected
		}
	}
	ps.updateStats()
}

// IsBanned checks if a peer is banned.
func (ps *PeerStore) IsBanned(id PeerID) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if unbanTime, banned := ps.banned[id]; banned {
		return time.Now().Before(unbanTime)
	}
	return false
}

// SetPeerActive marks a peer as active.
func (ps *PeerStore) SetPeerActive(id PeerID) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	peer, exists := ps.peers[id]
	if !exists {
		return ErrPeerNotFound
	}

	if ps.activeCount >= ps.config.MaxActivePeers && peer.State != PeerActive {
		return ErrTooManyActivePeers
	}

	if peer.State != PeerActive {
		ps.activeCount++
	}
	peer.State = PeerActive
	peer.ConnectedAt = time.Now()
	ps.updateStats()

	return nil
}

// SetPeerInactive marks a peer as inactive.
func (ps *PeerStore) SetPeerInactive(id PeerID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if peer, exists := ps.peers[id]; exists {
		if peer.State == PeerActive {
			ps.activeCount--
		}
		peer.State = PeerDisconnected
		ps.updateStats()
	}
}

// GetStats returns peer store statistics.
func (ps *PeerStore) GetStats() PeerStoreStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.stats
}

// GetAverageLatency returns the average latency across active peers.
func (ps *PeerStore) GetAverageLatency() float64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var total float64
	var count int
	for _, peer := range ps.peers {
		if peer.IsActive() && peer.AvgLatencyMs > 0 {
			total += peer.AvgLatencyMs
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func (ps *PeerStore) updateStats() {
	var inbound, outbound, active, banned int
	var totalLatency float64
	var latencyCount int
	var bytesSent, bytesRecv uint64

	for _, peer := range ps.peers {
		if peer.Direction == PeerInbound {
			inbound++
		} else {
			outbound++
		}
		if peer.IsActive() {
			active++
			if peer.AvgLatencyMs > 0 {
				totalLatency += peer.AvgLatencyMs
				latencyCount++
			}
		}
		bytesSent += peer.BytesSent
		bytesRecv += peer.BytesReceived
	}

	for id := range ps.banned {
		if time.Now().Before(ps.banned[id]) {
			banned++
		}
	}

	avgLatency := float64(0)
	if latencyCount > 0 {
		avgLatency = totalLatency / float64(latencyCount)
	}

	ps.stats = PeerStoreStats{
		TotalPeers:     len(ps.peers),
		ActivePeers:    active,
		BannedPeers:    banned,
		InboundPeers:   inbound,
		OutboundPeers:  outbound,
		AvgLatencyMs:   avgLatency,
		TotalBytesSent: bytesSent,
		TotalBytesRecv: bytesRecv,
	}
}

// pruneLoop periodically removes stale peers.
func (ps *PeerStore) pruneLoop() {
	ticker := time.NewTicker(ps.config.PruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ps.prune()
		case <-ps.stopPrune:
			return
		}
	}
}

func (ps *PeerStore) prune() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	var toRemove []PeerID

	for id, peer := range ps.peers {
		// Remove stale inactive peers
		if peer.State == PeerDisconnected && now.Sub(peer.LastSeen) > ps.config.StaleTimeout {
			toRemove = append(toRemove, id)
			continue
		}

		// Ban peers with very low score
		if peer.Score < ps.config.MinScore {
			ps.banned[id] = now.Add(ps.config.BanDuration)
			peer.State = PeerBanned
		}
	}

	// Clean up expired bans
	for id, unbanTime := range ps.banned {
		if now.After(unbanTime) {
			delete(ps.banned, id)
		}
	}

	for _, id := range toRemove {
		if peer, exists := ps.peers[id]; exists {
			delete(ps.byAddress, peer.Address)
		}
		delete(ps.peers, id)
	}

	ps.updateStats()
}

// Stop stops the peer store.
func (ps *PeerStore) Stop() {
	close(ps.stopPrune)
}

// Count returns the total number of peers.
func (ps *PeerStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}

// ActiveCount returns the number of active peers.
func (ps *PeerStore) ActiveCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.activeCount
}

// Error types
var (
	ErrPeerNotFound       = errors.New("peer not found")
	ErrPeerBanned         = errors.New("peer is banned")
	ErrTooManyPeers       = errors.New("too many peers")
	ErrTooManyActivePeers = errors.New("too many active peers")
)
