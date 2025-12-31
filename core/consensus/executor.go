// Package consensus implements the hybrid consensus orchestrator for NovaCoin.
package consensus

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
	"github.com/novacoin/novacoin/core/evm"
	"github.com/novacoin/novacoin/core/state"
	"github.com/novacoin/novacoin/core/types"
)

// ExecutionResult contains the result of block execution.
type ExecutionResult struct {
	// Block info
	BlockHash   dag.Hash
	BlockNumber uint64

	// Execution results
	StateRoot    state.Hash
	ReceiptRoot  state.Hash
	GasUsed      uint64
	Receipts     types.Receipts
	Logs         []*types.Log

	// Timing
	ExecutionTime time.Duration

	// Errors
	FailedTxs []FailedTx
}

// FailedTx represents a failed transaction.
type FailedTx struct {
	Index uint
	Hash  state.Hash
	Error string
}

// TxExecutor handles transaction execution.
type TxExecutor struct {
	consensus *HybridConsensus

	// EVM configuration
	evmConfig *evm.Config

	// Gas tracking
	blockGasUsed uint64

	// Execution metrics
	totalExecuted uint64
	totalFailed   uint64

	mu sync.Mutex
}

// NewTxExecutor creates a new transaction executor.
func NewTxExecutor(consensus *HybridConsensus) *TxExecutor {
	return &TxExecutor{
		consensus: consensus,
		evmConfig: evm.DefaultConfig(),
	}
}

// Execute executes all transactions in a block.
func (te *TxExecutor) Execute(vertex *dag.Vertex) (*ExecutionResult, error) {
	te.mu.Lock()
	defer te.mu.Unlock()

	start := time.Now()

	result := &ExecutionResult{
		BlockHash:   vertex.Hash,
		BlockNumber: vertex.Height,
		Receipts:    make(types.Receipts, 0, len(vertex.Transactions)),
		Logs:        make([]*types.Log, 0),
		FailedTxs:   make([]FailedTx, 0),
	}

	// Create state snapshot for rollback on failure
	snapshot := te.consensus.stateDB.Snapshot()

	// Create EVM context
	ctx := te.createEVMContext(vertex)

	// Reset block gas
	te.blockGasUsed = 0

	// Execute each transaction
	for i, txData := range vertex.Transactions {
		receipt, logs, err := te.executeTx(ctx, txData, uint(i))
		if err != nil {
			result.FailedTxs = append(result.FailedTxs, FailedTx{
				Index: uint(i),
				Error: err.Error(),
			})
			te.totalFailed++
			continue
		}

		result.Receipts = append(result.Receipts, receipt)
		result.Logs = append(result.Logs, logs...)
		te.totalExecuted++
	}

	// Compute state root
	stateRoot, err := te.consensus.stateDB.Commit()
	if err != nil {
		// Rollback on failure
		te.consensus.stateDB.RevertToSnapshot(snapshot)
		return nil, errors.New("failed to commit state: " + err.Error())
	}
	result.StateRoot = stateRoot

	// Compute receipt root
	result.ReceiptRoot = te.computeReceiptRoot(result.Receipts)

	// Set gas used
	result.GasUsed = te.blockGasUsed

	// Set execution time
	result.ExecutionTime = time.Since(start)

	// Update vertex
	copy(vertex.StateRoot[:], result.StateRoot[:])
	copy(vertex.ReceiptRoot[:], result.ReceiptRoot[:])

	// Update metrics
	te.consensus.metrics.mu.Lock()
	te.consensus.metrics.TxExecuted += uint64(len(result.Receipts))
	te.consensus.metrics.TxFailed += uint64(len(result.FailedTxs))
	te.consensus.metrics.mu.Unlock()

	return result, nil
}

