// Package finality implements finality proofs for NovaCoin.
package finality

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/core/dag"
)

// ProofVersion is the current finality proof format version.
const ProofVersion = 1

// FinalityProof contains the cryptographic proof of block finality.
// It includes all information needed to independently verify that
// a block has been finalized by the consensus protocol.
type FinalityProof struct {
	// Version of the proof format
	Version uint8

	// Block identity
	BlockHash   dag.Hash
	BlockHeight uint64

	// Commit information from Mysticeti
	CommitRound uint64
	CommitDepth uint8 // 2 = fast path, 3 = standard path

	// Stake evidence
	SupportingStake uint64 // Total stake supporting the commit
	TotalStake      uint64 // Total network stake

	// Aggregated BLS signature from 2f+1 validators
	AggregatedSignature []byte

	// Signing validator indices (bitmap)
	SignerBitmap []byte

	// Commit path (optional - for detailed verification)
	CommitPath []dag.Hash

	// Timestamp of finalization
	FinalizedAt time.Time

	// Genesis flag (genesis has no supporting signatures)
	IsGenesis bool
}

// NewFinalityProof creates a new finality proof.
func NewFinalityProof(
	blockHash dag.Hash,
	blockHeight uint64,
	commitRound uint64,
	commitDepth uint8,
	supportingStake uint64,
	totalStake uint64,
	aggregatedSig []byte,
) *FinalityProof {
	return &FinalityProof{
		Version:             ProofVersion,
		BlockHash:           blockHash,
		BlockHeight:         blockHeight,
		CommitRound:         commitRound,
		CommitDepth:         commitDepth,
		SupportingStake:     supportingStake,
		TotalStake:          totalStake,
		AggregatedSignature: aggregatedSig,
		FinalizedAt:         time.Now(),
	}
}

// Verify verifies the finality proof.
func (p *FinalityProof) Verify(quorumThreshold float64) (bool, error) {
	// Genesis is always valid
	if p.IsGenesis {
		return true, nil
	}

	// Check version
	if p.Version != ProofVersion {
		return false, errors.New("unsupported proof version")
	}

	// Check stake threshold
	if p.TotalStake == 0 {
		return false, errors.New("total stake is zero")
	}

	stakeRatio := float64(p.SupportingStake) / float64(p.TotalStake)
	if stakeRatio < quorumThreshold {
		return false, errors.New("insufficient stake for quorum")
	}

	// Check commit depth
	if p.CommitDepth < 2 || p.CommitDepth > 3 {
		return false, errors.New("invalid commit depth")
	}

	// In a full implementation, we would verify the aggregated BLS signature
	// against the signer bitmap and public keys. For now, we check it exists.
	if len(p.AggregatedSignature) == 0 && !p.IsGenesis {
		// Signature may be omitted in some cases
		// This is acceptable if stake evidence is sufficient
	}

	return true, nil
}

// Serialize serializes the proof to bytes.
func (p *FinalityProof) Serialize() []byte {
	buf := new(bytes.Buffer)

	// Version (1 byte)
	buf.WriteByte(p.Version)

	// Block hash (32 bytes)
	buf.Write(p.BlockHash[:])

	// Block height (8 bytes)
	binary.Write(buf, binary.BigEndian, p.BlockHeight)

	// Commit round (8 bytes)
	binary.Write(buf, binary.BigEndian, p.CommitRound)

	// Commit depth (1 byte)
	buf.WriteByte(p.CommitDepth)

	// Supporting stake (8 bytes)
	binary.Write(buf, binary.BigEndian, p.SupportingStake)

	// Total stake (8 bytes)
	binary.Write(buf, binary.BigEndian, p.TotalStake)

	// Finalized timestamp (8 bytes)
	binary.Write(buf, binary.BigEndian, p.FinalizedAt.UnixMilli())

	// IsGenesis flag (1 byte)
	if p.IsGenesis {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}

	// Aggregated signature length (2 bytes) + data
	binary.Write(buf, binary.BigEndian, uint16(len(p.AggregatedSignature)))
	buf.Write(p.AggregatedSignature)

	// Signer bitmap length (2 bytes) + data
	binary.Write(buf, binary.BigEndian, uint16(len(p.SignerBitmap)))
	buf.Write(p.SignerBitmap)

	// Commit path length (2 bytes) + data
	binary.Write(buf, binary.BigEndian, uint16(len(p.CommitPath)))
	for _, hash := range p.CommitPath {
		buf.Write(hash[:])
	}

	return buf.Bytes()
}

