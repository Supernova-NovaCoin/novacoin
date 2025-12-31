// Package mysticeti implements the Mysticeti 3-round commit consensus protocol.
package mysticeti

import (
	"sync"

	"github.com/novacoin/novacoin/core/dag"
)

// ImplicitVote represents a vote derived from DAG structure.
// In Mysticeti, when block B references block A as a parent,
// B implicitly votes for A with the proposer's stake weight.
type ImplicitVote struct {
	// Voter is the block that casts the vote (child block).
	Voter dag.Hash

	// VoterRound is the round of the voting block.
	VoterRound uint64

	// VoterValidator is the validator who proposed the voting block.
	VoterValidator uint32

	// VoteWeight is the stake weight of the vote.
	VoteWeight uint64

	// Target is the block being voted for (parent block).
	Target dag.Hash

	// TargetRound is the round of the target block.
	TargetRound uint64

	// IsDirectVote is true if the vote is a direct parent reference.
	IsDirectVote bool
}

// VoteTally tracks the accumulated votes for a block.
type VoteTally struct {
	// Block being tallied
	Block dag.Hash
	Round uint64

	// Direct votes (immediate children)
	DirectVotes    []ImplicitVote
	DirectVoteStake uint64

	// Indirect votes (descendants)
	IndirectVotes    []ImplicitVote
	IndirectVoteStake uint64

	// Total vote stake
	TotalVoteStake uint64

	// Unique voters (by validator index)
	UniqueVoters map[uint32]bool

	// Votes by round (for depth analysis)
	VotesByRound map[uint64]uint64 // round -> total stake
}

// NewVoteTally creates a new vote tally for a block.
func NewVoteTally(block dag.Hash, round uint64) *VoteTally {
	return &VoteTally{
		Block:        block,
		Round:        round,
		DirectVotes:  make([]ImplicitVote, 0),
		IndirectVotes: make([]ImplicitVote, 0),
		UniqueVoters: make(map[uint32]bool),
		VotesByRound: make(map[uint64]uint64),
	}
}

// AddVote adds a vote to the tally.
func (vt *VoteTally) AddVote(vote ImplicitVote) {
	if vote.IsDirectVote {
		vt.DirectVotes = append(vt.DirectVotes, vote)
		vt.DirectVoteStake += vote.VoteWeight
	} else {
		vt.IndirectVotes = append(vt.IndirectVotes, vote)
		vt.IndirectVoteStake += vote.VoteWeight
	}

	vt.TotalVoteStake += vote.VoteWeight
	vt.UniqueVoters[vote.VoterValidator] = true
	vt.VotesByRound[vote.VoterRound] += vote.VoteWeight
}

// HasQuorumAtRound returns true if the tally has quorum at a specific round.
func (vt *VoteTally) HasQuorumAtRound(round uint64, totalStake uint64, threshold float64) bool {
	if totalStake == 0 {
		return false
	}
	stake := vt.VotesByRound[round]
	return float64(stake)/float64(totalStake) >= threshold
}

// GetCumulativeStakeToRound returns cumulative vote stake up to a round.
func (vt *VoteTally) GetCumulativeStakeToRound(round uint64) uint64 {
	var cumulative uint64
	for r, stake := range vt.VotesByRound {
		if r <= round {
			cumulative += stake
		}
	}
	return cumulative
}

// VoteTracker tracks implicit votes across the DAG.
type VoteTracker struct {
	config *VoteConfig

	// Vote tallies by block hash
	tallies map[dag.Hash]*VoteTally

	// Votes by voter (to trace vote chains)
	votesByVoter map[dag.Hash][]dag.Hash // voter -> targets

	// DAG interface
	getVertex func(dag.Hash) *dag.Vertex
	getChildren func(dag.Hash) []*dag.Vertex

	// Validator info
	validators map[uint32]uint64 // validator index -> stake
	totalStake uint64

	mu sync.RWMutex
}

// VoteConfig holds configuration for vote tracking.
type VoteConfig struct {
	// MaxVoteDepth is the maximum depth to trace indirect votes.
	MaxVoteDepth int

	// QuorumThreshold is the stake fraction required for quorum.
	QuorumThreshold float64

	// TrackIndirectVotes determines whether to track indirect votes.
	TrackIndirectVotes bool

	// MaxTallies is the maximum number of tallies to keep in memory.
	MaxTallies int
}

// DefaultVoteConfig returns default vote configuration.
func DefaultVoteConfig() *VoteConfig {
	return &VoteConfig{
		MaxVoteDepth:       5,
		QuorumThreshold:    2.0 / 3.0,
		TrackIndirectVotes: true,
		MaxTallies:         100000,
	}
}