// createEVMContext creates the EVM context for execution.
func (te *TxExecutor) createEVMContext(vertex *dag.Vertex) *evm.CallContext {
	ctx := &evm.CallContext{
		Origin:      state.Address{}, // Will be set per transaction
		GasPrice:    big.NewInt(1),   // Default gas price
		Coinbase:    te.validatorToAddress(vertex.ValidatorPubKey),
		BlockNumber: big.NewInt(int64(vertex.Height)),
		Time:        uint64(vertex.Timestamp.Unix()),
		GasLimit:    te.consensus.config.BlockGasLimit,
		ChainID:     te.consensus.config.ChainID,
		BaseFee:     big.NewInt(1), // EIP-1559 base fee
	}

	// Set PrevRandao from parent block (for randomness)
	if len(vertex.Parents) > 0 {
		parent := te.consensus.dagStore.Get(vertex.Parents[0])
		if parent != nil {
			copy(ctx.PrevRandao[:], parent.Hash[:])
		}
	}

	return ctx
}

// executeTx executes a single transaction.
func (te *TxExecutor) executeTx(
	ctx *evm.CallContext,
	txData []byte,
	index uint,
) (*types.Receipt, []*types.Log, error) {
	// Decode transaction
	tx, err := te.decodeTx(txData)
	if err != nil {
		return nil, nil, err
	}

	// Check block gas limit
	if te.blockGasUsed+tx.Gas() > te.consensus.config.BlockGasLimit {
		return nil, nil, errors.New("block gas limit exceeded")
	}

	// Get sender
	sender, err := te.getSender(tx)
	if err != nil {
		return nil, nil, err
	}

	// Check nonce
	stateNonce := te.consensus.stateDB.GetNonce(sender)
	if tx.Nonce() != stateNonce {
		return nil, nil, errors.New("invalid nonce")
	}

	// Check balance
	cost := tx.Cost()
	balance := te.consensus.stateDB.GetBalance(sender)
	if balance.Cmp(cost) < 0 {
		return nil, nil, errors.New("insufficient balance")
	}

	// Create snapshot for this tx
	snapshot := te.consensus.stateDB.Snapshot()

	// Deduct gas cost upfront
	te.consensus.stateDB.SubBalance(sender, cost)

	// Increment nonce
	te.consensus.stateDB.SetNonce(sender, stateNonce+1)

	// Create EVM
	stateAdapter := &stateDBAdapter{db: te.consensus.stateDB}
	evmInstance := evm.NewEVM(ctx, stateAdapter, te.evmConfig)
	interpreter := evm.NewInterpreter(evmInstance)

	// Execute
	var (
		ret     []byte
		gasUsed uint64
		execErr error
		logs    []*types.Log
	)

	if tx.To() == nil {
		// Contract creation
		ret, gasUsed, execErr = te.executeCreate(interpreter, sender, tx)
	} else {
		// Call
		ret, gasUsed, execErr = te.executeCall(interpreter, sender, *tx.To(), tx)
	}

	// Handle execution result
	receipt := &types.Receipt{
		Type:              tx.Type(),
		CumulativeGasUsed: te.blockGasUsed + gasUsed,
		GasUsed:           gasUsed,
	}

	if execErr != nil {
		// Revert state changes
		te.consensus.stateDB.RevertToSnapshot(snapshot)
		receipt.Status = types.ReceiptStatusFailed

		// Still consume gas
		te.blockGasUsed += gasUsed
	} else {
		receipt.Status = types.ReceiptStatusSuccess
		te.blockGasUsed += gasUsed

		// Refund unused gas
		gasRefund := tx.Gas() - gasUsed
		refundValue := new(big.Int).Mul(big.NewInt(int64(gasRefund)), tx.GasPrice())
		te.consensus.stateDB.AddBalance(sender, refundValue)
	}

	// Compute logs bloom
	receipt.Bloom = types.LogsBloom(logs)
	receipt.Logs = logs

	_ = ret // Return data (for contract calls)

	return receipt, logs, nil
}

