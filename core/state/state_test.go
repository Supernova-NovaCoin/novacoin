// Package state implements the blockchain state management layer for NovaCoin.
package state

import (
	"bytes"
	"math/big"
	"testing"
)

// === Account Tests ===

func TestAccount_NewAccount(t *testing.T) {
	acc := NewAccount()

	if acc.Nonce != 0 {
		t.Errorf("Expected nonce 0, got %d", acc.Nonce)
	}
	if acc.Balance == nil || acc.Balance.Sign() != 0 {
		t.Error("Expected zero balance")
	}
	if acc.StorageRoot != EmptyRootHash {
		t.Error("Expected empty storage root")
	}
}

func TestAccount_NewAccountWithBalance(t *testing.T) {
	balance := big.NewInt(1000)
	acc := NewAccountWithBalance(balance)

	if acc.Balance.Cmp(balance) != 0 {
		t.Errorf("Expected balance %v, got %v", balance, acc.Balance)
	}
}

func TestAccount_IsEmpty(t *testing.T) {
	acc := NewAccount()
	if !acc.IsEmpty() {
		t.Error("New account should be empty")
	}

	acc.Nonce = 1
	if acc.IsEmpty() {
		t.Error("Account with nonce should not be empty")
	}

	acc.Nonce = 0
	acc.Balance = big.NewInt(100)
	if acc.IsEmpty() {
		t.Error("Account with balance should not be empty")
	}
}

func TestAccount_Copy(t *testing.T) {
	acc := NewAccount()
	acc.Nonce = 5
	acc.Balance = big.NewInt(1000)
	acc.StorageRoot = Hash{1, 2, 3}

	cpy := acc.Copy()

	if cpy.Nonce != acc.Nonce {
		t.Error("Nonce not copied")
	}
	if cpy.Balance.Cmp(acc.Balance) != 0 {
		t.Error("Balance not copied")
	}
	if cpy.StorageRoot != acc.StorageRoot {
		t.Error("StorageRoot not copied")
	}

	// Ensure deep copy
	cpy.Balance.SetInt64(999)
	if acc.Balance.Int64() == 999 {
		t.Error("Balance should be deep copied")
	}
}

func TestAccount_Serialize(t *testing.T) {
	acc := NewAccount()
	acc.Nonce = 42
	acc.Balance = big.NewInt(123456789)
	acc.StorageRoot = Hash{1, 2, 3, 4, 5}
	acc.CodeHash = Hash{9, 8, 7, 6, 5}

	data := acc.Serialize()
	restored, err := DeserializeAccount(data)
	if err != nil {
		t.Fatalf("Failed to deserialize: %v", err)
	}

	if restored.Nonce != acc.Nonce {
		t.Errorf("Nonce mismatch: %d vs %d", restored.Nonce, acc.Nonce)
	}
	if restored.Balance.Cmp(acc.Balance) != 0 {
		t.Errorf("Balance mismatch: %v vs %v", restored.Balance, acc.Balance)
	}
	if restored.StorageRoot != acc.StorageRoot {
		t.Error("StorageRoot mismatch")
	}
	if restored.CodeHash != acc.CodeHash {
		t.Error("CodeHash mismatch")
	}
}

func TestAddress_String(t *testing.T) {
	addr := Address{0x12, 0x34, 0xab, 0xcd}
	s := addr.String()

	if len(s) != 42 {
		t.Errorf("Expected 42 chars, got %d", len(s))
	}
	if s[:2] != "0x" {
		t.Error("Should start with 0x")
	}
}

func TestAddressFromPublicKey(t *testing.T) {
	pubKey := []byte("test public key data for address derivation")
	addr := AddressFromPublicKey(pubKey)

	// Should be deterministic
	addr2 := AddressFromPublicKey(pubKey)
	if addr != addr2 {
		t.Error("Address derivation should be deterministic")
	}
}

// === Database Tests ===

func TestMemoryDatabase_PutGet(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	key := []byte("test-key")
	value := []byte("test-value")

	if err := db.Put(key, value); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}

	if !bytes.Equal(got, value) {
		t.Errorf("Value mismatch: %v vs %v", got, value)
	}
}

