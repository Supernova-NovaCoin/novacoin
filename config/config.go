// Package config provides configuration structures for the NovaCoin node.
package config

import (
	"time"
)

// Config holds the complete node configuration.
type Config struct {
	// Node identity
	NodeID    string
	DataDir   string
	LogLevel  string
	NetworkID uint64 // 1=mainnet, 2=testnet, 3=devnet

	// Network configuration
	Network *NetworkConfig

	// Consensus configuration
	Consensus *ConsensusConfig

	// RPC configuration
	RPC *RPCConfig

	// MEV configuration
	MEV *MEVConfig
}

// NetworkConfig holds P2P network settings.
type NetworkConfig struct {
	ListenAddr     string
	ListenPort     int
	BootstrapPeers []string
	MaxPeers       int
	EnableMDNS     bool
	EnableDHT      bool
}

// ConsensusConfig holds all consensus-related configuration.
type ConsensusConfig struct {
	// Mode selection
	Mode string // "legacy", "dagknight", "hybrid"

	// DAGKnight configuration
	DAGKnight *DAGKnightConfig

	// GHOSTDAG configuration
	GHOSTDAG *GHOSTDAGConfig

	// Shoal++ configuration
	Shoal *ShoalConfig

	// Mysticeti configuration
	Mysticeti *MysticetiConfig
}

// DAGKnightConfig configures the DAGKnight adaptive consensus layer.
type DAGKnightConfig struct {
	// K parameter bounds (Byzantine tolerance)
	MinK     float64 // 0.25 - tolerate 25% Byzantine
	MaxK     float64 // 0.50 - tolerate 50% Byzantine (theoretical max)
	DefaultK float64 // 0.33 - default 1/3 Byzantine

	// Block time bounds
	MinBlockTime    time.Duration // 500ms floor
	MaxBlockTime    time.Duration // 10s ceiling
	TargetBlockTime time.Duration // 1s target

	// Confidence thresholds
	HighConfidenceLatency float64 // 50ms - high confidence threshold
	LowConfidenceLatency  float64 // 1000ms - low confidence threshold

	// Adaptation rates
	KAdaptationRate    float64 // How fast K changes (0.01-0.1)
	BlockTimeAdaptRate float64 // How fast block time changes
}

// GHOSTDAGConfig configures the GHOSTDAG coloring algorithm.
type GHOSTDAGConfig struct {
	MaxParents   int    // Maximum parent references (default: 10)
	PruningDepth uint64 // DAG pruning depth
}

// ShoalConfig configures the Shoal++ multi-anchor layer.
type ShoalConfig struct {
	QuorumThreshold float64       // 2/3 for BFT
	WaveTimeout     time.Duration // Max time per wave
	NumParallelDAGs int           // Number of parallel DAGs (default: 4)
	StaggerOffset   time.Duration // Offset between parallel DAGs
}

// MysticetiConfig configures the Mysticeti 3-round commit layer.
type MysticetiConfig struct {
	QuorumThreshold   float64 // 2/3 for BFT
	MaxPendingCommits int     // Max commits in flight
	EnableFastPath    bool    // 2-round fast path for good conditions
}

// MEVConfig configures MEV resistance features.
type MEVConfig struct {
	Enabled        bool
	ThresholdN     int   // Total validators for threshold encryption
	ThresholdT     int   // Threshold (t-of-n)
	CommitWindowMs int64 // Commit phase duration
	RevealWindowMs int64 // Reveal phase duration
	BatchSize      int   // Transactions per batch
}

// RPCConfig configures the JSON-RPC server.
type RPCConfig struct {
	// HTTP JSON-RPC
	HTTPEnabled bool
	HTTPHost    string
	HTTPPort    int

	// WebSocket
	WSEnabled bool
	WSHost    string
	WSPort    int

	// Legacy fields for compatibility
	Enabled      bool
	ListenAddr   string
	EnableWS     bool
	WSListenAddr string

	// CORS configuration
	CorsOrigins []string
}

// DefaultConfig returns production-ready default configuration.
func DefaultConfig() *Config {
	return &Config{
		NodeID:    "",
		DataDir:   "./data",
		LogLevel:  "info",
		NetworkID: 1, // mainnet

		Network:   DefaultNetworkConfig(),
		Consensus: DefaultConsensusConfig(),
		RPC:       DefaultRPCConfig(),
		MEV:       DefaultMEVConfig(),
	}
}

// DefaultNetworkConfig returns default network settings.
func DefaultNetworkConfig() *NetworkConfig {
	return &NetworkConfig{
		ListenAddr:     "/ip4/0.0.0.0/tcp/30303",
		ListenPort:     30303,
		BootstrapPeers: []string{},
		MaxPeers:       50,
		EnableMDNS:     true,
		EnableDHT:      true,
	}
}

// DefaultConsensusConfig returns production-ready consensus defaults.
func DefaultConsensusConfig() *ConsensusConfig {
	return &ConsensusConfig{
		Mode: "hybrid",

		DAGKnight: &DAGKnightConfig{
			MinK:                  0.25,
			MaxK:                  0.50,
			DefaultK:              0.33,
			MinBlockTime:          500 * time.Millisecond,
			MaxBlockTime:          10 * time.Second,
			TargetBlockTime:       1 * time.Second,
			HighConfidenceLatency: 50,
			LowConfidenceLatency:  1000,
			KAdaptationRate:       0.02,
			BlockTimeAdaptRate:    0.1,
		},

		GHOSTDAG: &GHOSTDAGConfig{
			MaxParents:   10,
			PruningDepth: 100000,
		},

		Shoal: &ShoalConfig{
			QuorumThreshold: 2.0 / 3.0,
			WaveTimeout:     5 * time.Second,
			NumParallelDAGs: 4,
			StaggerOffset:   125 * time.Millisecond,
		},

		Mysticeti: &MysticetiConfig{
			QuorumThreshold:   2.0 / 3.0,
			MaxPendingCommits: 10000,
			EnableFastPath:    true,
		},
	}
}

// DefaultMEVConfig returns default MEV resistance settings.
func DefaultMEVConfig() *MEVConfig {
	return &MEVConfig{
		Enabled:        true,
		ThresholdN:     100,
		ThresholdT:     67, // 2/3 + 1
		CommitWindowMs: 500,
		RevealWindowMs: 500,
		BatchSize:      1000,
	}
}

// DefaultRPCConfig returns default RPC settings.
func DefaultRPCConfig() *RPCConfig {
	return &RPCConfig{
		// New fields
		HTTPEnabled: true,
		HTTPHost:    "localhost",
		HTTPPort:    8545,
		WSEnabled:   true,
		WSHost:      "localhost",
		WSPort:      8546,
		// Legacy fields
		Enabled:      true,
		ListenAddr:   "127.0.0.1:8545",
		EnableWS:     true,
		WSListenAddr: "127.0.0.1:8546",
		CorsOrigins:  []string{"*"},
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Consensus == nil {
		return ErrNilConsensusConfig
	}
	if c.Network == nil {
		return ErrNilNetworkConfig
	}
	return nil
}

// Error types for configuration validation.
var (
	ErrNilConsensusConfig = configError("consensus config is nil")
	ErrNilNetworkConfig   = configError("network config is nil")
)

type configError string

func (e configError) Error() string {
	return string(e)
}
