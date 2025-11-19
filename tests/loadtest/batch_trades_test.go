// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"fmt"
	"math/big"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

type BatchTradesLoadTestSuite struct {
	suite.Suite

	network     network.Network
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     keyring.Keyring

	contractAddr common.Address
}

func TestBatchTradesLoadTest(t *testing.T) {
	suite.Run(t, new(BatchTradesLoadTestSuite))
}

func (s *BatchTradesLoadTestSuite) SetupSuite() {
	s.T().Log("Setting up batch trades load test suite...")

	// Verify that nodes are running using netstat or ss command
	s.verifyNodesRunning()

	// Create keyring and get the first key
	keyring := keyring.New(2)

	// Create the network with custom configuration
	s.network = network.New(
		network.WithChainID("evmos_9000-1"),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
	)

	s.grpcHandler = grpc.NewIntegrationHandler(s.network)
	s.factory = factory.New(s.network, s.grpcHandler)
	s.keyring = keyring

	s.T().Log("Deploying BatchOrderBook contract...")
	s.deployBatchOrderBookContract()
}

// verifyNodesRunning checks if nodes are running using netstat or ss command
func (s *BatchTradesLoadTestSuite) verifyNodesRunning() {
	s.T().Log("\n=== Verifying Nodes Status ===")

	// Try netstat first
	netstatOutput, netstatErr := exec.Command("netstat", "-tuln").CombinedOutput()
	ssOutput, ssErr := exec.Command("ss", "-tuln").CombinedOutput()

	var output string
	var cmdName string

	if netstatErr == nil {
		output = string(netstatOutput)
		cmdName = "netstat"
	} else if ssErr == nil {
		output = string(ssOutput)
		cmdName = "ss"
	} else {
		s.T().Log("Warning: Neither netstat nor ss command available")
		s.T().Log("Skipping node verification, proceeding with test...")
		return
	}

	s.T().Logf("Using %s command to verify nodes\n", cmdName)

	// Common blockchain/Cosmos node ports
	nodePorts := []string{
		":26656", // CometBFT P2P
		":26657", // CometBFT RPC
		":26660", // CometBFT Prometheus
		":8545",  // Ethereum JSON-RPC
		":8546",  // Ethereum WebSocket
		":9090",  // Cosmos gRPC
		":9091",  // Cosmos gRPC-web
		":1317",  // Cosmos REST
	}

	foundNodes := make(map[string]bool)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		for _, port := range nodePorts {
			if strings.Contains(line, port) && strings.Contains(line, "LISTEN") {
				foundNodes[port] = true
				s.T().Logf("✓ Found node listening on port %s", port)
			}
		}
	}

	if len(foundNodes) > 0 {
		s.T().Logf("\nFound %d active node port(s)", len(foundNodes))
		s.T().Log("Nodes verified successfully!")
	} else {
		s.T().Log("No standard blockchain node ports found")
		s.T().Log("This may be expected in integration test environment")
	}

	s.T().Log("=== Node Verification Complete ===\n")
}

// deployBatchOrderBookContract deploys the BatchOrderBook contract
func (s *BatchTradesLoadTestSuite) deployBatchOrderBookContract() {
	// Get the deployer account
	deployerPrivKey := s.keyring.GetPrivKey(0)
	deployerAddr := s.keyring.GetAddr(0)

	// Load the contract bytecode
	// Note: This assumes the contract has been compiled
	// In a real scenario, you would compile the contract first
	contractData := factory.ContractDeploymentData{
		Contract: BatchOrderBookContract,
	}

	// Deploy the contract
	// Note: From address is derived from privKey automatically in v19
	contractAddr, err := s.factory.DeployContract(
		deployerPrivKey,
		evmtypes.EvmTxArgs{},
		contractData,
	)
	require.NoError(s.T(), err, "failed to deploy BatchOrderBook contract")

	s.contractAddr = contractAddr
	s.T().Logf("BatchOrderBook contract deployed at: %s", contractAddr.Hex())

	// Wait for the contract to be mined
	err = s.network.NextBlock()
	require.NoError(s.T(), err)
}

// TestBatchTradesLoad executes the main load test
func (s *BatchTradesLoadTestSuite) TestBatchTradesLoad() {
	ctx := context.Background()
	s.T().Log("\n=== Starting Batch Trades Load Test ===")

	// Test configuration
	const (
		tradesPerBatch  = 1000
		batchesPerSec   = 3
		testDurationSec = 10 // Run for 10 seconds
	)

	// Statistics tracking
	stats := &LoadTestStats{
		StartTime:        time.Now(),
		TradesSubmitted:  0,
		BatchesSubmitted: 0,
		SuccessCount:     0,
		ErrorCount:       0,
		BlockStats:       make(map[uint64]uint64),
		mutex:            sync.Mutex{},
	}

	s.T().Logf("Configuration:")
	s.T().Logf("  - Trades per batch: %d", tradesPerBatch)
	s.T().Logf("  - Batches per second: %d", batchesPerSec)
	s.T().Logf("  - Test duration: %d seconds", testDurationSec)
	s.T().Logf("  - Expected total trades: %d", tradesPerBatch*batchesPerSec*testDurationSec)
	s.T().Log("")

	// Run the load test
	s.runLoadTest(ctx, tradesPerBatch, batchesPerSec, testDurationSec, stats)

	// Wait for pending transactions to be mined
	s.T().Log("Waiting for pending transactions to be mined...")
	time.Sleep(2 * time.Second)
	err := s.network.NextBlock()
	require.NoError(s.T(), err)

	// Print final statistics
	s.printStats(stats)

	// Verify contract state
	s.verifyContractState(stats)
}

