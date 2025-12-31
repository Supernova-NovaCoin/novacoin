// Package main provides the Node implementation for NovaCoin.
package main

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/config"
	"github.com/Supernova-NovaCoin/novacoin/core/consensus"
	"github.com/Supernova-NovaCoin/novacoin/core/dag"
	"github.com/Supernova-NovaCoin/novacoin/core/finality"
	"github.com/Supernova-NovaCoin/novacoin/core/mev"
	"github.com/Supernova-NovaCoin/novacoin/core/pos"
	"github.com/Supernova-NovaCoin/novacoin/core/state"
	"github.com/Supernova-NovaCoin/novacoin/core/zk"
	"github.com/Supernova-NovaCoin/novacoin/crypto"
	"github.com/Supernova-NovaCoin/novacoin/p2p"
	"github.com/Supernova-NovaCoin/novacoin/rpc"
)

// Node represents a NovaCoin node with all components.
type Node struct {
	config *config.Config

	// Core components
	stateDB   *state.StateDB
	dagStore  *dag.Store
	txPool    *TxPool
	validator *ValidatorInfo

	// Consensus orchestrator (contains all sub-engines)
	hybrid      *consensus.HybridConsensus
	finalityEng *finality.Engine

	// MEV protection
	mevEngine *mev.MEVEngine

	// ZK prover/verifier
	zkProver   *zk.Prover
	zkVerifier *zk.Verifier

	// P2P network
	p2pHost *p2p.Host

	// RPC server
	rpcServer *rpc.Server

	// Logging
	verbosity int
	metrics   bool

	// Lifecycle
	running bool
	mu      sync.RWMutex
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// Stats
	stats *NodeStats
}

// NodeStats tracks node statistics.
type NodeStats struct {
	StartTime       time.Time
	BlocksProcessed uint64
	TxsProcessed    uint64
	PeersConnected  int
	LastBlockTime   time.Time
	SyncProgress    float64
	mu              sync.RWMutex
}

// ValidatorInfo holds validator information if running as validator.
type ValidatorInfo struct {
	Enabled   bool
	PublicKey crypto.BLSPublicKey
	Index     uint64
	Stake     uint64
}

// TxPool manages pending transactions.
type TxPool struct {
	pending map[dag.Hash]*PendingTx
	queued  map[dag.Address][]*PendingTx
	byPrice []*PendingTx // sorted by gas price
	mu      sync.RWMutex
	maxSize int
	minGas  uint64
}

// PendingTx represents a pending transaction.
type PendingTx struct {
	Hash      dag.Hash
	From      dag.Address
	To        *dag.Address
	Value     uint64
	GasPrice  uint64
	GasLimit  uint64
	Nonce     uint64
	Data      []byte
	Signature []byte
	AddedAt   time.Time
}

// NewTxPool creates a new transaction pool.
func NewTxPool(maxSize int, minGas uint64) *TxPool {
	return &TxPool{
		pending: make(map[dag.Hash]*PendingTx),
		queued:  make(map[dag.Address][]*PendingTx),
		byPrice: make([]*PendingTx, 0),
		maxSize: maxSize,
		minGas:  minGas,
	}
}

// Add adds a transaction to the pool.
func (p *TxPool) Add(tx *PendingTx) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pending) >= p.maxSize {
		return fmt.Errorf("pool is full")
	}
	if tx.GasPrice < p.minGas {
		return fmt.Errorf("gas price too low")
	}

	tx.AddedAt = time.Now()
	p.pending[tx.Hash] = tx
	return nil
}

// Get retrieves a transaction by hash.
func (p *TxPool) Get(hash dag.Hash) *PendingTx {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pending[hash]
}

// Remove removes a transaction from the pool.
func (p *TxPool) Remove(hash dag.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, hash)
}

// Count returns number of pending transactions.
func (p *TxPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pending)
}

// GetPending returns pending transactions for block production.
func (p *TxPool) GetPending(limit int) []*PendingTx {
	p.mu.RLock()
	defer p.mu.RUnlock()

	txs := make([]*PendingTx, 0, limit)
	for _, tx := range p.pending {
		if len(txs) >= limit {
			break
		}
		txs = append(txs, tx)
	}
	return txs
}

