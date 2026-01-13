// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"fmt"
	"math/rand"
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

	// Validators (for staking operations)
	validators []stakingtypes.Validator
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

	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Step 1: Create keyring with 100 users
	s.T().Log("Step 1: Creating keyring with 100 users...")
	s.keyring = keyring.New(numUsers)
	s.T().Logf("✓ Created keyring with %d users\n", numUsers)

	// Step 2: Create integration network for transaction building
	// This network is used only for building transactions, not for execution
	s.T().Log("Step 2: Creating integration network for transaction utilities...")
	s.integrationNetwork = network.New(
		network.WithChainID("evmos_9002-1"), // Must match ProductionNetwork chain ID
		network.WithPreFundedAccounts(s.keyring.GetAllAccAddrs()...),
		network.WithAmountOfValidators(numValidators),
		network.WithOtherDenoms([]string{"abtc", "aeth", "asol", "xusd"}),
	)
	s.grpcHandler = grpc.NewIntegrationHandler(s.integrationNetwork)
	s.factory = factory.New(s.integrationNetwork, s.grpcHandler)
	s.T().Log("✓ Integration network created for transaction building\n")

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

	// Step 4: Wait for network to be ready
	s.T().Log("Step 4: Waiting for validators to stabilize...")
	time.Sleep(5 * time.Second)
	s.T().Log("✓ Validators stabilized\n")

	// Step 5: Deploy BatchOrderBook contract (using integration network)
	s.T().Log("Step 5: Deploying BatchOrderBook contract...")
	err = s.deployContract()
	require.NoError(s.T(), err, "failed to deploy contract")
	s.T().Logf("✓ Contract deployed at: %s\n", s.contractAddr.Hex())

	// Step 6: Create NodeClient for each validator's gRPC port
	s.T().Log("Step 6: Creating gRPC clients for each validator...")
	s.nodeClients = make([]*NodeClient, numValidators)
	for i, validator := range s.prodNetwork.validators {
		client, err := NewNodeClient(validator)
		require.NoError(s.T(), err, "failed to create node client for validator %d", i)
		s.nodeClients[i] = client
		s.T().Logf("✓ Created client for validator %d (gRPC: %s)\n", i, client.GRPCAddr)
	}

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

	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Setup Complete - Ready for Load Testing               ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")
}

// TearDownSuite cleans up after all tests
func (s *LocalNodesContractLoadTestSuite) TearDownSuite() {
	s.T().Log("\n╔════════════════════════════════════════════════════════════╗")
	s.T().Log("║     Test Teardown - Cleanup                                ║")
	s.T().Log("╚════════════════════════════════════════════════════════════╝\n")

	// Print final comprehensive statistics
	s.printFinalStatistics()

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
			GasLimit: 3000000, // Sufficient for contract deployment
		},
		deploymentData,
	)
	if err != nil {
		return fmt.Errorf("failed to deploy contract: %w", err)
	}

	s.contractAddr = contractAddr
	return nil
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

		// Select target node (round-robin)
		nodeIdx := s.distributor.GetNextNode()
		nodeClient := s.nodeClients[nodeIdx]

		// Build transaction
		submitTime := time.Now()
		txBytes, metadata, err := txBuilder.BuildTx(user, s.factory, nil)
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
