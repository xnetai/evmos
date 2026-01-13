// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
)

// ProductionNetwork manages multiple validator processes running separately
type ProductionNetwork struct {
	baseDir    string
	validators []*ValidatorProcess
	network    network.Network // Underlying integration network for setup
	mutex      sync.Mutex
	binaryPath string // Cached path to the binary
	binaryName string // Name of the binary (xcoind or evmosd)
}

// ValidatorProcess represents a single validator running in its own process
type ValidatorProcess struct {
	Index       int
	NodeDir     string
	RPCPort     int
	P2PPort     int
	GRPCPort    int
	APIPort     int
	JSONRPCPort int
	Cmd         *exec.Cmd
	LogFile     *os.File
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// findOrBuildBinary attempts to find xcoind or evmosd binary, building if necessary
func findOrBuildBinary() (binaryPath, binaryName string, err error) {
	// Try to find xcoind binary first
	binaryName = "xcoind"
	binaryPath, err = exec.LookPath(binaryName)
	if err == nil {
		fmt.Printf("Found %s in PATH: %s\n", binaryName, binaryPath)
		return binaryPath, binaryName, nil
	}

	// Try evmosd as fallback
	binaryName = "evmosd"
	binaryPath, err = exec.LookPath(binaryName)
	if err == nil {
		fmt.Printf("Found %s in PATH: %s\n", binaryName, binaryPath)
		return binaryPath, binaryName, nil
	}

	// Neither binary found, attempt to build from source
	fmt.Println("Neither xcoind nor evmosd found in PATH, attempting to build from source...")

	// Find the evmos root directory
	// We're in tests/loadtest, so go up two levels
	currentDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Try to find evmos root by looking for go.mod
	evmosRoot := currentDir
	for i := 0; i < 5; i++ { // Try up to 5 levels up
		if _, err := os.Stat(filepath.Join(evmosRoot, "go.mod")); err == nil {
			break
		}
		evmosRoot = filepath.Dir(evmosRoot)
	}

	fmt.Printf("Building binary in directory: %s\n", evmosRoot)

	// Run make build
	buildCmd := exec.Command("make", "build")
	buildCmd.Dir = evmosRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	fmt.Println("Running: make build")
	if err := buildCmd.Run(); err != nil {
		return "", "", fmt.Errorf("failed to build binary: %w", err)
	}

	// Check for built binaries in build/ directory
	buildDir := filepath.Join(evmosRoot, "build")

	// Try xcoind first
	binaryName = "xcoind"
	binaryPath = filepath.Join(buildDir, binaryName)
	if _, err := os.Stat(binaryPath); err == nil {
		fmt.Printf("Successfully built %s: %s\n", binaryName, binaryPath)
		return binaryPath, binaryName, nil
	}

	// Try evmosd
	binaryName = "evmosd"
	binaryPath = filepath.Join(buildDir, binaryName)
	if _, err := os.Stat(binaryPath); err == nil {
		fmt.Printf("Successfully built %s: %s\n", binaryName, binaryPath)
		return binaryPath, binaryName, nil
	}

	return "", "", fmt.Errorf("build completed but could not find binary in %s", buildDir)
}

// NewProductionNetwork creates a network with separate validator processes
func NewProductionNetwork(numValidators int) (*ProductionNetwork, error) {
	return NewProductionNetworkWithAccounts(numValidators, nil)
}

// NewProductionNetworkWithAccounts creates a network with additional funded accounts
func NewProductionNetworkWithAccounts(numValidators int, kr keyring.Keyring) (*ProductionNetwork, error) {
	// Create temporary directory for validator nodes
	baseDir, err := os.MkdirTemp("", "evmos-loadtest-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Find or build the binary
	binaryPath, binaryName, err := findOrBuildBinary()
	if err != nil {
		os.RemoveAll(baseDir)
		return nil, err
	}

	pn := &ProductionNetwork{
		baseDir:    baseDir,
		validators: make([]*ValidatorProcess, numValidators),
		binaryPath: binaryPath,
		binaryName: binaryName,
	}

	// Initialize validator configuration files
	if err := pn.initValidatorConfigs(numValidators); err != nil {
		os.RemoveAll(baseDir)
		return nil, err
	}

	// Enable gRPC in app.toml for all validators
	if err := pn.enableGRPC(); err != nil {
		os.RemoveAll(baseDir)
		return nil, fmt.Errorf("failed to enable gRPC: %w", err)
	}

	// Fund additional accounts in genesis if keyring provided
	if kr != nil {
		fmt.Printf("Funding %d additional accounts in genesis...\n", len(kr.GetKeys()))
		if err := pn.fundAccountsInGenesis(kr); err != nil {
			os.RemoveAll(baseDir)
			return nil, fmt.Errorf("failed to fund accounts in genesis: %w", err)
		}
		fmt.Printf("✓ Funded %d accounts in genesis\n", len(kr.GetKeys()))
	}

	// Start each validator in its own process
	if err := pn.startValidators(); err != nil {
		pn.Cleanup()
		return nil, err
	}

	// Wait for network to be ready
	if err := pn.waitForNetwork(); err != nil {
		pn.Cleanup()
		return nil, err
	}

	return pn, nil
}

// initValidatorConfigs initializes configuration for each validator
func (pn *ProductionNetwork) initValidatorConfigs(numValidators int) error {
	fmt.Printf("Initializing validator configs with %s (path: %s)\n", pn.binaryName, pn.binaryPath)

	// Initialize validator configuration using testnet init-files
	args := []string{
		"testnet",
		"init-files",
		"--v", fmt.Sprintf("%d", numValidators),
		"--output-dir", pn.baseDir,
		"--starting-ip-address", "127.0.0.1",
		"--chain-id", "evmos_9002-1",
		"--keyring-backend", "test",
	}

	cmd := exec.Command(pn.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initialize validator configs with %s: %w\nOutput: %s", pn.binaryName, err, string(output))
	}

	// Set up validator process info
	// The daemon home directory name depends on the binary (evmosd or xcoind)
	daemonHome := pn.binaryName

	// First pass: collect node IDs and create validator structs
	nodeIDs := make([]string, numValidators)
	for i := 0; i < numValidators; i++ {
		nodeDir := filepath.Join(pn.baseDir, fmt.Sprintf("node%d", i), daemonHome)

		validator := &ValidatorProcess{
			Index:       i,
			NodeDir:     nodeDir,
			RPCPort:     26657 + (i * 10),
			P2PPort:     26656 + (i * 10),
			GRPCPort:    9090 + (i * 10),
			APIPort:     1317 + (i * 10),
			JSONRPCPort: 8545 + (i * 10),
		}

		pn.validators[i] = validator

		// Get node ID using the binary's show-node-id command
		// This is more reliable than parsing node_key.json
		showNodeIDCmd := exec.Command(pn.binaryPath, "tendermint", "show-node-id", "--home", nodeDir)
		nodeIDOutput, err := showNodeIDCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to get node ID for validator %d: %w\nOutput: %s", i, err, string(nodeIDOutput))
		}

		// Parse output to extract node ID (40 hex characters)
		// The command may output warnings or other messages, so we need to find the actual node ID
		var nodeID string
		outputLines := strings.Split(string(nodeIDOutput), "\n")
		for _, line := range outputLines {
			line = strings.TrimSpace(line)
			// Skip empty lines and lines that start with WARNING, INFO, ERROR, etc.
			if line == "" || strings.HasPrefix(line, "WARNING") || strings.HasPrefix(line, "WARN") ||
				strings.HasPrefix(line, "INFO") || strings.HasPrefix(line, "ERROR") {
				continue
			}
			// Node ID should be exactly 40 hex characters
			if len(line) == 40 && isHexString(line) {
				nodeID = line
				break
			}
		}

		// Validate node ID was found
		if nodeID == "" {
			return fmt.Errorf("could not find valid node ID in output for validator %d\nOutput: %s", i, string(nodeIDOutput))
		}

		nodeIDs[i] = nodeID
		fmt.Printf("Validator %d node ID: %s\n", i, nodeID)
	}

	// Second pass: update config.toml with correct persistent_peers
	for i, val := range pn.validators {
		configPath := filepath.Join(val.NodeDir, "config", "config.toml")

		// Build persistent_peers list (exclude self)
		var peers []string
		for j := 0; j < numValidators; j++ {
			if j != i {
				// Double-check node ID is not empty before adding to peers
				if nodeIDs[j] == "" {
					return fmt.Errorf("cannot build persistent_peers: node ID for validator %d is empty", j)
				}
				peerAddr := fmt.Sprintf("%s@127.0.0.1:%d", nodeIDs[j], 26656+(j*10))
				peers = append(peers, peerAddr)
			}
		}
		persistentPeers := strings.Join(peers, ",")

		// Validate persistent_peers doesn't contain newlines or other problematic characters
		if strings.ContainsAny(persistentPeers, "\n\r") {
			return fmt.Errorf("persistent_peers contains newline characters for validator %d: %q", i, persistentPeers)
		}

		// Validate persistent_peers is not empty
		if persistentPeers == "" && numValidators > 1 {
			return fmt.Errorf("persistent_peers is empty for validator %d, but we have %d validators", i, numValidators)
		}

		// Read config file
		configData, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config.toml for validator %d: %w", i, err)
		}

		// Replace persistent_peers line and set addr_book_strict = false
		configStr := string(configData)
		lines := strings.Split(configStr, "\n")
		persistentPeersReplaced := false
		addrBookStrictReplaced := false
		allowDuplicateIPReplaced := false

		for idx, line := range lines {
			trimmed := strings.TrimSpace(line)

			// Replace persistent_peers
			if strings.HasPrefix(trimmed, "persistent_peers = ") && !strings.HasPrefix(trimmed, "#") && !persistentPeersReplaced {
				oldValue := line
				lines[idx] = fmt.Sprintf(`persistent_peers = "%s"`, persistentPeers)
				fmt.Printf("Validator %d: Replacing '%s' with 'persistent_peers = \"%s\"'\n", i, strings.TrimSpace(oldValue), persistentPeers)
				persistentPeersReplaced = true
			}

			// Set addr_book_strict = false to allow localhost addresses
			if strings.HasPrefix(trimmed, "addr_book_strict = ") && !strings.HasPrefix(trimmed, "#") && !addrBookStrictReplaced {
				oldValue := line
				lines[idx] = `addr_book_strict = false`
				fmt.Printf("Validator %d: Replacing '%s' with 'addr_book_strict = false' (allow localhost)\n", i, strings.TrimSpace(oldValue))
				addrBookStrictReplaced = true
			}

			// Set allow_duplicate_ip = true to allow multiple peers from same IP
			if strings.HasPrefix(trimmed, "allow_duplicate_ip = ") && !strings.HasPrefix(trimmed, "#") && !allowDuplicateIPReplaced {
				oldValue := line
				lines[idx] = `allow_duplicate_ip = true`
				fmt.Printf("Validator %d: Replacing '%s' with 'allow_duplicate_ip = true' (allow multiple peers from 127.0.0.1)\n", i, strings.TrimSpace(oldValue))
				allowDuplicateIPReplaced = true
			}
		}

		if !persistentPeersReplaced {
			return fmt.Errorf("failed to find persistent_peers line in config.toml for validator %d", i)
		}

		if !addrBookStrictReplaced {
			return fmt.Errorf("failed to find addr_book_strict line in config.toml for validator %d", i)
		}

		if !allowDuplicateIPReplaced {
			return fmt.Errorf("failed to find allow_duplicate_ip line in config.toml for validator %d", i)
		}

		configStr = strings.Join(lines, "\n")

		// Write back config file
		if err := os.WriteFile(configPath, []byte(configStr), 0644); err != nil {
			return fmt.Errorf("failed to write config.toml for validator %d: %w", i, err)
		}

		fmt.Printf("Validator %d persistent_peers successfully updated\n", i)
	}

	return nil
}

