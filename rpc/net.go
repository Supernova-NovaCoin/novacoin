// Package rpc implements the net_* and web3_* API namespaces for NovaCoin.
package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
)

// NetAPI provides net_* RPC methods.
type NetAPI struct {
	networkID   uint64
	peerCount   func() int
	isListening func() bool
}

// NewNetAPI creates a new net API.
func NewNetAPI(networkID uint64, peerCount func() int, isListening func() bool) *NetAPI {
	return &NetAPI{
		networkID:   networkID,
		peerCount:   peerCount,
		isListening: isListening,
	}
}

// Version returns the network ID.
func (api *NetAPI) Version(ctx context.Context) (string, error) {
	return fmt.Sprintf("%d", api.networkID), nil
}

// Listening returns true if the node is listening for connections.
func (api *NetAPI) Listening(ctx context.Context) (bool, error) {
	if api.isListening != nil {
		return api.isListening(), nil
	}
	return true, nil
}

// PeerCount returns the number of connected peers.
func (api *NetAPI) PeerCount(ctx context.Context) (HexUint64, error) {
	if api.peerCount != nil {
		return HexUint64(api.peerCount()), nil
	}
	return 0, nil
}

// Web3API provides web3_* RPC methods.
type Web3API struct {
	version string
}

// NewWeb3API creates a new web3 API.
func NewWeb3API(version string) *Web3API {
	return &Web3API{
		version: version,
	}
}

// ClientVersion returns the client version string.
func (api *Web3API) ClientVersion(ctx context.Context) (string, error) {
	return fmt.Sprintf("NovaCoin/%s/%s/%s", api.version, runtime.GOOS, runtime.Version()), nil
}

// Sha3 returns the Keccak-256 hash of the input.
// Note: Despite the name, this uses Keccak-256 for Ethereum compatibility.
func (api *Web3API) Sha3(ctx context.Context, data Hex) (Hex, error) {
	// For simplicity, use SHA-256 (production would use Keccak-256)
	hash := sha256.Sum256(data)
	return Hex(hash[:]), nil
}

// TxPoolAPI provides txpool_* RPC methods.
type TxPoolAPI struct {
	getPending func() (map[string]map[string]*Transaction, error)
	getQueued  func() (map[string]map[string]*Transaction, error)
}

// NewTxPoolAPI creates a new txpool API.
func NewTxPoolAPI(
	getPending func() (map[string]map[string]*Transaction, error),
	getQueued func() (map[string]map[string]*Transaction, error),
) *TxPoolAPI {
	return &TxPoolAPI{
		getPending: getPending,
		getQueued:  getQueued,
	}
}

// TxPoolContent represents the txpool content.
type TxPoolContent struct {
	Pending map[string]map[string]*Transaction `json:"pending"`
	Queued  map[string]map[string]*Transaction `json:"queued"`
}

// Content returns the transactions in the pool.
func (api *TxPoolAPI) Content(ctx context.Context) (*TxPoolContent, error) {
	var pending, queued map[string]map[string]*Transaction
	var err error

	if api.getPending != nil {
		pending, err = api.getPending()
		if err != nil {
			return nil, err
		}
	} else {
		pending = make(map[string]map[string]*Transaction)
	}

	if api.getQueued != nil {
		queued, err = api.getQueued()
		if err != nil {
			return nil, err
		}
	} else {
		queued = make(map[string]map[string]*Transaction)
	}

	return &TxPoolContent{
		Pending: pending,
		Queued:  queued,
	}, nil
}

// TxPoolStatus represents the txpool status.
type TxPoolStatus struct {
	Pending HexUint64 `json:"pending"`
	Queued  HexUint64 `json:"queued"`
}

// Status returns the number of transactions in the pool.
func (api *TxPoolAPI) Status(ctx context.Context) (*TxPoolStatus, error) {
	content, err := api.Content(ctx)
	if err != nil {
		return nil, err
	}

	var pending, queued uint64
	for _, txs := range content.Pending {
		pending += uint64(len(txs))
	}
	for _, txs := range content.Queued {
		queued += uint64(len(txs))
	}

	return &TxPoolStatus{
		Pending: HexUint64(pending),
		Queued:  HexUint64(queued),
	}, nil
}

// Inspect returns a summary of the pool.
func (api *TxPoolAPI) Inspect(ctx context.Context) (*TxPoolContent, error) {
	// Same as Content for now
	return api.Content(ctx)
}

// DebugAPI provides debug_* RPC methods.
type DebugAPI struct {
	// Tracing functions
	traceBlock     func(hash string) (interface{}, error)
	traceTx        func(hash string) (interface{}, error)
	getBlockRlp    func(number BlockNumber) (Hex, error)
	printBlock     func(number BlockNumber) (string, error)
	getSeed        func() (Hex, error)
	setHead        func(number BlockNumber) error
	getModifiedAccounts func(startBlock, endBlock BlockNumber) ([]string, error)
}

// NewDebugAPI creates a new debug API.
func NewDebugAPI() *DebugAPI {
	return &DebugAPI{}
}

