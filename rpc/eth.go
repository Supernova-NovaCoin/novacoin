// Package rpc implements the eth_* API namespace for NovaCoin.
package rpc

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
	"github.com/novacoin/novacoin/core/types"
)

// EthAPI provides eth_* RPC methods.
type EthAPI struct {
	backend Backend

	// Filter management
	filters   map[FilterID]*filter
	filterMu  sync.RWMutex
	filterSeq uint64
}

// Backend interface defines the blockchain backend operations.
type Backend interface {
	// Chain info
	ChainID() *big.Int
	GasPrice() *big.Int
	MaxPriorityFeePerGas() *big.Int
	FeeHistory(blockCount uint64, lastBlock BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error)

	// Block retrieval
	GetBlockByHash(hash dag.Hash, fullTx bool) (*Block, error)
	GetBlockByNumber(number BlockNumber, fullTx bool) (*Block, error)
	GetBlockNumber() (uint64, error)

	// Transaction retrieval
	GetTransactionByHash(hash dag.Hash) (*Transaction, error)
	GetTransactionByBlockHashAndIndex(blockHash dag.Hash, index uint64) (*Transaction, error)
	GetTransactionByBlockNumberAndIndex(number BlockNumber, index uint64) (*Transaction, error)
	GetTransactionReceipt(hash dag.Hash) (*Receipt, error)
	GetTransactionCount(address dag.Address, number BlockNumber) (uint64, error)

	// Account info
	GetBalance(address dag.Address, number BlockNumber) (*big.Int, error)
	GetCode(address dag.Address, number BlockNumber) ([]byte, error)
	GetStorageAt(address dag.Address, key dag.Hash, number BlockNumber) (dag.Hash, error)

	// State execution
	Call(args CallArgs, number BlockNumber) ([]byte, error)
	EstimateGas(args CallArgs, number BlockNumber) (uint64, error)

	// Transaction submission
	SendRawTransaction(data []byte) (dag.Hash, error)
	SendTransaction(args SendTxArgs) (dag.Hash, error)

	// Logs
	GetLogs(query FilterQuery) ([]Log, error)

	// Sync status
	Syncing() (*SyncStatus, bool)

	// Mining (for compatibility)
	Mining() bool
	Hashrate() uint64
	Coinbase() dag.Address

	// Block count queries
	GetBlockTransactionCountByHash(hash dag.Hash) (uint64, error)
	GetBlockTransactionCountByNumber(number BlockNumber) (uint64, error)
	GetUncleCountByBlockHash(hash dag.Hash) (uint64, error)
	GetUncleCountByBlockNumber(number BlockNumber) (uint64, error)
}

// FeeHistoryResult contains fee history data.
type FeeHistoryResult struct {
	OldestBlock   HexUint64   `json:"oldestBlock"`
	BaseFeePerGas []HexBig    `json:"baseFeePerGas"`
	GasUsedRatio  []float64   `json:"gasUsedRatio"`
	Reward        [][]HexBig  `json:"reward,omitempty"`
}

// filter represents an active filter.
type filter struct {
	id         FilterID
	filterType string // "block", "pending", "log"
	query      FilterQuery
	lastBlock  uint64
	logs       []Log
	hashes     []dag.Hash
	created    time.Time
}

// NewEthAPI creates a new eth API.
func NewEthAPI(backend Backend) *EthAPI {
	return &EthAPI{
		backend: backend,
		filters: make(map[FilterID]*filter),
	}
}

// === Chain Info ===

// ChainId returns the chain ID.
func (api *EthAPI) ChainId(ctx context.Context) (HexBig, error) {
	chainID := api.backend.ChainID()
	if chainID == nil {
		return HexBig{}, nil
	}
	return HexBig(*chainID), nil
}

// GasPrice returns the current gas price.
func (api *EthAPI) GasPrice(ctx context.Context) (HexBig, error) {
	price := api.backend.GasPrice()
	if price == nil {
		return HexBig(*big.NewInt(1000000000)), nil // 1 gwei default
	}
	return HexBig(*price), nil
}

// MaxPriorityFeePerGas returns the suggested max priority fee.
func (api *EthAPI) MaxPriorityFeePerGas(ctx context.Context) (HexBig, error) {
	fee := api.backend.MaxPriorityFeePerGas()
	if fee == nil {
		return HexBig(*big.NewInt(1000000000)), nil // 1 gwei default
	}
	return HexBig(*fee), nil
}

