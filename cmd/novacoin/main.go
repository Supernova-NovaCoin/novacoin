// Package main provides the entry point for the NovaCoin node.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Supernova-NovaCoin/novacoin/config"
	"github.com/Supernova-NovaCoin/novacoin/crypto"
)

var (
	version   = "0.1.0"
	gitCommit = "development"
	buildDate = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	dataDir := flag.String("datadir", "./data", "Data directory for blockchain data")
	networkID := flag.Uint64("networkid", 1, "Network ID (1=mainnet, 2=testnet)")
	showVersion := flag.Bool("version", false, "Show version information")

	// RPC flags
	rpcEnabled := flag.Bool("rpc", true, "Enable HTTP JSON-RPC server")
	rpcHost := flag.String("rpchost", "localhost", "HTTP-RPC server host")
	rpcPort := flag.Int("rpcport", 8545, "HTTP-RPC server port")

	// WebSocket flags
	wsEnabled := flag.Bool("ws", true, "Enable WebSocket server")
	wsHost := flag.String("wshost", "localhost", "WebSocket server host")
	wsPort := flag.Int("wsport", 8546, "WebSocket server port")

	// P2P flags
	p2pPort := flag.Int("port", 30303, "P2P network port")
	maxPeers := flag.Int("maxpeers", 50, "Maximum number of peers")
	bootnodes := flag.String("bootnodes", "", "Comma-separated bootstrap node URLs")

	// Validator flags
	validator := flag.Bool("validator", false, "Run as validator node")
	validatorKey := flag.String("validatorkey", "", "Path to validator key file")

	// Debug flags
	verbosity := flag.Int("verbosity", 3, "Logging verbosity (0-5)")
	metrics := flag.Bool("metrics", false, "Enable metrics collection")

	flag.Parse()

	if *showVersion {
		fmt.Printf("NovaCoin %s\n", version)
		fmt.Printf("  Git Commit: %s\n", gitCommit)
		fmt.Printf("  Build Date: %s\n", buildDate)
		fmt.Println()
		fmt.Println("Features:")
		fmt.Println("  - PoS-DAGKnight consensus (50% Byzantine tolerance)")
		fmt.Println("  - Mysticeti 3-round finality (~1.5s)")
		fmt.Println("  - Shoal++ multi-anchor (100k+ TPS)")
		fmt.Println("  - MEV resistance via threshold encryption")
		fmt.Println("  - STARK proofs for scalability")
		os.Exit(0)
	}

	// Initialize BLS cryptography
	if err := crypto.InitBLS(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize BLS: %v\n", err)
		os.Exit(1)
	}

	// Load configuration
	cfg := config.DefaultConfig()
	if *configPath != "" {
		loadedCfg, err := loadConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
		cfg = loadedCfg
	}

	// Apply command line overrides
	cfg.DataDir = *dataDir
	cfg.NetworkID = *networkID
	cfg.RPC.HTTPEnabled = *rpcEnabled
	cfg.RPC.HTTPHost = *rpcHost
	cfg.RPC.HTTPPort = *rpcPort
	cfg.RPC.WSEnabled = *wsEnabled
	cfg.RPC.WSHost = *wsHost
	cfg.RPC.WSPort = *wsPort
	cfg.Network.ListenPort = *p2pPort
	cfg.Network.MaxPeers = *maxPeers

	if *bootnodes != "" {
		cfg.Network.BootstrapPeers = parseBootnodes(*bootnodes)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Print banner
	printBanner(cfg)

	// Create node
	node, err := NewNode(cfg, *validator, *validatorKey, *verbosity, *metrics)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create node: %v\n", err)
		os.Exit(1)
	}

	// Start node
	if err := node.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start node: %v\n", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println()
	fmt.Println("Node is running. Press Ctrl+C to shutdown...")

	<-sigCh

	fmt.Println()
	fmt.Println("Shutting down NovaCoin node...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := node.Stop(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
	}

	fmt.Println("Goodbye!")
}

// printBanner prints the startup banner.
func printBanner(cfg *config.Config) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║       NovaCoin - Ultra-Scalable PoS-DAG Blockchain    ║")
	fmt.Println("╠═══════════════════════════════════════════════════════╣")
	fmt.Printf("║  Version: %-44s ║\n", version)
	fmt.Printf("║  Network: %-44s ║\n", getNetworkName(cfg.NetworkID))
	fmt.Println("╚═══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Data directory:    %s\n", cfg.DataDir)
	fmt.Printf("  P2P port:          %d\n", cfg.Network.ListenPort)
	if cfg.RPC.HTTPEnabled {
		fmt.Printf("  HTTP-RPC:          http://%s:%d\n", cfg.RPC.HTTPHost, cfg.RPC.HTTPPort)
	}
	if cfg.RPC.WSEnabled {
		fmt.Printf("  WebSocket:         ws://%s:%d\n", cfg.RPC.WSHost, cfg.RPC.WSPort)
	}
	fmt.Println()
	fmt.Println("  Consensus:")
	fmt.Printf("    Mode:            %s\n", cfg.Consensus.Mode)
	fmt.Printf("    Block time:      %v\n", cfg.Consensus.DAGKnight.TargetBlockTime)
	fmt.Printf("    Adaptive K:      %.2f\n", cfg.Consensus.DAGKnight.DefaultK)
	fmt.Printf("    Parallel DAGs:   %d\n", cfg.Consensus.Shoal.NumParallelDAGs)
	fmt.Println()
}

// getNetworkName returns the network name for an ID.
func getNetworkName(id uint64) string {
	switch id {
	case 1:
		return "Mainnet"
	case 2:
		return "Testnet"
	case 3:
		return "Devnet"
	default:
		return fmt.Sprintf("Network-%d", id)
	}
}

// loadConfig loads configuration from a file.
func loadConfig(path string) (*config.Config, error) {
	// For now, just return default config
	// TODO: Implement TOML/JSON config file loading
	cfg := config.DefaultConfig()
	fmt.Printf("Loading config from: %s\n", path)
	return cfg, nil
}

// parseBootnodes parses comma-separated bootnode URLs.
func parseBootnodes(s string) []string {
	if s == "" {
		return nil
	}
	var nodes []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if start < i {
				nodes = append(nodes, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		nodes = append(nodes, s[start:])
	}
	return nodes
}
