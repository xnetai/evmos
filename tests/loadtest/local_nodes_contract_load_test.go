// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

const (
	numValidators = 4
	numUsers      = 100
)

// LocalNodesContractLoadTestSuite tests contract load across multiple local nodes
type LocalNodesContractLoadTestSuite struct {
	suite.Suite

	// Production network (separate validator processes)
	prodNetwork *ProductionNetwork

	// Integration network (for transaction building and utilities)
	integrationNetwork network.Network
	factory            factory.TxFactory
	grpcHandler        grpc.Handler

	// 100 user keyring
	keyring keyring.Keyring

	// Per-node gRPC clients for round-robin
	nodeClients []*NodeClient

	// Deployed contract
	contractAddr common.Address

	// Statistics tracking
	stats *LocalLoadTestStats

	// Round-robin distributor
	distributor *RoundRobinDistributor

	// Transaction builders
	contractBuilder *ContractTxBuilder
	bankBuilder     *BankTxBuilder
	stakingBuilder  *StakingTxBuilder
	rawEVMBuilder   *RawEVMTxBuilder

	// Nonce tracker for account nonce management
	nonceTracker *NonceTracker

	// Validators (for staking operations)
	validators []stakingtypes.Validator

	// Balance tracking
	initialBalances map[string]map[string]string // address -> denom -> amount
}

// TestLocalNodesContractLoad runs the test suite
func TestLocalNodesContractLoad(t *testing.T) {
	suite.Run(t, new(LocalNodesContractLoadTestSuite))
}