// enableGRPC enables gRPC in app.toml for all validators
func (pn *ProductionNetwork) enableGRPC() error {
	for i := 0; i < len(pn.validators); i++ {
		appConfigPath := filepath.Join(pn.validators[i].NodeDir, "config", "app.toml")

		// Read app.toml file
		appConfigData, err := os.ReadFile(appConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read app.toml for validator %d: %w", i, err)
		}

		// Replace enable = false with enable = true in [grpc] section
		appConfigStr := string(appConfigData)
		lines := strings.Split(appConfigStr, "\n")
		inGRPCSection := false
		grpcEnableReplaced := false

		for idx, line := range lines {
			trimmed := strings.TrimSpace(line)

			// Detect [grpc] section
			if trimmed == "[grpc]" {
				inGRPCSection = true
				continue
			}

			// Exit [grpc] section when we hit another section
			if inGRPCSection && strings.HasPrefix(trimmed, "[") && trimmed != "[grpc]" {
				inGRPCSection = false
			}

			// Replace enable = false with enable = true in [grpc] section
			if inGRPCSection && strings.HasPrefix(trimmed, "enable = ") && !strings.HasPrefix(trimmed, "#") && !grpcEnableReplaced {
				oldValue := line
				lines[idx] = "enable = true"
				fmt.Printf("Validator %d: Replacing '%s' with 'enable = true' in [grpc] section\n", i, strings.TrimSpace(oldValue))
				grpcEnableReplaced = true
			}
		}

		if !grpcEnableReplaced {
			return fmt.Errorf("failed to find enable line in [grpc] section of app.toml for validator %d", i)
		}

		appConfigStr = strings.Join(lines, "\n")

		// Write back app.toml file
		if err := os.WriteFile(appConfigPath, []byte(appConfigStr), 0644); err != nil {
			return fmt.Errorf("failed to write app.toml for validator %d: %w", i, err)
		}

		fmt.Printf("Validator %d gRPC enabled in app.toml\n", i)
	}

	return nil
}

