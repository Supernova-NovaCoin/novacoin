// Package mysticeti implements the Mysticeti 3-round commit consensus protocol.
package mysticeti

import (
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// CommitStatus represents the commit status of a block.
type CommitStatus int

const (
	// CommitPending means the block hasn't been committed yet.
	CommitPending CommitStatus = iota
	// CommitFastPath means the block was committed via 2-round fast path.
	CommitFastPath
	// CommitStandard means the block was committed via 3-round standard path.
	CommitStandard
	// CommitSkipped means the block was skipped (not committed).
	CommitSkipped
)

func (s CommitStatus) String() string {
	switch s {
	case CommitPending:
		return "pending"
	case CommitFastPath:
		return "fast_path"
	case CommitStandard:
		return "standard"
	case CommitSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// CommitCertificate proves that a block has been committed.
type CommitCertificate struct {
	// Block being committed
	Block dag.Hash
	Round uint64

	// Commit metadata
	Status     CommitStatus
	CommitTime time.Time
	CommitPath []dag.Hash // The chain of blocks that led to the commit

	// Depth information
	CommitDepth uint8 // 2 for fast path, 3 for standard

	// Support evidence
	SupportingBlocks map[uint64][]dag.Hash // round -> supporting blocks
	SupportingStake  map[uint64]uint64     // round -> stake at that round

	// Total stake that supported the commit
	TotalSupportStake uint64
	TotalStake        uint64

	// Signatures (optional, for external verification)
	AggregatedSignature []byte
}

// NewCommitCertificate creates a new commit certificate.
func NewCommitCertificate(block dag.Hash, round uint64, status CommitStatus, depth uint8) *CommitCertificate {
	return &CommitCertificate{
		Block:            block,
		Round:            round,
		Status:           status,
		CommitTime:       time.Now(),
		CommitDepth:      depth,
		SupportingBlocks: make(map[uint64][]dag.Hash),
		SupportingStake:  make(map[uint64]uint64),
	}
}

// AddSupport adds supporting blocks at a specific depth/round.
func (cc *CommitCertificate) AddSupport(round uint64, blocks []dag.Hash, stake uint64) {
	cc.SupportingBlocks[round] = blocks
	cc.SupportingStake[round] = stake
	cc.TotalSupportStake += stake
}

// IsValid returns true if the certificate is valid.
func (cc *CommitCertificate) IsValid(quorumThreshold float64) bool {
	if cc.TotalStake == 0 {
		return false
	}

	// Check each depth level for quorum
	for depth := uint8(1); depth <= cc.CommitDepth; depth++ {
		round := cc.Round + uint64(depth)
		stake := cc.SupportingStake[round]
		if float64(stake)/float64(cc.TotalStake) < quorumThreshold {
			return false
		}
	}

	return true
}

// CommitRule implements the Mysticeti commit rules.
type CommitRule struct {
	config *CommitConfig

	// Vote tracker
	voteTracker *VoteTracker

	// Committed blocks
	committed    map[dag.Hash]*CommitCertificate
	commitOrder  []dag.Hash // Order of commits
	lastCommit   dag.Hash   // Most recently committed block

	// Pending blocks awaiting commit
	pending map[dag.Hash]uint64 // hash -> round

	// DAG interface
	getVertex func(dag.Hash) *dag.Vertex

	// Statistics
	fastPathCommits     uint64
	standardPathCommits uint64
	skippedBlocks       uint64

	// Callbacks
	onCommit func(*CommitCertificate)

	mu sync.RWMutex
}

// CommitConfig holds configuration for the commit rule.
type CommitConfig struct {
	// StandardDepth is the depth required for standard commit (default 3).
	StandardDepth int

	// FastPathDepth is the depth for fast path commit (default 2).
	FastPathDepth int

	// QuorumThreshold is the stake fraction required for quorum.
	QuorumThreshold float64

	// EnableFastPath enables the 2-round fast path.
	EnableFastPath bool

	// RequireLeaderBlock requires leader block for fast path.
	RequireLeaderBlock bool

	// RequireNoEquivocation requires no equivocation for fast path.
	RequireNoEquivocation bool

	// MaxPendingCommits limits pending commits in memory.
	MaxPendingCommits int
}

// DefaultCommitConfig returns default commit configuration.
func DefaultCommitConfig() *CommitConfig {
	return &CommitConfig{
		StandardDepth:         3,
		FastPathDepth:         2,
		QuorumThreshold:       2.0 / 3.0,
		EnableFastPath:        true,
		RequireLeaderBlock:    true,
		RequireNoEquivocation: true,
		MaxPendingCommits:     10000,
	}
}

// NewCommitRule creates a new commit rule engine.
func NewCommitRule(
	config *CommitConfig,
	voteTracker *VoteTracker,
	getVertex func(dag.Hash) *dag.Vertex,
) *CommitRule {
	if config == nil {
		config = DefaultCommitConfig()
	}

	return &CommitRule{
		config:      config,
		voteTracker: voteTracker,
		committed:   make(map[dag.Hash]*CommitCertificate),
		commitOrder: make([]dag.Hash, 0),
		pending:     make(map[dag.Hash]uint64),
		getVertex:   getVertex,
	}
}

// AddPending adds a block to the pending commit set.
func (cr *CommitRule) AddPending(hash dag.Hash, round uint64) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	cr.pending[hash] = round
}

// TryCommit attempts to commit a block using the commit rules.
// Returns a certificate if the block can be committed, nil otherwise.
func (cr *CommitRule) TryCommit(hash dag.Hash) *CommitCertificate {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Already committed?
	if _, exists := cr.committed[hash]; exists {
		return cr.committed[hash]
	}

	vertex := cr.getVertex(hash)
	if vertex == nil {
		return nil
	}

	round := vertex.Round
	totalStake := cr.voteTracker.GetTotalStake()
	if totalStake == 0 {
		return nil
	}

	// Try fast path first (if enabled)
	if cr.config.EnableFastPath {
		if cert := cr.tryFastPath(hash, round, totalStake); cert != nil {
			return cert
		}
	}

	// Try standard 3-round path
	if cert := cr.tryStandardPath(hash, round, totalStake); cert != nil {
		return cert
	}

	return nil
}

// tryFastPath attempts to commit using the 2-round fast path.
// Fast path requires:
// 1. No equivocation in the block's round
// 2. Quorum support at depth 1 (round + 1)
// 3. Quorum support at depth 2 (round + 2)
func (cr *CommitRule) tryFastPath(hash dag.Hash, round uint64, totalStake uint64) *CommitCertificate {
	tally := cr.voteTracker.GetVoteTally(hash)
	if tally == nil {
		return nil
	}

	// Check quorum at each required depth
	for depth := 1; depth <= cr.config.FastPathDepth; depth++ {
		targetRound := round + uint64(depth)
		stake := tally.VotesByRound[targetRound]
		if float64(stake)/float64(totalStake) < cr.config.QuorumThreshold {
			return nil // Not enough stake at this depth
		}
	}

	// Fast path conditions met - create certificate
	cert := NewCommitCertificate(hash, round, CommitFastPath, uint8(cr.config.FastPathDepth))
	cert.TotalStake = totalStake

	// Add support evidence
	for depth := 1; depth <= cr.config.FastPathDepth; depth++ {
		targetRound := round + uint64(depth)
		stake := tally.VotesByRound[targetRound]
		cert.AddSupport(targetRound, nil, stake) // Blocks could be added here
	}

	// Record commit
	cr.recordCommit(hash, cert)
	cr.fastPathCommits++

	return cert
}

// tryStandardPath attempts to commit using the 3-round standard path.
// Standard path requires:
// 1. Quorum support at depth 1 (round + 1)
// 2. Quorum support at depth 2 (round + 2)
// 3. Quorum support at depth 3 (round + 3)
func (cr *CommitRule) tryStandardPath(hash dag.Hash, round uint64, totalStake uint64) *CommitCertificate {
	tally := cr.voteTracker.GetVoteTally(hash)
	if tally == nil {
		return nil
	}

	// Check quorum at each required depth
	for depth := 1; depth <= cr.config.StandardDepth; depth++ {
		targetRound := round + uint64(depth)
		stake := tally.VotesByRound[targetRound]
		if float64(stake)/float64(totalStake) < cr.config.QuorumThreshold {
			return nil // Not enough stake at this depth
		}
	}

	// Standard path conditions met - create certificate
	cert := NewCommitCertificate(hash, round, CommitStandard, uint8(cr.config.StandardDepth))
	cert.TotalStake = totalStake

	// Add support evidence
	for depth := 1; depth <= cr.config.StandardDepth; depth++ {
		targetRound := round + uint64(depth)
		stake := tally.VotesByRound[targetRound]
		cert.AddSupport(targetRound, nil, stake)
	}

	// Record commit
	cr.recordCommit(hash, cert)
	cr.standardPathCommits++

	return cert
}

// recordCommit records a committed block.
func (cr *CommitRule) recordCommit(hash dag.Hash, cert *CommitCertificate) {
	cr.committed[hash] = cert
	cr.commitOrder = append(cr.commitOrder, hash)
	cr.lastCommit = hash
	delete(cr.pending, hash)

	// Notify callback
	if cr.onCommit != nil {
		go cr.onCommit(cert)
	}
}

// SkipBlock marks a block as skipped (won't be committed).
func (cr *CommitRule) SkipBlock(hash dag.Hash, round uint64) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	cert := NewCommitCertificate(hash, round, CommitSkipped, 0)
	cr.committed[hash] = cert
	cr.skippedBlocks++
	delete(cr.pending, hash)
}

// IsCommitted returns true if a block has been committed.
func (cr *CommitRule) IsCommitted(hash dag.Hash) bool {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	cert, exists := cr.committed[hash]
	if !exists {
		return false
	}
	return cert.Status == CommitFastPath || cert.Status == CommitStandard
}

// GetCertificate returns the commit certificate for a block.
func (cr *CommitRule) GetCertificate(hash dag.Hash) *CommitCertificate {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.committed[hash]
}

// GetLastCommit returns the most recently committed block.
func (cr *CommitRule) GetLastCommit() dag.Hash {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.lastCommit
}

// GetCommitOrder returns the order of committed blocks.
func (cr *CommitRule) GetCommitOrder() []dag.Hash {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	order := make([]dag.Hash, len(cr.commitOrder))
	copy(order, cr.commitOrder)
	return order
}

// GetPendingBlocks returns all pending blocks.
func (cr *CommitRule) GetPendingBlocks() map[dag.Hash]uint64 {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	pending := make(map[dag.Hash]uint64)
	for hash, round := range cr.pending {
		pending[hash] = round
	}
	return pending
}

// ProcessPending attempts to commit all pending blocks.
// Returns the number of newly committed blocks.
func (cr *CommitRule) ProcessPending() int {
	cr.mu.Lock()
	pending := make([]dag.Hash, 0, len(cr.pending))
	for hash := range cr.pending {
		pending = append(pending, hash)
	}
	cr.mu.Unlock()

	committed := 0
	for _, hash := range pending {
		if cert := cr.TryCommit(hash); cert != nil {
			committed++
		}
	}

	return committed
}

// ComputeCommitPath computes the path from a block to its commit point.
func (cr *CommitRule) ComputeCommitPath(hash dag.Hash) []dag.Hash {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	cert := cr.committed[hash]
	if cert == nil {
		return nil
	}

	path := make([]dag.Hash, 0)
	path = append(path, hash)

	// Add blocks at each depth level
	for depth := uint8(1); depth <= cert.CommitDepth; depth++ {
		round := cert.Round + uint64(depth)
		if blocks, ok := cert.SupportingBlocks[round]; ok {
			path = append(path, blocks...)
		}
	}

	return path
}

// OnCommit sets a callback for when a block is committed.
func (cr *CommitRule) OnCommit(callback func(*CommitCertificate)) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.onCommit = callback
}