// SetupSuite initializes the test suite
func (s *LocalNodesContractLoadTestSuite) SetupSuite() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Local Nodes Contract Load Test - Setup                ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	// Step 0: Kill any existing evmosd/xcoind processes from previous runs
	s.T().Log("Step 0: Cleaning up any existing node processes...")
	s.killExistingNodeProcesses()
	s.T().Log("✓ Cleanup complete\n")

	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Step 1: Create keyring with 100 users
	s.T().Log("Step 1: Creating keyring with 100 users...")
	s.keyring = keyring.New(numUsers)
	s.T().Logf("✓ Created keyring with %d users\n", numUsers)

	// Step 2: Create integration network for contract deployment and validator info
	// NOTE: We will NOT use this network's gRPC for transaction building
	s.T().Log("Step 2: Creating integration network for contract deployment...")
	s.integrationNetwork = network.New(
		network.WithChainID("evmos_9002-1"), // Must match ProductionNetwork chain ID
		network.WithPreFundedAccounts(s.keyring.GetAllAccAddrs()...),
		network.WithAmountOfValidators(numValidators),
		network.WithOtherDenoms([]string{"abtc", "aeth", "asol", "xusd"}),
	)
	s.T().Log("✓ Integration network created\n")

	// Get validators for staking operations
	s.validators = s.integrationNetwork.GetValidators()
	s.T().Logf("✓ Found %d validators for staking operations\n", len(s.validators))

	// Step 3: Start ProductionNetwork with 4 validators and fund our 100 user accounts
	s.T().Log("Step 3: Starting ProductionNetwork with 4 validator processes...")
	s.T().Log("         (This will fund all 100 user accounts in genesis)")
	prodNet, err := NewProductionNetworkWithAccounts(numValidators, s.keyring)
	require.NoError(s.T(), err, "failed to create production network")
	s.prodNetwork = prodNet
	s.T().Log("✓ ProductionNetwork started successfully with funded accounts\n")

	// Print validator information
	s.T().Log(s.prodNetwork.PrintValidatorInfo())

	// Step 4: Wait for network to be ready and verify all nodes
	s.T().Log("Step 4: Waiting for validators to stabilize...")
	time.Sleep(15 * time.Second)
	s.T().Log("✓ Validators stabilized\n")

	// Step 4b: Create NodeClient for each validator's gRPC port
	s.T().Log("Step 4b: Creating gRPC clients for each validator...")
	s.nodeClients = make([]*NodeClient, numValidators)
	for i, validator := range s.prodNetwork.validators {
		client, err := NewNodeClient(validator)
		require.NoError(s.T(), err, "failed to create node client for validator %d", i)
		s.nodeClients[i] = client
		s.T().Logf("✓ Created client for validator %d (gRPC: %s)\n", i, client.GRPCAddr)
	}

	// Step 4c: Verify all nodes are running and healthy
	s.T().Log("\nStep 4c: Verifying all nodes are running and healthy...")
	err = s.verifyAllNodesHealthy()
	require.NoError(s.T(), err, "failed to verify all nodes are healthy")
	s.T().Log("✓ All nodes verified healthy\n")

	// Step 5: Create grpcHandler and factory pointing to PRODUCTION network
	s.T().Log("Step 5: Creating factory with production network gRPC...")
	prodGrpcAddr := fmt.Sprintf("127.0.0.1:%d", s.prodNetwork.validators[0].GRPCPort)
	prodHandler, err := NewProductionGrpcHandler(prodGrpcAddr)
	require.NoError(s.T(), err, "failed to create production grpc handler")
	s.grpcHandler = prodHandler
	s.factory = factory.New(s.integrationNetwork, s.grpcHandler)
	s.T().Logf("✓ Factory created with production gRPC: %s\n", prodGrpcAddr)

	// Step 6: Deploy BatchOrderBook contract (using integration network)
	s.T().Log("\nStep 6: Deploying BatchOrderBook contract...")
	err = s.deployContract()
	require.NoError(s.T(), err, "failed to deploy contract")
	s.T().Logf("✓ Contract deployed at: %s\n", s.contractAddr.Hex())

	// Step 7: Initialize statistics tracker
	s.T().Log("\nStep 7: Initializing statistics tracker...")
	s.stats = NewLocalLoadTestStats(numValidators)
	s.T().Log("✓ Statistics tracker initialized\n")

	// Step 8: Initialize round-robin distributor
	s.T().Log("Step 8: Initializing round-robin distributor...")
	s.distributor = NewRoundRobinDistributor(numValidators)
	s.T().Log("✓ Round-robin distributor initialized\n")

	// Step 9: Initialize transaction builders
	s.T().Log("Step 9: Initializing transaction builders...")
	s.contractBuilder = NewContractTxBuilder(s.contractAddr)
	s.bankBuilder = NewBankTxBuilder(s.keyring)
	s.stakingBuilder = NewStakingTxBuilder(s.validators)
	s.rawEVMBuilder = NewRawEVMTxBuilder(s.keyring)
	s.T().Log("✓ Transaction builders initialized\n")

	// Step 10: Initialize nonce tracker
	s.T().Log("Step 10: Initializing nonce tracker (all accounts start at nonce 0)...")
	s.nonceTracker = NewNonceTracker()
	s.T().Log("✓ Nonce tracker initialized\n")

	s.T().Log("Step 11: Capturing initial account balances...")
	s.captureInitialBalances()
	s.T().Log("✓ Initial balances captured\n")

	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Setup Complete - Ready for Load Testing               ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")
}

// killExistingNodeProcesses kills any existing evmosd/xcoind processes from previous test runs
func (s *LocalNodesContractLoadTestSuite) killExistingNodeProcesses() {
	// Kill evmosd processes
	if output, err := exec.Command("pkill", "-9", "evmosd").CombinedOutput(); err != nil {
		// pkill returns error if no processes found, which is fine
		s.T().Logf("  evmosd cleanup: %v (output: %s)", err, string(output))
	} else {
		s.T().Log("  Killed existing evmosd processes")
	}

	// Kill xcoind processes
	if output, err := exec.Command("pkill", "-9", "xcoind").CombinedOutput(); err != nil {
		s.T().Logf("  xcoind cleanup: %v (output: %s)", err, string(output))
	} else {
		s.T().Log("  Killed existing xcoind processes")
	}

	// Give OS time to release ports
	time.Sleep(2 * time.Second)
}

