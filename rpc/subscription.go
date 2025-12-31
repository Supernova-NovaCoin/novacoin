// Package rpc implements WebSocket subscriptions for NovaCoin.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// SubscriptionID represents a subscription identifier.
type SubscriptionID string

// SubscriptionType represents the type of subscription.
type SubscriptionType string

const (
	// NewHeads subscribes to new block headers.
	SubNewHeads SubscriptionType = "newHeads"
	// NewPendingTransactions subscribes to pending transactions.
	SubPendingTx SubscriptionType = "newPendingTransactions"
	// Logs subscribes to new logs matching a filter.
	SubLogs SubscriptionType = "logs"
	// Syncing subscribes to sync status changes.
	SubSyncing SubscriptionType = "syncing"
)

// SubscriptionRequest represents a subscription request.
type SubscriptionRequest struct {
	Type   SubscriptionType `json:"type"`
	Filter *FilterQuery     `json:"filter,omitempty"`
}

// SubscriptionResult represents a subscription notification.
type SubscriptionResult struct {
	Subscription SubscriptionID `json:"subscription"`
	Result       interface{}    `json:"result"`
}

// Subscription represents an active subscription.
type Subscription struct {
	ID        SubscriptionID
	Type      SubscriptionType
	Filter    *FilterQuery
	Channel   chan interface{}
	Created   time.Time
	LastSent  time.Time
	Sent      uint64
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
}

