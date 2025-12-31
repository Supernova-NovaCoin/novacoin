// Package shoal implements the Shoal++ multi-anchor high-throughput consensus layer.
// Shoal++ enables 100k+ TPS through parallel DAGs, wave-based coordination,
// and multi-anchor block production.
package shoal

import (
	"sort"
	"sync"
	"time"

	"github.com/novacoin/novacoin/core/dag"
)

// WaveState represents the state of a wave in the consensus.
type WaveState int

const (
	// WavePending means the wave is waiting to start.
	WavePending WaveState = iota
	// WaveProposing means validators are proposing blocks in this wave.
	WaveProposing
	// WaveVoting means validators are voting on anchor blocks.
	WaveVoting
	// WaveCommitting means the wave is being committed.
	WaveCommitting
	// WaveCommitted means the wave has been fully committed.
	WaveCommitted
	// WaveSkipped means the wave was skipped (timeout or insufficient participation).
	WaveSkipped
)

func (s WaveState) String() string {
	switch s {
	case WavePending:
		return "pending"
	case WaveProposing:
		return "proposing"
	case WaveVoting:
		return "voting"
	case WaveCommitting:
		return "committing"
	case WaveCommitted:
		return "committed"
	case WaveSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// Wave represents a consensus wave/epoch in Shoal++.
// Each wave contains anchor blocks from all participating validators.
type Wave struct {
	// Wave identity
	Number    uint64    // Wave number (monotonically increasing)
	StartTime time.Time // When the wave started
	EndTime   time.Time // When the wave ended (zero if ongoing)

	// State
	State WaveState // Current wave state

	// Anchor blocks in this wave
	Anchors []dag.Hash // Hashes of anchor blocks

	// Stake participation
	TotalStake      uint64            // Total stake in the network
	ParticipatedStake uint64          // Stake that participated in this wave
	StakeByValidator map[uint32]uint64 // Stake per validator that anchored

	// Voting data
	Votes map[dag.Hash]uint64 // Stake-weighted votes for each anchor

	// Commit data
	CommitAnchor  dag.Hash  // The winning anchor that commits this wave
	CommitTime    time.Time // When committed
	CommitStake   uint64    // Stake that committed
}

// NewWave creates a new wave with the given number.
func NewWave(number uint64) *Wave {
	return &Wave{
		Number:           number,
		StartTime:        time.Now(),
		State:            WavePending,
		Anchors:          make([]dag.Hash, 0),
		StakeByValidator: make(map[uint32]uint64),
		Votes:            make(map[dag.Hash]uint64),
	}
}

// AddAnchor adds an anchor block to this wave.
func (w *Wave) AddAnchor(hash dag.Hash, validatorIndex uint32, stake uint64) {
	w.Anchors = append(w.Anchors, hash)
	w.StakeByValidator[validatorIndex] = stake
	w.ParticipatedStake += stake
}

// AddVote adds a stake-weighted vote for an anchor.
func (w *Wave) AddVote(anchorHash dag.Hash, stake uint64) {
	w.Votes[anchorHash] += stake
}

// GetLeadingAnchor returns the anchor with the most stake-weighted votes.
func (w *Wave) GetLeadingAnchor() (dag.Hash, uint64) {
	var leading dag.Hash
	var maxVotes uint64

	for hash, votes := range w.Votes {
		if votes > maxVotes {
			maxVotes = votes
			leading = hash
		}
	}

	return leading, maxVotes
}

// HasQuorum returns true if the leading anchor has quorum (2/3+ stake).
func (w *Wave) HasQuorum(quorumThreshold float64) bool {
	_, maxVotes := w.GetLeadingAnchor()
	if w.TotalStake == 0 {
		return false
	}
	return float64(maxVotes)/float64(w.TotalStake) >= quorumThreshold
}

// ParticipationRate returns the participation rate for this wave.
func (w *Wave) ParticipationRate() float64 {
	if w.TotalStake == 0 {
		return 0
	}
	return float64(w.ParticipatedStake) / float64(w.TotalStake)
}

// Duration returns how long this wave has been running.
func (w *Wave) Duration() time.Duration {
	if w.EndTime.IsZero() {
		return time.Since(w.StartTime)
	}
	return w.EndTime.Sub(w.StartTime)
}

// WaveTracker manages waves and their progression in Shoal++.
type WaveTracker struct {
	config *WaveConfig

	// Current wave state
	currentWave *Wave
	waveNumber  uint64

	// Wave history
	waves      map[uint64]*Wave
	wavesOrder []uint64 // Ordered list of wave numbers

	// Callbacks
	onWaveCommit func(*Wave)

	mu sync.RWMutex
}

// WaveConfig holds configuration for wave tracking.
type WaveConfig struct {
	// QuorumThreshold is the stake fraction required for quorum (default 2/3).
	QuorumThreshold float64

	// WaveTimeout is the maximum duration for a wave.
	WaveTimeout time.Duration

	// ProposingDuration is how long validators can propose anchors.
	ProposingDuration time.Duration

	// VotingDuration is how long validators can vote.
	VotingDuration time.Duration

	// MinParticipation is the minimum stake participation to commit.
	MinParticipation float64

	// MaxWaveHistory is the maximum number of waves to keep in memory.
	MaxWaveHistory int
}

// DefaultWaveConfig returns default wave configuration.
func DefaultWaveConfig() *WaveConfig {
	return &WaveConfig{
		QuorumThreshold:   2.0 / 3.0,
		WaveTimeout:       5 * time.Second,
		ProposingDuration: 1 * time.Second,
		VotingDuration:    1 * time.Second,
		MinParticipation:  0.5, // 50% minimum participation
		MaxWaveHistory:    1000,
	}
}

// NewWaveTracker creates a new wave tracker.
func NewWaveTracker(config *WaveConfig) *WaveTracker {
	if config == nil {
		config = DefaultWaveConfig()
	}

	wt := &WaveTracker{
		config:     config,
		waveNumber: 0,
		waves:      make(map[uint64]*Wave),
		wavesOrder: make([]uint64, 0),
	}

	// Start first wave
	wt.startNewWave()

	return wt
}

// startNewWave initiates a new wave.
func (wt *WaveTracker) startNewWave() {
	wt.waveNumber++
	wave := NewWave(wt.waveNumber)
	wave.State = WaveProposing

	wt.currentWave = wave
	wt.waves[wt.waveNumber] = wave
	wt.wavesOrder = append(wt.wavesOrder, wt.waveNumber)

	// Prune old waves
	wt.pruneWaves()
}

// pruneWaves removes old waves beyond MaxWaveHistory.
func (wt *WaveTracker) pruneWaves() {
	if len(wt.wavesOrder) <= wt.config.MaxWaveHistory {
		return
	}

	toRemove := len(wt.wavesOrder) - wt.config.MaxWaveHistory
	for i := 0; i < toRemove; i++ {
		delete(wt.waves, wt.wavesOrder[i])
	}
	wt.wavesOrder = wt.wavesOrder[toRemove:]
}

// CurrentWave returns the current wave.
func (wt *WaveTracker) CurrentWave() *Wave {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.currentWave
}

// CurrentWaveNumber returns the current wave number.
func (wt *WaveTracker) CurrentWaveNumber() uint64 {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.waveNumber
}

// GetWave returns a wave by number.
func (wt *WaveTracker) GetWave(number uint64) *Wave {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.waves[number]
}

// SetTotalStake sets the total stake for the current wave.
func (wt *WaveTracker) SetTotalStake(stake uint64) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.currentWave != nil {
		wt.currentWave.TotalStake = stake
	}
}

// AddAnchor adds an anchor block to the current wave.
func (wt *WaveTracker) AddAnchor(hash dag.Hash, validatorIndex uint32, stake uint64) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return ErrNoActiveWave
	}

	if wt.currentWave.State != WaveProposing {
		return ErrWaveNotInProposingState
	}

	wt.currentWave.AddAnchor(hash, validatorIndex, stake)
	return nil
}

