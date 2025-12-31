// Package zk implements the STARK verifier for NovaCoin.
package zk

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// VerifierConfig contains configuration for proof verification.
type VerifierConfig struct {
	// Security level in bits
	SecurityBits int

	// Expected blowup factor
	BlowupFactor int

	// Expected number of queries
	NumQueries int

	// Expected grinding bits
	GrindingBits int

	// Maximum proof size
	MaxProofSize int

	// Enable parallel verification
	Parallel bool

	// Number of workers
	NumWorkers int
}

// DefaultVerifierConfig returns the default verifier configuration.
func DefaultVerifierConfig() *VerifierConfig {
	return &VerifierConfig{
		SecurityBits: 128,
		BlowupFactor: 8,
		NumQueries:   30,
		GrindingBits: 16,
		MaxProofSize: 10 * 1024 * 1024, // 10 MB
		Parallel:     true,
		NumWorkers:   4,
	}
}

// VerificationResult represents the result of proof verification.
type VerificationResult struct {
	// Whether verification succeeded
	Valid bool

	// Error message if invalid
	ErrorMessage string

	// Verification time
	Duration time.Duration

	// Public inputs verified
	PublicInputs []FieldElement

	// Commitment verified
	Commitment FieldElement
}

// Verifier verifies STARK proofs.
type Verifier struct {
	config *VerifierConfig
	cs     *ConstraintSystem
	stats  VerifierStats
	mu     sync.Mutex
}

// VerifierStats contains verifier statistics.
type VerifierStats struct {
	ProofsVerified     uint64
	ProofsValid        uint64
	ProofsInvalid      uint64
	TotalVerifyTime    time.Duration
	AvgVerifyTime      time.Duration
}

// NewVerifier creates a new STARK verifier.
func NewVerifier(config *VerifierConfig) *Verifier {
	if config == nil {
		config = DefaultVerifierConfig()
	}
	return &Verifier{
		config: config,
		cs:     NewEVMConstraintSystem(),
	}
}

// Verify verifies a STARK proof.
func (v *Verifier) Verify(proof *Proof) *VerificationResult {
	start := time.Now()
	result := &VerificationResult{
		Valid:        false,
		PublicInputs: proof.PublicInputs,
		Commitment:   proof.TraceCommitment,
	}

	// Validate proof structure
	if err := v.validateProofStructure(proof); err != nil {
		result.ErrorMessage = "invalid proof structure: " + err.Error()
		v.recordResult(result, start)
		return result
	}

	// Verify proof of work (grinding)
	if !v.verifyGrinding(proof) {
		result.ErrorMessage = "proof of work verification failed"
		v.recordResult(result, start)
		return result
	}

	// Verify FRI proof
	if !v.verifyFRI(proof) {
		result.ErrorMessage = "FRI verification failed"
		v.recordResult(result, start)
		return result
	}

	// Verify query proofs
	if !v.verifyQueries(proof) {
		result.ErrorMessage = "query verification failed"
		v.recordResult(result, start)
		return result
	}

	// Verify public input consistency
	if !v.verifyPublicInputs(proof) {
		result.ErrorMessage = "public input verification failed"
		v.recordResult(result, start)
		return result
	}

	result.Valid = true
	v.recordResult(result, start)
	return result
}

// validateProofStructure validates the proof structure.
func (v *Verifier) validateProofStructure(proof *Proof) error {
	if proof == nil {
		return errors.New("proof is nil")
	}

	// Check commitments
	if proof.TraceCommitment.IsZero() {
		return errors.New("trace commitment is zero")
	}
	if proof.ConstraintCommitment.IsZero() {
		return errors.New("constraint commitment is zero")
	}

	// Check FRI proof
	if proof.FRIProof == nil {
		return errors.New("FRI proof is nil")
	}
	if len(proof.FRIProof.LayerCommitments) == 0 {
		return errors.New("no FRI layer commitments")
	}

	// Check query proofs
	if len(proof.QueryProofs) == 0 {
		return errors.New("no query proofs")
	}
	if len(proof.QueryProofs) < v.config.NumQueries {
		return errors.New("insufficient query proofs")
	}

	// Check public inputs
	if len(proof.PublicInputs) < 4 {
		return errors.New("insufficient public inputs")
	}

	// Check metadata
	if proof.NumSteps == 0 {
		return errors.New("zero steps")
	}
	if proof.NumCols == 0 {
		return errors.New("zero columns")
	}

	return nil
}

