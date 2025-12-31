package zk

import (
	"math/big"
	"testing"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// === Field Element Tests ===

func TestFieldElementArithmetic(t *testing.T) {
	a := NewFieldElement(100)
	b := NewFieldElement(200)

	// Add
	sum := a.Add(b)
	if sum.Uint64() != 300 {
		t.Errorf("Add: expected 300, got %d", sum.Uint64())
	}

	// Sub
	diff := b.Sub(a)
	if diff.Uint64() != 100 {
		t.Errorf("Sub: expected 100, got %d", diff.Uint64())
	}

	// Mul
	prod := a.Mul(b)
	if prod.Uint64() != 20000 {
		t.Errorf("Mul: expected 20000, got %d", prod.Uint64())
	}

	// Div
	quot, err := prod.Div(a)
	if err != nil {
		t.Errorf("Div error: %v", err)
	}
	if quot.Uint64() != 200 {
		t.Errorf("Div: expected 200, got %d", quot.Uint64())
	}
}

func TestFieldElementModular(t *testing.T) {
	// Test modular reduction
	largeValue := new(big.Int).Set(Modulus)
	largeValue.Add(largeValue, big.NewInt(10))

	fe := NewFieldElementFromBig(largeValue)
	if fe.Uint64() != 10 {
		t.Errorf("Modular reduction: expected 10, got %d", fe.Uint64())
	}
}

func TestFieldElementNeg(t *testing.T) {
	a := NewFieldElement(100)
	neg := a.Neg()

	// a + (-a) should equal 0
	sum := a.Add(neg)
	if !sum.IsZero() {
		t.Errorf("Neg: a + (-a) should be zero, got %s", sum.String())
	}
}

func TestFieldElementInv(t *testing.T) {
	a := NewFieldElement(7)
	inv, err := a.Inv()
	if err != nil {
		t.Fatalf("Inv error: %v", err)
	}

	// a * a^(-1) should equal 1
	prod := a.Mul(inv)
	if !prod.Equal(One) {
		t.Errorf("Inv: a * a^(-1) should be 1, got %s", prod.String())
	}
}

func TestFieldElementPow(t *testing.T) {
	a := NewFieldElement(2)

	// 2^10 = 1024
	pow := a.PowUint64(10)
	if pow.Uint64() != 1024 {
		t.Errorf("Pow: expected 1024, got %d", pow.Uint64())
	}
}

func TestFieldElementSquare(t *testing.T) {
	a := NewFieldElement(7)
	sq := a.Square()
	if sq.Uint64() != 49 {
		t.Errorf("Square: expected 49, got %d", sq.Uint64())
	}
}

func TestFieldElementBytes(t *testing.T) {
	a := NewFieldElement(12345)
	bytes := a.Bytes()

	// Should be 32 bytes
	if len(bytes) != 32 {
		t.Errorf("Bytes: expected 32 bytes, got %d", len(bytes))
	}

	// Convert back
	b := NewFieldElementFromBytes(bytes)
	if !a.Equal(b) {
		t.Errorf("Bytes roundtrip failed")
	}
}

func TestRandomFieldElement(t *testing.T) {
	a, err := RandomFieldElement()
	if err != nil {
		t.Fatalf("RandomFieldElement error: %v", err)
	}

	b, err := RandomFieldElement()
	if err != nil {
		t.Fatalf("RandomFieldElement error: %v", err)
	}

	// Should be different (with overwhelming probability)
	if a.Equal(b) {
		t.Error("Two random elements should differ")
	}
}

// === Polynomial Tests ===

func TestPolynomialEvaluate(t *testing.T) {
	// p(x) = 2 + 3x + 5x^2
	coeffs := []FieldElement{
		NewFieldElement(2),
		NewFieldElement(3),
		NewFieldElement(5),
	}
	p := NewPolynomial(coeffs)

	// p(2) = 2 + 6 + 20 = 28
	result := p.Evaluate(NewFieldElement(2))
	if result.Uint64() != 28 {
		t.Errorf("Evaluate: expected 28, got %d", result.Uint64())
	}
}

func TestPolynomialAdd(t *testing.T) {
	// p1(x) = 1 + 2x
	p1 := NewPolynomial([]FieldElement{NewFieldElement(1), NewFieldElement(2)})
	// p2(x) = 3 + 4x
	p2 := NewPolynomial([]FieldElement{NewFieldElement(3), NewFieldElement(4)})

	// sum = 4 + 6x
	sum := p1.Add(p2)
	if sum.Coeffs[0].Uint64() != 4 || sum.Coeffs[1].Uint64() != 6 {
		t.Errorf("Add: expected [4, 6], got [%d, %d]",
			sum.Coeffs[0].Uint64(), sum.Coeffs[1].Uint64())
	}
}

func TestPolynomialMul(t *testing.T) {
	// p1(x) = 1 + x
	p1 := NewPolynomial([]FieldElement{NewFieldElement(1), NewFieldElement(1)})
	// p2(x) = 1 + x
	p2 := NewPolynomial([]FieldElement{NewFieldElement(1), NewFieldElement(1)})

	// product = (1+x)^2 = 1 + 2x + x^2
	prod := p1.Mul(p2)
	if len(prod.Coeffs) != 3 {
		t.Errorf("Mul: expected 3 coefficients, got %d", len(prod.Coeffs))
	}
	if prod.Coeffs[0].Uint64() != 1 || prod.Coeffs[1].Uint64() != 2 || prod.Coeffs[2].Uint64() != 1 {
		t.Error("Mul: incorrect coefficients")
	}
}

func TestInterpolate(t *testing.T) {
	// Points: (0, 1), (1, 2), (2, 5)
	// Should give p(x) = 1 + x + x^2
	points := []FieldElement{NewFieldElement(0), NewFieldElement(1), NewFieldElement(2)}
	values := []FieldElement{NewFieldElement(1), NewFieldElement(3), NewFieldElement(7)}

	poly, err := Interpolate(points, values)
	if err != nil {
		t.Fatalf("Interpolate error: %v", err)
	}

	// Verify interpolation
	for i, p := range points {
		result := poly.Evaluate(p)
		if !result.Equal(values[i]) {
			t.Errorf("Interpolation failed at point %d", i)
		}
	}
}

// === Merkle Tree Tests ===

func TestMerkleTree(t *testing.T) {
	leaves := []FieldElement{
		NewFieldElement(1),
		NewFieldElement(2),
		NewFieldElement(3),
		NewFieldElement(4),
	}

	tree := NewMerkleTree(leaves)

	if tree.Root.IsZero() {
		t.Error("Merkle root should not be zero")
	}

	// Get proof for leaf 0
	proof := tree.GetProof(0)
	if len(proof) == 0 {
		t.Error("Proof should not be empty")
	}

	// Verify proof
	valid := VerifyProof(tree.Root, leaves[0], 0, proof)
	if !valid {
		t.Error("Merkle proof verification failed")
	}
}

func TestMerkleProofInvalid(t *testing.T) {
	leaves := []FieldElement{
		NewFieldElement(1),
		NewFieldElement(2),
		NewFieldElement(3),
		NewFieldElement(4),
	}

	tree := NewMerkleTree(leaves)
	proof := tree.GetProof(0)

	// Wrong leaf should fail
	wrongLeaf := NewFieldElement(99)
	valid := VerifyProof(tree.Root, wrongLeaf, 0, proof)
	if valid {
		t.Error("Should reject invalid proof")
	}
}

// === Trace Tests ===

func TestTraceGenerator(t *testing.T) {
	tg := NewTraceGenerator(nil)

	// Create simple trace steps
	steps := []RawTraceStep{
		{PC: 0, Opcode: 0x60, GasCost: 3}, // PUSH1
		{PC: 2, Opcode: 0x60, GasCost: 3}, // PUSH1
		{PC: 4, Opcode: 0x01, GasCost: 3}, // ADD
		{PC: 5, Opcode: 0x00, GasCost: 0}, // STOP
	}

	var txHash, preState, postState dag.Hash
	txHash[0] = 1
	preState[0] = 2
	postState[0] = 3

	trace, err := tg.GenerateTrace(txHash, preState, postState, 9, true, steps)
	if err != nil {
		t.Fatalf("GenerateTrace error: %v", err)
	}

	if len(trace.Steps) != 4 {
		t.Errorf("Expected 4 steps, got %d", len(trace.Steps))
	}

	if trace.GasUsed.Uint64() != 9 {
		t.Errorf("Expected gas 9, got %d", trace.GasUsed.Uint64())
	}

	if !trace.Success {
		t.Error("Expected success = true")
	}
}

func TestTraceMatrix(t *testing.T) {
	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{Index: 0, PC: 0, Opcode: 0x60, GasUsed: NewFieldElement(3)},
			{Index: 1, PC: 2, Opcode: 0x01, GasUsed: NewFieldElement(3)},
		},
	}

	matrix := NewTraceMatrix(trace)

	if matrix.NumRows < 2 {
		t.Error("Matrix should have at least 2 rows")
	}

	if matrix.Width != int(NumTraceColumns) {
		t.Errorf("Expected width %d, got %d", NumTraceColumns, matrix.Width)
	}

	// Check PC column
	row0 := matrix.GetRow(0)
	if row0[ColPC].Uint64() != 0 {
		t.Errorf("Expected PC=0 at row 0, got %d", row0[ColPC].Uint64())
	}
}