// NewNode creates a new NovaCoin node.
func NewNode(cfg *config.Config, isValidator bool, validatorKeyPath string, verbosity int, metrics bool) (*Node, error) {
	node := &Node{
		config:    cfg,
		verbosity: verbosity,
		metrics:   metrics,
		stopCh:    make(chan struct{}),
		stats: &NodeStats{
			StartTime: time.Now(),
		},
	}

	// Initialize DAG store
	node.dagStore = dag.NewStore()

	// Initialize transaction pool
	node.txPool = NewTxPool(10000, 1) // 10k txs, min gas 1

	// Initialize state database
	memDB := state.NewMemoryDatabase()
	var err error
	node.stateDB, err = state.NewStateDB(memDB, state.Hash{})
	if err != nil {
		return nil, fmt.Errorf("failed to create state db: %w", err)
	}

	// Initialize validator info if enabled
	if isValidator {
		node.validator = &ValidatorInfo{
			Enabled: true,
		}

		if validatorKeyPath != "" {
			// Load validator key
			if err := node.loadValidatorKey(validatorKeyPath); err != nil {
				return nil, fmt.Errorf("failed to load validator key: %w", err)
			}
		} else {
			// Generate new validator key for testing
			node.validator.PublicKey = crypto.BLSPublicKey{}
			node.validator.Index = 0
			node.validator.Stake = 1000000 // 1M stake for testing
		}
	}

	// Initialize consensus engines
	if err := node.initConsensus(); err != nil {
		return nil, fmt.Errorf("failed to init consensus: %w", err)
	}

	// Initialize MEV protection
	if err := node.initMEV(); err != nil {
		return nil, fmt.Errorf("failed to init MEV: %w", err)
	}

	// Initialize ZK prover/verifier
	if err := node.initZK(); err != nil {
		return nil, fmt.Errorf("failed to init ZK: %w", err)
	}

	// Initialize P2P network
	if err := node.initP2P(); err != nil {
		return nil, fmt.Errorf("failed to init P2P: %w", err)
	}

	// Initialize RPC server
	if err := node.initRPC(); err != nil {
		return nil, fmt.Errorf("failed to init RPC: %w", err)
	}

	return node, nil
}

// initConsensus initializes the consensus orchestrator.
func (node *Node) initConsensus() error {
	cfg := node.config.Consensus

	// Create validator registry and set manager
	registryCfg := pos.DefaultRegistryConfig()
	registry := pos.NewValidatorRegistry(registryCfg)

	setConfig := pos.DefaultValidatorSetConfig()
	validatorSet := pos.NewValidatorSetManager(setConfig, registry)

	// Create hybrid consensus config
	hybridCfg := consensus.DefaultConfig()
	hybridCfg.ChainID = big.NewInt(int64(node.config.NetworkID))

	// Apply config overrides from node config
	if cfg.DAGKnight != nil {
		hybridCfg.DAGKnight.DefaultK = cfg.DAGKnight.DefaultK
		hybridCfg.DAGKnight.MinK = cfg.DAGKnight.MinK
		hybridCfg.DAGKnight.MaxK = cfg.DAGKnight.MaxK
		hybridCfg.DAGKnight.TargetBlockTime = cfg.DAGKnight.TargetBlockTime
	}
	if cfg.Shoal != nil && hybridCfg.Shoal.Parallel != nil {
		hybridCfg.Shoal.Parallel.NumDAGs = cfg.Shoal.NumParallelDAGs
	}
	if cfg.Mysticeti != nil && hybridCfg.Mysticeti.Vote != nil {
		hybridCfg.Mysticeti.Vote.QuorumThreshold = cfg.Mysticeti.QuorumThreshold
	}

	// Create the hybrid consensus orchestrator
	node.hybrid = consensus.NewHybridConsensus(
		hybridCfg,
		node.dagStore,
		node.stateDB,
		validatorSet,
	)

	// Initialize Finality Engine
	finalityCfg := finality.DefaultConfig()
	node.finalityEng = finality.NewEngine(finalityCfg, node.dagStore)

	return nil
}

// initMEV initializes MEV protection.
func (node *Node) initMEV() error {
	mevCfg := mev.DefaultMEVEngineConfig()

	// Apply overrides from node config
	if node.config.MEV != nil && node.config.MEV.Enabled {
		mevCfg.Threshold.Threshold = node.config.MEV.ThresholdT
		mevCfg.Threshold.TotalShares = node.config.MEV.ThresholdN
	}

	node.mevEngine = mev.NewMEVEngine(mevCfg)

	return nil
}

// initZK initializes the ZK prover and verifier.
func (node *Node) initZK() error {
	proverCfg := zk.DefaultProofConfig()
	node.zkProver = zk.NewProver(proverCfg)

	verifierCfg := zk.DefaultVerifierConfig()
	node.zkVerifier = zk.NewVerifier(verifierCfg)

	return nil
}