// NewVoteTracker creates a new vote tracker.
func NewVoteTracker(
	config *VoteConfig,
	getVertex func(dag.Hash) *dag.Vertex,
	getChildren func(dag.Hash) []*dag.Vertex,
) *VoteTracker {
	if config == nil {
		config = DefaultVoteConfig()
	}

	return &VoteTracker{
		config:       config,
		tallies:      make(map[dag.Hash]*VoteTally),
		votesByVoter: make(map[dag.Hash][]dag.Hash),
		getVertex:    getVertex,
		getChildren:  getChildren,
		validators:   make(map[uint32]uint64),
	}
}

// RegisterValidator registers a validator and their stake.
func (vt *VoteTracker) RegisterValidator(index uint32, stake uint64) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if oldStake, exists := vt.validators[index]; exists {
		vt.totalStake -= oldStake
	}
	vt.validators[index] = stake
	vt.totalStake += stake
}

// ProcessBlock processes a new block and records its implicit votes.
func (vt *VoteTracker) ProcessBlock(hash dag.Hash) error {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	vertex := vt.getVertex(hash)
	if vertex == nil {
		return ErrBlockNotFound
	}

	voterStake := vt.validators[vertex.ValidatorIndex]
	if voterStake == 0 {
		voterStake = vertex.Stake // Fallback to vertex stake
	}

	// Record votes for each parent (direct votes)
	for _, parentHash := range vertex.Parents {
		parent := vt.getVertex(parentHash)
		if parent == nil {
			continue
		}

		vote := ImplicitVote{
			Voter:          hash,
			VoterRound:     vertex.Round,
			VoterValidator: vertex.ValidatorIndex,
			VoteWeight:     voterStake,
			Target:         parentHash,
			TargetRound:    parent.Round,
			IsDirectVote:   true,
		}

		vt.recordVote(vote)

		// Track vote chain
		vt.votesByVoter[hash] = append(vt.votesByVoter[hash], parentHash)

		// Process indirect votes (ancestors of parents)
		if vt.config.TrackIndirectVotes {
			vt.processIndirectVotes(hash, vertex, parentHash, 1)
		}
	}

	// Initialize tally for this block
	if vt.tallies[hash] == nil {
		vt.tallies[hash] = NewVoteTally(hash, vertex.Round)
	}

	return nil
}

// processIndirectVotes recursively processes indirect votes.
func (vt *VoteTracker) processIndirectVotes(
	voter dag.Hash,
	voterVertex *dag.Vertex,
	currentHash dag.Hash,
	depth int,
) {
	if depth >= vt.config.MaxVoteDepth {
		return
	}

	current := vt.getVertex(currentHash)
	if current == nil {
		return
	}

	voterStake := vt.validators[voterVertex.ValidatorIndex]
	if voterStake == 0 {
		voterStake = voterVertex.Stake
	}

	// Vote for each parent of current block
	for _, parentHash := range current.Parents {
		parent := vt.getVertex(parentHash)
		if parent == nil {
			continue
		}

		vote := ImplicitVote{
			Voter:          voter,
			VoterRound:     voterVertex.Round,
			VoterValidator: voterVertex.ValidatorIndex,
			VoteWeight:     voterStake,
			Target:         parentHash,
			TargetRound:    parent.Round,
			IsDirectVote:   false,
		}

		vt.recordVote(vote)

		// Continue recursively
		vt.processIndirectVotes(voter, voterVertex, parentHash, depth+1)
	}
}

// recordVote records a vote in the appropriate tally.
func (vt *VoteTracker) recordVote(vote ImplicitVote) {
	tally := vt.tallies[vote.Target]
	if tally == nil {
		tally = NewVoteTally(vote.Target, vote.TargetRound)
		vt.tallies[vote.Target] = tally
	}

	tally.AddVote(vote)
}

// GetVoteTally returns the vote tally for a block.
func (vt *VoteTracker) GetVoteTally(hash dag.Hash) *VoteTally {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	return vt.tallies[hash]
}

// HasDirectQuorum returns true if a block has direct vote quorum.
func (vt *VoteTracker) HasDirectQuorum(hash dag.Hash) bool {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil || vt.totalStake == 0 {
		return false
	}

	return float64(tally.DirectVoteStake)/float64(vt.totalStake) >= vt.config.QuorumThreshold
}

