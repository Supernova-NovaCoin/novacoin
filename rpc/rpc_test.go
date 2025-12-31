package rpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// === Test Types ===

func TestBlockNumberUnmarshal(t *testing.T) {
	tests := []struct {
		input    string
		expected BlockNumber
	}{
		{`"latest"`, LatestBlockNumber},
		{`"pending"`, PendingBlockNumber},
		{`"earliest"`, EarliestBlockNumber},
		{`"0x0"`, BlockNumber(0)},
		{`"0x10"`, BlockNumber(16)},
		{`"0xff"`, BlockNumber(255)},
		{`123`, BlockNumber(123)},
	}

	for _, tt := range tests {
		var bn BlockNumber
		err := json.Unmarshal([]byte(tt.input), &bn)
		if err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if bn != tt.expected {
			t.Errorf("Unmarshal(%s) = %d, want %d", tt.input, bn, tt.expected)
		}
	}
}

func TestBlockNumberMarshal(t *testing.T) {
	tests := []struct {
		bn       BlockNumber
		expected string
	}{
		{LatestBlockNumber, `"latest"`},
		{PendingBlockNumber, `"pending"`},
		{EarliestBlockNumber, `"earliest"`},
		// BlockNumber(0) equals EarliestBlockNumber, so it marshals as "earliest"
		{BlockNumber(1), `"0x1"`},
		{BlockNumber(16), `"0x10"`},
		{BlockNumber(255), `"0xff"`},
	}

	for _, tt := range tests {
		data, err := json.Marshal(tt.bn)
		if err != nil {
			t.Errorf("Marshal(%d) error: %v", tt.bn, err)
			continue
		}
		if string(data) != tt.expected {
			t.Errorf("Marshal(%d) = %s, want %s", tt.bn, data, tt.expected)
		}
	}
}

func TestHexUint64(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{`"0x0"`, 0},
		{`"0x10"`, 16},
		{`"0xff"`, 255},
		{`"0x1000"`, 4096},
	}

	for _, tt := range tests {
		var h HexUint64
		err := json.Unmarshal([]byte(tt.input), &h)
		if err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if uint64(h) != tt.expected {
			t.Errorf("Unmarshal(%s) = %d, want %d", tt.input, h, tt.expected)
		}
	}
}

func TestHexBig(t *testing.T) {
	tests := []struct {
		input    string
		expected *big.Int
	}{
		{`"0x0"`, big.NewInt(0)},
		{`"0x10"`, big.NewInt(16)},
		{`"0xffffffffff"`, big.NewInt(1099511627775)},
	}

	for _, tt := range tests {
		var h HexBig
		err := json.Unmarshal([]byte(tt.input), &h)
		if err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if h.ToInt().Cmp(tt.expected) != 0 {
			t.Errorf("Unmarshal(%s) = %s, want %s", tt.input, h.ToInt(), tt.expected)
		}
	}
}

func TestHex(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{`"0x"`, []byte{}},
		{`"0x00"`, []byte{0}},
		{`"0xff"`, []byte{255}},
		{`"0x0102"`, []byte{1, 2}},
	}

	for _, tt := range tests {
		var h Hex
		err := json.Unmarshal([]byte(tt.input), &h)
		if err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if len(h) != len(tt.expected) {
			t.Errorf("Unmarshal(%s) len = %d, want %d", tt.input, len(h), len(tt.expected))
			continue
		}
		for i := range h {
			if h[i] != tt.expected[i] {
				t.Errorf("Unmarshal(%s)[%d] = %d, want %d", tt.input, i, h[i], tt.expected[i])
			}
		}
	}
}

func TestHashConversion(t *testing.T) {
	hashStr := "0x0102030405060708091011121314151617181920212223242526272829303132"
	h, err := HexToHash(hashStr)
	if err != nil {
		t.Fatalf("HexToHash error: %v", err)
	}

	result := HashToHex(h)
	if result != hashStr {
		t.Errorf("HashToHex = %s, want %s", result, hashStr)
	}
}

func TestAddressConversion(t *testing.T) {
	addrStr := "0x0102030405060708091011121314151617181920"
	a, err := HexToAddress(addrStr)
	if err != nil {
		t.Fatalf("HexToAddress error: %v", err)
	}

	result := AddressToHex(a)
	if result != addrStr {
		t.Errorf("AddressToHex = %s, want %s", result, addrStr)
	}
}

// === Test Handler ===