// NewSubscription creates a new subscription.
func NewSubscription(id SubscriptionID, subType SubscriptionType, filter *FilterQuery) *Subscription {
	ctx, cancel := context.WithCancel(context.Background())
	return &Subscription{
		ID:      id,
		Type:    subType,
		Filter:  filter,
		Channel: make(chan interface{}, 100),
		Created: time.Now(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Send sends data to the subscription channel.
func (s *Subscription) Send(data interface{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.ctx.Done():
		return false
	case s.Channel <- data:
		s.LastSent = time.Now()
		atomic.AddUint64(&s.Sent, 1)
		return true
	default:
		// Channel full, skip
		return false
	}
}

// Close closes the subscription.
func (s *Subscription) Close() {
	s.cancel()
	close(s.Channel)
}

// IsClosed returns true if the subscription is closed.
func (s *Subscription) IsClosed() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

// SubscriptionManager manages WebSocket subscriptions.
type SubscriptionManager struct {
	// Active subscriptions by ID
	subscriptions map[SubscriptionID]*Subscription

	// Subscriptions by type for efficient broadcasting
	byType map[SubscriptionType]map[SubscriptionID]*Subscription

	// Subscription ID counter
	idCounter uint64

	// Backend for log matching
	backend Backend

	// Max subscriptions per connection
	maxSubscriptions int

	mu sync.RWMutex
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager(backend Backend, maxSubscriptions int) *SubscriptionManager {
	if maxSubscriptions <= 0 {
		maxSubscriptions = 100
	}
	return &SubscriptionManager{
		subscriptions:    make(map[SubscriptionID]*Subscription),
		byType:           make(map[SubscriptionType]map[SubscriptionID]*Subscription),
		backend:          backend,
		maxSubscriptions: maxSubscriptions,
	}
}

// Subscribe creates a new subscription.
func (sm *SubscriptionManager) Subscribe(subType SubscriptionType, filter *FilterQuery) (*Subscription, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check subscription limit
	if len(sm.subscriptions) >= sm.maxSubscriptions {
		return nil, errors.New("max subscriptions reached")
	}

	// Validate subscription type
	switch subType {
	case SubNewHeads, SubPendingTx, SubLogs, SubSyncing:
		// Valid
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", subType)
	}

	// Validate logs filter
	if subType == SubLogs && filter == nil {
		return nil, errors.New("logs subscription requires a filter")
	}

	// Generate ID
	id := sm.generateID()

	// Create subscription
	sub := NewSubscription(id, subType, filter)

	// Register
	sm.subscriptions[id] = sub
	if sm.byType[subType] == nil {
		sm.byType[subType] = make(map[SubscriptionID]*Subscription)
	}
	sm.byType[subType][id] = sub

	return sub, nil
}

// Unsubscribe removes a subscription.
func (sm *SubscriptionManager) Unsubscribe(id SubscriptionID) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sub, exists := sm.subscriptions[id]
	if !exists {
		return false
	}

	// Close subscription
	sub.Close()

	// Remove from maps
	delete(sm.subscriptions, id)
	if sm.byType[sub.Type] != nil {
		delete(sm.byType[sub.Type], id)
	}

	return true
}

// Get returns a subscription by ID.
func (sm *SubscriptionManager) Get(id SubscriptionID) *Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.subscriptions[id]
}

// generateID generates a unique subscription ID.
func (sm *SubscriptionManager) generateID() SubscriptionID {
	sm.idCounter++
	return SubscriptionID(fmt.Sprintf("0x%016x", sm.idCounter))
}

// BroadcastNewHead broadcasts a new block header to subscribers.
func (sm *SubscriptionManager) BroadcastNewHead(block *Block) {
	sm.mu.RLock()
	subs := sm.byType[SubNewHeads]
	sm.mu.RUnlock()

	for _, sub := range subs {
		sub.Send(block)
	}
}

// BroadcastPendingTx broadcasts a pending transaction hash to subscribers.
func (sm *SubscriptionManager) BroadcastPendingTx(hash dag.Hash) {
	sm.mu.RLock()
	subs := sm.byType[SubPendingTx]
	sm.mu.RUnlock()

	for _, sub := range subs {
		sub.Send(HashToHex(hash))
	}
}

// BroadcastLog broadcasts a log to matching subscribers.
func (sm *SubscriptionManager) BroadcastLog(log Log) {
	sm.mu.RLock()
	subs := sm.byType[SubLogs]
	sm.mu.RUnlock()

	for _, sub := range subs {
		if sub.Filter != nil && sm.matchesFilter(log, sub.Filter) {
			sub.Send(log)
		} else if sub.Filter == nil {
			sub.Send(log)
		}
	}
}

// BroadcastLogs broadcasts multiple logs to matching subscribers.
func (sm *SubscriptionManager) BroadcastLogs(logs []Log) {
	for _, log := range logs {
		sm.BroadcastLog(log)
	}
}

// BroadcastSyncStatus broadcasts sync status to subscribers.
func (sm *SubscriptionManager) BroadcastSyncStatus(syncing bool, status *SyncStatus) {
	sm.mu.RLock()
	subs := sm.byType[SubSyncing]
	sm.mu.RUnlock()

	var data interface{}
	if syncing {
		data = status
	} else {
		data = false
	}

	for _, sub := range subs {
		sub.Send(data)
	}
}

// matchesFilter checks if a log matches a filter.
func (sm *SubscriptionManager) matchesFilter(log Log, filter *FilterQuery) bool {
	// Check address
	if len(filter.Addresses) > 0 {
		matched := false
		for _, addr := range filter.Addresses {
			if log.Address == addr {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check topics
	for i, topicFilter := range filter.Topics {
		if len(topicFilter) == 0 {
			continue // Null = wildcard
		}

		if i >= len(log.Topics) {
			return false // Log doesn't have this topic position
		}

		// Any of the filter topics must match
		matched := false
		for _, topic := range topicFilter {
			if log.Topics[i] == topic {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// GetStats returns subscription statistics.
func (sm *SubscriptionManager) GetStats() SubscriptionStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := SubscriptionStats{
		Total:   len(sm.subscriptions),
		ByType:  make(map[SubscriptionType]int),
	}

	for subType, subs := range sm.byType {
		stats.ByType[subType] = len(subs)
	}

	return stats
}

// SubscriptionStats contains subscription statistics.
type SubscriptionStats struct {
	Total  int                       `json:"total"`
	ByType map[SubscriptionType]int  `json:"byType"`
}

// Cleanup removes closed subscriptions.
func (sm *SubscriptionManager) Cleanup() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	removed := 0
	for id, sub := range sm.subscriptions {
		if sub.IsClosed() {
			delete(sm.subscriptions, id)
			if sm.byType[sub.Type] != nil {
				delete(sm.byType[sub.Type], id)
			}
			removed++
		}
	}

	return removed
}

// CloseAll closes all subscriptions.
func (sm *SubscriptionManager) CloseAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, sub := range sm.subscriptions {
		sub.Close()
	}

	sm.subscriptions = make(map[SubscriptionID]*Subscription)
	sm.byType = make(map[SubscriptionType]map[SubscriptionID]*Subscription)
}

// SubscribeAPI provides eth_subscribe and eth_unsubscribe methods.
type SubscribeAPI struct {
	manager *SubscriptionManager
}

// NewSubscribeAPI creates a new subscribe API.
func NewSubscribeAPI(manager *SubscriptionManager) *SubscribeAPI {
	return &SubscribeAPI{
		manager: manager,
	}
}

// Subscribe creates a new subscription.
func (api *SubscribeAPI) Subscribe(ctx context.Context, subType SubscriptionType, filter *FilterQuery) (SubscriptionID, error) {
	sub, err := api.manager.Subscribe(subType, filter)
	if err != nil {
		return "", err
	}
	return sub.ID, nil
}

// Unsubscribe removes a subscription.
func (api *SubscribeAPI) Unsubscribe(ctx context.Context, id SubscriptionID) (bool, error) {
	return api.manager.Unsubscribe(id), nil
}

// NotificationWriter defines the interface for sending subscription notifications.
type NotificationWriter interface {
	WriteNotification(subID SubscriptionID, data interface{}) error
}

// SubscriptionNotification represents a subscription notification message.
type SubscriptionNotification struct {
	JSONRPC string             `json:"jsonrpc"`
	Method  string             `json:"method"`
	Params  SubscriptionParams `json:"params"`
}

// SubscriptionParams represents subscription notification params.
type SubscriptionParams struct {
	Subscription SubscriptionID `json:"subscription"`
	Result       interface{}    `json:"result"`
}

// MarshalNotification marshals a subscription notification.
func MarshalNotification(subID SubscriptionID, result interface{}) ([]byte, error) {
	notif := SubscriptionNotification{
		JSONRPC: "2.0",
		Method:  "eth_subscription",
		Params: SubscriptionParams{
			Subscription: subID,
			Result:       result,
		},
	}
	return json.Marshal(notif)
}

// SubscriptionDispatcher dispatches subscription notifications.
type SubscriptionDispatcher struct {
	manager *SubscriptionManager
	writer  NotificationWriter
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewSubscriptionDispatcher creates a new subscription dispatcher.
func NewSubscriptionDispatcher(manager *SubscriptionManager, writer NotificationWriter) *SubscriptionDispatcher {
	return &SubscriptionDispatcher{
		manager: manager,
		writer:  writer,
		done:    make(chan struct{}),
	}
}

// Start starts the dispatcher.
func (d *SubscriptionDispatcher) Start() {
	d.wg.Add(1)
	go d.dispatchLoop()
}

// Stop stops the dispatcher.
func (d *SubscriptionDispatcher) Stop() {
	close(d.done)
	d.wg.Wait()
}

// dispatchLoop dispatches notifications.
func (d *SubscriptionDispatcher) dispatchLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.dispatchPending()
		}
	}
}

// dispatchPending sends pending notifications.
func (d *SubscriptionDispatcher) dispatchPending() {
	d.manager.mu.RLock()
	subs := make([]*Subscription, 0, len(d.manager.subscriptions))
	for _, sub := range d.manager.subscriptions {
		subs = append(subs, sub)
	}
	d.manager.mu.RUnlock()

	for _, sub := range subs {
		select {
		case data := <-sub.Channel:
			if d.writer != nil {
				_ = d.writer.WriteNotification(sub.ID, data)
			}
		default:
			// No pending data
		}
	}
}