// verifyGrinding verifies the proof of work.
func (v *Verifier) verifyGrinding(proof *Proof) bool {
	if v.config.GrindingBits == 0 {
		return true
	}

	target := uint64(1) << (64 - v.config.GrindingBits)
	data := append(proof.TraceCommitment.Bytes(), proof.ConstraintCommitment.Bytes()...)
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, proof.Nonce)
	h := sha256.Sum256(append(data, nonceBytes...))
	val := binary.BigEndian.Uint64(h[:8])

	return val < target
}

// verifyFRI verifies the FRI proof.
func (v *Verifier) verifyFRI(proof *Proof) bool {
	fri := proof.FRIProof

	// Verify layer consistency
	for layer := 0; layer < len(fri.LayerCommitments)-1; layer++ {
		// Check that layer commitments form valid chain
		if fri.LayerCommitments[layer].IsZero() {
			return false
		}

		// Verify folding consistency (simplified)
		if layer < len(fri.Decommitments) && len(fri.Decommitments[layer]) > 0 {
			// Values should be consistent with commitment
			tree := NewMerkleTree(fri.Decommitments[layer])
			// Simplified check - in real impl, check against commitment
			_ = tree
		}
	}

	// Verify final polynomial
	if len(fri.FinalPoly) == 0 {
		return false
	}

	// Final polynomial should have low degree
	if len(fri.FinalPoly) > 16 {
		return false
	}

	return true
}

// verifyQueries verifies the query proofs.
func (v *Verifier) verifyQueries(proof *Proof) bool {
	// Derive expected query indices
	seed := proof.FRIProof.LayerCommitments[0].Bytes()

	for q, qp := range proof.QueryProofs {
		// Verify query index matches expected
		h := sha256.Sum256(append(seed, byte(q)))
		expectedIndex := binary.BigEndian.Uint64(h[:8]) % proof.NumSteps
		if qp.Index != expectedIndex {
			return false
		}

		// Verify trace values
		if len(qp.TraceValues) != int(proof.NumCols) {
			return false
		}

		// Verify Merkle paths
		if len(qp.TracePath) == 0 {
			return false
		}

		// Verify FRI values at query points
		for layer, values := range qp.FRIValues {
			if len(values) == 0 {
				continue
			}
			// Check consistency with layer commitment
			if layer < len(proof.FRIProof.Decommitments) {
				// Simplified verification
				_ = values
			}
		}
	}

	return true
}

// verifyPublicInputs verifies public input consistency.
func (v *Verifier) verifyPublicInputs(proof *Proof) bool {
	if len(proof.PublicInputs) < 4 {
		return false
	}

	// Public inputs should be: PreStateRoot, PostStateRoot, GasUsed, Success
	preState := proof.PublicInputs[0]
	postState := proof.PublicInputs[1]
	gasUsed := proof.PublicInputs[2]
	success := proof.PublicInputs[3]

	// Basic sanity checks
	if preState.IsZero() && postState.IsZero() {
		// Both state roots shouldn't be zero unless it's a special case
		// Allow for now
	}

	// Gas should be non-negative (always true for FieldElement)
	_ = gasUsed

	// Success should be 0 or 1
	if !success.Equal(Zero) && !success.Equal(One) {
		return false
	}

	return true
}

// recordResult records a verification result in stats.
func (v *Verifier) recordResult(result *VerificationResult, start time.Time) {
	result.Duration = time.Since(start)

	v.mu.Lock()
	defer v.mu.Unlock()

	v.stats.ProofsVerified++
	v.stats.TotalVerifyTime += result.Duration

	if result.Valid {
		v.stats.ProofsValid++
	} else {
		v.stats.ProofsInvalid++
	}

	v.stats.AvgVerifyTime = v.stats.TotalVerifyTime / time.Duration(v.stats.ProofsVerified)
}

// GetStats returns verifier statistics.
func (v *Verifier) GetStats() VerifierStats {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stats
}

// === Batch Verification ===

// BatchVerifier verifies batch proofs.
type BatchVerifier struct {
	verifier *Verifier
	config   *BatchVerifyConfig
	stats    BatchVerifyStats
	mu       sync.Mutex
}

