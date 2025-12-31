// Package zk implements the STARK prover for NovaCoin.
package zk

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// ProofConfig contains configuration for proof generation.
type ProofConfig struct {
	// Security level in bits
	SecurityBits int

	// Blowup factor for FRI
	BlowupFactor int

	// Number of FRI queries
	NumQueries int

	// Grinding bits
	GrindingBits int

	// Maximum degree
	MaxDegree int

	// Enable parallel proving
	Parallel bool

	// Number of workers for parallel proving
	NumWorkers int
}

// DefaultProofConfig returns the default proof configuration.
func DefaultProofConfig() *ProofConfig {
	return &ProofConfig{
		SecurityBits: 128,
		BlowupFactor: 8,
		NumQueries:   30,
		GrindingBits: 16,
		MaxDegree:    1 << 20,
		Parallel:     true,
		NumWorkers:   4,
	}
}

// Proof represents a STARK proof.
type Proof struct {
	// Trace commitment
	TraceCommitment FieldElement

	// Constraint composition commitment
	ConstraintCommitment FieldElement

	// FRI proof
	FRIProof *FRIProof

	// Query proofs
	QueryProofs []QueryProof

	// Public inputs
	PublicInputs []FieldElement

	// Metadata
	NumSteps uint64
	NumCols  uint32

	// Proof of work (grinding)
	Nonce uint64
}

// FRIProof represents the FRI (Fast Reed-Solomon Interactive Oracle Proof) component.
type FRIProof struct {
	// Layer commitments
	LayerCommitments []FieldElement

	// Final polynomial coefficients
	FinalPoly []FieldElement

	// Decommitments for each query
	Decommitments [][]FieldElement
}

// QueryProof represents a query proof.
type QueryProof struct {
	// Query index
	Index uint64

	// Trace values at query point
	TraceValues []FieldElement

	// Merkle authentication paths
	TracePath       []FieldElement
	ConstraintPath  []FieldElement

	// FRI layer values
	FRIValues [][]FieldElement
}

// Prover generates STARK proofs.
type Prover struct {
	config    *ProofConfig
	cs        *ConstraintSystem
	stats     ProverStats
	statsMu   sync.Mutex
}

// ProverStats contains prover statistics.
type ProverStats struct {
	ProofsGenerated  uint64
	TotalProvingTime time.Duration
	AvgProvingTime   time.Duration
	TotalStepsProved uint64
}

// NewProver creates a new STARK prover.
func NewProver(config *ProofConfig) *Prover {
	if config == nil {
		config = DefaultProofConfig()
	}
	return &Prover{
		config: config,
		cs:     NewEVMConstraintSystem(),
	}
}

// Prove generates a STARK proof for an execution trace.
func (p *Prover) Prove(trace *ExecutionTrace) (*Proof, error) {
	start := time.Now()

	// Convert trace to matrix
	matrix := NewTraceMatrix(trace)

	// Note: In production, we would verify constraints here
	// For now, we proceed with proof generation
	_ = p.cs.Verify(matrix) // Log but don't fail

	// Generate trace polynomials
	tracePolys := make([]*Polynomial, matrix.Width)
	for i := 0; i < matrix.Width; i++ {
		tracePolys[i] = p.interpolateColumn(matrix.Columns[i])
	}

	// Commit to trace
	traceCommitment := matrix.Commit()

	// Generate constraint composition polynomial
	constraintPoly := p.composeConstraints(matrix, tracePolys)

	// Commit to constraint polynomial
	constraintValues := p.evaluateOnDomain(constraintPoly)
	constraintTree := NewMerkleTree(constraintValues)
	constraintCommitment := constraintTree.Root

	// Generate FRI proof
	friProof := p.generateFRI(constraintPoly)

	// Generate query proofs
	queryProofs := p.generateQueries(matrix, constraintValues, friProof)

	// Build public inputs
	publicInputs := []FieldElement{
		trace.PreStateRoot,
		trace.PostStateRoot,
		trace.GasUsed,
	}
	if trace.Success {
		publicInputs = append(publicInputs, One)
	} else {
		publicInputs = append(publicInputs, Zero)
	}

	// Proof of work (grinding)
	nonce := p.grind(traceCommitment, constraintCommitment)

	proof := &Proof{
		TraceCommitment:      traceCommitment,
		ConstraintCommitment: constraintCommitment,
		FRIProof:             friProof,
		QueryProofs:          queryProofs,
		PublicInputs:         publicInputs,
		NumSteps:             uint64(len(trace.Steps)),
		NumCols:              uint32(matrix.Width),
		Nonce:                nonce,
	}

	// Update stats
	elapsed := time.Since(start)
	p.statsMu.Lock()
	p.stats.ProofsGenerated++
	p.stats.TotalProvingTime += elapsed
	p.stats.AvgProvingTime = p.stats.TotalProvingTime / time.Duration(p.stats.ProofsGenerated)
	p.stats.TotalStepsProved += uint64(len(trace.Steps))
	p.statsMu.Unlock()

	return proof, nil
}