func TestTraceCommitment(t *testing.T) {
	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{Index: 0, PC: 0, Opcode: 0x60, GasUsed: NewFieldElement(3)},
		},
		PreStateRoot:  NewFieldElement(100),
		PostStateRoot: NewFieldElement(200),
		GasUsed:       NewFieldElement(3),
		Success:       true,
	}

	hash := ComputeTraceHash(trace)
	if hash.IsZero() {
		t.Error("Trace hash should not be zero")
	}
}

// === Prover Tests ===

func TestProver(t *testing.T) {
	prover := NewProver(nil)

	trace := createTestTrace()
	proof, err := prover.Prove(trace)
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	if proof.TraceCommitment.IsZero() {
		t.Error("Trace commitment should not be zero")
	}

	if proof.FRIProof == nil {
		t.Error("FRI proof should not be nil")
	}

	if len(proof.QueryProofs) == 0 {
		t.Error("Should have query proofs")
	}

	if len(proof.PublicInputs) < 4 {
		t.Error("Should have at least 4 public inputs")
	}
}

func TestProofSerialization(t *testing.T) {
	prover := NewProver(nil)
	trace := createTestTrace()
	proof, err := prover.Prove(trace)
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	if proof == nil {
		t.Fatal("Proof is nil")
	}

	// Serialize
	data := proof.Serialize()
	if len(data) == 0 {
		t.Error("Serialized proof should not be empty")
	}

	// Deserialize
	restored, err := DeserializeProof(data)
	if err != nil {
		t.Fatalf("DeserializeProof error: %v", err)
	}

	// Check key fields
	if !proof.TraceCommitment.Equal(restored.TraceCommitment) {
		t.Error("Trace commitment mismatch after deserialization")
	}
}