func TestHandler(t *testing.T) {
	handler := NewHandler()

	// Register a test API
	testAPI := &TestAPI{}
	err := handler.RegisterAPI("test", testAPI)
	if err != nil {
		t.Fatalf("RegisterAPI error: %v", err)
	}

	// Check methods are registered
	methods := handler.GetMethods()
	if len(methods) == 0 {
		t.Error("No methods registered")
	}

	// Test simple method call
	req := `{"jsonrpc":"2.0","method":"test_echo","params":["hello"],"id":1}`
	resp := handler.Handle(context.Background(), []byte(req))

	var response Response
	if err := json.Unmarshal(resp, &response); err != nil {
		t.Fatalf("Unmarshal response error: %v", err)
	}

	if response.Error != nil {
		t.Errorf("Unexpected error: %v", response.Error)
	}
	if response.Result != "hello" {
		t.Errorf("Result = %v, want 'hello'", response.Result)
	}
}

func TestHandlerBatch(t *testing.T) {
	handler := NewHandler()
	testAPI := &TestAPI{}
	_ = handler.RegisterAPI("test", testAPI)

	// Batch request
	req := `[
		{"jsonrpc":"2.0","method":"test_echo","params":["one"],"id":1},
		{"jsonrpc":"2.0","method":"test_echo","params":["two"],"id":2}
	]`
	resp := handler.Handle(context.Background(), []byte(req))

	var responses BatchResponse
	if err := json.Unmarshal(resp, &responses); err != nil {
		t.Fatalf("Unmarshal response error: %v", err)
	}

	if len(responses) != 2 {
		t.Errorf("Expected 2 responses, got %d", len(responses))
	}
}

func TestHandlerMethodNotFound(t *testing.T) {
	handler := NewHandler()

	req := `{"jsonrpc":"2.0","method":"unknown_method","params":[],"id":1}`
	resp := handler.Handle(context.Background(), []byte(req))

	var response Response
	if err := json.Unmarshal(resp, &response); err != nil {
		t.Fatalf("Unmarshal response error: %v", err)
	}

	if response.Error == nil {
		t.Error("Expected error for unknown method")
	}
	if response.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", response.Error.Code, ErrCodeMethodNotFound)
	}
}

func TestHandlerInvalidJSON(t *testing.T) {
	handler := NewHandler()

	req := `{invalid json}`
	resp := handler.Handle(context.Background(), []byte(req))

	var response Response
	if err := json.Unmarshal(resp, &response); err != nil {
		t.Fatalf("Unmarshal response error: %v", err)
	}

	if response.Error == nil {
		t.Error("Expected error for invalid JSON")
	}
	if response.Error.Code != ErrCodeParse {
		t.Errorf("Error code = %d, want %d", response.Error.Code, ErrCodeParse)
	}
}

// TestAPI is a simple test API.
type TestAPI struct{}

func (api *TestAPI) Echo(ctx context.Context, msg string) (string, error) {
	return msg, nil
}

func (api *TestAPI) Add(ctx context.Context, a, b int) (int, error) {
	return a + b, nil
}

func (api *TestAPI) GetBlock(ctx context.Context) (*Block, error) {
	return &Block{
		Number: HexUint64(100),
		Hash:   dag.Hash{1, 2, 3},
	}, nil
}

// === Test Subscription Manager ===

func TestSubscriptionManager(t *testing.T) {
	mgr := NewSubscriptionManager(nil, 100)

	// Create subscription
	sub, err := mgr.Subscribe(SubNewHeads, nil)
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}

	if sub.ID == "" {
		t.Error("Subscription ID is empty")
	}

	if sub.Type != SubNewHeads {
		t.Errorf("Subscription type = %v, want %v", sub.Type, SubNewHeads)
	}

	// Check stats
	stats := mgr.GetStats()
	if stats.Total != 1 {
		t.Errorf("Stats.Total = %d, want 1", stats.Total)
	}

	// Unsubscribe
	if !mgr.Unsubscribe(sub.ID) {
		t.Error("Unsubscribe returned false")
	}

	stats = mgr.GetStats()
	if stats.Total != 0 {
		t.Errorf("Stats.Total after unsubscribe = %d, want 0", stats.Total)
	}
}