// initP2P initializes the P2P network.
func (node *Node) initP2P() error {
	cfg := node.config.Network

	hostCfg := p2p.DefaultHostConfig()
	// Use ListenAddress from config if set
	if cfg.ListenAddr != "" {
		hostCfg.ListenAddress = cfg.ListenAddr
	} else if cfg.ListenPort > 0 {
		hostCfg.ListenAddress = fmt.Sprintf("0.0.0.0:%d", cfg.ListenPort)
	}
	hostCfg.MaxInboundPeers = cfg.MaxPeers / 2
	hostCfg.MaxOutboundPeers = cfg.MaxPeers / 2
	hostCfg.BootstrapPeers = cfg.BootstrapPeers

	var err error
	node.p2pHost, err = p2p.NewHost(hostCfg)
	if err != nil {
		return fmt.Errorf("failed to create P2P host: %w", err)
	}

	// Register message handlers
	node.registerP2PHandlers()

	return nil
}

// registerP2PHandlers sets up P2P message handlers.
func (node *Node) registerP2PHandlers() {
	// Handle new blocks
	node.p2pHost.RegisterHandler(p2p.MsgNewBlock, func(msg *p2p.Message, peer *p2p.Peer) error {
		return node.handleBlockMessage(msg, peer)
	})

	// Handle transactions
	node.p2pHost.RegisterHandler(p2p.MsgNewTransaction, func(msg *p2p.Message, peer *p2p.Peer) error {
		return node.handleTxMessage(msg, peer)
	})

	// Handle consensus votes
	node.p2pHost.RegisterHandler(p2p.MsgVote, func(msg *p2p.Message, peer *p2p.Peer) error {
		return node.handleConsensusMessage(msg, peer)
	})
}

// handleBlockMessage processes incoming block announcements.
func (node *Node) handleBlockMessage(msg *p2p.Message, peer *p2p.Peer) error {
	node.stats.mu.Lock()
	node.stats.BlocksProcessed++
	node.stats.LastBlockTime = time.Now()
	node.stats.mu.Unlock()
	return nil
}

// handleTxMessage processes incoming transactions.
func (node *Node) handleTxMessage(msg *p2p.Message, peer *p2p.Peer) error {
	node.stats.mu.Lock()
	node.stats.TxsProcessed++
	node.stats.mu.Unlock()
	return nil
}

// handleConsensusMessage processes consensus protocol messages.
func (node *Node) handleConsensusMessage(msg *p2p.Message, peer *p2p.Peer) error {
	return nil
}

// initRPC initializes the RPC server.
func (node *Node) initRPC() error {
	cfg := node.config.RPC

	serverCfg := &rpc.ServerConfig{
		HTTPEnabled: cfg.HTTPEnabled,
		HTTPHost:    cfg.HTTPHost,
		HTTPPort:    cfg.HTTPPort,
		WSEnabled:   cfg.WSEnabled,
		WSHost:      cfg.WSHost,
		WSPort:      cfg.WSPort,
		HTTPCors:    cfg.CorsOrigins,
		WSOrigins:   cfg.CorsOrigins,
	}

	// Create backend for RPC
	backend := NewNodeBackend(node)
	node.rpcServer = rpc.NewServer(serverCfg, backend)

	// Register RPC APIs
	ethAPI := rpc.NewEthAPI(backend)
	netAPI := rpc.NewNetAPI(
		node.config.NetworkID,
		func() int { return node.p2pHost.GetPeerStore().ActiveCount() },
		func() bool { return node.running },
	)

	node.rpcServer.RegisterAPI("eth", ethAPI)
	node.rpcServer.RegisterAPI("net", netAPI)
	node.rpcServer.RegisterAPI("web3", netAPI)

	return nil
}

// loadValidatorKey loads the validator key from file.
func (node *Node) loadValidatorKey(path string) error {
	// TODO: Implement proper key loading from file
	node.validator.PublicKey = crypto.BLSPublicKey{}
	node.validator.Index = 0
	node.validator.Stake = 1000000
	return nil
}

