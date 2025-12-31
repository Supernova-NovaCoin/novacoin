package mev

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

func testAddress(index uint32) dag.Address {
	var addr dag.Address
	addr[0] = byte(index)
	addr[1] = byte(index >> 8)
	return addr
}

func testHash(index uint32) dag.Hash {
	var h dag.Hash
	h[0] = byte(index)
	h[1] = byte(index >> 8)
	return h
}

// === Threshold Encryption Tests ===

func TestSecretSharer_SplitReconstruct(t *testing.T) {
	config := &ThresholdConfig{
		TotalShares: 10,
		Threshold:   6,
	}
	prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	config.FieldPrime = prime

	sharer := NewSecretSharer(config)

	secret := []byte("this is a 32 byte test secret!!")
	if len(secret) != 32 {
		secret = make([]byte, 32)
		copy(secret, []byte("test secret"))
	}

	shares, err := sharer.Split(secret)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	if len(shares) != 10 {
		t.Errorf("Expected 10 shares, got %d", len(shares))
	}

	// Reconstruct with exactly threshold shares
	reconstructed, err := sharer.Reconstruct(shares[:6])
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	if !bytes.Equal(reconstructed, secret) {
		t.Errorf("Reconstructed secret does not match original")
	}
}

func TestSecretSharer_InsufficientShares(t *testing.T) {
	config := &ThresholdConfig{
		TotalShares: 10,
		Threshold:   6,
	}
	prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	config.FieldPrime = prime

	sharer := NewSecretSharer(config)

	secret := make([]byte, 32)
	copy(secret, []byte("test secret"))

	shares, _ := sharer.Split(secret)

	// Try to reconstruct with fewer than threshold shares
	_, err := sharer.Reconstruct(shares[:5])
	if err != ErrInsufficientShares {
		t.Errorf("Expected ErrInsufficientShares, got %v", err)
	}
}