// AddVote adds a vote for an anchor in the current wave.
func (wt *WaveTracker) AddVote(anchorHash dag.Hash, stake uint64) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return ErrNoActiveWave
	}

	if wt.currentWave.State != WaveVoting {
		return ErrWaveNotInVotingState
	}

	wt.currentWave.AddVote(anchorHash, stake)
	return nil
}

// TransitionToVoting moves the current wave to voting state.
func (wt *WaveTracker) TransitionToVoting() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return ErrNoActiveWave
	}

	if wt.currentWave.State != WaveProposing {
		return ErrInvalidWaveTransition
	}

	wt.currentWave.State = WaveVoting
	return nil
}

// TransitionToCommitting moves the current wave to committing state.
func (wt *WaveTracker) TransitionToCommitting() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return ErrNoActiveWave
	}

	if wt.currentWave.State != WaveVoting {
		return ErrInvalidWaveTransition
	}

	wt.currentWave.State = WaveCommitting
	return nil
}

// CommitWave commits the current wave with the winning anchor.
func (wt *WaveTracker) CommitWave() (*Wave, error) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return nil, ErrNoActiveWave
	}

	if wt.currentWave.State != WaveCommitting {
		return nil, ErrWaveNotInCommittingState
	}

	// Check for quorum
	leadingAnchor, votes := wt.currentWave.GetLeadingAnchor()
	if !wt.currentWave.HasQuorum(wt.config.QuorumThreshold) {
		return nil, ErrNoQuorum
	}

	// Commit the wave
	wt.currentWave.State = WaveCommitted
	wt.currentWave.CommitAnchor = leadingAnchor
	wt.currentWave.CommitStake = votes
	wt.currentWave.CommitTime = time.Now()
	wt.currentWave.EndTime = time.Now()

	committedWave := wt.currentWave

	// Notify callback
	if wt.onWaveCommit != nil {
		go wt.onWaveCommit(committedWave)
	}

	// Start next wave
	wt.startNewWave()

	return committedWave, nil
}