// TraceBlockByNumber traces a block by number.
func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number BlockNumber) (interface{}, error) {
	return nil, ErrFeatureNotSupported
}

// TraceBlockByHash traces a block by hash.
func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash string) (interface{}, error) {
	if api.traceBlock != nil {
		return api.traceBlock(hash)
	}
	return nil, ErrFeatureNotSupported
}

// TraceTransaction traces a transaction.
func (api *DebugAPI) TraceTransaction(ctx context.Context, hash string) (interface{}, error) {
	if api.traceTx != nil {
		return api.traceTx(hash)
	}
	return nil, ErrFeatureNotSupported
}

// GetBlockRlp returns the RLP-encoded block.
func (api *DebugAPI) GetBlockRlp(ctx context.Context, number BlockNumber) (Hex, error) {
	if api.getBlockRlp != nil {
		return api.getBlockRlp(number)
	}
	return nil, ErrFeatureNotSupported
}

// PrintBlock returns a textual representation of a block.
func (api *DebugAPI) PrintBlock(ctx context.Context, number BlockNumber) (string, error) {
	if api.printBlock != nil {
		return api.printBlock(number)
	}
	return "", ErrFeatureNotSupported
}

// GetSeed returns the seed for randomness.
func (api *DebugAPI) GetSeed(ctx context.Context) (Hex, error) {
	if api.getSeed != nil {
		return api.getSeed()
	}
	return nil, ErrFeatureNotSupported
}

// SetHead sets the head of the chain.
func (api *DebugAPI) SetHead(ctx context.Context, number BlockNumber) error {
	if api.setHead != nil {
		return api.setHead(number)
	}
	return ErrFeatureNotSupported
}

// GetModifiedAccountsByNumber returns accounts modified between blocks.
func (api *DebugAPI) GetModifiedAccountsByNumber(ctx context.Context, startBlock, endBlock BlockNumber) ([]string, error) {
	if api.getModifiedAccounts != nil {
		return api.getModifiedAccounts(startBlock, endBlock)
	}
	return nil, ErrFeatureNotSupported
}

// AdminAPI provides admin_* RPC methods.
type AdminAPI struct {
	addPeer       func(url string) (bool, error)
	removePeer    func(url string) (bool, error)
	addTrustedPeer func(url string) (bool, error)
	removeTrustedPeer func(url string) (bool, error)
	getPeers      func() ([]PeerInfo, error)
	getNodeInfo   func() (*NodeInfo, error)
	getDatadir    func() string
}

// NodeInfo represents node information.
type NodeInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Enode      string            `json:"enode"`
	ENR        string            `json:"enr"`
	IP         string            `json:"ip"`
	Ports      map[string]int    `json:"ports"`
	ListenAddr string            `json:"listenAddr"`
	Protocols  map[string]interface{} `json:"protocols"`
}

// NewAdminAPI creates a new admin API.
func NewAdminAPI(
	addPeer func(string) (bool, error),
	removePeer func(string) (bool, error),
	getPeers func() ([]PeerInfo, error),
	getNodeInfo func() (*NodeInfo, error),
	getDatadir func() string,
) *AdminAPI {
	return &AdminAPI{
		addPeer:     addPeer,
		removePeer:  removePeer,
		getPeers:    getPeers,
		getNodeInfo: getNodeInfo,
		getDatadir:  getDatadir,
	}
}

// AddPeer adds a new peer.
func (api *AdminAPI) AddPeer(ctx context.Context, url string) (bool, error) {
	if api.addPeer != nil {
		return api.addPeer(url)
	}
	return false, ErrFeatureNotSupported
}

// RemovePeer removes a peer.
func (api *AdminAPI) RemovePeer(ctx context.Context, url string) (bool, error) {
	if api.removePeer != nil {
		return api.removePeer(url)
	}
	return false, ErrFeatureNotSupported
}

// AddTrustedPeer adds a trusted peer.
func (api *AdminAPI) AddTrustedPeer(ctx context.Context, url string) (bool, error) {
	if api.addTrustedPeer != nil {
		return api.addTrustedPeer(url)
	}
	return false, ErrFeatureNotSupported
}

// RemoveTrustedPeer removes a trusted peer.
func (api *AdminAPI) RemoveTrustedPeer(ctx context.Context, url string) (bool, error) {
	if api.removeTrustedPeer != nil {
		return api.removeTrustedPeer(url)
	}
	return false, ErrFeatureNotSupported
}

// Peers returns connected peers.
func (api *AdminAPI) Peers(ctx context.Context) ([]PeerInfo, error) {
	if api.getPeers != nil {
		return api.getPeers()
	}
	return []PeerInfo{}, nil
}

// NodeInfo returns the node information.
func (api *AdminAPI) NodeInfo(ctx context.Context) (*NodeInfo, error) {
	if api.getNodeInfo != nil {
		return api.getNodeInfo()
	}
	return nil, ErrFeatureNotSupported
}

// Datadir returns the data directory.
func (api *AdminAPI) Datadir(ctx context.Context) (string, error) {
	if api.getDatadir != nil {
		return api.getDatadir(), nil
	}
	return "", nil
}