func TestSubscriptionBroadcast(t *testing.T) {
	mgr := NewSubscriptionManager(nil, 100)

	// Create subscription
	sub, _ := mgr.Subscribe(SubNewHeads, nil)

	// Broadcast a block
	block := &Block{Number: HexUint64(100)}
	mgr.BroadcastNewHead(block)

	// Check subscription received it
	select {
	case data := <-sub.Channel:
		if b, ok := data.(*Block); !ok || b.Number != 100 {
			t.Errorf("Received wrong block: %v", data)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscription didn't receive broadcast")
	}
}

func TestLogSubscriptionFilter(t *testing.T) {
	mgr := NewSubscriptionManager(nil, 100)

	// Create log filter for specific address
	var filterAddr dag.Address
	filterAddr[0] = 1
	filter := &FilterQuery{Addresses: []dag.Address{filterAddr}}
	sub, _ := mgr.Subscribe(SubLogs, filter)

	// Broadcast matching log
	matchingLog := Log{Address: filterAddr}
	mgr.BroadcastLog(matchingLog)

	// Should receive
	select {
	case <-sub.Channel:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscription didn't receive matching log")
	}

	// Broadcast non-matching log
	var otherAddr dag.Address
	otherAddr[0] = 2
	nonMatchingLog := Log{Address: otherAddr}
	mgr.BroadcastLog(nonMatchingLog)

	// Should not receive
	select {
	case data := <-sub.Channel:
		t.Errorf("Subscription received non-matching log: %v", data)
	case <-time.After(50 * time.Millisecond):
		// Good - no message
	}
}

func TestSubscriptionLimit(t *testing.T) {
	mgr := NewSubscriptionManager(nil, 3)

	// Create max subscriptions
	for i := 0; i < 3; i++ {
		_, err := mgr.Subscribe(SubNewHeads, nil)
		if err != nil {
			t.Fatalf("Subscribe %d error: %v", i, err)
		}
	}

	// Should fail at limit
	_, err := mgr.Subscribe(SubNewHeads, nil)
	if err == nil {
		t.Error("Expected error at subscription limit")
	}
}

// === Test Rate Limiter ===

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	// Should allow burst
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("Allow() = false at request %d, want true", i)
		}
	}

	// Should be rate limited
	if rl.Allow() {
		t.Error("Allow() = true after burst, want false")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Should allow again
	if !rl.Allow() {
		t.Error("Allow() = false after refill, want true")
	}
}

func TestIPRateLimiter(t *testing.T) {
	rl := NewIPRateLimiter(10, 2)

	// Different IPs should have separate limits
	for i := 0; i < 2; i++ {
		if !rl.Allow("1.1.1.1") {
			t.Errorf("Allow(1.1.1.1) = false at %d", i)
		}
		if !rl.Allow("2.2.2.2") {
			t.Errorf("Allow(2.2.2.2) = false at %d", i)
		}
	}

	// Both should be limited now
	if rl.Allow("1.1.1.1") {
		t.Error("Allow(1.1.1.1) = true after limit")
	}
	if rl.Allow("2.2.2.2") {
		t.Error("Allow(2.2.2.2) = true after limit")
	}
}

// === Test Server Config ===

func TestDefaultServerConfig(t *testing.T) {
	config := DefaultServerConfig()

	if config.HTTPPort != 8545 {
		t.Errorf("HTTPPort = %d, want 8545", config.HTTPPort)
	}
	if config.WSPort != 8546 {
		t.Errorf("WSPort = %d, want 8546", config.WSPort)
	}
	if !config.HTTPEnabled {
		t.Error("HTTPEnabled = false, want true")
	}
	if !config.WSEnabled {
		t.Error("WSEnabled = false, want true")
	}
}

// === Test Error Types ===

func TestErrors(t *testing.T) {
	err := NewError(ErrCodeInternal, "test error")
	if err.Code != ErrCodeInternal {
		t.Errorf("Error.Code = %d, want %d", err.Code, ErrCodeInternal)
	}
	if err.Message != "test error" {
		t.Errorf("Error.Message = %s, want 'test error'", err.Message)
	}

	errStr := err.Error()
	if errStr != "rpc error -32603: test error" {
		t.Errorf("Error.Error() = %s", errStr)
	}
}

// === Test Mock Backend ===

type mockBackend struct{}