// fundAccountsInGenesis adds funded accounts to all validator genesis files
func (pn *ProductionNetwork) fundAccountsInGenesis(kr keyring.Keyring) error {
	// Define balances for each account
	balances := make([]banktypes.Balance, len(kr.GetKeys()))
	for i, key := range kr.GetKeys() {
		// Fund each account with the same denoms as the test
		// Include aevmos for gas fees (EVM transactions use base denom)
		coins := sdk.NewCoins(
			sdk.NewCoin("abtc", sdkmath.NewInt(100000)),
			sdk.NewCoin("aeth", sdkmath.NewInt(100000)),
			sdk.NewCoin("aevmos", sdkmath.NewInt(1000000000000000000)), // 1 EVMOS = 10^18 aevmos
			sdk.NewCoin("asol", sdkmath.NewInt(100000)),
			sdk.NewCoin("txcoin", sdkmath.NewInt(1000000000)),
			sdk.NewCoin("xusd", sdkmath.NewInt(100000)),
		)
		balances[i] = banktypes.Balance{
			Address: key.AccAddr.String(),
			Coins:   coins,
		}
	}

	// Modify genesis file for each validator
	daemonHome := pn.binaryName
	for i := 0; i < len(pn.validators); i++ {
		nodeDir := filepath.Join(pn.baseDir, fmt.Sprintf("node%d", i), daemonHome)
		genesisPath := filepath.Join(nodeDir, "config", "genesis.json")

		// Read genesis file
		genesisBytes, err := os.ReadFile(genesisPath)
		if err != nil {
			return fmt.Errorf("failed to read genesis file for validator %d: %w", i, err)
		}

		// Parse genesis
		var genesis map[string]interface{}
		if err := json.Unmarshal(genesisBytes, &genesis); err != nil {
			return fmt.Errorf("failed to parse genesis for validator %d: %w", i, err)
		}

		// Get app_state
		appState, ok := genesis["app_state"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid app_state in genesis for validator %d", i)
		}

		// Get bank module
		bankState, ok := appState["bank"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid bank state in genesis for validator %d", i)
		}

		// Get existing balances
		existingBalances := []interface{}{}
		if balancesRaw, ok := bankState["balances"].([]interface{}); ok {
			existingBalances = balancesRaw
		}

		// Calculate and update total supply
		// Start with existing supply from all existing balances (including validator accounts)
		supply := make(map[string]sdkmath.Int)

		// First, sum up all existing balances to get the baseline supply
		for _, balRaw := range existingBalances {
			if balMap, ok := balRaw.(map[string]interface{}); ok {
				if coinsRaw, ok := balMap["coins"].([]interface{}); ok {
					for _, coinRaw := range coinsRaw {
						if coinMap, ok := coinRaw.(map[string]interface{}); ok {
							denom, ok1 := coinMap["denom"].(string)
							amountStr, ok2 := coinMap["amount"].(string)
							if ok1 && ok2 {
								amount, ok := sdkmath.NewIntFromString(amountStr)
								if ok {
									if existing, exists := supply[denom]; exists {
										supply[denom] = existing.Add(amount)
									} else {
										supply[denom] = amount
									}
								}
							}
						}
					}
				}
			}
		}

		// Add our new balances
		for _, balance := range balances {
			// Convert coins to JSON format
			coinsJSON := make([]map[string]interface{}, len(balance.Coins))
			for j, coin := range balance.Coins {
				coinsJSON[j] = map[string]interface{}{
					"denom":  coin.Denom,
					"amount": coin.Amount.String(),
				}
			}

			balanceJSON := map[string]interface{}{
				"address": balance.Address,
				"coins":   coinsJSON,
			}
			existingBalances = append(existingBalances, balanceJSON)

			// Add to supply
			for _, coin := range balance.Coins {
				if existing, ok := supply[coin.Denom]; ok {
					supply[coin.Denom] = existing.Add(coin.Amount)
				} else {
					supply[coin.Denom] = coin.Amount
				}
			}
		}

		bankState["balances"] = existingBalances

		// Convert supply back to JSON format (sorted by denom)
		supplyJSON := make([]map[string]interface{}, 0, len(supply))
		denoms := make([]string, 0, len(supply))
		for denom := range supply {
			denoms = append(denoms, denom)
		}
		// Sort denoms alphabetically (CRITICAL!)
		for i := 0; i < len(denoms); i++ {
			for j := i + 1; j < len(denoms); j++ {
				if denoms[i] > denoms[j] {
					denoms[i], denoms[j] = denoms[j], denoms[i]
				}
			}
		}
		for _, denom := range denoms {
			supplyJSON = append(supplyJSON, map[string]interface{}{
				"denom":  denom,
				"amount": supply[denom].String(),
			})
		}

		bankState["supply"] = supplyJSON

		// Write back genesis file
		genesisBytes, err = json.MarshalIndent(genesis, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal genesis for validator %d: %w", i, err)
		}

		if err := os.WriteFile(genesisPath, genesisBytes, 0644); err != nil {
			return fmt.Errorf("failed to write genesis for validator %d: %w", i, err)
		}

		fmt.Printf("✓ Validator %d: Added %d funded accounts to genesis\n", i, len(balances))
	}

	return nil
}

