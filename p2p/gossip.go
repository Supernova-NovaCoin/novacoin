// Package p2p implements the peer-to-peer networking layer for NovaCoin.
package p2p

import (
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// Topic represents a gossip topic.
type Topic string

const (
	TopicBlocks       Topic = "blocks"
	TopicTransactions Topic = "transactions"
	TopicConsensus    Topic = "consensus"
	TopicEncryptedTx  Topic = "encrypted_tx"
)

// GossipConfig holds gossip protocol configuration.
type GossipConfig struct {
	Fanout           int           // Number of peers to forward to
	MaxPropagation   int           // Maximum hops for a message
	DedupeWindow     time.Duration // Time to remember seen messages
	HeartbeatPeriod  time.Duration // Period between heartbeats
	HistoryLength    int           // Number of recent messages to track
	MaxPendingMsgs   int           // Maximum pending messages per topic
	LazyPushDelay    time.Duration // Delay before lazy push
	EagerPushRatio   float64       // Ratio of peers for eager push
}

// DefaultGossipConfig returns default configuration.
func DefaultGossipConfig() *GossipConfig {
	return &GossipConfig{
		Fanout:          6,
		MaxPropagation:  10,
		DedupeWindow:    2 * time.Minute,
		HeartbeatPeriod: 1 * time.Second,
		HistoryLength:   5000,
		MaxPendingMsgs:  1000,
		LazyPushDelay:   100 * time.Millisecond,
		EagerPushRatio:  0.5,
	}
}

// GossipMessage wraps a message for gossip propagation.
type GossipMessage struct {
	Topic      Topic
	Message    *Message
	Hops       int
	ReceivedAt time.Time
	SeenBy     map[PeerID]bool
}

// TopicSubscription represents a subscription to a topic.
type TopicSubscription struct {
	Topic    Topic
	Handler  func(*GossipMessage, PeerID)
	Priority int // Higher priority handlers run first
}

// GossipProtocol implements gossip-based message propagation.
type GossipProtocol struct {
	config    *GossipConfig
	peerStore *PeerStore

	// Subscriptions
	subscriptions map[Topic][]*TopicSubscription

	// Message deduplication
	seen      map[dag.Hash]time.Time
	seenOrder []dag.Hash

	// Message history per topic
	history map[Topic][]*GossipMessage

	// Pending messages for lazy push
	pending map[Topic][]*GossipMessage

	// Stats
	stats GossipStats

	// Callbacks
	sendFunc func(PeerID, *Message) error

	stopCh chan struct{}
	mu     sync.RWMutex
}

// GossipStats tracks gossip statistics.
type GossipStats struct {
	MessagesReceived  uint64
	MessagesSent      uint64
	DuplicatesDropped uint64
	PropagationFailed uint64
	ByTopic           map[Topic]uint64
}

// NewGossipProtocol creates a new gossip protocol.
func NewGossipProtocol(config *GossipConfig, peerStore *PeerStore) *GossipProtocol {
	if config == nil {
		config = DefaultGossipConfig()
	}

	return &GossipProtocol{
		config:        config,
		peerStore:     peerStore,
		subscriptions: make(map[Topic][]*TopicSubscription),
		seen:          make(map[dag.Hash]time.Time),
		seenOrder:     make([]dag.Hash, 0, config.HistoryLength),
		history:       make(map[Topic][]*GossipMessage),
		pending:       make(map[Topic][]*GossipMessage),
		stats:         GossipStats{ByTopic: make(map[Topic]uint64)},
		stopCh:        make(chan struct{}),
	}
}

// SetSendFunc sets the function used to send messages to peers.
func (gp *GossipProtocol) SetSendFunc(fn func(PeerID, *Message) error) {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	gp.sendFunc = fn
}

// Subscribe subscribes to a topic.
func (gp *GossipProtocol) Subscribe(topic Topic, handler func(*GossipMessage, PeerID), priority int) {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	sub := &TopicSubscription{
		Topic:    topic,
		Handler:  handler,
		Priority: priority,
	}

	subs := gp.subscriptions[topic]
	subs = append(subs, sub)

	// Sort by priority (descending)
	for i := len(subs) - 1; i > 0; i-- {
		if subs[i].Priority > subs[i-1].Priority {
			subs[i], subs[i-1] = subs[i-1], subs[i]
		}
	}

	gp.subscriptions[topic] = subs
}

// Unsubscribe removes a subscription handler.
func (gp *GossipProtocol) Unsubscribe(topic Topic, handler func(*GossipMessage, PeerID)) {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	subs := gp.subscriptions[topic]
	for i, sub := range subs {
		// Compare function pointers
		if &sub.Handler == &handler {
			gp.subscriptions[topic] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

// Publish publishes a message to a topic.
func (gp *GossipProtocol) Publish(topic Topic, msg *Message) error {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	hash := msg.Hash()

	// Check if already seen
	if _, exists := gp.seen[hash]; exists {
		return nil
	}

	// Mark as seen
	gp.markSeen(hash)

	// Create gossip message
	gossipMsg := &GossipMessage{
		Topic:      topic,
		Message:    msg,
		Hops:       0,
		ReceivedAt: time.Now(),
		SeenBy:     make(map[PeerID]bool),
	}

	// Add to history
	gp.addToHistory(topic, gossipMsg)

	// Propagate to peers
	gp.propagate(gossipMsg)

	gp.stats.MessagesSent++
	gp.stats.ByTopic[topic]++

	return nil
}

// HandleMessage handles an incoming gossip message.
func (gp *GossipProtocol) HandleMessage(msg *Message, sender PeerID, topic Topic) error {
	gp.mu.Lock()

	hash := msg.Hash()

	// Check if already seen
	if _, exists := gp.seen[hash]; exists {
		gp.stats.DuplicatesDropped++
		gp.mu.Unlock()
		return nil
	}

	// Mark as seen
	gp.markSeen(hash)

	// Create gossip message
	gossipMsg := &GossipMessage{
		Topic:      topic,
		Message:    msg,
		Hops:       1, // Will be incremented during propagation
		ReceivedAt: time.Now(),
		SeenBy:     map[PeerID]bool{sender: true},
	}

	// Add to history
	gp.addToHistory(topic, gossipMsg)

	// Get subscribers
	subs := gp.subscriptions[topic]
	gp.stats.MessagesReceived++
	gp.stats.ByTopic[topic]++

	gp.mu.Unlock()

	// Notify subscribers (outside lock)
	for _, sub := range subs {
		sub.Handler(gossipMsg, sender)
	}

	// Forward to other peers
	gp.mu.Lock()
	gp.propagate(gossipMsg)
	gp.mu.Unlock()

	return nil
}

func (gp *GossipProtocol) markSeen(hash dag.Hash) {
	gp.seen[hash] = time.Now()
	gp.seenOrder = append(gp.seenOrder, hash)

	// Prune if necessary
	if len(gp.seenOrder) > gp.config.HistoryLength {
		// Remove oldest entries
		pruneCount := len(gp.seenOrder) - gp.config.HistoryLength
		for i := 0; i < pruneCount; i++ {
			delete(gp.seen, gp.seenOrder[i])
		}
		gp.seenOrder = gp.seenOrder[pruneCount:]
	}
}

func (gp *GossipProtocol) addToHistory(topic Topic, msg *GossipMessage) {
	history := gp.history[topic]
	history = append(history, msg)

	// Keep limited history
	if len(history) > gp.config.HistoryLength/10 {
		history = history[1:]
	}
	gp.history[topic] = history
}

func (gp *GossipProtocol) propagate(msg *GossipMessage) {
	if gp.sendFunc == nil {
		return
	}

	// Don't propagate if max hops reached
	if msg.Hops >= gp.config.MaxPropagation {
		return
	}

	// Get active peers
	peers := gp.peerStore.GetActivePeers()
	if len(peers) == 0 {
		return
	}

	// Filter out peers who have seen this message
	var eligible []*Peer
	for _, peer := range peers {
		if !msg.SeenBy[peer.ID] {
			eligible = append(eligible, peer)
		}
	}

	if len(eligible) == 0 {
		return
	}

	// Select peers for eager push (random subset)
	eagerCount := int(float64(gp.config.Fanout) * gp.config.EagerPushRatio)
	if eagerCount > len(eligible) {
		eagerCount = len(eligible)
	}

	// Simple random selection (in production, use proper randomization)
	selectedPeers := eligible[:eagerCount]

	// Increment hop count for forwarding
	msg.Hops++

	// Send to selected peers
	for _, peer := range selectedPeers {
		msg.SeenBy[peer.ID] = true
		if err := gp.sendFunc(peer.ID, msg.Message); err != nil {
			gp.stats.PropagationFailed++
		} else {
			gp.stats.MessagesSent++
		}
	}

	// Queue remaining peers for lazy push
	if eagerCount < len(eligible) {
		remaining := eligible[eagerCount:]
		gp.queueLazyPush(msg.Topic, msg, remaining)
	}
}

func (gp *GossipProtocol) queueLazyPush(topic Topic, msg *GossipMessage, peers []*Peer) {
	// In a full implementation, this would queue messages for delayed sending
	// For now, we'll send immediately but with lower priority
	pending := gp.pending[topic]
	if len(pending) < gp.config.MaxPendingMsgs {
		gp.pending[topic] = append(pending, msg)
	}
}

// Start starts the gossip protocol background tasks.
func (gp *GossipProtocol) Start() {
	go gp.maintenanceLoop()
}

// Stop stops the gossip protocol.
func (gp *GossipProtocol) Stop() {
	close(gp.stopCh)
}

func (gp *GossipProtocol) maintenanceLoop() {
	ticker := time.NewTicker(gp.config.HeartbeatPeriod)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(gp.config.DedupeWindow / 2)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ticker.C:
			gp.processLazyPush()
		case <-cleanupTicker.C:
			gp.cleanupSeen()
		case <-gp.stopCh:
			return
		}
	}
}

func (gp *GossipProtocol) processLazyPush() {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	if gp.sendFunc == nil {
		return
	}

	// Process pending messages
	for topic, msgs := range gp.pending {
		for _, msg := range msgs {
			// Get peers who haven't seen this message
			peers := gp.peerStore.GetActivePeers()
			for _, peer := range peers {
				if !msg.SeenBy[peer.ID] {
					msg.SeenBy[peer.ID] = true
					gp.sendFunc(peer.ID, msg.Message)
				}
			}
		}
		gp.pending[topic] = nil
	}
}

func (gp *GossipProtocol) cleanupSeen() {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-gp.config.DedupeWindow)

	var newOrder []dag.Hash
	for _, hash := range gp.seenOrder {
		if seenTime, exists := gp.seen[hash]; exists {
			if seenTime.After(cutoff) {
				newOrder = append(newOrder, hash)
			} else {
				delete(gp.seen, hash)
			}
		}
	}
	gp.seenOrder = newOrder
}

// GetHistory returns recent messages for a topic.
func (gp *GossipProtocol) GetHistory(topic Topic, limit int) []*GossipMessage {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	history := gp.history[topic]
	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}

	// Return most recent
	start := len(history) - limit
	if start < 0 {
		start = 0
	}

	result := make([]*GossipMessage, limit)
	copy(result, history[start:])
	return result
}

// HasSeen checks if a message has been seen.
func (gp *GossipProtocol) HasSeen(hash dag.Hash) bool {
	gp.mu.RLock()
	defer gp.mu.RUnlock()
	_, exists := gp.seen[hash]
	return exists
}

// GetStats returns gossip statistics.
func (gp *GossipProtocol) GetStats() GossipStats {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	// Copy stats
	stats := gp.stats
	stats.ByTopic = make(map[Topic]uint64)
	for k, v := range gp.stats.ByTopic {
		stats.ByTopic[k] = v
	}
	return stats
}

// PeerScore returns a gossip-specific score for a peer.
func (gp *GossipProtocol) PeerScore(id PeerID) int32 {
	peer := gp.peerStore.GetPeer(id)
	if peer == nil {
		return 0
	}

	// Combine latency and reputation
	score := peer.GetScore()

	// Penalize high latency
	latency := peer.GetLatency()
	if latency > 500 {
		score -= int32((latency - 500) / 100)
	}

	return score
}
