// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
)

// ProductionNetwork manages multiple validator processes running separately
type ProductionNetwork struct {
	baseDir    string
	validators []*ValidatorProcess
	network    network.Network // Underlying integration network for setup
	mutex      sync.Mutex
}

// ValidatorProcess represents a single validator running in its own process
type ValidatorProcess struct {
	Index      int
	NodeDir    string
	RPCPort    int
	P2PPort    int
	GRPCPort   int
	APIPort    int
	JSONRPCPort int
	Cmd        *exec.Cmd
	LogFile    *os.File
}

// NewProductionNetwork creates a network with separate validator processes
func NewProductionNetwork(numValidators int) (*ProductionNetwork, error) {
	// Create temporary directory for validator nodes
	baseDir, err := os.MkdirTemp("", "evmos-loadtest-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	pn := &ProductionNetwork{
		baseDir:    baseDir,
		validators: make([]*ValidatorProcess, numValidators),
	}

	// Initialize validator configuration files
	if err := pn.initValidatorConfigs(numValidators); err != nil {
		os.RemoveAll(baseDir)
		return nil, err
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
	// Use evmosd testnet init-files to create validator configurations
	evmosdPath, err := exec.LookPath("evmosd")
	if err != nil {
		return fmt.Errorf("evmosd binary not found in PATH: %w", err)
	}

	// Initialize validator configuration using evmosd testnet init-files
	args := []string{
		"testnet",
		"init-files",
		"--v", fmt.Sprintf("%d", numValidators),
		"--output-dir", pn.baseDir,
		"--starting-ip-address", "127.0.0.1",
		"--chain-id", "evmos_9002-1",
		"--keyring-backend", "test",
	}

	cmd := exec.Command(evmosdPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to initialize validator configs: %w\nOutput: %s", err, string(output))
	}

	// Set up validator process info
	for i := 0; i < numValidators; i++ {
		nodeDir := filepath.Join(pn.baseDir, fmt.Sprintf("node%d", i), "evmosd")

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
	}

	return nil
}

// startValidators starts each validator in its own process
func (pn *ProductionNetwork) startValidators() error {
	// Find evmosd binary
	evmosdPath, err := exec.LookPath("evmosd")
	if err != nil {
		return fmt.Errorf("evmosd binary not found in PATH: %w", err)
	}

	for _, val := range pn.validators {
		// Create log file
		logPath := filepath.Join(val.NodeDir, "evmosd.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file for validator %d: %w", val.Index, err)
		}
		val.LogFile = logFile

		// Build command arguments
		args := []string{
			"start",
			"--home", val.NodeDir,
			"--rpc.laddr", fmt.Sprintf("tcp://0.0.0.0:%d", val.RPCPort),
			"--p2p.laddr", fmt.Sprintf("tcp://0.0.0.0:%d", val.P2PPort),
			"--grpc.address", fmt.Sprintf("0.0.0.0:%d", val.GRPCPort),
			"--api.address", fmt.Sprintf("tcp://0.0.0.0:%d", val.APIPort),
			"--json-rpc.address", fmt.Sprintf("0.0.0.0:%d", val.JSONRPCPort),
			"--json-rpc.ws-address", fmt.Sprintf("0.0.0.0:%d", val.JSONRPCPort+1),
		}

		// Create command
		cmd := exec.Command(evmosdPath, args...)
		cmd.Dir = val.NodeDir
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		// Set process group so we can kill child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		// Start the validator process
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start validator %d: %w", val.Index, err)
		}

		val.Cmd = cmd
	}

	return nil
}

// waitForNetwork waits for all validators to be ready
func (pn *ProductionNetwork) waitForNetwork() error {
	// Wait for RPC endpoints to become available
	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Println("Waiting for all validators to start...")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for validators to start")
		case <-ticker.C:
			allReady := true
			readyCount := 0

			for _, val := range pn.validators {
				// Check if validator process is still running
				if val.Cmd.ProcessState != nil && val.Cmd.ProcessState.Exited() {
					return fmt.Errorf("validator %d exited prematurely", val.Index)
				}

				// Check if port is listening using ss command
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
				fmt.Printf("Validators ready: %d/%d\n", readyCount, len(pn.validators))
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