// startValidators starts each validator in its own process
func (pn *ProductionNetwork) startValidators() error {
	fmt.Printf("Starting validators with binary: %s (path: %s)\n", pn.binaryName, pn.binaryPath)

	for _, val := range pn.validators {
		// Create log file
		logPath := filepath.Join(val.NodeDir, fmt.Sprintf("%s.log", pn.binaryName))
		logFile, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file for validator %d: %w", val.Index, err)
		}
		val.LogFile = logFile

		// Build command arguments
		args := []string{
			"start",
			"--home", val.NodeDir,
			"--chain-id", "evmos_9002-1",
			"--rpc.laddr", fmt.Sprintf("tcp://0.0.0.0:%d", val.RPCPort),
			"--p2p.laddr", fmt.Sprintf("tcp://0.0.0.0:%d", val.P2PPort),
			"--grpc.address", fmt.Sprintf("0.0.0.0:%d", val.GRPCPort),
			"--grpc.enable", "true",                                 // Enable gRPC server
			"--json-rpc.address", fmt.Sprintf("0.0.0.0:%d", val.JSONRPCPort),
			"--json-rpc.ws-address", fmt.Sprintf("0.0.0.0:%d", val.JSONRPCPort+1),
			"--json-rpc.api", "eth,txpool,personal,net,debug,web3", // Enable all EVM JSON-RPC APIs
			"--log_level", "info",                                   // Set log level for better debugging
			"--minimum-gas-prices", "0txcoin",                       // Use txcoin for testing chain (evmos_9002-1)
		}

		// Create command
		cmd := exec.Command(pn.binaryPath, args...)
		cmd.Dir = val.NodeDir
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		// Set process group so we can kill child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		// Start the validator process
		fmt.Printf("Starting validator %d: %s %v\n", val.Index, pn.binaryPath, args)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start validator %d: %w", val.Index, err)
		}

		fmt.Printf("Validator %d started with PID %d\n", val.Index, cmd.Process.Pid)
		val.Cmd = cmd
	}

	return nil
}