// FeeHistory returns fee history for EIP-1559.
func (api *EthAPI) FeeHistory(ctx context.Context, blockCount HexUint64, lastBlock BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	return api.backend.FeeHistory(uint64(blockCount), lastBlock, rewardPercentiles)
}

// BlockNumber returns the current block number.
func (api *EthAPI) BlockNumber(ctx context.Context) (HexUint64, error) {
	num, err := api.backend.GetBlockNumber()
	if err != nil {
		return 0, err
	}
	return HexUint64(num), nil
}

// ProtocolVersion returns the protocol version.
func (api *EthAPI) ProtocolVersion(ctx context.Context) (string, error) {
	return "0x41", nil // Protocol version 65
}

// Syncing returns sync status or false if not syncing.
func (api *EthAPI) Syncing(ctx context.Context) (interface{}, error) {
	status, syncing := api.backend.Syncing()
	if !syncing {
		return false, nil
	}
	return status, nil
}

// === Block Retrieval ===

// GetBlockByHash returns a block by hash.
func (api *EthAPI) GetBlockByHash(ctx context.Context, hash dag.Hash, fullTx bool) (*Block, error) {
	return api.backend.GetBlockByHash(hash, fullTx)
}

// GetBlockByNumber returns a block by number.
func (api *EthAPI) GetBlockByNumber(ctx context.Context, number BlockNumber, fullTx bool) (*Block, error) {
	return api.backend.GetBlockByNumber(number, fullTx)
}

// GetBlockTransactionCountByHash returns the transaction count in a block by hash.
func (api *EthAPI) GetBlockTransactionCountByHash(ctx context.Context, hash dag.Hash) (*HexUint64, error) {
	count, err := api.backend.GetBlockTransactionCountByHash(hash)
	if err != nil {
		return nil, err
	}
	result := HexUint64(count)
	return &result, nil
}

// GetBlockTransactionCountByNumber returns the transaction count in a block by number.
func (api *EthAPI) GetBlockTransactionCountByNumber(ctx context.Context, number BlockNumber) (*HexUint64, error) {
	count, err := api.backend.GetBlockTransactionCountByNumber(number)
	if err != nil {
		return nil, err
	}
	result := HexUint64(count)
	return &result, nil
}

// GetUncleCountByBlockHash returns the uncle count (always 0 for DAG).
func (api *EthAPI) GetUncleCountByBlockHash(ctx context.Context, hash dag.Hash) (*HexUint64, error) {
	count, err := api.backend.GetUncleCountByBlockHash(hash)
	if err != nil {
		return nil, err
	}
	result := HexUint64(count)
	return &result, nil
}

// GetUncleCountByBlockNumber returns the uncle count (always 0 for DAG).
func (api *EthAPI) GetUncleCountByBlockNumber(ctx context.Context, number BlockNumber) (*HexUint64, error) {
	count, err := api.backend.GetUncleCountByBlockNumber(number)
	if err != nil {
		return nil, err
	}
	result := HexUint64(count)
	return &result, nil
}

// GetUncleByBlockHashAndIndex returns an uncle (always nil for DAG).
func (api *EthAPI) GetUncleByBlockHashAndIndex(ctx context.Context, hash dag.Hash, index HexUint64) (*Block, error) {
	return nil, nil // DAG doesn't have uncles
}

// GetUncleByBlockNumberAndIndex returns an uncle (always nil for DAG).
func (api *EthAPI) GetUncleByBlockNumberAndIndex(ctx context.Context, number BlockNumber, index HexUint64) (*Block, error) {
	return nil, nil // DAG doesn't have uncles
}

// === Transaction Retrieval ===

// GetTransactionByHash returns a transaction by hash.
func (api *EthAPI) GetTransactionByHash(ctx context.Context, hash dag.Hash) (*Transaction, error) {
	return api.backend.GetTransactionByHash(hash)
}

// GetTransactionByBlockHashAndIndex returns a transaction by block hash and index.
func (api *EthAPI) GetTransactionByBlockHashAndIndex(ctx context.Context, hash dag.Hash, index HexUint64) (*Transaction, error) {
	return api.backend.GetTransactionByBlockHashAndIndex(hash, uint64(index))
}

// GetTransactionByBlockNumberAndIndex returns a transaction by block number and index.
func (api *EthAPI) GetTransactionByBlockNumberAndIndex(ctx context.Context, number BlockNumber, index HexUint64) (*Transaction, error) {
	return api.backend.GetTransactionByBlockNumberAndIndex(number, uint64(index))
}