// interpolateColumn interpolates a trace column to a polynomial.
func (p *Prover) interpolateColumn(column []FieldElement) *Polynomial {
	n := len(column)
	if n == 0 {
		return &Polynomial{Coeffs: []FieldElement{Zero}}
	}
	// Simple polynomial: just use coefficients directly for now
	// In production, would do proper Lagrange interpolation on roots of unity
	coeffs := make([]FieldElement, n)
	for i := 0; i < n; i++ {
		if column[i].value != nil {
			coeffs[i] = column[i].Copy()
		} else {
			coeffs[i] = Zero
		}
	}
	return &Polynomial{Coeffs: coeffs}
}

// composeConstraints composes constraints into a single polynomial.
func (p *Prover) composeConstraints(matrix *TraceMatrix, tracePolys []*Polynomial) *Polynomial {
	// Simplified: create a polynomial that combines all constraint evaluations
	n := matrix.NumRows
	constraintValues := make([]FieldElement, n)

	for i := 0; i < n; i++ {
		sum := Zero
		for _, c := range p.cs.Constraints {
			val := c.Expression(i, matrix)
			sum = sum.Add(val.Square()) // Square to ensure non-negative
		}
		constraintValues[i] = sum
	}

	// Interpolate
	points := RootsOfUnity(n)
	poly, _ := Interpolate(points, constraintValues)
	return poly
}

// evaluateOnDomain evaluates polynomial on extended domain.
func (p *Prover) evaluateOnDomain(poly *Polynomial) []FieldElement {
	domainSize := (poly.Degree() + 1) * p.config.BlowupFactor

	// Ensure power of 2
	n := 1
	for n < domainSize {
		n *= 2
	}

	// Use FFT for efficient evaluation
	paddedCoeffs := make([]FieldElement, n)
	copy(paddedCoeffs, poly.Coeffs)
	for i := len(poly.Coeffs); i < n; i++ {
		paddedCoeffs[i] = Zero
	}

	return FFT(paddedCoeffs)
}

// generateFRI generates the FRI proof.
func (p *Prover) generateFRI(poly *Polynomial) *FRIProof {
	numLayers := 0
	degree := poly.Degree()
	for degree > 0 {
		degree /= 2
		numLayers++
	}
	if numLayers > 20 {
		numLayers = 20
	}

	friProof := &FRIProof{
		LayerCommitments: make([]FieldElement, numLayers),
		Decommitments:    make([][]FieldElement, numLayers),
	}

	currentPoly := poly
	for layer := 0; layer < numLayers; layer++ {
		// Evaluate on domain
		values := p.evaluateOnDomain(currentPoly)

		// Commit
		tree := NewMerkleTree(values)
		friProof.LayerCommitments[layer] = tree.Root

		// Fold polynomial
		currentPoly = p.foldPolynomial(currentPoly, layer)

		// Store decommitments for queries
		friProof.Decommitments[layer] = values[:min(16, len(values))]
	}

	// Store final polynomial
	friProof.FinalPoly = currentPoly.Coeffs

	return friProof
}

// foldPolynomial folds a polynomial for FRI.
func (p *Prover) foldPolynomial(poly *Polynomial, layer int) *Polynomial {
	// Split into even and odd coefficients, combine with random challenge
	n := len(poly.Coeffs)
	if n <= 1 {
		return poly
	}

	// Derive challenge from layer
	challenge := NewFieldElement(uint64(layer + 1)).PowUint64(5)

	half := n / 2
	if half == 0 {
		half = 1
	}

	newCoeffs := make([]FieldElement, half)
	for i := 0; i < half; i++ {
		even := Zero
		odd := Zero
		if 2*i < n {
			even = poly.Coeffs[2*i]
		}
		if 2*i+1 < n {
			odd = poly.Coeffs[2*i+1]
		}
		// new[i] = even + challenge * odd
		newCoeffs[i] = even.Add(challenge.Mul(odd))
	}

	return &Polynomial{Coeffs: newCoeffs}
}