// waitForNetwork waits for all validators to be ready
func (pn *ProductionNetwork) waitForNetwork() error {
	// Wait for RPC endpoints to become available
	timeout := time.After(120 * time.Second) // Increased timeout to 120 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	fmt.Println("Waiting for all validators to start...")

	for {
		select {
		case <-timeout:
			// Print diagnostic information before returning error
			fmt.Println("\n=== Timeout Diagnostics ===")
			for _, val := range pn.validators {
				fmt.Printf("\nValidator %d (PID %d):\n", val.Index, val.Cmd.Process.Pid)

				// Check process state
				if val.Cmd.ProcessState != nil && val.Cmd.ProcessState.Exited() {
					fmt.Printf("  Status: EXITED (exit code: %d)\n", val.Cmd.ProcessState.ExitCode())
				} else {
					fmt.Printf("  Status: RUNNING\n")
				}

				// Check if port is listening
				cmd := exec.Command("ss", "-tln")
				output, err := cmd.CombinedOutput()
				portStr := fmt.Sprintf(":%d", val.RPCPort)
				if err == nil && strings.Contains(string(output), portStr) {
					fmt.Printf("  RPC Port %d: LISTENING\n", val.RPCPort)
				} else {
					fmt.Printf("  RPC Port %d: NOT DETECTED (ss error: %v)\n", val.RPCPort, err)
				}

				// Print last 50 lines of log
				fmt.Printf("  Log file: %s\n", filepath.Join(val.NodeDir, fmt.Sprintf("%s.log", pn.binaryName)))
				if logContent, err := os.ReadFile(filepath.Join(val.NodeDir, fmt.Sprintf("%s.log", pn.binaryName))); err == nil {
					lines := strings.Split(string(logContent), "\n")
					startIdx := len(lines) - 51
					if startIdx < 0 {
						startIdx = 0
					}
					fmt.Printf("  Last log lines:\n")
					for _, line := range lines[startIdx:] {
						if line != "" {
							fmt.Printf("    %s\n", line)
						}
					}
				}
			}
			return fmt.Errorf("timeout waiting for validators to start")
		case <-ticker.C:
			allReady := true
			readyCount := 0

			for _, val := range pn.validators {
				// Check if validator process is still running
				if val.Cmd.ProcessState != nil && val.Cmd.ProcessState.Exited() {
					// Print log tail before returning error
					fmt.Printf("\nValidator %d exited prematurely. Last log lines:\n", val.Index)
					if logContent, err := os.ReadFile(filepath.Join(val.NodeDir, fmt.Sprintf("%s.log", pn.binaryName))); err == nil {
						lines := strings.Split(string(logContent), "\n")
						startIdx := len(lines) - 31
						if startIdx < 0 {
							startIdx = 0
						}
						for _, line := range lines[startIdx:] {
							if line != "" {
								fmt.Printf("  %s\n", line)
							}
						}
					}
					return fmt.Errorf("validator %d exited prematurely", val.Index)
				}

				// Check if validator is producing blocks by looking at log
				// If we see "indexed block" or "committed state" in recent logs, validator is ready
				logPath := filepath.Join(val.NodeDir, fmt.Sprintf("%s.log", pn.binaryName))
				if logContent, err := os.ReadFile(logPath); err == nil {
					logStr := string(logContent)
					// Check for signs of active consensus/block production
					if strings.Contains(logStr, "indexed block") ||
						strings.Contains(logStr, "committed state") ||
						strings.Contains(logStr, "Finalizing commit") {
						readyCount++
						continue
					}
				}

				// Fallback: Check if port is listening using ss command
				cmd := exec.Command("ss", "-tln")
				output, err := cmd.CombinedOutput()
				if err == nil {
					portStr := fmt.Sprintf(":%d", val.RPCPort)
					if strings.Contains(string(output), portStr) {
						readyCount++
					} else {
						allReady = false
					}
				} else {
					allReady = false
				}
			}

			if allReady && readyCount == len(pn.validators) {
				fmt.Printf("All %d validators are ready!\n", readyCount)
				return nil
			}

			if readyCount > 0 {
				fmt.Printf("Validators ready: %d/%d (checking block production and RPC ports)\n", readyCount, len(pn.validators))
			}
		}
	}
}

