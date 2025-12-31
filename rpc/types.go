// Package rpc implements the JSON-RPC 2.0 server for NovaCoin.
//
// The RPC server provides Ethereum-compatible APIs including:
//   - eth_* methods for blockchain queries and transactions
//   - net_* methods for network information
//   - web3_* utility methods
//   - WebSocket subscriptions for real-time updates
package rpc

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/novacoin/novacoin/core/dag"
)

// JSON-RPC 2.0 types

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// BatchRequest represents a batch of JSON-RPC requests.
type BatchRequest []Request

// BatchResponse represents a batch of JSON-RPC responses.
type BatchResponse []Response

// Standard JSON-RPC 2.0 error codes
const (
	ErrCodeParse          = -32700 // Parse error
	ErrCodeInvalidRequest = -32600 // Invalid request
	ErrCodeMethodNotFound = -32601 // Method not found
	ErrCodeInvalidParams  = -32602 // Invalid params
	ErrCodeInternal       = -32603 // Internal error

	// Custom error codes (range: -32000 to -32099)
	ErrCodeExecution     = -32000 // Execution error
	ErrCodeNotFound      = -32001 // Resource not found
	ErrCodeInvalidInput  = -32002 // Invalid input
	ErrCodeGasLimit      = -32003 // Gas limit exceeded
	ErrCodeNonce         = -32004 // Nonce too low
	ErrCodeBalance       = -32005 // Insufficient balance
	ErrCodeTxRejected    = -32006 // Transaction rejected
	ErrCodeNotSupported  = -32007 // Feature not supported
	ErrCodeTimeout       = -32008 // Request timeout
	ErrCodeSubscription  = -32009 // Subscription error
)

// Standard errors
var (
	ErrParseError          = &Error{Code: ErrCodeParse, Message: "parse error"}
	ErrInvalidRequest      = &Error{Code: ErrCodeInvalidRequest, Message: "invalid request"}
	ErrMethodNotFound      = &Error{Code: ErrCodeMethodNotFound, Message: "method not found"}
	ErrInvalidParams       = &Error{Code: ErrCodeInvalidParams, Message: "invalid params"}
	ErrInternalError       = &Error{Code: ErrCodeInternal, Message: "internal error"}
	ErrResourceNotFound    = &Error{Code: ErrCodeNotFound, Message: "resource not found"}
	ErrFeatureNotSupported = &Error{Code: ErrCodeNotSupported, Message: "feature not supported"}
)

// NewError creates a new RPC error.
func NewError(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewErrorWithData creates a new RPC error with additional data.
func NewErrorWithData(code int, message string, data interface{}) *Error {
	return &Error{Code: code, Message: message, Data: data}
}

// === Ethereum-Compatible Types ===

// BlockNumber represents a block number or tag.
type BlockNumber int64

const (
	PendingBlockNumber  BlockNumber = -2
	LatestBlockNumber   BlockNumber = -1
	EarliestBlockNumber BlockNumber = 0
)

// UnmarshalJSON unmarshals a block number from JSON.
func (bn *BlockNumber) UnmarshalJSON(data []byte) error {
	var input string
	if err := json.Unmarshal(data, &input); err != nil {
		// Try parsing as number
		var num int64
		if err := json.Unmarshal(data, &num); err != nil {
			return err
		}
		*bn = BlockNumber(num)
		return nil
	}

	switch strings.ToLower(input) {
	case "pending":
		*bn = PendingBlockNumber
	case "latest":
		*bn = LatestBlockNumber
	case "earliest":
		*bn = EarliestBlockNumber
	default:
		// Parse hex number
		if !strings.HasPrefix(input, "0x") {
			return errors.New("invalid block number format")
		}
		n, err := parseHexUint64(input)
		if err != nil {
			return err
		}
		*bn = BlockNumber(n)
	}
	return nil
}

// MarshalJSON marshals a block number to JSON.
func (bn BlockNumber) MarshalJSON() ([]byte, error) {
	switch bn {
	case PendingBlockNumber:
		return json.Marshal("pending")
	case LatestBlockNumber:
		return json.Marshal("latest")
	case EarliestBlockNumber:
		return json.Marshal("earliest")
	default:
		return json.Marshal(fmt.Sprintf("0x%x", uint64(bn)))
	}
}

// Hex represents a hex-encoded byte slice.
type Hex []byte

// UnmarshalJSON unmarshals hex data from JSON.
func (h *Hex) UnmarshalJSON(data []byte) error {
	var input string
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	if !strings.HasPrefix(input, "0x") {
		return errors.New("hex data must start with 0x")
	}
	decoded, err := hex.DecodeString(input[2:])
	if err != nil {
		return err
	}
	*h = decoded
	return nil
}

// MarshalJSON marshals hex data to JSON.
func (h Hex) MarshalJSON() ([]byte, error) {
	return json.Marshal("0x" + hex.EncodeToString(h))
}

// String returns the hex string.
func (h Hex) String() string {
	return "0x" + hex.EncodeToString(h)
}

// HexUint64 represents a hex-encoded uint64.
type HexUint64 uint64

// UnmarshalJSON unmarshals a hex uint64 from JSON.
func (h *HexUint64) UnmarshalJSON(data []byte) error {
	var input string
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	n, err := parseHexUint64(input)
	if err != nil {
		return err
	}
	*h = HexUint64(n)
	return nil
}

// MarshalJSON marshals a hex uint64 to JSON.
func (h HexUint64) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%x", uint64(h)))
}