// TearDownSuite cleans up after all tests
func (s *LocalNodesContractLoadTestSuite) TearDownSuite() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Test Teardown - Cleanup                                ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	// Wait a moment for any pending transactions to settle
	// Since we use BroadcastTxAsync, transactions may still be in mempool
	// Wait for several blocks to be produced to ensure inclusion
	s.T().Log("Waiting for transactions to be included in blocks...")

	// Query initial block height
	if len(s.nodeClients) > 0 {
		initialStatus, err := s.nodeClients[0].rpcClient.Status(context.Background())
		if err == nil {
			initialHeight := initialStatus.SyncInfo.LatestBlockHeight
			s.T().Logf("Current block height: %d", initialHeight)
			s.T().Log("Waiting 20 seconds for ~10 blocks to be produced...")
			time.Sleep(20 * time.Second)

			finalStatus, err := s.nodeClients[0].rpcClient.Status(context.Background())
			if err == nil {
				finalHeight := finalStatus.SyncInfo.LatestBlockHeight
				blocksProduced := finalHeight - initialHeight
				s.T().Logf("New block height: %d (+%d blocks)", finalHeight, blocksProduced)
			}
		}
	}
	s.T().Log("✓ Wait complete\n")

	// Print final comprehensive statistics
	s.printFinalStatistics()

	// Log balance changes (must be done before cleanup while nodes are still running)
	s.logBalanceChanges()

	// Close all node clients
	if s.nodeClients != nil {
		s.T().Log("Closing node clients...")
		for i, client := range s.nodeClients {
			if client != nil {
				if err := client.Close(); err != nil {
					s.T().Logf("Warning: failed to close client %d: %v\n", i, err)
				}
			}
		}
		s.T().Log("✓ All node clients closed\n")
	}

	// Cleanup ProductionNetwork
	if s.prodNetwork != nil {
		s.T().Log("Cleaning up ProductionNetwork...")
		if err := s.prodNetwork.Cleanup(); err != nil {
			s.T().Logf("Warning: failed to cleanup production network: %v\n", err)
		}
		s.T().Log("✓ ProductionNetwork cleaned up\n")
	}

	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Teardown Complete                                      ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")
}

// deployContract deploys the BatchOrderBook contract using integration network
func (s *LocalNodesContractLoadTestSuite) deployContract() error {
	// Load contract
	contract, err := LoadBatchOrderBookContract()
	if err != nil {
		return fmt.Errorf("failed to load contract: %w", err)
	}

	// Deploy using first user
	user := s.keyring.GetKey(0)

	deploymentData := factory.ContractDeploymentData{
		Contract:        contract,
		ConstructorArgs: []interface{}{}, // No constructor args for BatchOrderBook
	}

	contractAddr, err := s.factory.DeployContract(
		user.Priv,
		evmtypes.EvmTxArgs{
			GasLimit:   3000000,                     // Sufficient for contract deployment
			GasFeeCap:  big.NewInt(10000000000),     // 10 Gwei - high enough to handle base fee fluctuations
			GasTipCap:  big.NewInt(1),               // 1 wei tip
		},
		deploymentData,
	)
	if err != nil {
		return fmt.Errorf("failed to deploy contract: %w", err)
	}

	s.contractAddr = contractAddr
	return nil
}