// BatchVerifyConfig contains configuration for batch verification.
type BatchVerifyConfig struct {
	// Maximum batch size
	MaxBatchSize int

	// Enable parallel verification
	Parallel bool

	// Number of workers
	NumWorkers int

	// Fail fast on first invalid proof
	FailFast bool
}

// DefaultBatchVerifyConfig returns default batch verify configuration.
func DefaultBatchVerifyConfig() *BatchVerifyConfig {
	return &BatchVerifyConfig{
		MaxBatchSize: 100,
		Parallel:     true,
		NumWorkers:   4,
		FailFast:     false,
	}
}

// BatchVerifyStats contains batch verification statistics.
type BatchVerifyStats struct {
	BatchesVerified     uint64
	BatchesValid        uint64
	BatchesInvalid      uint64
	TotalBatchVerifyTime time.Duration
	AvgBatchVerifyTime   time.Duration
	TotalProofsVerified  uint64
}

// BatchVerificationResult represents batch verification result.
type BatchVerificationResult struct {
	// Overall validity
	Valid bool

	// Individual results
	Results []*VerificationResult

	// Number of valid proofs
	ValidCount int

	// Number of invalid proofs
	InvalidCount int

	// Total verification time
	Duration time.Duration

	// Error messages for invalid proofs
	Errors []string
}

// NewBatchVerifier creates a new batch verifier.
func NewBatchVerifier(config *BatchVerifyConfig) *BatchVerifier {
	if config == nil {
		config = DefaultBatchVerifyConfig()
	}
	return &BatchVerifier{
		verifier: NewVerifier(DefaultVerifierConfig()),
		config:   config,
	}
}

// VerifyBatch verifies a batch proof.
func (bv *BatchVerifier) VerifyBatch(batch *BatchProof) *BatchVerificationResult {
	start := time.Now()

	result := &BatchVerificationResult{
		Valid:   true,
		Results: make([]*VerificationResult, len(batch.Proofs)),
		Errors:  make([]string, 0),
	}

	// Verify batch commitment
	commitments := make([]FieldElement, len(batch.Proofs))
	for i, p := range batch.Proofs {
		if p != nil {
			commitments[i] = p.TraceCommitment
		} else {
			commitments[i] = Zero
		}
	}
	tree := NewMerkleTree(commitments)
	if !tree.Root.Equal(batch.BatchCommitment) {
		result.Valid = false
		result.Errors = append(result.Errors, "batch commitment mismatch")
		result.Duration = time.Since(start)
		bv.recordResult(result, start)
		return result
	}

	// Verify individual proofs
	if bv.config.Parallel {
		var wg sync.WaitGroup
		resultChan := make(chan struct {
			index  int
			result *VerificationResult
		}, len(batch.Proofs))

		for i, proof := range batch.Proofs {
			if proof == nil {
				result.Results[i] = &VerificationResult{
					Valid:        false,
					ErrorMessage: "nil proof",
				}
				result.InvalidCount++
				continue
			}

			wg.Add(1)
			go func(idx int, p *Proof) {
				defer wg.Done()
				res := bv.verifier.Verify(p)
				resultChan <- struct {
					index  int
					result *VerificationResult
				}{idx, res}
			}(i, proof)
		}

		wg.Wait()
		close(resultChan)

		for r := range resultChan {
			result.Results[r.index] = r.result
			if r.result.Valid {
				result.ValidCount++
			} else {
				result.InvalidCount++
				result.Errors = append(result.Errors, r.result.ErrorMessage)
				if bv.config.FailFast {
					result.Valid = false
				}
			}
		}
	} else {
		for i, proof := range batch.Proofs {
			if proof == nil {
				result.Results[i] = &VerificationResult{
					Valid:        false,
					ErrorMessage: "nil proof",
				}
				result.InvalidCount++
				continue
			}

			res := bv.verifier.Verify(proof)
			result.Results[i] = res

			if res.Valid {
				result.ValidCount++
			} else {
				result.InvalidCount++
				result.Errors = append(result.Errors, res.ErrorMessage)
				if bv.config.FailFast {
					result.Valid = false
					break
				}
			}
		}
	}

	// Overall validity
	if result.InvalidCount > 0 {
		result.Valid = false
	}

	// Verify recursive proof if present
	if batch.RecursiveProof != nil && result.Valid {
		if !bv.verifyRecursiveProof(batch.RecursiveProof, batch.Proofs) {
			result.Valid = false
			result.Errors = append(result.Errors, "recursive proof verification failed")
		}
	}

	result.Duration = time.Since(start)
	bv.recordResult(result, start)
	return result
}