func (m *mockBackend) ChainID() *big.Int                    { return big.NewInt(1) }
func (m *mockBackend) GasPrice() *big.Int                   { return big.NewInt(1000000000) }
func (m *mockBackend) MaxPriorityFeePerGas() *big.Int       { return big.NewInt(1000000000) }
func (m *mockBackend) FeeHistory(blockCount uint64, lastBlock BlockNumber, rewardPercentiles []float64) (*FeeHistoryResult, error) {
	return &FeeHistoryResult{}, nil
}
func (m *mockBackend) GetBlockByHash(hash dag.Hash, fullTx bool) (*Block, error) {
	return &Block{Hash: hash}, nil
}
func (m *mockBackend) GetBlockByNumber(number BlockNumber, fullTx bool) (*Block, error) {
	return &Block{Number: HexUint64(number)}, nil
}
func (m *mockBackend) GetBlockNumber() (uint64, error) { return 100, nil }
func (m *mockBackend) GetTransactionByHash(hash dag.Hash) (*Transaction, error) {
	return &Transaction{Hash: hash}, nil
}
func (m *mockBackend) GetTransactionByBlockHashAndIndex(blockHash dag.Hash, index uint64) (*Transaction, error) {
	return nil, nil
}
func (m *mockBackend) GetTransactionByBlockNumberAndIndex(number BlockNumber, index uint64) (*Transaction, error) {
	return nil, nil
}
func (m *mockBackend) GetTransactionReceipt(hash dag.Hash) (*Receipt, error) {
	return &Receipt{TransactionHash: hash}, nil
}
func (m *mockBackend) GetTransactionCount(address dag.Address, number BlockNumber) (uint64, error) {
	return 0, nil
}
func (m *mockBackend) GetBalance(address dag.Address, number BlockNumber) (*big.Int, error) {
	return big.NewInt(1000000000000000000), nil
}
func (m *mockBackend) GetCode(address dag.Address, number BlockNumber) ([]byte, error) { return nil, nil }
func (m *mockBackend) GetStorageAt(address dag.Address, key dag.Hash, number BlockNumber) (dag.Hash, error) {
	return dag.Hash{}, nil
}
func (m *mockBackend) Call(args CallArgs, number BlockNumber) ([]byte, error)  { return nil, nil }
func (m *mockBackend) EstimateGas(args CallArgs, number BlockNumber) (uint64, error) { return 21000, nil }
func (m *mockBackend) SendRawTransaction(data []byte) (dag.Hash, error)        { return dag.Hash{}, nil }
func (m *mockBackend) SendTransaction(args SendTxArgs) (dag.Hash, error)       { return dag.Hash{}, nil }
func (m *mockBackend) GetLogs(query FilterQuery) ([]Log, error)                { return nil, nil }
func (m *mockBackend) Syncing() (*SyncStatus, bool)                            { return nil, false }
func (m *mockBackend) Mining() bool                                            { return false }
func (m *mockBackend) Hashrate() uint64                                        { return 0 }
func (m *mockBackend) Coinbase() dag.Address                                   { return dag.Address{} }
func (m *mockBackend) GetBlockTransactionCountByHash(hash dag.Hash) (uint64, error)     { return 0, nil }
func (m *mockBackend) GetBlockTransactionCountByNumber(number BlockNumber) (uint64, error) { return 0, nil }
func (m *mockBackend) GetUncleCountByBlockHash(hash dag.Hash) (uint64, error)           { return 0, nil }
func (m *mockBackend) GetUncleCountByBlockNumber(number BlockNumber) (uint64, error)    { return 0, nil }

func TestEthAPI(t *testing.T) {
	backend := &mockBackend{}
	api := NewEthAPI(backend)

	ctx := context.Background()

	// Test ChainId
	chainID, err := api.ChainId(ctx)
	if err != nil {
		t.Errorf("ChainId error: %v", err)
	}
	if chainID.ToInt().Cmp(big.NewInt(1)) != 0 {
		t.Errorf("ChainId = %v, want 1", chainID.ToInt())
	}

	// Test BlockNumber
	blockNum, err := api.BlockNumber(ctx)
	if err != nil {
		t.Errorf("BlockNumber error: %v", err)
	}
	if uint64(blockNum) != 100 {
		t.Errorf("BlockNumber = %d, want 100", blockNum)
	}

	// Test GetBalance
	var addr dag.Address
	balance, err := api.GetBalance(ctx, addr, LatestBlockNumber)
	if err != nil {
		t.Errorf("GetBalance error: %v", err)
	}
	expectedBalance := big.NewInt(1000000000000000000)
	if balance.ToInt().Cmp(expectedBalance) != 0 {
		t.Errorf("GetBalance = %v, want %v", balance.ToInt(), expectedBalance)
	}

	// Test EstimateGas
	gas, err := api.EstimateGas(ctx, CallArgs{}, LatestBlockNumber)
	if err != nil {
		t.Errorf("EstimateGas error: %v", err)
	}
	if uint64(gas) != 21000 {
		t.Errorf("EstimateGas = %d, want 21000", gas)
	}
}