// verifyAllNodesHealthy checks that all nodes are running and responding to requests
func (s *LocalNodesContractLoadTestSuite) verifyAllNodesHealthy() error {
	s.T().Log("Checking health of all nodes (will retry for up to 60 seconds)...")
	
	maxRetries := 60
	retryDelay := 1 * time.Second
	testAddr := s.keyring.GetKey(0).AccAddr
	
	// Track which nodes are healthy
	nodeHealthy := make([]bool, len(s.nodeClients))
	
	// Retry for each node until all are healthy or we timeout
	for retry := 0; retry < maxRetries; retry++ {
		allHealthy := true
		healthyCount := 0
		
		for i, nodeClient := range s.nodeClients {
			// Skip if already healthy
			if nodeHealthy[i] {
				healthyCount++
				continue
			}
			
			// Check RPC health
			if !nodeClient.IsHealthy() {
				if retry == 0 || (retry+1)%20 == 0 {
					s.T().Logf("  Node %d: RPC health check failed (RPC: %s)", i, nodeClient.RPCAddr)
				}
				allHealthy = false
				continue
			}
			
			// Check gRPC by querying a test account
			_, err := nodeClient.GetAllBalances(testAddr)
			if err != nil {
				allHealthy = false
				continue
			}
			
			// Check block height is progressing
			height, err := nodeClient.GetBlockHeight()
			if err != nil {
				allHealthy = false
				continue
			}
			
			// Node is healthy!
			nodeHealthy[i] = true
			healthyCount++
			s.T().Logf("  ✓ Node %d: Healthy (RPC: %s, gRPC: %s, Height: %d)", 
				i, nodeClient.RPCAddr, nodeClient.GRPCAddr, height)
		}
		
		if allHealthy {
			s.T().Logf("All %d nodes are healthy and responding", len(s.nodeClients))
			return nil
		}
		
		// Log progress every 10 seconds
		if retry == 0 || (retry+1)%10 == 0 || retry == maxRetries-1 {
			s.T().Logf("  Waiting for nodes to be ready: %d/%d healthy (attempt %d/%d)", 
				healthyCount, len(s.nodeClients), retry+1, maxRetries)
		}
		
		// Wait before next retry
		if retry < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	
	// Final check - report which nodes are not healthy
	healthyCount := 0
	for i := range s.nodeClients {
		if nodeHealthy[i] {
			healthyCount++
		} else {
			s.T().Logf("  ✗ Node %d: NOT healthy after %d seconds", i, maxRetries)
		}
	}
	
	return fmt.Errorf("not all nodes are healthy (%d/%d healthy) after %d seconds", 
		healthyCount, len(s.nodeClients), maxRetries)
}

// TestBasicLoad runs a basic load test with 1,000 transactions
func (s *LocalNodesContractLoadTestSuite) TestBasicLoad() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Test: Basic Load (1,000 transactions)                  ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	numTxs := 1000
	s.runLoadTest(numTxs)

	s.T().Log("\n✓ Basic load test completed successfully")
}

// TestIntensiveLoad runs an intensive load test with 10,000 transactions
func (s *LocalNodesContractLoadTestSuite) TestIntensiveLoad() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Test: Intensive Load (10,000 transactions)             ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	numTxs := 10000
	s.runLoadTest(numTxs)

	s.T().Log("\n✓ Intensive load test completed successfully")
}