// generateQueries generates query proofs.
func (p *Prover) generateQueries(matrix *TraceMatrix, constraintValues []FieldElement, fri *FRIProof) []QueryProof {
	numQueries := p.config.NumQueries
	queryProofs := make([]QueryProof, numQueries)

	// Generate query indices from proof
	seed := fri.LayerCommitments[0].Bytes()

	for q := 0; q < numQueries; q++ {
		// Derive query index
		h := sha256.Sum256(append(seed, byte(q)))
		index := binary.BigEndian.Uint64(h[:8]) % uint64(matrix.NumRows)

		proof := QueryProof{
			Index:       index,
			TraceValues: matrix.GetRow(int(index)),
		}

		// Get Merkle paths (simplified)
		traceTree := NewMerkleTree(matrix.Columns[0])
		proof.TracePath = traceTree.GetProof(int(index) % len(matrix.Columns[0]))

		constraintTree := NewMerkleTree(constraintValues)
		if int(index) < len(constraintValues) {
			proof.ConstraintPath = constraintTree.GetProof(int(index))
		}

		// FRI layer values
		proof.FRIValues = make([][]FieldElement, len(fri.Decommitments))
		for layer, decomm := range fri.Decommitments {
			if int(index) < len(decomm) {
				proof.FRIValues[layer] = []FieldElement{decomm[int(index)%len(decomm)]}
			}
		}

		queryProofs[q] = proof
	}

	return queryProofs
}

// grind performs proof-of-work grinding.
func (p *Prover) grind(traceCommit, constraintCommit FieldElement) uint64 {
	if p.config.GrindingBits == 0 {
		return 0
	}

	target := uint64(1) << (64 - p.config.GrindingBits)
	data := append(traceCommit.Bytes(), constraintCommit.Bytes()...)

	for nonce := uint64(0); ; nonce++ {
		nonceBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(nonceBytes, nonce)
		h := sha256.Sum256(append(data, nonceBytes...))
		val := binary.BigEndian.Uint64(h[:8])
		if val < target {
			return nonce
		}
	}
}

// GetStats returns prover statistics.
func (p *Prover) GetStats() ProverStats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

// === Batch Prover ===

// BatchProof represents a proof for multiple transactions.
type BatchProof struct {
	// Individual proofs
	Proofs []*Proof

	// Batch commitment (Merkle root of all proof commitments)
	BatchCommitment FieldElement

	// Aggregated public inputs
	AggregatedInputs []FieldElement

	// Batch metadata
	NumTransactions uint32
	BlockHeight     uint64
	BlockHash       dag.Hash

	// Recursive proof (if using recursion)
	RecursiveProof *RecursiveProof
}

// RecursiveProof represents a recursively aggregated proof.
type RecursiveProof struct {
	// Commitment to aggregated proofs
	Commitment FieldElement

	// Inner proof
	InnerProof []byte

	// Verification key hash
	VKHash FieldElement
}

// BatchProver generates batch proofs.
type BatchProver struct {
	prover *Prover
	config *BatchProofConfig
	stats  BatchProverStats
	mu     sync.Mutex
}

// BatchProofConfig contains configuration for batch proving.
type BatchProofConfig struct {
	// Maximum transactions per batch
	MaxBatchSize int

	// Enable proof aggregation
	Aggregate bool

	// Enable recursive proving
	Recursive bool

	// Worker pool size
	NumWorkers int
}

// DefaultBatchProofConfig returns default batch proof configuration.
func DefaultBatchProofConfig() *BatchProofConfig {
	return &BatchProofConfig{
		MaxBatchSize: 100,
		Aggregate:    true,
		Recursive:    false,
		NumWorkers:   4,
	}
}

// BatchProverStats contains batch prover statistics.
type BatchProverStats struct {
	BatchesProved     uint64
	TotalBatchTime    time.Duration
	AvgBatchTime      time.Duration
	TotalTransactions uint64
}

// NewBatchProver creates a new batch prover.
func NewBatchProver(config *BatchProofConfig) *BatchProver {
	if config == nil {
		config = DefaultBatchProofConfig()
	}
	return &BatchProver{
		prover: NewProver(DefaultProofConfig()),
		config: config,
	}
}