func TestMemoryDatabase_Delete(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	key := []byte("test-key")
	value := []byte("test-value")

	db.Put(key, value)
	db.Delete(key)

	_, err := db.Get(key)
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestMemoryDatabase_Has(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	key := []byte("test-key")

	has, _ := db.Has(key)
	if has {
		t.Error("Should not have key initially")
	}

	db.Put(key, []byte("value"))

	has, _ = db.Has(key)
	if !has {
		t.Error("Should have key after put")
	}
}

func TestMemoryDatabase_Batch(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	batch := db.NewBatch()

	batch.Put([]byte("key1"), []byte("value1"))
	batch.Put([]byte("key2"), []byte("value2"))
	batch.Delete([]byte("key1"))

	// Values shouldn't be visible yet
	_, err := db.Get([]byte("key2"))
	if err != ErrNotFound {
		t.Error("Batch values shouldn't be visible before write")
	}

	batch.Write()

	// Now key2 should be visible, key1 should not
	_, err = db.Get([]byte("key1"))
	if err != ErrNotFound {
		t.Error("key1 should be deleted")
	}

	val, err := db.Get([]byte("key2"))
	if err != nil || !bytes.Equal(val, []byte("value2")) {
		t.Error("key2 should be present")
	}
}

func TestCachingDatabase(t *testing.T) {
	underlying := NewMemoryDatabase()
	defer underlying.Close()

	db := NewCachingDatabase(underlying, 100)

	// Put some values
	db.Put([]byte("key1"), []byte("value1"))
	db.Put([]byte("key2"), []byte("value2"))

	// Should be readable from dirty cache
	val, err := db.Get([]byte("key1"))
	if err != nil || !bytes.Equal(val, []byte("value1")) {
		t.Error("Should read from dirty cache")
	}

	// Flush to underlying
	db.Flush()

	// Should be readable from underlying now
	val, err = underlying.Get([]byte("key1"))
	if err != nil || !bytes.Equal(val, []byte("value1")) {
		t.Error("Should be in underlying after flush")
	}
}

func TestPrefixedDatabase(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	prefixed := NewPrefixedDatabase(db, []byte("prefix:"))

	prefixed.Put([]byte("key"), []byte("value"))

	// Check actual key in underlying db
	val, err := db.Get([]byte("prefix:key"))
	if err != nil || !bytes.Equal(val, []byte("value")) {
		t.Error("Value should be stored with prefix")
	}

	// Get through prefixed db
	val, err = prefixed.Get([]byte("key"))
	if err != nil || !bytes.Equal(val, []byte("value")) {
		t.Error("Should get through prefixed db")
	}
}

// === Trie Tests ===

func TestTrie_PutGet(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	trie := NewEmptyTrie(db)

	// Put some values
	trie.Put([]byte("key1"), []byte("value1"))
	trie.Put([]byte("key2"), []byte("value2"))
	trie.Put([]byte("longer-key"), []byte("longer-value"))

	// Get values
	val, err := trie.Get([]byte("key1"))
	if err != nil || !bytes.Equal(val, []byte("value1")) {
		t.Errorf("key1 mismatch: %v, %v", val, err)
	}

	val, err = trie.Get([]byte("key2"))
	if err != nil || !bytes.Equal(val, []byte("value2")) {
		t.Errorf("key2 mismatch: %v, %v", val, err)
	}

	val, err = trie.Get([]byte("longer-key"))
	if err != nil || !bytes.Equal(val, []byte("longer-value")) {
		t.Errorf("longer-key mismatch: %v, %v", val, err)
	}
}

func TestTrie_Update(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	trie := NewEmptyTrie(db)

	trie.Put([]byte("key"), []byte("value1"))
	trie.Put([]byte("key"), []byte("value2"))

	val, _ := trie.Get([]byte("key"))
	if !bytes.Equal(val, []byte("value2")) {
		t.Error("Value should be updated")
	}
}

func TestTrie_Delete(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	trie := NewEmptyTrie(db)

	trie.Put([]byte("key1"), []byte("value1"))
	trie.Put([]byte("key2"), []byte("value2"))

	trie.Delete([]byte("key1"))

	val, _ := trie.Get([]byte("key1"))
	if val != nil {
		t.Error("Deleted key should return nil")
	}

	val, _ = trie.Get([]byte("key2"))
	if !bytes.Equal(val, []byte("value2")) {
		t.Error("Other keys should be unaffected")
	}
}

func TestTrie_Commit(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	trie := NewEmptyTrie(db)

	trie.Put([]byte("key1"), []byte("value1"))
	trie.Put([]byte("key2"), []byte("value2"))

	root, err := trie.Commit()
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	if root == EmptyRootHash {
		t.Error("Root should not be empty after commit")
	}

	// Create new trie from committed root
	trie2 := NewTrie(db, root)

	val, _ := trie2.Get([]byte("key1"))
	if !bytes.Equal(val, []byte("value1")) {
		t.Error("Should be able to read from committed trie")
	}
}

func TestTrie_RootChanges(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	trie := NewEmptyTrie(db)

	root1 := trie.Root()

	trie.Put([]byte("key"), []byte("value"))
	trie.Commit()
	root2 := trie.Root()

	if root1 == root2 {
		t.Error("Root should change after modification")
	}

	trie.Put([]byte("key"), []byte("different"))
	trie.Commit()
	root3 := trie.Root()

	if root2 == root3 {
		t.Error("Root should change after update")
	}
}

// === Journal Tests ===

func TestJournal_Snapshot(t *testing.T) {
	journal := NewJournal()

	snap1 := journal.Snapshot()
	if snap1 != 0 {
		t.Errorf("First snapshot should be 0, got %d", snap1)
	}

	snap2 := journal.Snapshot()
	if snap2 != 1 {
		t.Errorf("Second snapshot should be 1, got %d", snap2)
	}
}

func TestAccessList(t *testing.T) {
	al := NewAccessList()

	addr := Address{1, 2, 3}
	slot := Hash{4, 5, 6}

	// Initially empty
	if al.ContainsAddress(addr) {
		t.Error("Should not contain address initially")
	}

	// Add address
	isNew := al.AddAddress(addr)
	if !isNew {
		t.Error("Should be new address")
	}
	if !al.ContainsAddress(addr) {
		t.Error("Should contain address after add")
	}

	// Add again
	isNew = al.AddAddress(addr)
	if isNew {
		t.Error("Should not be new on second add")
	}

	// Add slot
	addrNew, slotNew := al.AddSlot(addr, slot)
	if addrNew {
		t.Error("Address should already exist")
	}
	if !slotNew {
		t.Error("Slot should be new")
	}

	// Check contains
	addrOk, slotOk := al.Contains(addr, slot)
	if !addrOk || !slotOk {
		t.Error("Should contain both address and slot")
	}
}

func TestTransientStorage(t *testing.T) {
	ts := NewTransientStorage()

	addr := Address{1, 2, 3}
	key := Hash{4, 5, 6}
	value := Hash{7, 8, 9}

	// Initially empty
	if ts.Get(addr, key) != EmptyHash {
		t.Error("Should return empty hash initially")
	}

	// Set value
	ts.Set(addr, key, value)
	if ts.Get(addr, key) != value {
		t.Error("Should return set value")
	}

	// Copy
	cpy := ts.Copy()
	if cpy.Get(addr, key) != value {
		t.Error("Copy should have same values")
	}

	// Modify original
	ts.Set(addr, key, EmptyHash)
	if cpy.Get(addr, key) != value {
		t.Error("Copy should be independent")
	}
}

// === StateDB Tests ===

func TestStateDB_CreateAccount(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1, 2, 3, 4, 5}
	state.CreateAccount(addr)

	if !state.Exist(addr) {
		t.Error("Account should exist after creation")
	}

	if !state.Empty(addr) {
		t.Error("New account should be empty")
	}
}