// === Verifier Tests ===

func TestVerifier(t *testing.T) {
	prover := NewProver(nil)
	verifier := NewVerifier(nil)

	trace := createTestTrace()
	proof, err := prover.Prove(trace)
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	result := verifier.Verify(proof)
	if !result.Valid {
		t.Errorf("Verification failed: %s", result.ErrorMessage)
	}
}

func TestVerifierInvalidProof(t *testing.T) {
	verifier := NewVerifier(nil)

	// Create invalid proof
	proof := &Proof{
		TraceCommitment:      Zero, // Invalid - should be non-zero
		ConstraintCommitment: NewFieldElement(1),
		FRIProof:             &FRIProof{LayerCommitments: []FieldElement{NewFieldElement(1)}},
		QueryProofs:          make([]QueryProof, 30),
		PublicInputs:         []FieldElement{Zero, Zero, Zero, Zero},
		NumSteps:             1,
		NumCols:              1,
	}

	result := verifier.Verify(proof)
	if result.Valid {
		t.Error("Should reject invalid proof")
	}
}

// === Batch Tests ===

func TestBatchProver(t *testing.T) {
	bp := NewBatchProver(nil)

	traces := []*ExecutionTrace{
		createTestTrace(),
		createTestTrace(),
		createTestTrace(),
	}

	var blockHash dag.Hash
	blockHash[0] = 1

	batch, err := bp.ProveBatch(traces, 100, blockHash)
	if err != nil {
		t.Fatalf("ProveBatch error: %v", err)
	}

	if int(batch.NumTransactions) != len(traces) {
		t.Errorf("Expected %d transactions, got %d", len(traces), batch.NumTransactions)
	}

	if batch.BatchCommitment.IsZero() {
		t.Error("Batch commitment should not be zero")
	}

	if len(batch.Proofs) != len(traces) {
		t.Errorf("Expected %d proofs, got %d", len(traces), len(batch.Proofs))
	}
}

func TestBatchVerifier(t *testing.T) {
	bp := NewBatchProver(nil)
	bv := NewBatchVerifier(nil)

	traces := []*ExecutionTrace{
		createTestTrace(),
		createTestTrace(),
	}

	var blockHash dag.Hash
	batch, _ := bp.ProveBatch(traces, 100, blockHash)

	result := bv.VerifyBatch(batch)
	if !result.Valid {
		t.Errorf("Batch verification failed: %v", result.Errors)
	}

	if result.ValidCount != len(traces) {
		t.Errorf("Expected %d valid proofs, got %d", len(traces), result.ValidCount)
	}
}

// === Constraint System Tests ===

func TestConstraintSystem(t *testing.T) {
	cs := NewEVMConstraintSystem()

	if len(cs.Constraints) == 0 {
		t.Error("Constraint system should have constraints")
	}

	// Create a simple trace matrix
	trace := createTestTrace()
	matrix := NewTraceMatrix(trace)

	// Evaluate constraints
	results := cs.Evaluate(0, matrix)
	if len(results) != len(cs.Constraints) {
		t.Errorf("Expected %d results, got %d", len(cs.Constraints), len(results))
	}
}