// HexBig represents a hex-encoded big.Int.
type HexBig big.Int

// UnmarshalJSON unmarshals a hex big.Int from JSON.
func (h *HexBig) UnmarshalJSON(data []byte) error {
	var input string
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	if !strings.HasPrefix(input, "0x") {
		return errors.New("hex number must start with 0x")
	}
	n, ok := new(big.Int).SetString(input[2:], 16)
	if !ok {
		return errors.New("invalid hex number")
	}
	*h = HexBig(*n)
	return nil
}

// MarshalJSON marshals a hex big.Int to JSON.
func (h HexBig) MarshalJSON() ([]byte, error) {
	return json.Marshal("0x" + (*big.Int)(&h).Text(16))
}

// ToInt returns the big.Int value.
func (h *HexBig) ToInt() *big.Int {
	return (*big.Int)(h)
}

// === Block Types ===

// Block represents a block in the RPC response.
type Block struct {
	Number           HexUint64   `json:"number"`
	Hash             dag.Hash    `json:"hash"`
	ParentHash       dag.Hash    `json:"parentHash"`
	Nonce            Hex         `json:"nonce"`
	Sha3Uncles       dag.Hash    `json:"sha3Uncles"`
	LogsBloom        Hex         `json:"logsBloom"`
	TransactionsRoot dag.Hash    `json:"transactionsRoot"`
	StateRoot        dag.Hash    `json:"stateRoot"`
	ReceiptsRoot     dag.Hash    `json:"receiptsRoot"`
	Miner            dag.Address `json:"miner"`
	Difficulty       HexUint64   `json:"difficulty"`
	TotalDifficulty  HexBig      `json:"totalDifficulty"`
	ExtraData        Hex         `json:"extraData"`
	Size             HexUint64   `json:"size"`
	GasLimit         HexUint64   `json:"gasLimit"`
	GasUsed          HexUint64   `json:"gasUsed"`
	Timestamp        HexUint64   `json:"timestamp"`
	Transactions     interface{} `json:"transactions"` // []Hash or []Transaction
	Uncles           []dag.Hash  `json:"uncles"`

	// NovaCoin-specific fields
	BlueScore     HexUint64 `json:"blueScore,omitempty"`
	Wave          HexUint64 `json:"wave,omitempty"`
	Round         HexUint64 `json:"round,omitempty"`
	IsFinalized   bool      `json:"isFinalized,omitempty"`
	ParallelDAGId uint8     `json:"parallelDagId,omitempty"`
}

// Transaction represents a transaction in the RPC response.
type Transaction struct {
	BlockHash        *dag.Hash    `json:"blockHash"`
	BlockNumber      *HexUint64   `json:"blockNumber"`
	From             dag.Address  `json:"from"`
	Gas              HexUint64    `json:"gas"`
	GasPrice         HexBig       `json:"gasPrice"`
	MaxFeePerGas     *HexBig      `json:"maxFeePerGas,omitempty"`
	MaxPriorityFee   *HexBig      `json:"maxPriorityFeePerGas,omitempty"`
	Hash             dag.Hash     `json:"hash"`
	Input            Hex          `json:"input"`
	Nonce            HexUint64    `json:"nonce"`
	To               *dag.Address `json:"to"`
	TransactionIndex *HexUint64   `json:"transactionIndex"`
	Value            HexBig       `json:"value"`
	Type             HexUint64    `json:"type"`
	ChainID          *HexBig      `json:"chainId,omitempty"`
	V                HexUint64    `json:"v"`
	R                HexBig       `json:"r"`
	S                HexBig       `json:"s"`
}

// Receipt represents a transaction receipt.
type Receipt struct {
	TransactionHash   dag.Hash    `json:"transactionHash"`
	TransactionIndex  HexUint64   `json:"transactionIndex"`
	BlockHash         dag.Hash    `json:"blockHash"`
	BlockNumber       HexUint64   `json:"blockNumber"`
	From              dag.Address `json:"from"`
	To                dag.Address `json:"to"`
	CumulativeGasUsed HexUint64   `json:"cumulativeGasUsed"`
	GasUsed           HexUint64   `json:"gasUsed"`
	ContractAddress   dag.Address `json:"contractAddress,omitempty"`
	Logs              []Log       `json:"logs"`
	LogsBloom         Hex         `json:"logsBloom"`
	Status            HexUint64   `json:"status"`
	EffectiveGasPrice HexBig      `json:"effectiveGasPrice"`
	Type              HexUint64   `json:"type"`
}