func TestStateDB_Balance(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1, 2, 3}

	// Initial balance is zero
	balance := state.GetBalance(addr)
	if balance.Sign() != 0 {
		t.Error("Initial balance should be zero")
	}

	// Set balance
	state.SetBalance(addr, big.NewInt(1000))
	balance = state.GetBalance(addr)
	if balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Balance should be 1000, got %v", balance)
	}

	// Add balance
	state.AddBalance(addr, big.NewInt(500))
	balance = state.GetBalance(addr)
	if balance.Cmp(big.NewInt(1500)) != 0 {
		t.Errorf("Balance should be 1500, got %v", balance)
	}

	// Sub balance
	err := state.SubBalance(addr, big.NewInt(300))
	if err != nil {
		t.Errorf("SubBalance failed: %v", err)
	}
	balance = state.GetBalance(addr)
	if balance.Cmp(big.NewInt(1200)) != 0 {
		t.Errorf("Balance should be 1200, got %v", balance)
	}

	// Sub too much
	err = state.SubBalance(addr, big.NewInt(10000))
	if err != ErrInsufficientBalance {
		t.Errorf("Expected insufficient balance error, got %v", err)
	}
}

func TestStateDB_Transfer(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	from := Address{1}
	to := Address{2}

	state.SetBalance(from, big.NewInt(1000))

	// Transfer
	err := state.Transfer(from, to, big.NewInt(300))
	if err != nil {
		t.Fatalf("Transfer failed: %v", err)
	}

	fromBalance := state.GetBalance(from)
	toBalance := state.GetBalance(to)

	if fromBalance.Cmp(big.NewInt(700)) != 0 {
		t.Errorf("From balance should be 700, got %v", fromBalance)
	}
	if toBalance.Cmp(big.NewInt(300)) != 0 {
		t.Errorf("To balance should be 300, got %v", toBalance)
	}
}