// === FFT Tests ===

func TestRootsOfUnity(t *testing.T) {
	n := 8
	roots := RootsOfUnity(n)

	if len(roots) != n {
		t.Errorf("Expected %d roots, got %d", n, len(roots))
	}

	// First root should be 1
	if !roots[0].Equal(One) {
		t.Error("First root of unity should be 1")
	}

	// omega^n should equal 1
	omega := roots[1]
	omegaN := omega.PowUint64(uint64(n))
	if !omegaN.Equal(One) {
		t.Error("omega^n should equal 1")
	}
}

func TestFFT(t *testing.T) {
	// Simple FFT test with power of 2 size
	coeffs := []FieldElement{
		NewFieldElement(1),
		NewFieldElement(2),
		NewFieldElement(3),
		NewFieldElement(4),
	}

	result := FFT(coeffs)
	if len(result) != len(coeffs) {
		t.Errorf("FFT output size mismatch: expected %d, got %d", len(coeffs), len(result))
	}
}

// === Public Input Tests ===

func TestPublicInputs(t *testing.T) {
	pi := &PublicInputs{
		PreStateRoot:  dag.Hash{1, 2, 3},
		PostStateRoot: dag.Hash{4, 5, 6},
		GasUsed:       21000,
		Success:       true,
		BlockHeight:   100,
		BlockHash:     dag.Hash{7, 8, 9},
	}

	elements := pi.ToFieldElements()
	if len(elements) < 4 {
		t.Error("Should have at least 4 field elements")
	}
}

func TestExtractPublicInputs(t *testing.T) {
	proof := &Proof{
		PublicInputs: []FieldElement{
			NewFieldElementFromBytes(dag.Hash{1}.Bytes()),
			NewFieldElementFromBytes(dag.Hash{2}.Bytes()),
			NewFieldElement(21000),
			One,
		},
	}

	pi, err := ExtractPublicInputs(proof)
	if err != nil {
		t.Fatalf("ExtractPublicInputs error: %v", err)
	}

	if pi.GasUsed != 21000 {
		t.Errorf("Expected gas 21000, got %d", pi.GasUsed)
	}

	if !pi.Success {
		t.Error("Expected success = true")
	}
}

// === Stats Tests ===

func TestProverStats(t *testing.T) {
	prover := NewProver(nil)

	// Generate a proof
	trace := createTestTrace()
	_, _ = prover.Prove(trace)

	stats := prover.GetStats()
	if stats.ProofsGenerated != 1 {
		t.Errorf("Expected 1 proof generated, got %d", stats.ProofsGenerated)
	}
}

func TestVerifierStats(t *testing.T) {
	prover := NewProver(nil)
	verifier := NewVerifier(nil)

	trace := createTestTrace()
	proof, _ := prover.Prove(trace)
	verifier.Verify(proof)

	stats := verifier.GetStats()
	if stats.ProofsVerified != 1 {
		t.Errorf("Expected 1 proof verified, got %d", stats.ProofsVerified)
	}
}

// === Helper Functions ===

func createTestTrace() *ExecutionTrace {
	return &ExecutionTrace{
		TxHash: dag.Hash{1, 2, 3},
		Steps: []TraceStep{
			{Index: 0, PC: 0, Opcode: 0x60, GasUsed: NewFieldElement(3)}, // PUSH1
			{Index: 1, PC: 2, Opcode: 0x60, GasUsed: NewFieldElement(3)}, // PUSH1
			{Index: 2, PC: 4, Opcode: 0x01, GasUsed: NewFieldElement(3)}, // ADD
			{Index: 3, PC: 5, Opcode: 0x00, GasUsed: NewFieldElement(0)}, // STOP
		},
		PreStateRoot:  NewFieldElement(100),
		PostStateRoot: NewFieldElement(200),
		GasUsed:       NewFieldElement(9),
		Success:       true,
	}
}

// === Benchmark Tests ===

func BenchmarkFieldMul(b *testing.B) {
	a := NewFieldElement(12345)
	c := NewFieldElement(67890)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Mul(c)
	}
}

func BenchmarkProve(b *testing.B) {
	prover := NewProver(nil)
	trace := createTestTrace()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = prover.Prove(trace)
	}
}

func BenchmarkVerify(b *testing.B) {
	prover := NewProver(nil)
	verifier := NewVerifier(nil)
	trace := createTestTrace()
	proof, _ := prover.Prove(trace)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		verifier.Verify(proof)
	}
}