func TestNetAPI(t *testing.T) {
	peerCount := func() int { return 5 }
	isListening := func() bool { return true }
	api := NewNetAPI(1, peerCount, isListening)

	ctx := context.Background()

	// Test Version
	version, err := api.Version(ctx)
	if err != nil {
		t.Errorf("Version error: %v", err)
	}
	if version != "1" {
		t.Errorf("Version = %s, want '1'", version)
	}

	// Test PeerCount
	count, err := api.PeerCount(ctx)
	if err != nil {
		t.Errorf("PeerCount error: %v", err)
	}
	if uint64(count) != 5 {
		t.Errorf("PeerCount = %d, want 5", count)
	}

	// Test Listening
	listening, err := api.Listening(ctx)
	if err != nil {
		t.Errorf("Listening error: %v", err)
	}
	if !listening {
		t.Error("Listening = false, want true")
	}
}

func TestWeb3API(t *testing.T) {
	api := NewWeb3API("1.0.0")

	ctx := context.Background()

	// Test ClientVersion
	version, err := api.ClientVersion(ctx)
	if err != nil {
		t.Errorf("ClientVersion error: %v", err)
	}
	if version == "" {
		t.Error("ClientVersion is empty")
	}

	// Test Sha3
	hash, err := api.Sha3(ctx, Hex([]byte("hello")))
	if err != nil {
		t.Errorf("Sha3 error: %v", err)
	}
	if len(hash) != 32 {
		t.Errorf("Sha3 hash length = %d, want 32", len(hash))
	}
}

// === Test Filter Management ===

func TestFilterManagement(t *testing.T) {
	backend := &mockBackend{}
	api := NewEthAPI(backend)

	ctx := context.Background()

	// Create block filter
	id, err := api.NewBlockFilter(ctx)
	if err != nil {
		t.Fatalf("NewBlockFilter error: %v", err)
	}
	if id == "" {
		t.Error("Filter ID is empty")
	}

	// Get changes (should be empty)
	changes, err := api.GetFilterChanges(ctx, id)
	if err != nil {
		t.Errorf("GetFilterChanges error: %v", err)
	}
	if changes == nil {
		t.Error("Changes is nil")
	}

	// Uninstall filter
	removed, err := api.UninstallFilter(ctx, id)
	if err != nil {
		t.Errorf("UninstallFilter error: %v", err)
	}
	if !removed {
		t.Error("Filter was not removed")
	}

	// Uninstalling again should return false
	removed, err = api.UninstallFilter(ctx, id)
	if err != nil {
		t.Errorf("UninstallFilter error: %v", err)
	}
	if removed {
		t.Error("Filter was removed twice")
	}
}

func TestLogFilter(t *testing.T) {
	backend := &mockBackend{}
	api := NewEthAPI(backend)

	ctx := context.Background()

	// Create log filter
	filter := FilterQuery{
		FromBlock: new(BlockNumber),
		ToBlock:   new(BlockNumber),
	}
	*filter.FromBlock = LatestBlockNumber
	*filter.ToBlock = LatestBlockNumber

	id, err := api.NewFilter(ctx, filter)
	if err != nil {
		t.Fatalf("NewFilter error: %v", err)
	}
	if id == "" {
		t.Error("Filter ID is empty")
	}

	// Get filter logs
	logs, err := api.GetFilterLogs(ctx, id)
	if err != nil {
		t.Errorf("GetFilterLogs error: %v", err)
	}
	// Empty is fine, just verify no error
	_ = logs
}

// === Test Notification Marshaling ===

func TestMarshalNotification(t *testing.T) {
	subID := SubscriptionID("0x123")
	result := map[string]interface{}{"number": "0x1"}

	data, err := MarshalNotification(subID, result)
	if err != nil {
		t.Fatalf("MarshalNotification error: %v", err)
	}

	var notif SubscriptionNotification
	if err := json.Unmarshal(data, &notif); err != nil {
		t.Fatalf("Unmarshal notification error: %v", err)
	}

	if notif.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %s, want '2.0'", notif.JSONRPC)
	}
	if notif.Method != "eth_subscription" {
		t.Errorf("Method = %s, want 'eth_subscription'", notif.Method)
	}
	if notif.Params.Subscription != subID {
		t.Errorf("Subscription = %s, want %s", notif.Params.Subscription, subID)
	}
}