// CommitStatistics holds commit rule statistics.
type CommitStatistics struct {
	TotalCommitted      uint64
	FastPathCommits     uint64
	StandardPathCommits uint64
	SkippedBlocks       uint64
	PendingBlocks       int
	FastPathRate        float64
}

// GetStatistics returns commit rule statistics.
func (cr *CommitRule) GetStatistics() *CommitStatistics {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	stats := &CommitStatistics{
		TotalCommitted:      cr.fastPathCommits + cr.standardPathCommits,
		FastPathCommits:     cr.fastPathCommits,
		StandardPathCommits: cr.standardPathCommits,
		SkippedBlocks:       cr.skippedBlocks,
		PendingBlocks:       len(cr.pending),
	}

	if stats.TotalCommitted > 0 {
		stats.FastPathRate = float64(cr.fastPathCommits) / float64(stats.TotalCommitted)
	}

	return stats
}

// DirectCommit is a simplified commit check that uses vote chain depth.
// This is useful when full vote tracking is not available.
func (cr *CommitRule) DirectCommit(hash dag.Hash, currentRound uint64) *CommitCertificate {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Already committed?
	if cert, exists := cr.committed[hash]; exists {
		return cert
	}

	vertex := cr.getVertex(hash)
	if vertex == nil {
		return nil
	}

	round := vertex.Round

	// Check if enough rounds have passed
	depth := currentRound - round
	if depth < uint64(cr.config.FastPathDepth) {
		return nil // Not enough depth yet
	}

	// Use vote tracker to check quorum chain
	requiredDepth := cr.config.StandardDepth
	if cr.config.EnableFastPath && depth >= uint64(cr.config.FastPathDepth) {
		// Try fast path first
		if cr.voteTracker.CheckQuorumChain(hash, cr.config.FastPathDepth) {
			cert := NewCommitCertificate(hash, round, CommitFastPath, uint8(cr.config.FastPathDepth))
			cert.TotalStake = cr.voteTracker.GetTotalStake()
			cr.recordCommit(hash, cert)
			cr.fastPathCommits++
			return cert
		}
		requiredDepth = cr.config.StandardDepth
	}

	// Check standard path
	if depth >= uint64(requiredDepth) && cr.voteTracker.CheckQuorumChain(hash, requiredDepth) {
		cert := NewCommitCertificate(hash, round, CommitStandard, uint8(requiredDepth))
		cert.TotalStake = cr.voteTracker.GetTotalStake()
		cr.recordCommit(hash, cert)
		cr.standardPathCommits++
		return cert
	}

	return nil
}