// SkipWave skips the current wave (due to timeout or insufficient participation).
func (wt *WaveTracker) SkipWave(reason string) (*Wave, error) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	if wt.currentWave == nil {
		return nil, ErrNoActiveWave
	}

	wt.currentWave.State = WaveSkipped
	wt.currentWave.EndTime = time.Now()

	skippedWave := wt.currentWave

	// Start next wave
	wt.startNewWave()

	return skippedWave, nil
}

// CheckTimeout checks if the current wave has timed out.
func (wt *WaveTracker) CheckTimeout() bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	if wt.currentWave == nil {
		return false
	}

	return wt.currentWave.Duration() >= wt.config.WaveTimeout
}

// ShouldTransitionToVoting returns true if it's time to move to voting.
func (wt *WaveTracker) ShouldTransitionToVoting() bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	if wt.currentWave == nil || wt.currentWave.State != WaveProposing {
		return false
	}

	return wt.currentWave.Duration() >= wt.config.ProposingDuration
}

// ShouldTransitionToCommitting returns true if it's time to move to committing.
func (wt *WaveTracker) ShouldTransitionToCommitting() bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	if wt.currentWave == nil || wt.currentWave.State != WaveVoting {
		return false
	}

	// Check if voting duration has passed
	votingStartTime := wt.currentWave.StartTime.Add(wt.config.ProposingDuration)
	return time.Since(votingStartTime) >= wt.config.VotingDuration
}

// OnWaveCommit sets a callback to be called when a wave is committed.
func (wt *WaveTracker) OnWaveCommit(callback func(*Wave)) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.onWaveCommit = callback
}

// GetRecentWaves returns the most recent waves.
func (wt *WaveTracker) GetRecentWaves(count int) []*Wave {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	if count > len(wt.wavesOrder) {
		count = len(wt.wavesOrder)
	}

	waves := make([]*Wave, count)
	for i := 0; i < count; i++ {
		idx := len(wt.wavesOrder) - count + i
		waves[i] = wt.waves[wt.wavesOrder[idx]]
	}

	return waves
}