// Start starts the node.
func (node *Node) Start() error {
	node.mu.Lock()
	defer node.mu.Unlock()

	if node.running {
		return fmt.Errorf("node already running")
	}

	node.log("Starting NovaCoin node...")

	// Start P2P network
	if err := node.p2pHost.Start(); err != nil {
		return fmt.Errorf("failed to start P2P: %w", err)
	}
	node.log("  P2P network started on port %d", node.config.Network.ListenPort)

	// Start consensus orchestrator
	if err := node.hybrid.Start(); err != nil {
		return fmt.Errorf("failed to start HybridConsensus: %w", err)
	}
	node.log("  HybridConsensus orchestrator started")

	// Finality engine is stateless, no Start needed
	node.log("  Finality engine ready")

	// Start MEV engine
	if err := node.mevEngine.Start(); err != nil {
		return fmt.Errorf("failed to start MEV engine: %w", err)
	}
	node.log("  MEV protection engine started")

	// Start RPC server
	if err := node.rpcServer.Start(); err != nil {
		return fmt.Errorf("failed to start RPC server: %w", err)
	}
	if node.config.RPC.HTTPEnabled {
		node.log("  HTTP-RPC server started on http://%s:%d", node.config.RPC.HTTPHost, node.config.RPC.HTTPPort)
	}
	if node.config.RPC.WSEnabled {
		node.log("  WebSocket server started on ws://%s:%d", node.config.RPC.WSHost, node.config.RPC.WSPort)
	}

	// Start background tasks
	node.wg.Add(1)
	go node.runMainLoop()

	if node.validator != nil && node.validator.Enabled {
		node.wg.Add(1)
		go node.runValidatorLoop()
		node.log("  Validator mode enabled")
	}

	node.running = true
	node.stats.StartTime = time.Now()

	node.log("NovaCoin node started successfully")

	return nil
}

// Stop stops the node gracefully.
func (node *Node) Stop(ctx context.Context) error {
	node.mu.Lock()
	if !node.running {
		node.mu.Unlock()
		return nil
	}
	node.running = false
	close(node.stopCh)
	node.mu.Unlock()

	node.log("Stopping NovaCoin node...")

	// Stop RPC server first
	if err := node.rpcServer.Stop(); err != nil {
		node.log("  Warning: RPC server stop error: %v", err)
	}
	node.log("  RPC server stopped")

	// Stop MEV engine
	if err := node.mevEngine.Stop(); err != nil {
		node.log("  Warning: MEV engine stop error: %v", err)
	}
	node.log("  MEV engine stopped")

	// Finality engine is stateless, no Stop needed
	node.log("  Finality engine stopped")

	// Stop consensus orchestrator
	if err := node.hybrid.Stop(); err != nil {
		node.log("  Warning: HybridConsensus stop error: %v", err)
	}
	node.log("  HybridConsensus stopped")

	// Stop P2P last
	if err := node.p2pHost.Stop(); err != nil {
		node.log("  Warning: P2P host stop error: %v", err)
	}
	node.log("  P2P network stopped")

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		node.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		node.log("All goroutines stopped")
	case <-ctx.Done():
		node.log("Shutdown timeout - some goroutines may still be running")
	}

	node.log("NovaCoin node stopped")

	return nil
}

// runMainLoop runs the main node processing loop.
func (node *Node) runMainLoop() {
	defer node.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-node.stopCh:
			return
		case <-ticker.C:
			node.updateStats()
		}
	}
}

// runValidatorLoop runs the validator block production loop.
func (node *Node) runValidatorLoop() {
	defer node.wg.Done()

	blockTime := node.config.Consensus.DAGKnight.TargetBlockTime
	ticker := time.NewTicker(blockTime)
	defer ticker.Stop()

	for {
		select {
		case <-node.stopCh:
			return
		case <-ticker.C:
			if err := node.produceBlock(); err != nil {
				node.log("Block production error: %v", err)
			}
		}
	}
}

// produceBlock produces a new block if this node is the validator.
func (node *Node) produceBlock() error {
	// Get pending transactions
	txs := node.txPool.GetPending(100)
	if len(txs) == 0 {
		return nil // Nothing to produce
	}

	// Block production handled by hybrid consensus
	node.stats.mu.Lock()
	node.stats.BlocksProcessed++
	node.stats.mu.Unlock()

	return nil
}

// updateStats updates node statistics.
func (node *Node) updateStats() {
	node.stats.mu.Lock()
	defer node.stats.mu.Unlock()

	// Update peer count
	if node.p2pHost != nil {
		node.stats.PeersConnected = node.p2pHost.GetPeerStore().ActiveCount()
	}
}

// log logs a message if verbosity allows.
func (node *Node) log(format string, args ...interface{}) {
	if node.verbosity >= 1 {
		fmt.Printf(format+"\n", args...)
	}
}