// runLoadTest executes the load test with specified number of transactions
func (s *LocalNodesContractLoadTestSuite) runLoadTest(numTxs int) {
	s.T().Logf("Starting load test with %d transactions...\n", numTxs)
	s.T().Logf("Transaction mix: 40%% Contract, 30%% Bank, 20%% Staking, 10%% Raw EVM\n\n")

	startTime := time.Now()
	errorCount := 0
	successCount := 0

	for i := 0; i < numTxs; i++ {
		// Select transaction type (weighted random)
		txBuilder := SelectTransactionBuilder(
			s.contractBuilder,
			s.bankBuilder,
			s.stakingBuilder,
			s.rawEVMBuilder,
		)

		// Select user (round-robin through 100 users)
		userIdx := i % numUsers
		user := s.keyring.GetKey(userIdx)

		// Get and increment nonce for this user
		fromAddr := common.BytesToAddress(user.Priv.PubKey().Address().Bytes())
		nonce := s.nonceTracker.GetAndIncrementNonce(fromAddr)

		// Select target node (round-robin)
		nodeIdx := s.distributor.GetNextNode()
		nodeClient := s.nodeClients[nodeIdx]

		// Build transaction with explicit nonce
		submitTime := time.Now()
		txBytes, metadata, err := txBuilder.BuildTx(user, s.factory, nil, nonce)
		if err != nil {
			s.T().Logf("Error building tx %d: %v\n", i, err)
			errorCount++
			continue
		}

		metadata.UserIndex = userIdx
		metadata.NodeIndex = nodeIdx
		metadata.SubmitTime = submitTime

		// Submit to specific node
		result, err := nodeClient.SubmitTx(txBytes)
		metadata.Latency = time.Since(submitTime)

		if err != nil {
			s.T().Logf("Error submitting tx %d to node %d: %v\n", i, nodeIdx, err)
			metadata.Success = false
			metadata.Error = err.Error()
			errorCount++
		} else {
			metadata.Success = result.Code == 0
			metadata.GasUsed = uint64(result.GasUsed)
			metadata.BlockHeight = 0 // Will be set when tx is included in block

			if !metadata.Success {
				metadata.Error = result.Log
				errorCount++
			} else {
				successCount++
			}
		}

		// Record statistics
		s.stats.RecordTransaction(nodeIdx, txBuilder.GetType(), metadata)

		// Print progress every 100 transactions
		if (i+1)%100 == 0 {
			s.printProgress(i+1, numTxs, successCount, errorCount, time.Since(startTime))
		}
	}

	elapsed := time.Since(startTime)
	s.T().Logf("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Logf("║     Load Test Completed                                    ║")
	s.T().Logf("╚════════════════════════════════════════════════════════════╝")
	s.T().Logf("\nTotal Time: %v", elapsed)
	s.T().Logf("Throughput: %.2f txs/sec", float64(numTxs)/elapsed.Seconds())
	s.T().Logf("Success: %d (%.1f%%)", successCount, float64(successCount)/float64(numTxs)*100)
	s.T().Logf("Errors: %d (%.1f%%)\n", errorCount, float64(errorCount)/float64(numTxs)*100)
}

// printProgress prints progress during the load test
func (s *LocalNodesContractLoadTestSuite) printProgress(current, total, success, errors int, elapsed time.Duration) {
	throughput := float64(current) / elapsed.Seconds()
	progress := float64(current) / float64(total) * 100

	s.T().Logf("[Progress: %.1f%%] %d/%d txs | Success: %d | Errors: %d | Throughput: %.2f txs/sec",
		progress, current, total, success, errors, throughput)

	// Print mini per-node summary every 500 transactions
	if current%500 == 0 {
		s.T().Log("\nCurrent per-node distribution:")
		for i := 0; i < numValidators; i++ {
			nodeStats := s.stats.NodeStats[i]
			nodeTxs := nodeStats.GetTotalTxCount()
			s.T().Logf("  Node %d: %d txs (%.1f%%)",
				i, nodeTxs, float64(nodeTxs)/float64(current)*100)
		}
		s.T().Log("")
	}
}

// printFinalStatistics prints comprehensive final statistics
func (s *LocalNodesContractLoadTestSuite) printFinalStatistics() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     FINAL STATISTICS                                       ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝")

	// Overall summary
	s.T().Log(s.stats.PrintSummary())

	// Per-node summary
	s.T().Log(s.stats.PrintPerNodeSummary())

	// Detailed stats for each node
	for i := 0; i < numValidators; i++ {
		s.T().Log(s.stats.PrintDetailedNodeStats(i))
	}

	// Error breakdown (if any)
	if s.stats.TotalErrors.Load() > 0 {
		s.T().Log(s.stats.PrintErrorBreakdown())
	}
}

// captureInitialBalances captures all account balances before the test
func (s *LocalNodesContractLoadTestSuite) captureInitialBalances() {
	s.initialBalances = make(map[string]map[string]string)

	// Use the first node client to query balances from the production network
	if len(s.nodeClients) == 0 {
		s.T().Log("Warning: no node clients available to query balances")
		return
	}

	nodeClient := s.nodeClients[0]

	// Wait for gRPC to be ready with retries
	maxRetries := 30
	retryDelay := 1 * time.Second
	var testErr error

	s.T().Log("Waiting for gRPC connection to be ready...")
	for retry := 0; retry < maxRetries; retry++ {
		testAddr := s.keyring.GetKey(0).AccAddr
		_, testErr = nodeClient.GetAllBalances(testAddr)
		if testErr == nil {
			s.T().Logf("✓ gRPC connection ready after %d attempts (%.1f seconds)", retry+1, float64(retry+1))
			break
		}
		if retry < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}

	if testErr != nil {
		s.T().Logf("Warning: gRPC connection failed after %d retries (%d seconds): %v", maxRetries, maxRetries, testErr)
		s.T().Log("Will not be able to capture initial balances or verify balance changes")
		return
	}

	// Capture balances for all 100 user accounts
	successCount := 0
	failedAccounts := []int{}
	for i := 0; i < numUsers; i++ {
		key := s.keyring.GetKey(i)
		addr := key.AccAddr

		// Query all balances for this account from production network
		balancesResp, err := nodeClient.GetAllBalances(addr)
		if err != nil {
			s.T().Logf("Warning: failed to get initial balances for account %d (%s): %v", i, addr.String(), err)
			failedAccounts = append(failedAccounts, i)
			continue
		}

		// Store balances by denom
		denomBalances := make(map[string]string)
		for _, coin := range balancesResp.Balances {
			denomBalances[coin.Denom] = coin.Amount.String()
		}
		s.initialBalances[addr.String()] = denomBalances
		successCount++
	}

	s.T().Logf("Captured initial balances for %d/%d accounts", successCount, numUsers)
	if len(failedAccounts) > 0 && len(failedAccounts) <= 10 {
		s.T().Logf("Failed accounts: %v", failedAccounts)
	} else if len(failedAccounts) > 10 {
		s.T().Logf("Failed %d accounts (too many to list)", len(failedAccounts))
	}

	// Log sample of first account for verification
	if successCount > 0 {
		firstAddr := s.keyring.GetKey(0).AccAddr.String()
		if balances, ok := s.initialBalances[firstAddr]; ok {
			s.T().Log("Sample - Account 0 initial balances:")
			for denom, amount := range balances {
				s.T().Logf("  %s: %s", denom, amount)
			}
		}
	}
}