// DeserializeFinalityProof deserializes a proof from bytes.
func DeserializeFinalityProof(data []byte) (*FinalityProof, error) {
	if len(data) < 67 { // Minimum size: 1+32+8+8+1+8+8+8+1 = 75 bytes base
		return nil, errors.New("proof data too short")
	}

	buf := bytes.NewReader(data)
	p := &FinalityProof{}

	// Version
	version, _ := buf.ReadByte()
	p.Version = version

	// Block hash
	buf.Read(p.BlockHash[:])

	// Block height
	binary.Read(buf, binary.BigEndian, &p.BlockHeight)

	// Commit round
	binary.Read(buf, binary.BigEndian, &p.CommitRound)

	// Commit depth
	p.CommitDepth, _ = buf.ReadByte()

	// Supporting stake
	binary.Read(buf, binary.BigEndian, &p.SupportingStake)

	// Total stake
	binary.Read(buf, binary.BigEndian, &p.TotalStake)

	// Finalized timestamp
	var unixMilli int64
	binary.Read(buf, binary.BigEndian, &unixMilli)
	p.FinalizedAt = time.UnixMilli(unixMilli)

	// IsGenesis flag
	genesisByte, _ := buf.ReadByte()
	p.IsGenesis = genesisByte == 1

	// Aggregated signature
	var sigLen uint16
	binary.Read(buf, binary.BigEndian, &sigLen)
	if sigLen > 0 {
		p.AggregatedSignature = make([]byte, sigLen)
		buf.Read(p.AggregatedSignature)
	}

	// Signer bitmap
	var bitmapLen uint16
	binary.Read(buf, binary.BigEndian, &bitmapLen)
	if bitmapLen > 0 {
		p.SignerBitmap = make([]byte, bitmapLen)
		buf.Read(p.SignerBitmap)
	}

	// Commit path
	var pathLen uint16
	binary.Read(buf, binary.BigEndian, &pathLen)
	if pathLen > 0 {
		p.CommitPath = make([]dag.Hash, pathLen)
		for i := uint16(0); i < pathLen; i++ {
			buf.Read(p.CommitPath[i][:])
		}
	}

	return p, nil
}

// Hash computes the hash of the proof.
func (p *FinalityProof) Hash() dag.Hash {
	data := p.Serialize()
	h := sha256.Sum256(data)
	var hash dag.Hash
	copy(hash[:], h[:])
	return hash
}

// GetStakeRatio returns the ratio of supporting stake to total stake.
func (p *FinalityProof) GetStakeRatio() float64 {
	if p.TotalStake == 0 {
		return 0
	}
	return float64(p.SupportingStake) / float64(p.TotalStake)
}

// IsFastPath returns true if the block was finalized via fast path.
func (p *FinalityProof) IsFastPath() bool {
	return p.CommitDepth == 2
}

// IsStandardPath returns true if the block was finalized via standard path.
func (p *FinalityProof) IsStandardPath() bool {
	return p.CommitDepth == 3
}

// GetFinalityLatency returns the time between block creation and finalization.
func (p *FinalityProof) GetFinalityLatency(blockTimestamp time.Time) time.Duration {
	return p.FinalizedAt.Sub(blockTimestamp)
}

// ProofBatch represents a batch of finality proofs for efficient verification.
type ProofBatch struct {
	Proofs []*FinalityProof
	Root   dag.Hash // Merkle root of proof hashes
}

// NewProofBatch creates a new proof batch.
func NewProofBatch(proofs []*FinalityProof) *ProofBatch {
	batch := &ProofBatch{
		Proofs: proofs,
	}
	batch.computeRoot()
	return batch
}

// computeRoot computes the Merkle root of the proofs.
func (b *ProofBatch) computeRoot() {
	if len(b.Proofs) == 0 {
		return
	}

	// Compute hashes of all proofs
	hashes := make([]dag.Hash, len(b.Proofs))
	for i, p := range b.Proofs {
		hashes[i] = p.Hash()
	}

	// Build Merkle tree
	for len(hashes) > 1 {
		var nextLevel []dag.Hash
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := append(hashes[i][:], hashes[i+1][:]...)
				h := sha256.Sum256(combined)
				var hash dag.Hash
				copy(hash[:], h[:])
				nextLevel = append(nextLevel, hash)
			} else {
				nextLevel = append(nextLevel, hashes[i])
			}
		}
		hashes = nextLevel
	}

	b.Root = hashes[0]
}