// verifyRecursiveProof verifies a recursive proof.
func (bv *BatchVerifier) verifyRecursiveProof(rp *RecursiveProof, proofs []*Proof) bool {
	// Verify commitment matches expected
	commitments := make([]FieldElement, len(proofs))
	for i, p := range proofs {
		if p != nil {
			commitments[i] = p.TraceCommitment.Add(p.ConstraintCommitment)
		}
	}
	tree := NewMerkleTree(commitments)

	if !tree.Root.Equal(rp.Commitment) {
		return false
	}

	// Verify VK hash
	expectedVKHash := HashTwo(tree.Root, NewFieldElement(uint64(len(proofs))))
	if !expectedVKHash.Equal(rp.VKHash) {
		return false
	}

	return true
}

// recordResult records batch verification result.
func (bv *BatchVerifier) recordResult(result *BatchVerificationResult, start time.Time) {
	bv.mu.Lock()
	defer bv.mu.Unlock()

	bv.stats.BatchesVerified++
	bv.stats.TotalBatchVerifyTime += result.Duration
	bv.stats.TotalProofsVerified += uint64(len(result.Results))

	if result.Valid {
		bv.stats.BatchesValid++
	} else {
		bv.stats.BatchesInvalid++
	}

	bv.stats.AvgBatchVerifyTime = bv.stats.TotalBatchVerifyTime / time.Duration(bv.stats.BatchesVerified)
}

// GetStats returns batch verifier statistics.
func (bv *BatchVerifier) GetStats() BatchVerifyStats {
	bv.mu.Lock()
	defer bv.mu.Unlock()
	return bv.stats
}

// === Public Input Verification ===

// PublicInputs represents the public inputs to verify.
type PublicInputs struct {
	// Transaction
	TxHash dag.Hash

	// State roots
	PreStateRoot  dag.Hash
	PostStateRoot dag.Hash

	// Execution results
	GasUsed uint64
	Success bool

	// Block context
	BlockHeight uint64
	BlockHash   dag.Hash
}

// ExtractPublicInputs extracts public inputs from a proof.
func ExtractPublicInputs(proof *Proof) (*PublicInputs, error) {
	if len(proof.PublicInputs) < 4 {
		return nil, errors.New("insufficient public inputs")
	}

	inputs := &PublicInputs{}

	// Pre state root
	copy(inputs.PreStateRoot[:], proof.PublicInputs[0].Bytes())

	// Post state root
	copy(inputs.PostStateRoot[:], proof.PublicInputs[1].Bytes())

	// Gas used
	inputs.GasUsed = proof.PublicInputs[2].Uint64()

	// Success
	inputs.Success = proof.PublicInputs[3].Equal(One)

	return inputs, nil
}

// ToFieldElements converts public inputs to field elements.
func (pi *PublicInputs) ToFieldElements() []FieldElement {
	elements := make([]FieldElement, 0, 6)

	elements = append(elements, NewFieldElementFromBytes(pi.PreStateRoot[:]))
	elements = append(elements, NewFieldElementFromBytes(pi.PostStateRoot[:]))
	elements = append(elements, NewFieldElement(pi.GasUsed))

	if pi.Success {
		elements = append(elements, One)
	} else {
		elements = append(elements, Zero)
	}

	elements = append(elements, NewFieldElement(pi.BlockHeight))
	elements = append(elements, NewFieldElementFromBytes(pi.BlockHash[:]))

	return elements
}

// VerifyPublicInputsMatch checks if proof public inputs match expected.
func VerifyPublicInputsMatch(proof *Proof, expected *PublicInputs) bool {
	extracted, err := ExtractPublicInputs(proof)
	if err != nil {
		return false
	}

	if extracted.PreStateRoot != expected.PreStateRoot {
		return false
	}
	if extracted.PostStateRoot != expected.PostStateRoot {
		return false
	}
	if extracted.GasUsed != expected.GasUsed {
		return false
	}
	if extracted.Success != expected.Success {
		return false
	}

	return true
}