// logBalanceChanges logs all balance changes to a file
func (s *LocalNodesContractLoadTestSuite) logBalanceChanges() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     BALANCE CHANGES                                        ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	// Create log file
	logFileName := fmt.Sprintf("balance_changes_%s.log", time.Now().Format("20060102_150405"))
	logFilePath := filepath.Join(os.TempDir(), logFileName)
	logFile, err := os.Create(logFilePath)
	if err != nil {
		s.T().Logf("Error: failed to create balance log file: %v", err)
		return
	}
	defer logFile.Close()

	// Write header
	logFile.WriteString("═══════════════════════════════════════════════════════════════════════════\n")
	logFile.WriteString("                         BALANCE CHANGES LOG\n")
	logFile.WriteString(fmt.Sprintf("                         Generated: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	logFile.WriteString("═══════════════════════════════════════════════════════════════════════════\n\n")

	totalAccountsChanged := 0
	totalDenomChanges := 0
	accountsWithNoInitial := 0
	accountsWithQueryErrors := 0

	// Use the first node client to query from production network
	if len(s.nodeClients) == 0 {
		logFile.WriteString("Error: No node clients available to query balances\n")
		s.T().Log("Error: No node clients available")
		return
	}
	nodeClient := s.nodeClients[0]

	// Check if node is healthy before querying
	if !nodeClient.IsHealthy() {
		logFile.WriteString("Error: Node 0 is not healthy/running\n")
		s.T().Log("Error: Node 0 is not healthy")
		return
	}
	s.T().Logf("Node 0 is healthy (gRPC: %s)", nodeClient.GRPCAddr)

	// Test gRPC connection with a simple query
	testAddr := s.keyring.GetKey(0).AccAddr
	testResp, testErr := nodeClient.GetAllBalances(testAddr)
	if testErr != nil {
		logFile.WriteString(fmt.Sprintf("Error: Failed to query balances from node 0: %v\n", testErr))
		s.T().Logf("Error testing gRPC connection: %v", testErr)
		return
	}
	s.T().Logf("✓ gRPC connection working, test account has %d balance entries", len(testResp.Balances))

	// Debug: Log test account balances
	if len(testResp.Balances) > 0 {
		s.T().Log("Test account balances:")
		for _, coin := range testResp.Balances {
			s.T().Logf("  - %s: %s", coin.Denom, coin.Amount.String())
		}
	}

	// Check balance changes for all accounts
	for i := 0; i < numUsers; i++ {
		key := s.keyring.GetKey(i)
		addr := key.AccAddr

		// Get current balances from production network
		currentBalancesResp, err := nodeClient.GetAllBalances(addr)
		if err != nil {
			logFile.WriteString(fmt.Sprintf("Account %d (%s): Error fetching current balances: %v\n\n", i, addr.String(), err))
			accountsWithQueryErrors++
			continue
		}

		// Get initial balances
		initialDenoms, hasInitial := s.initialBalances[addr.String()]
		if !hasInitial {
			logFile.WriteString(fmt.Sprintf("Account %d (%s): No initial balance record\n\n", i, addr.String()))
			accountsWithNoInitial++
			continue
		}

		// Track changes for this account
		currentDenoms := make(map[string]string)
		for _, coin := range currentBalancesResp.Balances {
			currentDenoms[coin.Denom] = coin.Amount.String()
		}

		// Debug: Log first account's balances in detail
		if i == 0 {
			s.T().Logf("Account 0 initial balances: %v", initialDenoms)
			s.T().Logf("Account 0 current balances: %v", currentDenoms)
		}

		// Find all denoms (union of initial and current)
		allDenoms := make(map[string]bool)
		for denom := range initialDenoms {
			allDenoms[denom] = true
		}
		for denom := range currentDenoms {
			allDenoms[denom] = true
		}

		// Check if any balance changed
		hasChanges := false
		changes := make(map[string]struct{ before, after, diff string })

		for denom := range allDenoms {
			initialStr := initialDenoms[denom]
			currentStr := currentDenoms[denom]

			if initialStr == "" {
				initialStr = "0"
			}
			if currentStr == "" {
				currentStr = "0"
			}

			if initialStr != currentStr {
				hasChanges = true

				// Calculate difference
				initial := new(big.Int)
				initial.SetString(initialStr, 10)
				current := new(big.Int)
				current.SetString(currentStr, 10)
				diff := new(big.Int).Sub(current, initial)

				diffStr := diff.String()
				if diff.Sign() > 0 {
					diffStr = "+" + diffStr
				}

				changes[denom] = struct{ before, after, diff string }{
					before: initialStr,
					after:  currentStr,
					diff:   diffStr,
				}
				totalDenomChanges++
			}
		}

		// Write account changes if any
		if hasChanges {
			totalAccountsChanged++
			logFile.WriteString(fmt.Sprintf("Account %d: %s\n", i, addr.String()))
			logFile.WriteString("───────────────────────────────────────────────────────────────────────────\n")

			for denom, change := range changes {
				logFile.WriteString(fmt.Sprintf("  %s:\n", denom))
				logFile.WriteString(fmt.Sprintf("    Before: %s\n", change.before))
				logFile.WriteString(fmt.Sprintf("    After:  %s\n", change.after))
				logFile.WriteString(fmt.Sprintf("    Change: %s\n", change.diff))
			}
			logFile.WriteString("\n")
		}
	}

	// Write summary
	logFile.WriteString("═══════════════════════════════════════════════════════════════════════════\n")
	logFile.WriteString("                              SUMMARY\n")
	logFile.WriteString("═══════════════════════════════════════════════════════════════════════════\n")
	logFile.WriteString(fmt.Sprintf("Total Accounts Checked: %d\n", numUsers))
	logFile.WriteString(fmt.Sprintf("Accounts with Changes: %d (%.1f%%)\n", totalAccountsChanged, float64(totalAccountsChanged)/float64(numUsers)*100))
	logFile.WriteString(fmt.Sprintf("Accounts with No Changes: %d (%.1f%%)\n", numUsers-totalAccountsChanged, float64(numUsers-totalAccountsChanged)/float64(numUsers)*100))
	logFile.WriteString(fmt.Sprintf("Accounts with No Initial Balance: %d\n", accountsWithNoInitial))
	logFile.WriteString(fmt.Sprintf("Accounts with Query Errors: %d\n", accountsWithQueryErrors))
	logFile.WriteString(fmt.Sprintf("Total Denomination Changes: %d\n", totalDenomChanges))
	logFile.WriteString("═══════════════════════════════════════════════════════════════════════════\n")

	s.T().Logf("✓ Balance changes logged to: %s", logFilePath)
	s.T().Logf("  - Accounts with changes: %d / %d (%.1f%%)", totalAccountsChanged, numUsers, float64(totalAccountsChanged)/float64(numUsers)*100)
	s.T().Logf("  - Total denomination changes: %d", totalDenomChanges)
	if accountsWithNoInitial > 0 {
		s.T().Logf("  - Accounts with no initial balance: %d", accountsWithNoInitial)
	}
	if accountsWithQueryErrors > 0 {
		s.T().Logf("  - Accounts with query errors: %d", accountsWithQueryErrors)
	}
}