// Stats returns a copy of node statistics.
func (node *Node) Stats() NodeStats {
	node.stats.mu.RLock()
	defer node.stats.mu.RUnlock()
	return NodeStats{
		StartTime:       node.stats.StartTime,
		BlocksProcessed: node.stats.BlocksProcessed,
		TxsProcessed:    node.stats.TxsProcessed,
		PeersConnected:  node.stats.PeersConnected,
		LastBlockTime:   node.stats.LastBlockTime,
		SyncProgress:    node.stats.SyncProgress,
	}
}

// IsRunning returns whether the node is running.
func (node *Node) IsRunning() bool {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.running
}

// IsValidator returns whether this node is a validator.
func (node *Node) IsValidator() bool {
	return node.validator != nil && node.validator.Enabled
}

// NodeBackend implements rpc.Backend interface.
type NodeBackend struct {
	node *Node
}

// NewNodeBackend creates a new node backend for RPC.
func NewNodeBackend(node *Node) *NodeBackend {
	return &NodeBackend{node: node}
}

// ChainID returns the chain ID.
func (b *NodeBackend) ChainID() *big.Int {
	return big.NewInt(int64(b.node.config.NetworkID))
}

// GasPrice returns the suggested gas price.
func (b *NodeBackend) GasPrice() *big.Int {
	return big.NewInt(1000000000) // 1 Gwei
}

// MaxPriorityFeePerGas returns the suggested max priority fee.
func (b *NodeBackend) MaxPriorityFeePerGas() *big.Int {
	return big.NewInt(1000000000) // 1 Gwei
}

// FeeHistory returns fee history.
func (b *NodeBackend) FeeHistory(blockCount uint64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*rpc.FeeHistoryResult, error) {
	return &rpc.FeeHistoryResult{
		OldestBlock:   rpc.HexUint64(0),
		BaseFeePerGas: []rpc.HexBig{},
		GasUsedRatio:  []float64{},
		Reward:        [][]rpc.HexBig{},
	}, nil
}

// GetBlockByHash returns a block by hash.
func (b *NodeBackend) GetBlockByHash(hash dag.Hash, fullTx bool) (*rpc.Block, error) {
	vertex := b.node.dagStore.Get(hash)
	if vertex == nil {
		return nil, nil
	}
	return b.vertexToBlock(vertex, fullTx), nil
}

// GetBlockByNumber returns a block by number.
func (b *NodeBackend) GetBlockByNumber(number rpc.BlockNumber, fullTx bool) (*rpc.Block, error) {
	vertices := b.node.dagStore.GetByHeight(uint64(number))
	if len(vertices) == 0 {
		return nil, nil
	}
	return b.vertexToBlock(vertices[0], fullTx), nil
}

// GetBlockNumber returns the current block number.
func (b *NodeBackend) GetBlockNumber() (uint64, error) {
	tips := b.node.dagStore.GetTips()
	var maxHeight uint64
	for _, tip := range tips {
		if tip.Height > maxHeight {
			maxHeight = tip.Height
		}
	}
	return maxHeight, nil
}

// GetTransactionByHash returns a transaction by hash.
func (b *NodeBackend) GetTransactionByHash(hash dag.Hash) (*rpc.Transaction, error) {
	return nil, nil // Not implemented
}

// GetTransactionByBlockHashAndIndex returns a transaction by block hash and index.
func (b *NodeBackend) GetTransactionByBlockHashAndIndex(blockHash dag.Hash, index uint64) (*rpc.Transaction, error) {
	return nil, nil // Not implemented
}

// GetTransactionByBlockNumberAndIndex returns a transaction by block number and index.
func (b *NodeBackend) GetTransactionByBlockNumberAndIndex(number rpc.BlockNumber, index uint64) (*rpc.Transaction, error) {
	return nil, nil // Not implemented
}

// GetTransactionReceipt returns a transaction receipt.
func (b *NodeBackend) GetTransactionReceipt(hash dag.Hash) (*rpc.Receipt, error) {
	return nil, nil // Not implemented
}

// GetTransactionCount returns the transaction count for an address.
func (b *NodeBackend) GetTransactionCount(address dag.Address, number rpc.BlockNumber) (uint64, error) {
	return b.node.stateDB.GetNonce(state.Address(address)), nil
}

// GetBalance returns the balance for an address.
func (b *NodeBackend) GetBalance(address dag.Address, number rpc.BlockNumber) (*big.Int, error) {
	return b.node.stateDB.GetBalance(state.Address(address)), nil
}