func TestStateDB_Nonce(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}

	// Initial nonce is 0
	nonce := state.GetNonce(addr)
	if nonce != 0 {
		t.Errorf("Initial nonce should be 0, got %d", nonce)
	}

	// Set nonce
	state.SetNonce(addr, 5)
	nonce = state.GetNonce(addr)
	if nonce != 5 {
		t.Errorf("Nonce should be 5, got %d", nonce)
	}

	// Increment nonce
	state.IncrementNonce(addr)
	nonce = state.GetNonce(addr)
	if nonce != 6 {
		t.Errorf("Nonce should be 6, got %d", nonce)
	}
}

func TestStateDB_Code(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // Simple EVM bytecode

	// No code initially
	if state.GetCodeSize(addr) != 0 {
		t.Error("Should have no code initially")
	}

	// Set code
	state.SetCode(addr, code)

	// Verify code
	retrievedCode := state.GetCode(addr)
	if !bytes.Equal(retrievedCode, code) {
		t.Error("Code mismatch")
	}

	if state.GetCodeSize(addr) != len(code) {
		t.Errorf("Code size should be %d", len(code))
	}

	// Account should be a contract
	acc := state.GetAccount(addr)
	if !acc.IsContract() {
		t.Error("Should be a contract")
	}
}

func TestStateDB_Storage(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	key := Hash{2, 3, 4}
	value := Hash{5, 6, 7}

	// Empty initially
	if state.GetState(addr, key) != EmptyHash {
		t.Error("Storage should be empty initially")
	}

	// Set storage
	state.SetState(addr, key, value)

	// Get storage
	retrieved := state.GetState(addr, key)
	if retrieved != value {
		t.Errorf("Storage mismatch: %v vs %v", retrieved, value)
	}

	// Update storage
	newValue := Hash{8, 9, 10}
	state.SetState(addr, key, newValue)
	retrieved = state.GetState(addr, key)
	if retrieved != newValue {
		t.Error("Storage should be updated")
	}
}

func TestStateDB_Snapshot(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	state.SetBalance(addr, big.NewInt(1000))

	// Take snapshot
	snap := state.Snapshot()

	// Modify state
	state.SetBalance(addr, big.NewInt(500))
	state.SetNonce(addr, 5)

	// Verify modifications
	if state.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Error("Balance should be 500")
	}

	// Revert
	state.RevertToSnapshot(snap)

	// Verify revert
	if state.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Balance should be 1000 after revert, got %v", state.GetBalance(addr))
	}
	if state.GetNonce(addr) != 0 {
		t.Errorf("Nonce should be 0 after revert, got %d", state.GetNonce(addr))
	}
}

func TestStateDB_NestedSnapshots(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	state.SetBalance(addr, big.NewInt(100))

	snap1 := state.Snapshot()
	state.AddBalance(addr, big.NewInt(50))

	snap2 := state.Snapshot()
	state.AddBalance(addr, big.NewInt(25))

	// Balance should be 175
	if state.GetBalance(addr).Cmp(big.NewInt(175)) != 0 {
		t.Error("Balance should be 175")
	}

	// Revert to snap2 (should be 150)
	state.RevertToSnapshot(snap2)
	if state.GetBalance(addr).Cmp(big.NewInt(150)) != 0 {
		t.Error("Balance should be 150 after reverting to snap2")
	}

	// Revert to snap1 (should be 100)
	state.RevertToSnapshot(snap1)
	if state.GetBalance(addr).Cmp(big.NewInt(100)) != 0 {
		t.Error("Balance should be 100 after reverting to snap1")
	}
}