// executeCreate executes a contract creation.
func (te *TxExecutor) executeCreate(
	interpreter *evm.Interpreter,
	sender state.Address,
	tx *types.Transaction,
) ([]byte, uint64, error) {
	// Create contract address
	nonce := te.consensus.stateDB.GetNonce(sender)
	contractAddr := te.createContractAddress(sender, nonce)

	// Create contract account
	te.consensus.stateDB.CreateAccount(contractAddr)

	// Transfer value
	if tx.Value().Sign() > 0 {
		te.consensus.stateDB.AddBalance(contractAddr, tx.Value())
	}

	// Create contract for execution
	contract := evm.NewContract(sender, contractAddr, tx.Value(), tx.Gas())
	contract.SetCode(state.Hash{}, evm.Code(tx.Data()))

	// Run initialization code
	ret, err := interpreter.Run(contract, nil, false)

	// Store deployed code
	if err == nil && len(ret) > 0 {
		te.consensus.stateDB.SetCode(contractAddr, ret)
	}

	gasUsed := tx.Gas() - contract.Gas

	return ret, gasUsed, err
}

// executeCall executes a contract call.
func (te *TxExecutor) executeCall(
	interpreter *evm.Interpreter,
	sender state.Address,
	to state.Address,
	tx *types.Transaction,
) ([]byte, uint64, error) {
	// Transfer value
	if tx.Value().Sign() > 0 {
		te.consensus.stateDB.AddBalance(to, tx.Value())
	}

	// Get code
	code := te.consensus.stateDB.GetCode(to)
	if len(code) == 0 {
		// Simple transfer, no code to execute
		return nil, 21000, nil
	}

	// Create contract for execution
	contract := evm.NewContract(sender, to, tx.Value(), tx.Gas())
	codeHash := te.consensus.stateDB.GetCodeHash(to)
	contract.SetCode(codeHash, evm.Code(code))

	// Run code
	ret, err := interpreter.Run(contract, tx.Data(), false)

	gasUsed := tx.Gas() - contract.Gas

	return ret, gasUsed, err
}

// decodeTx decodes a transaction from bytes.
func (te *TxExecutor) decodeTx(data []byte) (*types.Transaction, error) {
	if len(data) < 1 {
		return nil, errors.New("empty transaction data")
	}

	// Simplified decoding - real implementation would use RLP
	// For now, create a dummy transaction
	tx := types.NewTransaction(
		0,                        // nonce
		state.Address{},          // to
		big.NewInt(0),            // value
		21000,                    // gas
		big.NewInt(1),            // gasPrice
		data,                     // data
	)

	return tx, nil
}

// getSender extracts the sender from a transaction.
func (te *TxExecutor) getSender(tx *types.Transaction) (state.Address, error) {
	signer := types.NewLondonSigner(te.consensus.config.ChainID)
	return signer.Sender(tx)
}

// createContractAddress creates a contract address.
func (te *TxExecutor) createContractAddress(sender state.Address, nonce uint64) state.Address {
	// Simplified - real implementation uses RLP(sender, nonce)
	var addr state.Address
	copy(addr[:], sender[:])
	addr[19] = byte(nonce)
	return addr
}

// validatorToAddress converts a validator public key to an address.
func (te *TxExecutor) validatorToAddress(pubKey dag.PublicKey) state.Address {
	var addr state.Address
	// Take last 20 bytes of public key hash
	copy(addr[:], pubKey[len(pubKey)-20:])
	return addr
}

// computeReceiptRoot computes the Merkle root of receipts.
func (te *TxExecutor) computeReceiptRoot(receipts types.Receipts) state.Hash {
	if len(receipts) == 0 {
		return state.EmptyHash
	}

	// Simplified - real implementation uses Merkle Patricia Trie
	var root state.Hash
	for _, r := range receipts {
		root[0] ^= byte(r.Status)
		root[1] ^= byte(r.GasUsed >> 8)
		root[2] ^= byte(r.GasUsed)
	}

	return root
}

// GetTotalExecuted returns the total number of executed transactions.
func (te *TxExecutor) GetTotalExecuted() uint64 {
	te.mu.Lock()
	defer te.mu.Unlock()
	return te.totalExecuted
}