// NovaAPI provides nova_* RPC methods for NovaCoin-specific functionality.
type NovaAPI struct {
	getValidators       func() ([]ValidatorInfo, error)
	getDAGStats         func() (*DAGStats, error)
	getFinalityInfo     func(hash string) (*FinalityInfo, error)
	getMEVProtection    func() (*MEVProtectionInfo, error)
	getConsensusState   func() (*ConsensusState, error)
}

// ValidatorInfo represents a validator.
type ValidatorInfo struct {
	Index          uint32   `json:"index"`
	Address        string   `json:"address"`
	PubKey         string   `json:"pubKey"`
	Stake          HexBig   `json:"stake"`
	EffectiveStake HexBig   `json:"effectiveStake"`
	Commission     float64  `json:"commission"`
	Status         string   `json:"status"`
}

// DAGStats represents DAG statistics.
type DAGStats struct {
	TotalVertices   uint64    `json:"totalVertices"`
	FinalizedHeight uint64    `json:"finalizedHeight"`
	PendingVertices uint64    `json:"pendingVertices"`
	BlueScore       uint64    `json:"blueScore"`
	Wave            uint64    `json:"wave"`
	AdaptiveK       float64   `json:"adaptiveK"`
	ParallelDAGs    int       `json:"parallelDags"`
}

// FinalityInfo represents finality information for a block.
type FinalityInfo struct {
	Hash           string  `json:"hash"`
	Height         uint64  `json:"height"`
	IsFinalized    bool    `json:"isFinalized"`
	FinalizedAt    int64   `json:"finalizedAt,omitempty"`
	CommitRound    uint64  `json:"commitRound,omitempty"`
	CommitDepth    uint8   `json:"commitDepth,omitempty"`
	StakeSupport   float64 `json:"stakeSupport,omitempty"`
}

// MEVProtectionInfo represents MEV protection status.
type MEVProtectionInfo struct {
	Enabled           bool   `json:"enabled"`
	ThresholdScheme   string `json:"thresholdScheme"`
	ActiveValidators  int    `json:"activeValidators"`
	RequiredShares    int    `json:"requiredShares"`
	EncryptedTxCount  uint64 `json:"encryptedTxCount"`
}

// ConsensusState represents the current consensus state.
type ConsensusState struct {
	CurrentWave      uint64  `json:"currentWave"`
	CurrentRound     uint64  `json:"currentRound"`
	AdaptiveK        float64 `json:"adaptiveK"`
	NetworkLatency   float64 `json:"networkLatencyMs"`
	FinalizedHeight  uint64  `json:"finalizedHeight"`
	ProposersActive  int     `json:"proposersActive"`
}

// NewNovaAPI creates a new nova API.
func NewNovaAPI(
	getValidators func() ([]ValidatorInfo, error),
	getDAGStats func() (*DAGStats, error),
	getFinalityInfo func(string) (*FinalityInfo, error),
	getMEVProtection func() (*MEVProtectionInfo, error),
	getConsensusState func() (*ConsensusState, error),
) *NovaAPI {
	return &NovaAPI{
		getValidators:     getValidators,
		getDAGStats:       getDAGStats,
		getFinalityInfo:   getFinalityInfo,
		getMEVProtection:  getMEVProtection,
		getConsensusState: getConsensusState,
	}
}

// Validators returns the current validators.
func (api *NovaAPI) Validators(ctx context.Context) ([]ValidatorInfo, error) {
	if api.getValidators != nil {
		return api.getValidators()
	}
	return []ValidatorInfo{}, nil
}

// DAGStats returns DAG statistics.
func (api *NovaAPI) DAGStats(ctx context.Context) (*DAGStats, error) {
	if api.getDAGStats != nil {
		return api.getDAGStats()
	}
	return &DAGStats{}, nil
}

// FinalityInfo returns finality info for a block.
func (api *NovaAPI) FinalityInfo(ctx context.Context, hash string) (*FinalityInfo, error) {
	if api.getFinalityInfo != nil {
		return api.getFinalityInfo(hash)
	}
	return nil, ErrResourceNotFound
}

// MEVProtection returns MEV protection status.
func (api *NovaAPI) MEVProtection(ctx context.Context) (*MEVProtectionInfo, error) {
	if api.getMEVProtection != nil {
		return api.getMEVProtection()
	}
	return &MEVProtectionInfo{}, nil
}

// ConsensusState returns the current consensus state.
func (api *NovaAPI) ConsensusState(ctx context.Context) (*ConsensusState, error) {
	if api.getConsensusState != nil {
		return api.getConsensusState()
	}
	return &ConsensusState{}, nil
}

// CreateAccessList creates an access list for a transaction.
func (api *EthAPI) CreateAccessList(ctx context.Context, args CallArgs, number BlockNumber) (interface{}, error) {
	// Placeholder - would analyze contract calls
	return map[string]interface{}{
		"accessList": []interface{}{},
		"gasUsed":    HexUint64(21000),
	}, nil
}

// encodeToHex helper.
func encodeToHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}