// GetTransactionReceipt returns a transaction receipt.
func (api *EthAPI) GetTransactionReceipt(ctx context.Context, hash dag.Hash) (*Receipt, error) {
	return api.backend.GetTransactionReceipt(hash)
}

// GetTransactionCount returns the transaction count (nonce) for an address.
func (api *EthAPI) GetTransactionCount(ctx context.Context, address dag.Address, number BlockNumber) (HexUint64, error) {
	count, err := api.backend.GetTransactionCount(address, number)
	if err != nil {
		return 0, err
	}
	return HexUint64(count), nil
}

// === Account Info ===

// GetBalance returns the balance of an address.
func (api *EthAPI) GetBalance(ctx context.Context, address dag.Address, number BlockNumber) (HexBig, error) {
	balance, err := api.backend.GetBalance(address, number)
	if err != nil {
		return HexBig{}, err
	}
	if balance == nil {
		balance = big.NewInt(0)
	}
	return HexBig(*balance), nil
}

// GetCode returns the code at an address.
func (api *EthAPI) GetCode(ctx context.Context, address dag.Address, number BlockNumber) (Hex, error) {
	code, err := api.backend.GetCode(address, number)
	if err != nil {
		return nil, err
	}
	return Hex(code), nil
}

// GetStorageAt returns the storage value at a position.
func (api *EthAPI) GetStorageAt(ctx context.Context, address dag.Address, position dag.Hash, number BlockNumber) (Hex, error) {
	value, err := api.backend.GetStorageAt(address, position, number)
	if err != nil {
		return nil, err
	}
	return Hex(value[:]), nil
}

// === State Execution ===

// Call executes a call without creating a transaction.
func (api *EthAPI) Call(ctx context.Context, args CallArgs, number BlockNumber) (Hex, error) {
	result, err := api.backend.Call(args, number)
	if err != nil {
		return nil, err
	}
	return Hex(result), nil
}

// EstimateGas estimates the gas needed for a transaction.
func (api *EthAPI) EstimateGas(ctx context.Context, args CallArgs, number BlockNumber) (HexUint64, error) {
	gas, err := api.backend.EstimateGas(args, number)
	if err != nil {
		return 0, err
	}
	return HexUint64(gas), nil
}

// === Transaction Submission ===

// SendRawTransaction submits a signed transaction.
func (api *EthAPI) SendRawTransaction(ctx context.Context, data Hex) (dag.Hash, error) {
	return api.backend.SendRawTransaction(data)
}

// SendTransaction submits an unsigned transaction (requires unlocked account).
func (api *EthAPI) SendTransaction(ctx context.Context, args SendTxArgs) (dag.Hash, error) {
	return api.backend.SendTransaction(args)
}

// === Logs ===

// GetLogs returns logs matching a filter.
func (api *EthAPI) GetLogs(ctx context.Context, query FilterQuery) ([]Log, error) {
	return api.backend.GetLogs(query)
}

// === Filter Management ===

// NewFilter creates a new log filter.
func (api *EthAPI) NewFilter(ctx context.Context, query FilterQuery) (FilterID, error) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	api.filterSeq++
	id := FilterID(hex.EncodeToString([]byte{byte(api.filterSeq >> 24), byte(api.filterSeq >> 16), byte(api.filterSeq >> 8), byte(api.filterSeq)}))

	blockNum, _ := api.backend.GetBlockNumber()
	api.filters[id] = &filter{
		id:         id,
		filterType: "log",
		query:      query,
		lastBlock:  blockNum,
		created:    time.Now(),
	}

	return id, nil
}

// NewBlockFilter creates a new block filter.
func (api *EthAPI) NewBlockFilter(ctx context.Context) (FilterID, error) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	api.filterSeq++
	id := FilterID(hex.EncodeToString([]byte{byte(api.filterSeq >> 24), byte(api.filterSeq >> 16), byte(api.filterSeq >> 8), byte(api.filterSeq)}))

	blockNum, _ := api.backend.GetBlockNumber()
	api.filters[id] = &filter{
		id:         id,
		filterType: "block",
		lastBlock:  blockNum,
		created:    time.Now(),
	}

	return id, nil
}

// NewPendingTransactionFilter creates a new pending transaction filter.
func (api *EthAPI) NewPendingTransactionFilter(ctx context.Context) (FilterID, error) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	api.filterSeq++
	id := FilterID(hex.EncodeToString([]byte{byte(api.filterSeq >> 24), byte(api.filterSeq >> 16), byte(api.filterSeq >> 8), byte(api.filterSeq)}))

	api.filters[id] = &filter{
		id:         id,
		filterType: "pending",
		created:    time.Now(),
	}

	return id, nil
}