// GetCommittedWaves returns all committed waves in order.
func (wt *WaveTracker) GetCommittedWaves() []*Wave {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	var committed []*Wave
	for _, num := range wt.wavesOrder {
		if w := wt.waves[num]; w != nil && w.State == WaveCommitted {
			committed = append(committed, w)
		}
	}

	return committed
}

// Statistics returns wave tracker statistics.
type WaveStatistics struct {
	TotalWaves     uint64
	CommittedWaves uint64
	SkippedWaves   uint64
	CurrentWave    uint64
	AvgCommitTime  time.Duration
	AvgParticipation float64
}

// GetStatistics returns current wave statistics.
func (wt *WaveTracker) GetStatistics() *WaveStatistics {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	stats := &WaveStatistics{
		TotalWaves:  uint64(len(wt.wavesOrder)),
		CurrentWave: wt.waveNumber,
	}

	var totalCommitTime time.Duration
	var totalParticipation float64
	var participationCount int

	for _, num := range wt.wavesOrder {
		w := wt.waves[num]
		if w == nil {
			continue
		}

		switch w.State {
		case WaveCommitted:
			stats.CommittedWaves++
			totalCommitTime += w.Duration()
		case WaveSkipped:
			stats.SkippedWaves++
		}

		if w.ParticipationRate() > 0 {
			totalParticipation += w.ParticipationRate()
			participationCount++
		}
	}

	if stats.CommittedWaves > 0 {
		stats.AvgCommitTime = totalCommitTime / time.Duration(stats.CommittedWaves)
	}
	if participationCount > 0 {
		stats.AvgParticipation = totalParticipation / float64(participationCount)
	}

	return stats
}

// WaveOrder represents the ordering of blocks within a wave.
type WaveOrder struct {
	WaveNumber uint64
	Blocks     []dag.Hash // Ordered blocks in this wave
}

// ComputeWaveOrder computes the deterministic ordering of blocks in a wave.
// Blocks are ordered by: 1) Blue status, 2) Stake, 3) Hash (tiebreaker)
func ComputeWaveOrder(wave *Wave, getVertex func(dag.Hash) *dag.Vertex) *WaveOrder {
	if wave == nil || len(wave.Anchors) == 0 {
		return &WaveOrder{WaveNumber: wave.Number}
	}

	type anchorInfo struct {
		hash   dag.Hash
		isBlue bool
		stake  uint64
	}

	anchors := make([]anchorInfo, 0, len(wave.Anchors))
	for _, hash := range wave.Anchors {
		v := getVertex(hash)
		if v == nil {
			continue
		}
		anchors = append(anchors, anchorInfo{
			hash:   hash,
			isBlue: v.IsBlue,
			stake:  v.Stake,
		})
	}

	// Sort: blue first, then by stake (descending), then by hash
	sort.Slice(anchors, func(i, j int) bool {
		if anchors[i].isBlue != anchors[j].isBlue {
			return anchors[i].isBlue // Blue comes first
		}
		if anchors[i].stake != anchors[j].stake {
			return anchors[i].stake > anchors[j].stake // Higher stake first
		}
		// Hash tiebreaker (lexicographic)
		for k := 0; k < 32; k++ {
			if anchors[i].hash[k] != anchors[j].hash[k] {
				return anchors[i].hash[k] < anchors[j].hash[k]
			}
		}
		return false
	})

	order := &WaveOrder{
		WaveNumber: wave.Number,
		Blocks:     make([]dag.Hash, len(anchors)),
	}
	for i, a := range anchors {
		order.Blocks[i] = a.hash
	}

	return order
}

// Error types for wave operations.
var (
	ErrNoActiveWave             = waveError("no active wave")
	ErrWaveNotInProposingState  = waveError("wave not in proposing state")
	ErrWaveNotInVotingState     = waveError("wave not in voting state")
	ErrWaveNotInCommittingState = waveError("wave not in committing state")
	ErrInvalidWaveTransition    = waveError("invalid wave state transition")
	ErrNoQuorum                 = waveError("no quorum reached")
)

type waveError string

func (e waveError) Error() string {
	return string(e)
}