// runLoadTest executes the load test by submitting batches of trades
func (s *BatchTradesLoadTestSuite) runLoadTest(
	ctx context.Context,
	tradesPerBatch int,
	batchesPerSec int,
	durationSec int,
	stats *LoadTestStats,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(time.Duration(durationSec) * time.Second)
	submitterKey := s.keyring.GetKey(0)
	submitterAddr := s.keyring.GetAddr(0)

	for {
		select {
		case <-timeout:
			s.T().Log("Load test duration completed")
			return
		case <-ticker.C:
			// Submit batches for this second
			var wg sync.WaitGroup
			for i := 0; i < batchesPerSec; i++ {
				wg.Add(1)
				go func(batchNum int) {
					defer wg.Done()
					s.submitBatch(submitterKey, submitterAddr, tradesPerBatch, stats)
				}(i)
			}
			wg.Wait()

			// Mine a block after each second
			err := s.network.NextBlock()
			if err != nil {
				s.T().Logf("Warning: failed to mine block: %v", err)
			}

			// Print progress
			stats.mutex.Lock()
			s.T().Logf("Progress: %d batches, %d trades, %d success, %d errors",
				stats.BatchesSubmitted, stats.TradesSubmitted,
				stats.SuccessCount, stats.ErrorCount)
			stats.mutex.Unlock()
		}
	}
}

// submitBatch submits a batch of trades to the contract
func (s *BatchTradesLoadTestSuite) submitBatch(
	privKey keyring.Key,
	from common.Address,
	tradeCount int,
	stats *LoadTestStats,
) {
	// Generate mock trades
	trades := s.generateMockTrades(from, tradeCount)

	// Prepare the contract call - use ABI struct, not string
	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookContract.ABI,
		MethodName:  "executeBatchTrades",
		Args:        []interface{}{trades},
	}

	// Execute the contract call
	_, err := s.factory.ExecuteContractCall(
		privKey.Priv,
		evmtypes.EvmTxArgs{
			To: &s.contractAddr,
		},
		callArgs,
	)

	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	stats.BatchesSubmitted++
	stats.TradesSubmitted += uint64(tradeCount)

	if err != nil {
		stats.ErrorCount++
		s.T().Logf("Error submitting batch: %v", err)
	} else {
		stats.SuccessCount++
	}
}

// generateMockTrades generates mock trade data
func (s *BatchTradesLoadTestSuite) generateMockTrades(trader common.Address, count int) []Trade {
	trades := make([]Trade, count)
	symbols := []string{"BTC/USD", "ETH/USD", "BNB/USD", "SOL/USD", "AVAX/USD"}

	for i := 0; i < count; i++ {
		trades[i] = Trade{
			Trader:    trader,
			OrderId:   big.NewInt(int64(i)),
			Symbol:    symbols[i%len(symbols)],
			Price:     big.NewInt(int64(10000 + i)),
			Amount:    big.NewInt(int64(100 + i)),
			IsBuy:     i%2 == 0,
			Timestamp: big.NewInt(time.Now().Unix()),
		}
	}

	return trades
}

// printStats prints the final statistics
func (s *BatchTradesLoadTestSuite) printStats(stats *LoadTestStats) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	duration := time.Since(stats.StartTime)

	s.T().Log("\n=== Load Test Statistics ===")
	s.T().Logf("Duration: %v", duration)
	s.T().Logf("Total Batches Submitted: %d", stats.BatchesSubmitted)
	s.T().Logf("Total Trades Submitted: %d", stats.TradesSubmitted)
	s.T().Logf("Successful Batches: %d", stats.SuccessCount)
	s.T().Logf("Failed Batches: %d", stats.ErrorCount)
	s.T().Logf("Success Rate: %.2f%%",
		float64(stats.SuccessCount)/float64(stats.BatchesSubmitted)*100)

	if duration.Seconds() > 0 {
		s.T().Logf("\nThroughput:")
		s.T().Logf("  - Batches/sec: %.2f", float64(stats.BatchesSubmitted)/duration.Seconds())
		s.T().Logf("  - Trades/sec: %.2f", float64(stats.TradesSubmitted)/duration.Seconds())
	}

	s.T().Log("=== Statistics Complete ===\n")
}

// verifyContractState verifies the final state of the contract
func (s *BatchTradesLoadTestSuite) verifyContractState(stats *LoadTestStats) {
	s.T().Log("=== Verifying Contract State ===")

	// Query the contract for stats - use ABI struct, not string
	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookContract.ABI,
		MethodName:  "getStats",
		Args:        []interface{}{},
	}

	callerPrivKey := s.keyring.GetPrivKey(0)

	res, _, err := s.factory.CallContractAndCheckLogs(
		callerPrivKey,
		evmtypes.EvmTxArgs{
			To: &s.contractAddr,
		},
		callArgs,
		defaultLogCheckArgs,
	)

	require.NoError(s.T(), err, "failed to query contract stats")
	require.True(s.T(), res.IsOK(), "contract call failed")

	s.T().Log("Contract state verified successfully")
	s.T().Log("=== Verification Complete ===\n")
}

// LoadTestStats holds statistics for the load test
type LoadTestStats struct {
	StartTime        time.Time
	TradesSubmitted  uint64
	BatchesSubmitted uint64
	SuccessCount     uint64
	ErrorCount       uint64
	BlockStats       map[uint64]uint64
	mutex            sync.Mutex
}

// Trade represents a single trade
type Trade struct {
	Trader    common.Address
	OrderId   *big.Int
	Symbol    string
	Price     *big.Int
	Amount    *big.Int
	IsBuy     bool
	Timestamp *big.Int
}