// GetTotalFailed returns the total number of failed transactions.
func (te *TxExecutor) GetTotalFailed() uint64 {
	te.mu.Lock()
	defer te.mu.Unlock()
	return te.totalFailed
}

// === State DB Adapter ===

// stateDBAdapter adapts our StateDB to evm.StateDB interface.
type stateDBAdapter struct {
	db *state.StateDB
}

func (a *stateDBAdapter) CreateAccount(addr state.Address) {
	a.db.CreateAccount(addr)
}

func (a *stateDBAdapter) SubBalance(addr state.Address, amount *big.Int) {
	a.db.SubBalance(addr, amount)
}

func (a *stateDBAdapter) AddBalance(addr state.Address, amount *big.Int) {
	a.db.AddBalance(addr, amount)
}

func (a *stateDBAdapter) GetBalance(addr state.Address) *big.Int {
	return a.db.GetBalance(addr)
}

func (a *stateDBAdapter) GetNonce(addr state.Address) uint64 {
	return a.db.GetNonce(addr)
}

func (a *stateDBAdapter) SetNonce(addr state.Address, nonce uint64) {
	a.db.SetNonce(addr, nonce)
}

func (a *stateDBAdapter) GetCode(addr state.Address) []byte {
	return a.db.GetCode(addr)
}

func (a *stateDBAdapter) GetCodeSize(addr state.Address) int {
	return len(a.db.GetCode(addr))
}

func (a *stateDBAdapter) GetCodeHash(addr state.Address) state.Hash {
	return a.db.GetCodeHash(addr)
}

func (a *stateDBAdapter) SetCode(addr state.Address, code []byte) {
	a.db.SetCode(addr, code)
}

func (a *stateDBAdapter) GetState(addr state.Address, key state.Hash) state.Hash {
	return a.db.GetState(addr, key)
}

func (a *stateDBAdapter) SetState(addr state.Address, key, value state.Hash) {
	a.db.SetState(addr, key, value)
}

func (a *stateDBAdapter) GetCommittedState(addr state.Address, key state.Hash) state.Hash {
	return a.db.GetCommittedState(addr, key)
}

func (a *stateDBAdapter) GetTransientState(addr state.Address, key state.Hash) state.Hash {
	return a.db.GetTransientState(addr, key)
}

func (a *stateDBAdapter) SetTransientState(addr state.Address, key, value state.Hash) {
	a.db.SetTransientState(addr, key, value)
}

func (a *stateDBAdapter) HasSuicided(addr state.Address) bool {
	return a.db.HasSuicided(addr)
}

func (a *stateDBAdapter) Suicide(addr state.Address) bool {
	return a.db.Suicide(addr)
}

func (a *stateDBAdapter) Exist(addr state.Address) bool {
	return a.db.Exist(addr)
}

func (a *stateDBAdapter) Empty(addr state.Address) bool {
	return a.db.Empty(addr)
}

func (a *stateDBAdapter) AddAddressToAccessList(addr state.Address) {
	a.db.AddAddressToAccessList(addr)
}

func (a *stateDBAdapter) AddSlotToAccessList(addr state.Address, slot state.Hash) {
	a.db.AddSlotToAccessList(addr, slot)
}

func (a *stateDBAdapter) AddressInAccessList(addr state.Address) bool {
	return a.db.AddressInAccessList(addr)
}

func (a *stateDBAdapter) SlotInAccessList(addr state.Address, slot state.Hash) (bool, bool) {
	return a.db.SlotInAccessList(addr, slot)
}

func (a *stateDBAdapter) Snapshot() int {
	return a.db.Snapshot()
}

func (a *stateDBAdapter) RevertToSnapshot(id int) {
	a.db.RevertToSnapshot(id)
}

func (a *stateDBAdapter) AddRefund(gas uint64) {
	a.db.AddRefund(gas)
}

func (a *stateDBAdapter) SubRefund(gas uint64) {
	a.db.SubRefund(gas)
}

func (a *stateDBAdapter) GetRefund() uint64 {
	return a.db.GetRefund()
}