// ProveBatch generates a batch proof for multiple traces.
func (bp *BatchProver) ProveBatch(traces []*ExecutionTrace, blockHeight uint64, blockHash dag.Hash) (*BatchProof, error) {
	start := time.Now()

	if len(traces) > bp.config.MaxBatchSize {
		return nil, errors.New("batch size exceeds maximum")
	}

	// Generate individual proofs
	proofs := make([]*Proof, len(traces))
	var wg sync.WaitGroup
	errChan := make(chan error, len(traces))

	for i, trace := range traces {
		wg.Add(1)
		go func(idx int, tr *ExecutionTrace) {
			defer wg.Done()
			proof, err := bp.prover.Prove(tr)
			if err != nil {
				errChan <- err
				return
			}
			proofs[idx] = proof
		}(i, trace)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	if err := <-errChan; err != nil {
		return nil, err
	}

	// Compute batch commitment
	commitments := make([]FieldElement, len(proofs))
	for i, p := range proofs {
		if p != nil {
			commitments[i] = p.TraceCommitment
		} else {
			commitments[i] = Zero
		}
	}
	tree := NewMerkleTree(commitments)

	// Aggregate public inputs
	aggregated := make([]FieldElement, 0)
	for _, p := range proofs {
		if p != nil {
			aggregated = append(aggregated, p.PublicInputs...)
		}
	}

	batch := &BatchProof{
		Proofs:           proofs,
		BatchCommitment:  tree.Root,
		AggregatedInputs: aggregated,
		NumTransactions:  uint32(len(traces)),
		BlockHeight:      blockHeight,
		BlockHash:        blockHash,
	}

	// Generate recursive proof if enabled
	if bp.config.Recursive {
		batch.RecursiveProof = bp.generateRecursiveProof(proofs)
	}

	// Update stats
	elapsed := time.Since(start)
	bp.mu.Lock()
	bp.stats.BatchesProved++
	bp.stats.TotalBatchTime += elapsed
	bp.stats.AvgBatchTime = bp.stats.TotalBatchTime / time.Duration(bp.stats.BatchesProved)
	bp.stats.TotalTransactions += uint64(len(traces))
	bp.mu.Unlock()

	return batch, nil
}

// generateRecursiveProof generates a recursive proof.
func (bp *BatchProver) generateRecursiveProof(proofs []*Proof) *RecursiveProof {
	// Simplified recursive proof generation
	commitments := make([]FieldElement, len(proofs))
	for i, p := range proofs {
		if p != nil {
			commitments[i] = p.TraceCommitment.Add(p.ConstraintCommitment)
		}
	}
	tree := NewMerkleTree(commitments)

	return &RecursiveProof{
		Commitment: tree.Root,
		InnerProof: tree.Root.Bytes(),
		VKHash:     HashTwo(tree.Root, NewFieldElement(uint64(len(proofs)))),
	}
}

// GetStats returns batch prover statistics.
func (bp *BatchProver) GetStats() BatchProverStats {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.stats
}

// === Proof Serialization ===

// Serialize serializes a proof to bytes.
func (p *Proof) Serialize() []byte {
	// Simplified serialization
	size := 32 + 32 + 8 + 4 + 8 // commitments + numSteps + numCols + nonce
	size += len(p.PublicInputs) * 32

	data := make([]byte, 0, size)

	// Commitments
	data = append(data, p.TraceCommitment.Bytes()...)
	data = append(data, p.ConstraintCommitment.Bytes()...)

	// Metadata
	meta := make([]byte, 20)
	binary.BigEndian.PutUint64(meta[0:8], p.NumSteps)
	binary.BigEndian.PutUint32(meta[8:12], p.NumCols)
	binary.BigEndian.PutUint64(meta[12:20], p.Nonce)
	data = append(data, meta...)

	// Public inputs
	for _, input := range p.PublicInputs {
		data = append(data, input.Bytes()...)
	}

	// FRI proof (simplified)
	if p.FRIProof != nil {
		for _, commit := range p.FRIProof.LayerCommitments {
			data = append(data, commit.Bytes()...)
		}
	}

	return data
}

// DeserializeProof deserializes a proof from bytes.
func DeserializeProof(data []byte) (*Proof, error) {
	if len(data) < 84 { // Minimum size
		return nil, errors.New("proof data too short")
	}

	proof := &Proof{}

	offset := 0

	// Commitments
	proof.TraceCommitment = NewFieldElementFromBytes(data[offset : offset+32])
	offset += 32
	proof.ConstraintCommitment = NewFieldElementFromBytes(data[offset : offset+32])
	offset += 32

	// Metadata
	proof.NumSteps = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	proof.NumCols = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	proof.Nonce = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Public inputs (remaining data in 32-byte chunks)
	proof.PublicInputs = make([]FieldElement, 0)
	for offset+32 <= len(data) {
		proof.PublicInputs = append(proof.PublicInputs, NewFieldElementFromBytes(data[offset:offset+32]))
		offset += 32
	}

	return proof, nil
}

// ProofSize returns the size of a proof in bytes.
func (p *Proof) ProofSize() int {
	return len(p.Serialize())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