// GetFilterChanges returns filter changes since last poll.
func (api *EthAPI) GetFilterChanges(ctx context.Context, id FilterID) (interface{}, error) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	f, exists := api.filters[id]
	if !exists {
		return nil, errors.New("filter not found")
	}

	switch f.filterType {
	case "block":
		// Return new block hashes
		hashes := f.hashes
		f.hashes = nil
		return hashes, nil

	case "pending":
		// Return new pending tx hashes
		hashes := f.hashes
		f.hashes = nil
		return hashes, nil

	case "log":
		// Return new logs
		logs := f.logs
		f.logs = nil
		return logs, nil
	}

	return nil, nil
}

// GetFilterLogs returns all logs for a filter.
func (api *EthAPI) GetFilterLogs(ctx context.Context, id FilterID) ([]Log, error) {
	api.filterMu.RLock()
	f, exists := api.filters[id]
	api.filterMu.RUnlock()

	if !exists {
		return nil, errors.New("filter not found")
	}

	if f.filterType != "log" {
		return nil, errors.New("filter is not a log filter")
	}

	return api.backend.GetLogs(f.query)
}

// UninstallFilter removes a filter.
func (api *EthAPI) UninstallFilter(ctx context.Context, id FilterID) (bool, error) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	if _, exists := api.filters[id]; !exists {
		return false, nil
	}

	delete(api.filters, id)
	return true, nil
}

// === Mining (Compatibility) ===

// Mining returns true if mining (always false for PoS).
func (api *EthAPI) Mining(ctx context.Context) (bool, error) {
	return api.backend.Mining(), nil
}

// Hashrate returns the hashrate (always 0 for PoS).
func (api *EthAPI) Hashrate(ctx context.Context) (HexUint64, error) {
	return HexUint64(api.backend.Hashrate()), nil
}

// Coinbase returns the coinbase address.
func (api *EthAPI) Coinbase(ctx context.Context) (dag.Address, error) {
	return api.backend.Coinbase(), nil
}

// Accounts returns available accounts (empty for now).
func (api *EthAPI) Accounts(ctx context.Context) ([]dag.Address, error) {
	return []dag.Address{}, nil
}

// Sign signs data with an account (not implemented).
func (api *EthAPI) Sign(ctx context.Context, address dag.Address, data Hex) (Hex, error) {
	return nil, ErrFeatureNotSupported
}

// SignTransaction signs a transaction (not implemented).
func (api *EthAPI) SignTransaction(ctx context.Context, args SendTxArgs) (*types.Transaction, error) {
	return nil, ErrFeatureNotSupported
}

// === Block Receipts ===

// GetBlockReceipts returns all receipts for a block.
func (api *EthAPI) GetBlockReceipts(ctx context.Context, number BlockNumber) ([]Receipt, error) {
	block, err := api.backend.GetBlockByNumber(number, true)
	if err != nil || block == nil {
		return nil, err
	}

	// Get receipts for each transaction
	receipts := make([]Receipt, 0)
	if txHashes, ok := block.Transactions.([]dag.Hash); ok {
		for _, hash := range txHashes {
			receipt, err := api.backend.GetTransactionReceipt(hash)
			if err == nil && receipt != nil {
				receipts = append(receipts, *receipt)
			}
		}
	}

	return receipts, nil
}

// === Proof (not implemented) ===

// GetProof returns account/storage proof (not implemented).
func (api *EthAPI) GetProof(ctx context.Context, address dag.Address, storageKeys []dag.Hash, number BlockNumber) (interface{}, error) {
	return nil, ErrFeatureNotSupported
}

// AddToFilter adds new data to a filter (internal use).
func (api *EthAPI) AddToFilter(id FilterID, data interface{}) {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	f, exists := api.filters[id]
	if !exists {
		return
	}

	switch v := data.(type) {
	case dag.Hash:
		f.hashes = append(f.hashes, v)
	case Log:
		f.logs = append(f.logs, v)
	case []Log:
		f.logs = append(f.logs, v...)
	}
}

// PruneFilters removes old filters.
func (api *EthAPI) PruneFilters(maxAge time.Duration) int {
	api.filterMu.Lock()
	defer api.filterMu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for id, f := range api.filters {
		if f.created.Before(cutoff) {
			delete(api.filters, id)
			removed++
		}
	}

	return removed
}