// GetValidatorRPCAddr returns the RPC address for a validator
func (pn *ProductionNetwork) GetValidatorRPCAddr(index int) string {
	if index < 0 || index >= len(pn.validators) {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", pn.validators[index].RPCPort)
}

// GetValidatorP2PAddr returns the P2P address for a validator
func (pn *ProductionNetwork) GetValidatorP2PAddr(index int) string {
	if index < 0 || index >= len(pn.validators) {
		return ""
	}
	return fmt.Sprintf("tcp://localhost:%d", pn.validators[index].P2PPort)
}

// Cleanup stops all validator processes and removes temporary directories
func (pn *ProductionNetwork) Cleanup() error {
	pn.mutex.Lock()
	defer pn.mutex.Unlock()

	var errors []error

	// Stop all validator processes
	for i, val := range pn.validators {
		if val.Cmd != nil && val.Cmd.Process != nil {
			// Send SIGTERM to process group
			pgid, err := syscall.Getpgid(val.Cmd.Process.Pid)
			if err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			} else {
				_ = val.Cmd.Process.Signal(syscall.SIGTERM)
			}

			// Wait for process to exit (with timeout)
			done := make(chan error, 1)
			go func() {
				done <- val.Cmd.Wait()
			}()

			select {
			case <-time.After(5 * time.Second):
				// Force kill if not stopped
				_ = val.Cmd.Process.Kill()
			case <-done:
				// Process exited cleanly
			}
		}

		// Close log file
		if val.LogFile != nil {
			if err := val.LogFile.Close(); err != nil {
				errors = append(errors, fmt.Errorf("failed to close log file for validator %d: %w", i, err))
			}
		}
	}

	// Remove temporary directory
	if err := os.RemoveAll(pn.baseDir); err != nil {
		errors = append(errors, fmt.Errorf("failed to remove temp dir: %w", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %v", errors)
	}

	return nil
}

// PrintValidatorInfo prints information about running validators
func (pn *ProductionNetwork) PrintValidatorInfo() string {
	info := "Production Validator Processes:\n"
	for _, val := range pn.validators {
		info += fmt.Sprintf("  Validator %d:\n", val.Index)
		info += fmt.Sprintf("    PID: %d\n", val.Cmd.Process.Pid)
		info += fmt.Sprintf("    RPC: localhost:%d\n", val.RPCPort)
		info += fmt.Sprintf("    P2P: localhost:%d\n", val.P2PPort)
		info += fmt.Sprintf("    gRPC: localhost:%d\n", val.GRPCPort)
		info += fmt.Sprintf("    API: localhost:%d\n", val.APIPort)
		info += fmt.Sprintf("    Node Dir: %s\n", val.NodeDir)
	}
	return info
}