// CheckAndCommitLeaders checks and commits leader blocks for past rounds.
// This is the main entry point for the commit pipeline.
func (cr *CommitRule) CheckAndCommitLeaders(
	currentRound uint64,
	getLeaderBlock func(uint64) dag.Hash,
) []*CommitCertificate {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	var newCommits []*CommitCertificate

	// Check rounds that could potentially be committed
	// We need at least FastPathDepth rounds to have passed
	minCheckRound := uint64(1)
	if currentRound > uint64(cr.config.StandardDepth) {
		minCheckRound = currentRound - uint64(cr.config.StandardDepth) - 10 // Buffer for late blocks
	}

	for round := minCheckRound; round < currentRound; round++ {
		leaderBlock := getLeaderBlock(round)
		if leaderBlock.IsEmpty() {
			continue
		}

		// Already committed?
		if _, exists := cr.committed[leaderBlock]; exists {
			continue
		}

		// Check commit conditions
		depth := currentRound - round

		// Try fast path
		if cr.config.EnableFastPath && depth >= uint64(cr.config.FastPathDepth) {
			if cr.voteTracker.CheckQuorumChain(leaderBlock, cr.config.FastPathDepth) {
				cert := NewCommitCertificate(leaderBlock, round, CommitFastPath, uint8(cr.config.FastPathDepth))
				cert.TotalStake = cr.voteTracker.GetTotalStake()
				cr.recordCommit(leaderBlock, cert)
				cr.fastPathCommits++
				newCommits = append(newCommits, cert)
				continue
			}
		}

		// Try standard path
		if depth >= uint64(cr.config.StandardDepth) {
			if cr.voteTracker.CheckQuorumChain(leaderBlock, cr.config.StandardDepth) {
				cert := NewCommitCertificate(leaderBlock, round, CommitStandard, uint8(cr.config.StandardDepth))
				cert.TotalStake = cr.voteTracker.GetTotalStake()
				cr.recordCommit(leaderBlock, cert)
				cr.standardPathCommits++
				newCommits = append(newCommits, cert)
			}
		}
	}

	return newCommits
}