// HasTotalQuorum returns true if a block has total (direct + indirect) vote quorum.
func (vt *VoteTracker) HasTotalQuorum(hash dag.Hash) bool {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil || vt.totalStake == 0 {
		return false
	}

	return float64(tally.TotalVoteStake)/float64(vt.totalStake) >= vt.config.QuorumThreshold
}

// GetVoteStakeAtDepth returns the vote stake at a specific depth from the block's round.
func (vt *VoteTracker) GetVoteStakeAtDepth(hash dag.Hash, depth int) uint64 {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil {
		return 0
	}

	targetRound := tally.Round + uint64(depth)
	return tally.VotesByRound[targetRound]
}

// CheckQuorumChain verifies if there's a quorum chain from a block for n rounds.
// This is used for the n-round commit rule.
func (vt *VoteTracker) CheckQuorumChain(hash dag.Hash, requiredDepth int) bool {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil || vt.totalStake == 0 {
		return false
	}

	// Check each depth level for quorum
	for depth := 1; depth <= requiredDepth; depth++ {
		round := tally.Round + uint64(depth)
		stake := tally.VotesByRound[round]
		if float64(stake)/float64(vt.totalStake) < vt.config.QuorumThreshold {
			return false
		}
	}

	return true
}

// GetQuorumDepth returns the maximum depth at which quorum is maintained.
func (vt *VoteTracker) GetQuorumDepth(hash dag.Hash) int {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil || vt.totalStake == 0 {
		return 0
	}

	depth := 0
	for d := 1; d <= vt.config.MaxVoteDepth; d++ {
		round := tally.Round + uint64(d)
		stake := tally.VotesByRound[round]
		if float64(stake)/float64(vt.totalStake) >= vt.config.QuorumThreshold {
			depth = d
		} else {
			break
		}
	}

	return depth
}

// ComputeVoteChain computes the vote chain for a block.
// Returns all blocks that vote for the given block (directly or indirectly).
func (vt *VoteTracker) ComputeVoteChain(hash dag.Hash, maxDepth int) []dag.Hash {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	tally := vt.tallies[hash]
	if tally == nil {
		return nil
	}

	chain := make([]dag.Hash, 0)
	seen := make(map[dag.Hash]bool)

	// Add direct voters
	for _, vote := range tally.DirectVotes {
		if !seen[vote.Voter] {
			chain = append(chain, vote.Voter)
			seen[vote.Voter] = true
		}
	}

	// Add indirect voters
	for _, vote := range tally.IndirectVotes {
		if !seen[vote.Voter] {
			chain = append(chain, vote.Voter)
			seen[vote.Voter] = true
		}
	}

	return chain
}

// GetTotalStake returns the total stake in the validator set.
func (vt *VoteTracker) GetTotalStake() uint64 {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	return vt.totalStake
}

// VoteStatistics holds vote tracking statistics.
type VoteStatistics struct {
	TotalTallies       int
	TotalVotes         int
	DirectVotes        int
	IndirectVotes      int
	BlocksWithQuorum   int
	AvgVotesPerBlock   float64
}

// GetStatistics returns vote tracking statistics.
func (vt *VoteTracker) GetStatistics() *VoteStatistics {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	stats := &VoteStatistics{
		TotalTallies: len(vt.tallies),
	}

	for _, tally := range vt.tallies {
		stats.DirectVotes += len(tally.DirectVotes)
		stats.IndirectVotes += len(tally.IndirectVotes)
		stats.TotalVotes += len(tally.DirectVotes) + len(tally.IndirectVotes)

		if float64(tally.DirectVoteStake)/float64(vt.totalStake) >= vt.config.QuorumThreshold {
			stats.BlocksWithQuorum++
		}
	}

	if stats.TotalTallies > 0 {
		stats.AvgVotesPerBlock = float64(stats.TotalVotes) / float64(stats.TotalTallies)
	}

	return stats
}

// PruneTallies removes old tallies to limit memory usage.
func (vt *VoteTracker) PruneTallies(keepRounds uint64) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	// Find the minimum round to keep
	var maxRound uint64
	for _, tally := range vt.tallies {
		if tally.Round > maxRound {
			maxRound = tally.Round
		}
	}

	if maxRound < keepRounds {
		return
	}

	minKeepRound := maxRound - keepRounds

	// Remove old tallies
	for hash, tally := range vt.tallies {
		if tally.Round < minKeepRound {
			delete(vt.tallies, hash)
			delete(vt.votesByVoter, hash)
		}
	}
}

// Error types for vote operations.
var (
	ErrBlockNotFound = voteError("block not found")
)

type voteError string

func (e voteError) Error() string {
	return string(e)
}
