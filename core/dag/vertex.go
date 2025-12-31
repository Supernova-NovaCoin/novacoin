// Package dag provides the core DAG (Directed Acyclic Graph) data structures
// for the NovaCoin blockchain.
package dag

import (
	"fmt"
	"math/big"
	"time"
)

// Hash represents a 32-byte hash (SHA3-256)
type Hash [32]byte

// PublicKey represents a 48-byte BLS public key
type PublicKey [48]byte

// Signature represents a 96-byte BLS signature
type Signature [96]byte

// Address represents a 20-byte account address
type Address [20]byte

// Vertex represents a block in the DAG with all consensus metadata.
// This is the fundamental unit of the NovaCoin blockchain.
type Vertex struct {
	// === Core Identity ===
	Hash      Hash      // SHA3-256 of header
	Height    uint64    // DAG height (max parent height + 1)
	Timestamp time.Time // Block creation time

	// === DAG Structure ===
	Parents []Hash // Parent block hashes (multiple allowed in DAG)
	Tips    []Hash // DAG tips at creation time

	// === Proof of Stake ===
	ValidatorPubKey PublicKey // BLS public key of block producer
	ValidatorIndex  uint32    // Index in validator set
	Stake           uint64    // Validator's stake at block time
	Signature       Signature // BLS signature over header

	// === DAGKnight Adaptive ===
	ObservedLatencyMs uint32  // Network latency at creation (ms)
	NetworkConfidence uint8   // 0-100 confidence score
	AdaptiveK         float32 // K value used for this block

	// === Shoal++ Multi-Anchor ===
	Wave          uint64 // Wave number (increments every round)
	IsAnchor      bool   // true (all validators are anchors in Shoal++)
	AnchorWave    uint64 // Wave when anchored
	ParallelDAGId uint8  // Which parallel DAG (0-3)

	// === Mysticeti 3-Round ===
	Round         uint64 // Consensus round number
	ImplicitVotes []Hash // Blocks this vertex votes for (= Parents)
	CommitRound   uint64 // Round committed (0 = uncommitted)
	CommitDepth   uint8  // Depth when committed (1-3)

	// === GHOSTDAG Coloring ===
	BlueScore      uint64   // Cumulative blue score
	BlueWork       *big.Int // Cumulative proof of stake work
	IsBlue         bool     // Blue set membership
	SelectedParent Hash     // Main chain parent

	// === Finality ===
	FinalityScore uint8     // 0-100 finality confidence
	FinalizedAt   time.Time // When finalized (zero = not finalized)
	FinalityProof []byte    // Aggregated signature proof

	// === MEV Resistance ===
	EncryptedTxRoot Hash   // Root of encrypted transactions
	DecryptionProof []byte // Threshold decryption proof

	// === Transactions ===
	TxRoot      Hash   // Merkle root of transactions
	StateRoot   Hash   // State trie root after execution
	ReceiptRoot Hash   // Receipts trie root
	TxCount     uint32 // Number of transactions

	// === Payload ===
	Transactions [][]byte // Raw transaction bytes
}

// VertexHeader contains fields for hash computation.
// Only these fields are included in the block hash.
type VertexHeader struct {
	Parents         []Hash
	Timestamp       int64
	ValidatorPubKey PublicKey
	TxRoot          Hash
	StateRoot       Hash
	Wave            uint64
	Round           uint64
	AdaptiveK       float32
}

// NewVertex creates a new vertex with the given parameters.
func NewVertex(
	parents []Hash,
	validatorPK PublicKey,
	validatorIndex uint32,
	stake uint64,
) *Vertex {
	return &Vertex{
		Parents:         parents,
		Timestamp:       time.Now(),
		ValidatorPubKey: validatorPK,
		ValidatorIndex:  validatorIndex,
		Stake:           stake,
		IsAnchor:        true, // All validators are anchors in Shoal++
		BlueWork:        big.NewInt(0),
	}
}

// Header returns the VertexHeader for hash computation.
func (v *Vertex) Header() *VertexHeader {
	return &VertexHeader{
		Parents:         v.Parents,
		Timestamp:       v.Timestamp.UnixMilli(),
		ValidatorPubKey: v.ValidatorPubKey,
		TxRoot:          v.TxRoot,
		StateRoot:       v.StateRoot,
		Wave:            v.Wave,
		Round:           v.Round,
		AdaptiveK:       v.AdaptiveK,
	}
}

// IsGenesis returns true if this is the genesis block.
func (v *Vertex) IsGenesis() bool {
	return len(v.Parents) == 0
}

// IsFinalized returns true if the block has been finalized.
func (v *Vertex) IsFinalized() bool {
	return !v.FinalizedAt.IsZero()
}

// IsCommitted returns true if the block has been committed via Mysticeti.
func (v *Vertex) IsCommitted() bool {
	return v.CommitRound > 0
}

// GetAge returns the age of the vertex since creation.
func (v *Vertex) GetAge() time.Duration {
	return time.Since(v.Timestamp)
}

// Clone creates a deep copy of the vertex.
func (v *Vertex) Clone() *Vertex {
	clone := *v

	// Deep copy slices
	clone.Parents = make([]Hash, len(v.Parents))
	copy(clone.Parents, v.Parents)

	clone.Tips = make([]Hash, len(v.Tips))
	copy(clone.Tips, v.Tips)

	clone.ImplicitVotes = make([]Hash, len(v.ImplicitVotes))
	copy(clone.ImplicitVotes, v.ImplicitVotes)

	if v.BlueWork != nil {
		clone.BlueWork = new(big.Int).Set(v.BlueWork)
	}

	if v.FinalityProof != nil {
		clone.FinalityProof = make([]byte, len(v.FinalityProof))
		copy(clone.FinalityProof, v.FinalityProof)
	}

	if v.DecryptionProof != nil {
		clone.DecryptionProof = make([]byte, len(v.DecryptionProof))
		copy(clone.DecryptionProof, v.DecryptionProof)
	}

	clone.Transactions = make([][]byte, len(v.Transactions))
	for i, tx := range v.Transactions {
		clone.Transactions[i] = make([]byte, len(tx))
		copy(clone.Transactions[i], tx)
	}

	return &clone
}

// EmptyHash returns a zero-valued hash.
func EmptyHash() Hash {
	return Hash{}
}

// IsEmpty returns true if the hash is all zeros.
func (h Hash) IsEmpty() bool {
	return h == EmptyHash()
}

// Bytes returns the hash as a byte slice.
func (h Hash) Bytes() []byte {
	return h[:]
}

// String returns a hex string representation of the hash.
func (h Hash) String() string {
	return fmt.Sprintf("%x", h[:])
}

// Short returns a shortened hex string (first 8 chars).
func (h Hash) Short() string {
	return fmt.Sprintf("%x", h[:4])
}