// Log represents a contract log entry.
type Log struct {
	Address          dag.Address `json:"address"`
	Topics           []dag.Hash  `json:"topics"`
	Data             Hex         `json:"data"`
	BlockNumber      HexUint64   `json:"blockNumber"`
	TransactionHash  dag.Hash    `json:"transactionHash"`
	TransactionIndex HexUint64   `json:"transactionIndex"`
	BlockHash        dag.Hash    `json:"blockHash"`
	LogIndex         HexUint64   `json:"logIndex"`
	Removed          bool        `json:"removed"`
}

// === Call/Transaction Types ===

// CallArgs represents the arguments for eth_call.
type CallArgs struct {
	From                 *dag.Address `json:"from"`
	To                   *dag.Address `json:"to"`
	Gas                  *HexUint64   `json:"gas"`
	GasPrice             *HexBig      `json:"gasPrice"`
	MaxFeePerGas         *HexBig      `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *HexBig      `json:"maxPriorityFeePerGas"`
	Value                *HexBig      `json:"value"`
	Nonce                *HexUint64   `json:"nonce"`
	Data                 *Hex         `json:"data"`
	Input                *Hex         `json:"input"` // Alias for data
}

// GetData returns the call data (prefers input over data).
func (args *CallArgs) GetData() []byte {
	if args.Input != nil {
		return *args.Input
	}
	if args.Data != nil {
		return *args.Data
	}
	return nil
}

// SendTxArgs represents the arguments for eth_sendTransaction.
type SendTxArgs struct {
	From                 dag.Address  `json:"from"`
	To                   *dag.Address `json:"to"`
	Gas                  *HexUint64   `json:"gas"`
	GasPrice             *HexBig      `json:"gasPrice"`
	MaxFeePerGas         *HexBig      `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *HexBig      `json:"maxPriorityFeePerGas"`
	Value                *HexBig      `json:"value"`
	Nonce                *HexUint64   `json:"nonce"`
	Data                 *Hex         `json:"data"`
	Input                *Hex         `json:"input"` // Alias for data
	ChainID              *HexBig      `json:"chainId"`
}

// === Filter Types ===

// FilterQuery represents a filter for logs.
type FilterQuery struct {
	BlockHash *dag.Hash     `json:"blockHash"`
	FromBlock *BlockNumber  `json:"fromBlock"`
	ToBlock   *BlockNumber  `json:"toBlock"`
	Addresses []dag.Address `json:"address"`
	Topics    [][]dag.Hash  `json:"topics"`
}

// FilterID represents a filter identifier.
type FilterID string

// === Sync Status ===

// SyncStatus represents the synchronization status.
type SyncStatus struct {
	StartingBlock HexUint64 `json:"startingBlock"`
	CurrentBlock  HexUint64 `json:"currentBlock"`
	HighestBlock  HexUint64 `json:"highestBlock"`
}

// === Peer Info ===

// PeerInfo represents information about a connected peer.
type PeerInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Caps      []string `json:"caps"`
	Network   Network  `json:"network"`
	Protocols Protocol `json:"protocols"`
}

// Network represents peer network information.
type Network struct {
	LocalAddress  string `json:"localAddress"`
	RemoteAddress string `json:"remoteAddress"`
	Inbound       bool   `json:"inbound"`
	Trusted       bool   `json:"trusted"`
	Static        bool   `json:"static"`
}

// Protocol represents supported protocols.
type Protocol struct {
	Nova *NovaProtocol `json:"nova,omitempty"`
}

// NovaProtocol represents the NovaCoin protocol info.
type NovaProtocol struct {
	Version    uint64 `json:"version"`
	Difficulty uint64 `json:"difficulty"`
	Head       string `json:"head"`
}

// === Helper Functions ===

// parseHexUint64 parses a hex string to uint64.
func parseHexUint64(s string) (uint64, error) {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return 0, errors.New("hex string must start with 0x")
	}
	s = s[2:]
	if len(s) > 16 {
		return 0, errors.New("hex string too large")
	}
	var result uint64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result |= uint64(c - 'A' + 10)
		default:
			return 0, errors.New("invalid hex character")
		}
	}
	return result, nil
}

// HashToHex converts a hash to hex string.
func HashToHex(h dag.Hash) string {
	return "0x" + hex.EncodeToString(h[:])
}

// AddressToHex converts an address to hex string.
func AddressToHex(a dag.Address) string {
	return "0x" + hex.EncodeToString(a[:])
}

// HexToHash converts a hex string to hash.
func HexToHash(s string) (dag.Hash, error) {
	var h dag.Hash
	if !strings.HasPrefix(s, "0x") {
		return h, errors.New("hash must start with 0x")
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		return h, err
	}
	if len(b) != 32 {
		return h, errors.New("hash must be 32 bytes")
	}
	copy(h[:], b)
	return h, nil
}

// HexToAddress converts a hex string to address.
func HexToAddress(s string) (dag.Address, error) {
	var a dag.Address
	if !strings.HasPrefix(s, "0x") {
		return a, errors.New("address must start with 0x")
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		return a, err
	}
	if len(b) != 20 {
		return a, errors.New("address must be 20 bytes")
	}
	copy(a[:], b)
	return a, nil
}