func TestStateDB_Commit(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1, 2, 3}
	state.SetBalance(addr, big.NewInt(1000))
	state.SetNonce(addr, 5)
	state.SetCode(addr, []byte{0x01, 0x02, 0x03})
	state.SetState(addr, Hash{1}, Hash{2})

	root, err := state.Commit()
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if root == EmptyRootHash {
		t.Error("Root should not be empty after commit")
	}

	// Create new state from committed root
	state2, _ := NewStateDB(db, root)

	balance := state2.GetBalance(addr)
	if balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("Balance should be 1000, got %v", balance)
	}

	nonce := state2.GetNonce(addr)
	if nonce != 5 {
		t.Errorf("Nonce should be 5, got %d", nonce)
	}
}

func TestStateDB_Suicide(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	state.SetBalance(addr, big.NewInt(1000))

	// Suicide
	success := state.Suicide(addr)
	if !success {
		t.Error("Suicide should succeed")
	}

	// Check suicided
	if !state.HasSuicided(addr) {
		t.Error("Should be marked as suicided")
	}

	// Balance should be zero
	if state.GetBalance(addr).Sign() != 0 {
		t.Error("Balance should be zero after suicide")
	}
}

func TestStateDB_Refund(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	// Initial refund is 0
	if state.GetRefund() != 0 {
		t.Error("Initial refund should be 0")
	}

	// Add refund
	state.AddRefund(100)
	if state.GetRefund() != 100 {
		t.Error("Refund should be 100")
	}

	// Sub refund
	state.SubRefund(30)
	if state.GetRefund() != 70 {
		t.Error("Refund should be 70")
	}
}

func TestStateDB_Copy(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	state.SetBalance(addr, big.NewInt(1000))
	state.SetNonce(addr, 5)

	// Copy state
	stateCopy := state.Copy()

	// Modify original
	state.SetBalance(addr, big.NewInt(500))

	// Copy should be unchanged
	if stateCopy.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Error("Copy should be independent")
	}
}

func TestStateDB_TransientStorage(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	key := Hash{2}
	value := Hash{3}

	// Initially empty
	if state.GetTransientState(addr, key) != EmptyHash {
		t.Error("Transient storage should be empty initially")
	}

	// Set value
	state.SetTransientState(addr, key, value)

	// Get value
	if state.GetTransientState(addr, key) != value {
		t.Error("Should return set value")
	}

	// Commit clears transient storage
	state.Commit()
	if state.GetTransientState(addr, key) != EmptyHash {
		t.Error("Transient storage should be cleared after commit")
	}
}

func TestStateDB_AccessList(t *testing.T) {
	db := NewMemoryDatabase()
	defer db.Close()

	state, _ := NewStateDB(db, EmptyRootHash)

	addr := Address{1}
	slot := Hash{2}

	// Initially not in access list
	if state.AddressInAccessList(addr) {
		t.Error("Should not be in access list initially")
	}

	// Add address
	state.AddAddressToAccessList(addr)
	if !state.AddressInAccessList(addr) {
		t.Error("Should be in access list after add")
	}

	// Add slot
	state.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := state.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Error("Both address and slot should be in access list")
	}
}

// === Nibble Conversion Tests ===

func TestKeyToNibbles(t *testing.T) {
	key := []byte{0x12, 0x34, 0xab}
	nibbles := keyToNibbles(key)

	expected := []byte{0x1, 0x2, 0x3, 0x4, 0xa, 0xb}
	if !bytes.Equal(nibbles, expected) {
		t.Errorf("Nibbles mismatch: %v vs %v", nibbles, expected)
	}
}

func TestNibblesToKey(t *testing.T) {
	nibbles := []byte{0x1, 0x2, 0x3, 0x4, 0xa, 0xb}
	key := nibblesToKey(nibbles)

	expected := []byte{0x12, 0x34, 0xab}
	if !bytes.Equal(key, expected) {
		t.Errorf("Key mismatch: %v vs %v", key, expected)
	}
}

func TestCommonPrefixLength(t *testing.T) {
	tests := []struct {
		a, b     []byte
		expected int
	}{
		{[]byte{1, 2, 3}, []byte{1, 2, 4}, 2},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, 3},
		{[]byte{1, 2, 3}, []byte{4, 5, 6}, 0},
		{[]byte{1, 2}, []byte{1, 2, 3, 4}, 2},
		{[]byte{}, []byte{1, 2}, 0},
	}

	for i, tt := range tests {
		result := commonPrefixLength(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("Test %d: expected %d, got %d", i, tt.expected, result)
		}
	}
}