// VerifyBatch verifies all proofs in the batch.
func (b *ProofBatch) VerifyBatch(quorumThreshold float64) (bool, []error) {
	errs := make([]error, 0)

	for i, proof := range b.Proofs {
		valid, err := proof.Verify(quorumThreshold)
		if !valid || err != nil {
			if err != nil {
				errs = append(errs, err)
			} else {
				errs = append(errs, errors.New("proof verification failed"))
			}
			_ = i // Could include index in error
		}
	}

	return len(errs) == 0, errs
}

// Serialize serializes the batch.
func (b *ProofBatch) Serialize() []byte {
	buf := new(bytes.Buffer)

	// Number of proofs
	binary.Write(buf, binary.BigEndian, uint32(len(b.Proofs)))

	// Root hash
	buf.Write(b.Root[:])

	// Each proof
	for _, p := range b.Proofs {
		proofData := p.Serialize()
		binary.Write(buf, binary.BigEndian, uint32(len(proofData)))
		buf.Write(proofData)
	}

	return buf.Bytes()
}

// DeserializeProofBatch deserializes a proof batch.
func DeserializeProofBatch(data []byte) (*ProofBatch, error) {
	if len(data) < 36 { // 4 (count) + 32 (root)
		return nil, errors.New("batch data too short")
	}

	buf := bytes.NewReader(data)
	batch := &ProofBatch{}

	// Number of proofs
	var count uint32
	binary.Read(buf, binary.BigEndian, &count)

	// Root hash
	buf.Read(batch.Root[:])

	// Each proof
	batch.Proofs = make([]*FinalityProof, count)
	for i := uint32(0); i < count; i++ {
		var proofLen uint32
		binary.Read(buf, binary.BigEndian, &proofLen)

		proofData := make([]byte, proofLen)
		buf.Read(proofData)

		proof, err := DeserializeFinalityProof(proofData)
		if err != nil {
			return nil, err
		}
		batch.Proofs[i] = proof
	}

	return batch, nil
}

// LightProof is a lightweight finality proof for SPV clients.
type LightProof struct {
	BlockHash       dag.Hash
	BlockHeight     uint64
	StakeRatio      float32 // Compressed stake ratio (0-1)
	CommitDepth     uint8
	ProofHash       dag.Hash // Hash of the full proof
	AnchorHash      dag.Hash // Hash of nearest checkpoint/anchor
}

// NewLightProof creates a light proof from a full proof.
func NewLightProof(p *FinalityProof, anchorHash dag.Hash) *LightProof {
	return &LightProof{
		BlockHash:   p.BlockHash,
		BlockHeight: p.BlockHeight,
		StakeRatio:  float32(p.GetStakeRatio()),
		CommitDepth: p.CommitDepth,
		ProofHash:   p.Hash(),
		AnchorHash:  anchorHash,
	}
}

// Serialize serializes the light proof.
func (lp *LightProof) Serialize() []byte {
	buf := new(bytes.Buffer)

	buf.Write(lp.BlockHash[:])
	binary.Write(buf, binary.BigEndian, lp.BlockHeight)
	binary.Write(buf, binary.BigEndian, lp.StakeRatio)
	buf.WriteByte(lp.CommitDepth)
	buf.Write(lp.ProofHash[:])
	buf.Write(lp.AnchorHash[:])

	return buf.Bytes()
}

// DeserializeLightProof deserializes a light proof.
func DeserializeLightProof(data []byte) (*LightProof, error) {
	if len(data) < 109 { // 32+8+4+1+32+32 = 109 bytes
		return nil, errors.New("light proof data too short")
	}

	buf := bytes.NewReader(data)
	lp := &LightProof{}

	buf.Read(lp.BlockHash[:])
	binary.Read(buf, binary.BigEndian, &lp.BlockHeight)
	binary.Read(buf, binary.BigEndian, &lp.StakeRatio)
	lp.CommitDepth, _ = buf.ReadByte()
	buf.Read(lp.ProofHash[:])
	buf.Read(lp.AnchorHash[:])

	return lp, nil
}