// GetCode returns the code at an address.
func (b *NodeBackend) GetCode(address dag.Address, number rpc.BlockNumber) ([]byte, error) {
	return b.node.stateDB.GetCode(state.Address(address)), nil
}

// GetStorageAt returns the storage value at an address and key.
func (b *NodeBackend) GetStorageAt(address dag.Address, key dag.Hash, number rpc.BlockNumber) (dag.Hash, error) {
	value := b.node.stateDB.GetState(state.Address(address), state.Hash(key))
	return dag.Hash(value), nil
}

// Call executes a contract call.
func (b *NodeBackend) Call(args rpc.CallArgs, number rpc.BlockNumber) ([]byte, error) {
	return nil, nil // Not implemented - would need EVM execution
}

// EstimateGas estimates gas for a call.
func (b *NodeBackend) EstimateGas(args rpc.CallArgs, number rpc.BlockNumber) (uint64, error) {
	return 21000, nil // Default gas for simple transfer
}

// SendRawTransaction submits a raw transaction.
func (b *NodeBackend) SendRawTransaction(data []byte) (dag.Hash, error) {
	return dag.Hash{}, fmt.Errorf("not implemented")
}

// SendTransaction submits a transaction.
func (b *NodeBackend) SendTransaction(args rpc.SendTxArgs) (dag.Hash, error) {
	return dag.Hash{}, fmt.Errorf("not implemented")
}

// GetLogs returns logs matching a filter.
func (b *NodeBackend) GetLogs(query rpc.FilterQuery) ([]rpc.Log, error) {
	return nil, nil
}

// Mining returns whether mining is active (PoS - always true if validator).
func (b *NodeBackend) Mining() bool {
	return b.node.validator != nil && b.node.validator.Enabled
}

// Hashrate returns the mining hashrate (0 for PoS).
func (b *NodeBackend) Hashrate() uint64 {
	return 0
}

// GetBlockTransactionCountByHash returns tx count in a block by hash.
func (b *NodeBackend) GetBlockTransactionCountByHash(hash dag.Hash) (uint64, error) {
	vertex := b.node.dagStore.Get(hash)
	if vertex == nil {
		return 0, nil
	}
	return uint64(vertex.TxCount), nil
}

// GetBlockTransactionCountByNumber returns tx count in a block by number.
func (b *NodeBackend) GetBlockTransactionCountByNumber(number rpc.BlockNumber) (uint64, error) {
	vertices := b.node.dagStore.GetByHeight(uint64(number))
	if len(vertices) == 0 {
		return 0, nil
	}
	return uint64(vertices[0].TxCount), nil
}

// GetUncleCountByBlockHash returns uncle count (always 0 for DAG).
func (b *NodeBackend) GetUncleCountByBlockHash(hash dag.Hash) (uint64, error) {
	return 0, nil // DAG has no uncles
}

// GetUncleCountByBlockNumber returns uncle count (always 0 for DAG).
func (b *NodeBackend) GetUncleCountByBlockNumber(number rpc.BlockNumber) (uint64, error) {
	return 0, nil // DAG has no uncles
}

// Syncing returns sync status.
func (b *NodeBackend) Syncing() (*rpc.SyncStatus, bool) {
	if b.node.stats.SyncProgress >= 100.0 {
		return nil, false
	}
	return &rpc.SyncStatus{
		StartingBlock: rpc.HexUint64(0),
		CurrentBlock:  rpc.HexUint64(b.node.stats.BlocksProcessed),
		HighestBlock:  rpc.HexUint64(b.node.stats.BlocksProcessed),
	}, true
}

// Coinbase returns the coinbase address.
func (b *NodeBackend) Coinbase() dag.Address {
	if b.node.validator != nil && b.node.validator.Enabled {
		// Return a derived address from validator pubkey
		return dag.Address{}
	}
	return dag.Address{}
}

// vertexToBlock converts a DAG vertex to an RPC block.
func (b *NodeBackend) vertexToBlock(v *dag.Vertex, fullTx bool) *rpc.Block {
	return &rpc.Block{
		Number:           rpc.HexUint64(v.Height),
		Hash:             v.Hash,
		ParentHash:       v.SelectedParent,
		Timestamp:        rpc.HexUint64(uint64(v.Timestamp.Unix())),
		GasLimit:         rpc.HexUint64(30000000),
		GasUsed:          rpc.HexUint64(0),
		Transactions:     []interface{}{},
		TransactionsRoot: v.TxRoot,
		StateRoot:        v.StateRoot,
	}
}