func TestThresholdKeyManager_DecryptionSession(t *testing.T) {
	config := &ThresholdConfig{
		TotalShares: 5,
		Threshold:   3,
	}
	prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	config.FieldPrime = prime

	tkm := NewThresholdKeyManager(config)

	// Generate key and shares
	key, shares, err := tkm.GenerateEpochKey()
	if err != nil {
		t.Fatalf("GenerateEpochKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Errorf("Expected 32-byte key, got %d bytes", len(key))
	}

	// Start decryption session
	blockHash := testHash(1)
	tkm.StartDecryption(blockHash)

	// Submit shares
	for i := 0; i < 3; i++ {
		complete, decryptedKey, err := tkm.SubmitDecryptionShare(blockHash, shares[i])
		if err != nil {
			t.Fatalf("SubmitDecryptionShare failed: %v", err)
		}

		if i < 2 {
			if complete {
				t.Errorf("Should not be complete after %d shares", i+1)
			}
		} else {
			if !complete {
				t.Errorf("Should be complete after %d shares", i+1)
			}
			if !bytes.Equal(decryptedKey, key) {
				t.Errorf("Decrypted key does not match original")
			}
		}
	}
}

// === Transaction Encryption Tests ===

func TestTransactionEncryptor_EncryptDecrypt(t *testing.T) {
	tkm := NewThresholdKeyManager(nil)
	encryptor := NewTransactionEncryptor(tkm)

	key := make([]byte, 32)
	copy(key, []byte("test encryption key for txs!!"))

	tx := []byte(`{"from": "0x123", "to": "0x456", "value": "1000000000"}`)

	encTx, err := encryptor.EncryptTransaction(tx, key)
	if err != nil {
		t.Fatalf("EncryptTransaction failed: %v", err)
	}

	if bytes.Equal(encTx.Ciphertext, tx) {
		t.Errorf("Ciphertext should not equal plaintext")
	}

	// Decrypt
	decrypted, err := encryptor.DecryptTransaction(encTx, key)
	if err != nil {
		t.Fatalf("DecryptTransaction failed: %v", err)
	}

	if !bytes.Equal(decrypted, tx) {
		t.Errorf("Decrypted transaction does not match original")
	}
}

func TestTransactionEncryptor_WrongKey(t *testing.T) {
	tkm := NewThresholdKeyManager(nil)
	encryptor := NewTransactionEncryptor(tkm)

	key := make([]byte, 32)
	copy(key, []byte("correct key"))

	wrongKey := make([]byte, 32)
	copy(wrongKey, []byte("wrong key"))

	tx := []byte("secret transaction data")

	encTx, _ := encryptor.EncryptTransaction(tx, key)

	// Try to decrypt with wrong key
	_, err := encryptor.DecryptTransaction(encTx, wrongKey)
	if err != ErrDecryptionFailed {
		t.Errorf("Expected ErrDecryptionFailed, got %v", err)
	}
}

// === Fair Ordering Tests ===

func TestFairOrderer_CommitReveal(t *testing.T) {
	config := &OrderingConfig{
		CommitTimeout: 5 * time.Second,
		RevealTimeout: 5 * time.Second,
		MinCommits:    3,
		MinReveals:    3,
	}
	orderer := NewFairOrderer(config)

	// Start round with some transactions
	txHashes := []dag.Hash{testHash(1), testHash(2), testHash(3)}
	_, err := orderer.StartRound(txHashes)
	if err != nil {
		t.Fatalf("StartRound failed: %v", err)
	}

	// Generate seeds and commitments
	type validatorData struct {
		seed       [32]byte
		commitment dag.Hash
	}
	validators := make(map[uint32]*validatorData)

	for i := uint32(1); i <= 5; i++ {
		seed, _ := orderer.GenerateSeed()
		commitment := orderer.CreateCommitment(seed, i)
		validators[i] = &validatorData{seed: seed, commitment: commitment}

		err := orderer.SubmitCommit(i, commitment)
		if err != nil {
			t.Fatalf("SubmitCommit failed for validator %d: %v", i, err)
		}
	}

	if orderer.GetCommitCount() != 5 {
		t.Errorf("Expected 5 commits, got %d", orderer.GetCommitCount())
	}

	// Transition to reveal
	err = orderer.TransitionToReveal()
	if err != nil {
		t.Fatalf("TransitionToReveal failed: %v", err)
	}

	// Submit reveals
	for i := uint32(1); i <= 5; i++ {
		err := orderer.SubmitReveal(i, validators[i].seed)
		if err != nil {
			t.Fatalf("SubmitReveal failed for validator %d: %v", i, err)
		}
	}

	if orderer.GetRevealCount() != 5 {
		t.Errorf("Expected 5 reveals, got %d", orderer.GetRevealCount())
	}

	// Finalize ordering
	perm, err := orderer.FinalizeOrdering()
	if err != nil {
		t.Fatalf("FinalizeOrdering failed: %v", err)
	}

	if len(perm) != 3 {
		t.Errorf("Expected permutation of length 3, got %d", len(perm))
	}

	// Verify permutation contains all indices
	seen := make(map[int]bool)
	for _, idx := range perm {
		if idx < 0 || idx >= 3 {
			t.Errorf("Invalid permutation index: %d", idx)
		}
		seen[idx] = true
	}
	if len(seen) != 3 {
		t.Errorf("Permutation missing indices")
	}
}

func TestFairOrderer_InvalidReveal(t *testing.T) {
	config := &OrderingConfig{
		CommitTimeout: 5 * time.Second,
		RevealTimeout: 5 * time.Second,
		MinCommits:    1,
		MinReveals:    1,
	}
	orderer := NewFairOrderer(config)

	orderer.StartRound([]dag.Hash{testHash(1)})

	// Generate seed and commitment
	seed, _ := orderer.GenerateSeed()
	commitment := orderer.CreateCommitment(seed, 1)
	orderer.SubmitCommit(1, commitment)

	orderer.TransitionToReveal()

	// Try to reveal with wrong seed
	var wrongSeed [32]byte
	copy(wrongSeed[:], []byte("wrong seed"))

	err := orderer.SubmitReveal(1, wrongSeed)
	if err != ErrInvalidReveal {
		t.Errorf("Expected ErrInvalidReveal, got %v", err)
	}
}

func TestBlindPerm_Deterministic(t *testing.T) {
	orderer := NewFairOrderer(nil)

	// Same seed should produce same permutation
	seed := [32]byte{1, 2, 3, 4, 5}
	perm1 := orderer.blindPerm(10, seed)
	perm2 := orderer.blindPerm(10, seed)

	for i := range perm1 {
		if perm1[i] != perm2[i] {
			t.Errorf("Permutations differ at index %d", i)
		}
	}

	// Different seed should produce different permutation (with high probability)
	differentSeed := [32]byte{6, 7, 8, 9, 10}
	perm3 := orderer.blindPerm(10, differentSeed)

	same := true
	for i := range perm1 {
		if perm1[i] != perm3[i] {
			same = false
			break
		}
	}
	if same {
		t.Errorf("Different seeds produced same permutation (unlikely)")
	}
}

// === Encrypted Mempool Tests ===

func TestEncryptedMempool_AddRemove(t *testing.T) {
	mempool := NewEncryptedMempool(nil)
	defer mempool.Stop()

	// Create encrypted tx
	encTx := &EncryptedTransaction{
		Ciphertext: []byte("encrypted data"),
		TxHash:     testHash(1),
	}

	err := mempool.Add(encTx, testAddress(1), 100, 0)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if mempool.Size() != 1 {
		t.Errorf("Expected size 1, got %d", mempool.Size())
	}

	// Get entry
	entry := mempool.Get(testHash(1))
	if entry == nil {
		t.Fatal("Entry not found")
	}
	if entry.GasPrice != 100 {
		t.Errorf("Expected gas price 100, got %d", entry.GasPrice)
	}

	// Remove
	mempool.Remove(testHash(1))
	if mempool.Size() != 0 {
		t.Errorf("Expected size 0 after remove, got %d", mempool.Size())
	}
}

func TestEncryptedMempool_CreateBatch(t *testing.T) {
	config := &MempoolConfig{
		MaxSize:         100,
		MaxBatchSize:    5,
		BatchTimeout:    100 * time.Millisecond,
		EntryTTL:        time.Minute,
		CleanupInterval: time.Minute,
	}
	mempool := NewEncryptedMempool(config)
	defer mempool.Stop()

	// Add 10 transactions with different gas prices
	for i := uint32(0); i < 10; i++ {
		encTx := &EncryptedTransaction{
			Ciphertext: []byte("tx data"),
			TxHash:     testHash(i),
		}
		mempool.Add(encTx, testAddress(i), uint64(100-i), 0) // Higher index = lower gas price
	}

	if mempool.Size() != 10 {
		t.Errorf("Expected size 10, got %d", mempool.Size())
	}

	// Create batch (should get top 5 by gas price)
	batch, hashes := mempool.CreateBatch()

	if len(batch) != 5 {
		t.Errorf("Expected batch size 5, got %d", len(batch))
	}

	// Verify highest gas price transactions are selected
	for _, hash := range hashes {
		entry := mempool.Get(hash)
		if entry == nil {
			t.Errorf("Batch contains unknown transaction")
			continue
		}
		if entry.GasPrice < 95 {
			t.Errorf("Lower gas price tx in batch: %d", entry.GasPrice)
		}
	}
}

func TestEncryptedMempool_Duplicate(t *testing.T) {
	mempool := NewEncryptedMempool(nil)
	defer mempool.Stop()

	encTx := &EncryptedTransaction{
		TxHash: testHash(1),
	}

	err := mempool.Add(encTx, testAddress(1), 100, 0)
	if err != nil {
		t.Fatalf("First add failed: %v", err)
	}

	err = mempool.Add(encTx, testAddress(1), 100, 0)
	if err != ErrTxAlreadyExists {
		t.Errorf("Expected ErrTxAlreadyExists, got %v", err)
	}
}

// === MEV Engine Integration Test ===

func TestMEVEngine_FullFlow(t *testing.T) {
	config := &MEVEngineConfig{
		Threshold: &ThresholdConfig{
			TotalShares: 5,
			Threshold:   3,
		},
		Ordering: &OrderingConfig{
			CommitTimeout: 5 * time.Second,
			RevealTimeout: 5 * time.Second,
			MinCommits:    3,
			MinReveals:    3,
		},
		Mempool:       DefaultMempoolConfig(),
		BatchInterval: 100 * time.Millisecond,
	}

	// Set prime for threshold
	prime, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	config.Threshold.FieldPrime = prime

	engine := NewMEVEngine(config)

	// Start engine
	err := engine.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Submit encrypted transactions
	for i := 0; i < 3; i++ {
		encTx := &EncryptedTransaction{
			Ciphertext: []byte("tx data " + string(rune('0'+i))),
			TxHash:     testHash(uint32(i)),
		}
		err := engine.SubmitEncryptedTransaction(encTx, testAddress(uint32(i)), uint64(100-i), 0)
		if err != nil {
			t.Fatalf("SubmitEncryptedTransaction failed: %v", err)
		}
	}

	// Wait for batch creation
	time.Sleep(200 * time.Millisecond)

	// Manually advance to ordering phase if needed
	if engine.GetState() != EngineOrdering {
		t.Logf("Engine state: %s, mempool size: %d", engine.GetState(), engine.GetMempool().Size())
	}
}

func TestMEVEngine_StartStop(t *testing.T) {
	engine := NewMEVEngine(nil)

	// Start
	err := engine.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if engine.GetState() != EngineCollecting {
		t.Errorf("Expected collecting state, got %s", engine.GetState())
	}

	// Double start should fail
	err = engine.Start()
	if err != ErrEngineAlreadyRunning {
		t.Errorf("Expected ErrEngineAlreadyRunning, got %v", err)
	}

	// Stop
	err = engine.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if engine.GetState() != EngineIdle {
		t.Errorf("Expected idle state, got %s", engine.GetState())
	}

	// Double stop should fail
	err = engine.Stop()
	if err != ErrEngineNotRunning {
		t.Errorf("Expected ErrEngineNotRunning, got %v", err)
	}
}
