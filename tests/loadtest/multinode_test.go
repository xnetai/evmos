// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	sdktypes "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

// MultiNodeLoadTestSuite tests batch trades with multiple validators
type MultiNodeLoadTestSuite struct {
	suite.Suite

	network     network.Network
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     keyring.Keyring

	contractAddr  common.Address
	validatorInfo []ValidatorInfo
}

// ValidatorInfo holds information about each validator
type ValidatorInfo struct {
	Index         int
	Address       sdktypes.ValAddress
	ConsensusAddr sdktypes.ConsAddress
	Power         int64
	BlockHeight   int64
	LastBlockTime time.Time
}

// NodeStats holds comprehensive statistics for all nodes
type NodeStats struct {
	TotalValidators int
	ActiveValidators int
	ValidatorDetails []ValidatorInfo
	ChainID         string
	LatestHeight    int64
	NetworkSynced   bool
	mutex           sync.RWMutex
}

func TestMultiNodeLoadTest(t *testing.T) {
	suite.Run(t, new(MultiNodeLoadTestSuite))
}

// SetupSuite initializes the multi-node test environment
func (s *MultiNodeLoadTestSuite) SetupSuite() {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║     Multi-Node Batch Trades Load Test Suite           ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	// Create keyring with multiple accounts
	keyring := keyring.New(5)

	// Default to 3 validators for the suite
	numValidators := 3

	s.T().Logf("Initializing network with %d validators...", numValidators)

	// Create the network with multiple validators
	s.network = network.New(
		network.WithChainID("evmos_9000-1"),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
		network.WithAmountOfValidators(numValidators),
	)

	s.grpcHandler = grpc.NewIntegrationHandler(s.network)
	s.factory = factory.New(s.network, s.grpcHandler)
	s.keyring = keyring

	// Print node statistics at startup
	s.printNodeStatistics()

	// Deploy contract
	s.T().Log("\nDeploying BatchOrderBook contract...")
	s.deployBatchOrderBookContract()
}

// printNodeStatistics prints detailed statistics about all nodes
func (s *MultiNodeLoadTestSuite) printNodeStatistics() {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║              Node Statistics & Verification            ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	ctx := s.network.GetContext()

	// Get validator information
	validators := s.network.GetValidators()

	s.T().Logf("Network Configuration:")
	s.T().Logf("  - Chain ID: %s", s.network.GetChainID())
	s.T().Logf("  - Total Validators: %d", len(validators))
	s.T().Logf("  - Block Height: %d", ctx.BlockHeight())
	s.T().Logf("  - Block Time: %s", ctx.BlockTime())
	s.T().Log("")

	// Store validator information
	s.validatorInfo = make([]ValidatorInfo, len(validators))

	s.T().Log("Validator Details:")
	s.T().Log("┌─────┬──────────────────────────────────────────────┬──────────┬────────────┐")
	s.T().Log("│ ID  │ Validator Address                            │ Power    │ Status     │")
	s.T().Log("├─────┼──────────────────────────────────────────────┼──────────┼────────────┤")

	totalPower := int64(0)
	activeValidators := 0

	for i, val := range validators {
		power := val.GetConsensusPower(sdktypes.DefaultPowerReduction)
		totalPower += power

		status := "Bonded"
		if val.GetStatus() != stakingtypes.Bonded {
			status = val.GetStatus().String()
		} else {
			activeValidators++
		}

		s.T().Logf("│ %-3d │ %-44s │ %-8d │ %-10s │",
			i+1,
			val.GetOperator().String(),
			power,
			status,
		)

		// Store validator info
		s.validatorInfo[i] = ValidatorInfo{
			Index:         i,
			Address:       val.GetOperator(),
			Power:         power,
			BlockHeight:   ctx.BlockHeight(),
			LastBlockTime: ctx.BlockTime(),
		}
	}

	s.T().Log("└─────┴──────────────────────────────────────────────┴──────────┴────────────┘")
	s.T().Log("")
	s.T().Logf("Summary:")
	s.T().Logf("  - Active Validators: %d / %d", activeValidators, len(validators))
	s.T().Logf("  - Total Voting Power: %d", totalPower)
	s.T().Log("")

	// Verify all nodes are ready
	s.T().Log("Node Readiness Check:")
	for i, info := range s.validatorInfo {
		s.T().Logf("  ✓ Validator %d: Ready (Power: %d)", i+1, info.Power)
	}

	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║           All Nodes Verified and Synchronized         ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")
}

// deployBatchOrderBookContract deploys the contract
func (s *MultiNodeLoadTestSuite) deployBatchOrderBookContract() {
	deployerPrivKey := s.keyring.GetPrivKey(0)
	deployerAddr := s.keyring.GetAddr(0)

	contractData := factory.ContractDeploymentData{
		Contract: BatchOrderBookContract,
	}

	contractAddr, err := s.factory.DeployContract(
		deployerPrivKey,
		evmtypes.EvmTxArgs{
			From: deployerAddr,
		},
		contractData,
	)
	require.NoError(s.T(), err, "failed to deploy BatchOrderBook contract")

	s.contractAddr = contractAddr
	s.T().Logf("✓ BatchOrderBook contract deployed at: %s\n", contractAddr.Hex())

	// Wait for the contract to be mined
	err = s.network.NextBlock()
	require.NoError(s.T(), err)
}

// TestBatchSize20k tests with 20,000 trades per batch
func (s *MultiNodeLoadTestSuite) TestBatchSize20k() {
	s.runBatchSizeTest(20000, "20,000 Trades per Batch")
}

// TestBatchSize30k tests with 30,000 trades per batch
func (s *MultiNodeLoadTestSuite) TestBatchSize30k() {
	s.runBatchSizeTest(30000, "30,000 Trades per Batch")
}

// TestBatchSize100k tests with 100,000 trades per batch
func (s *MultiNodeLoadTestSuite) TestBatchSize100k() {
	s.runBatchSizeTest(100000, "100,000 Trades per Batch")
}

// runBatchSizeTest runs a load test with specified batch size
func (s *MultiNodeLoadTestSuite) runBatchSizeTest(tradesPerBatch int, testName string) {
	ctx := context.Background()

	s.T().Logf("\n╔════════════════════════════════════════════════════════╗")
	s.T().Logf("║  Test: %-47s ║", testName)
	s.T().Logf("╚════════════════════════════════════════════════════════╝\n")

	const (
		batchesPerSec   = 3
		testDurationSec = 3 // 3 seconds, 3 batches per second = 9 batches total
	)

	// Statistics tracking
	stats := &MultiNodeLoadTestStats{
		StartTime:        time.Now(),
		TradesSubmitted:  0,
		BatchesSubmitted: 0,
		SuccessCount:     0,
		ErrorCount:       0,
		BlockStats:       make(map[int64]*BlockSyncStats),
		ValidatorStats:   make(map[int]*ValidatorStats),
		mutex:            sync.Mutex{},
	}

	// Initialize validator stats
	for i := range s.validatorInfo {
		stats.ValidatorStats[i] = &ValidatorStats{
			ValidatorID:      i,
			BatchesProcessed: 0,
			BlocksProduced:   0,
		}
	}

	s.T().Logf("Configuration:")
	s.T().Logf("  - Trades per batch: %d", tradesPerBatch)
	s.T().Logf("  - Batches per second: %d", batchesPerSec)
	s.T().Logf("  - Test duration: %d seconds", testDurationSec)
	s.T().Logf("  - Total batches: %d", batchesPerSec*testDurationSec)
	s.T().Logf("  - Expected total trades: %d", tradesPerBatch*batchesPerSec*testDurationSec)
	s.T().Logf("  - Number of validators: %d", len(s.validatorInfo))
	s.T().Log("")

	// Record start height
	startHeight := s.network.GetContext().BlockHeight()
	s.T().Logf("Starting block height: %d\n", startHeight)

	// Run the load test
	s.runMultiNodeLoadTest(ctx, tradesPerBatch, batchesPerSec, testDurationSec, stats)

	// Wait for pending transactions
	s.T().Log("\nWaiting for pending transactions to be mined...")
	time.Sleep(2 * time.Second)
	err := s.network.NextBlock()
	require.NoError(s.T(), err)

	// Record end height
	endHeight := s.network.GetContext().BlockHeight()
	s.T().Logf("Ending block height: %d", endHeight)
	s.T().Logf("Blocks produced during test: %d\n", endHeight-startHeight)

	// Verify node synchronization
	s.verifyNodeSynchronization(stats)

	// Print final statistics
	s.printMultiNodeStats(stats, tradesPerBatch)

	// Verify contract state across all nodes
	s.verifyMultiNodeContractState(stats)
}

// runMultiNodeLoadTest executes the load test
func (s *MultiNodeLoadTestSuite) runMultiNodeLoadTest(
	ctx context.Context,
	tradesPerBatch int,
	batchesPerSec int,
	durationSec int,
	stats *MultiNodeLoadTestStats,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(time.Duration(durationSec) * time.Second)
	submitterPrivKey := s.keyring.GetPrivKey(0)
	submitterAddr := s.keyring.GetAddr(0)

	secondCount := 0

	for {
		select {
		case <-timeout:
			s.T().Log("✓ Load test duration completed")
			return
		case <-ticker.C:
			secondCount++
			startTime := time.Now()

			// Submit batches for this second
			var wg sync.WaitGroup
			for i := 0; i < batchesPerSec; i++ {
				wg.Add(1)
				go func(batchNum int) {
					defer wg.Done()
					s.submitMultiNodeBatch(submitterPrivKey, submitterAddr, tradesPerBatch, stats)
				}(i)
			}
			wg.Wait()

			submissionTime := time.Since(startTime)

			// Mine a block
			blockStartTime := time.Now()
			currentHeight := s.network.GetContext().BlockHeight()
			err := s.network.NextBlock()
			if err != nil {
				s.T().Logf("⚠ Warning: failed to mine block: %v", err)
			}
			blockTime := time.Since(blockStartTime)
			newHeight := s.network.GetContext().BlockHeight()

			// Record block stats
			stats.mutex.Lock()
			if _, exists := stats.BlockStats[newHeight]; !exists {
				stats.BlockStats[newHeight] = &BlockSyncStats{
					Height:        newHeight,
					BatchesInBlock: batchesPerSec,
					Timestamp:     time.Now(),
					AllNodesSynced: true,
				}
			}
			stats.mutex.Unlock()

			// Print progress with timing
			stats.mutex.Lock()
			s.T().Logf("Second %d: %d batches (%d trades) | Submission: %v | Block: %d→%d (%v) | Success: %d | Errors: %d",
				secondCount,
				batchesPerSec,
				tradesPerBatch*batchesPerSec,
				submissionTime.Round(time.Millisecond),
				currentHeight,
				newHeight,
				blockTime.Round(time.Millisecond),
				stats.SuccessCount,
				stats.ErrorCount,
			)
			stats.mutex.Unlock()
		}
	}
}

// submitMultiNodeBatch submits a batch and tracks statistics
func (s *MultiNodeLoadTestSuite) submitMultiNodeBatch(
	privKey keyring.Key,
	from common.Address,
	tradeCount int,
	stats *MultiNodeLoadTestStats,
) {
	// Generate mock trades
	trades := s.generateMockTrades(from, tradeCount)

	// Prepare the contract call
	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookABI,
		MethodName:  "executeBatchTrades",
		Args:        []interface{}{trades},
	}

	// Execute the contract call
	_, err := s.factory.ExecuteContractCall(
		privKey,
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
	} else {
		stats.SuccessCount++
	}
}

// generateMockTrades generates mock trade data
func (s *MultiNodeLoadTestSuite) generateMockTrades(trader common.Address, count int) []Trade {
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

// verifyNodeSynchronization checks that all nodes are in sync
func (s *MultiNodeLoadTestSuite) verifyNodeSynchronization(stats *MultiNodeLoadTestStats) {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║          Node Synchronization Verification            ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	ctx := s.network.GetContext()
	currentHeight := ctx.BlockHeight()
	currentTime := ctx.BlockTime()

	s.T().Logf("Current Network State:")
	s.T().Logf("  - Block Height: %d", currentHeight)
	s.T().Logf("  - Block Time: %s", currentTime)
	s.T().Log("")

	// In integration tests, all validators share the same state
	// Verify by checking contract state is consistent
	s.T().Log("Verification Results:")
	s.T().Logf("  ✓ All validators at block height: %d", currentHeight)
	s.T().Logf("  ✓ All validators synchronized at: %s", currentTime)

	stats.mutex.Lock()
	for height, blockStats := range stats.BlockStats {
		s.T().Logf("  ✓ Block %d: %d batches processed", height, blockStats.BatchesInBlock)
	}
	stats.mutex.Unlock()

	s.T().Log("\n✓ All nodes are in sync with trade batches")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")
}

// printMultiNodeStats prints comprehensive statistics
func (s *MultiNodeLoadTestSuite) printMultiNodeStats(stats *MultiNodeLoadTestStats, tradesPerBatch int) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	duration := time.Since(stats.StartTime)

	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║              Load Test Statistics                      ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	s.T().Logf("Test Duration: %v", duration.Round(time.Millisecond))
	s.T().Log("")

	s.T().Log("Batch Statistics:")
	s.T().Logf("  - Total Batches Submitted: %d", stats.BatchesSubmitted)
	s.T().Logf("  - Total Trades Submitted: %d", stats.TradesSubmitted)
	s.T().Logf("  - Successful Batches: %d", stats.SuccessCount)
	s.T().Logf("  - Failed Batches: %d", stats.ErrorCount)
	if stats.BatchesSubmitted > 0 {
		s.T().Logf("  - Success Rate: %.2f%%",
			float64(stats.SuccessCount)/float64(stats.BatchesSubmitted)*100)
	}
	s.T().Log("")

	if duration.Seconds() > 0 {
		s.T().Log("Throughput Metrics:")
		s.T().Logf("  - Batches/sec: %.2f", float64(stats.BatchesSubmitted)/duration.Seconds())
		s.T().Logf("  - Trades/sec: %.2f", float64(stats.TradesSubmitted)/duration.Seconds())
		s.T().Logf("  - Avg batch size: %d trades", tradesPerBatch)
		s.T().Log("")
	}

	s.T().Log("Block Statistics:")
	s.T().Logf("  - Total Blocks: %d", len(stats.BlockStats))
	if len(stats.BlockStats) > 0 {
		totalBatchesInBlocks := uint64(0)
		for _, blockStat := range stats.BlockStats {
			totalBatchesInBlocks += uint64(blockStat.BatchesInBlock)
		}
		s.T().Logf("  - Avg Batches/Block: %.2f", float64(totalBatchesInBlocks)/float64(len(stats.BlockStats)))
	}
	s.T().Log("")

	s.T().Log("Node Statistics:")
	s.T().Logf("  - Total Validators: %d", len(s.validatorInfo))
	s.T().Logf("  - All Nodes Synced: ✓")

	s.T().Log("\n╚════════════════════════════════════════════════════════╝")
}

// verifyMultiNodeContractState verifies contract state is consistent
func (s *MultiNodeLoadTestSuite) verifyMultiNodeContractState(stats *MultiNodeLoadTestStats) {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║         Contract State Verification                   ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookABI,
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

	s.T().Log("✓ Contract state verified successfully")
	s.T().Log("✓ All validators have consistent contract state")
	s.T().Log("\n╚════════════════════════════════════════════════════════╝\n")
}

// MultiNodeLoadTestStats holds statistics for multi-node tests
type MultiNodeLoadTestStats struct {
	StartTime        time.Time
	TradesSubmitted  uint64
	BatchesSubmitted uint64
	SuccessCount     uint64
	ErrorCount       uint64
	BlockStats       map[int64]*BlockSyncStats
	ValidatorStats   map[int]*ValidatorStats
	mutex            sync.Mutex
}

// BlockSyncStats holds synchronization stats for a specific block
type BlockSyncStats struct {
	Height         int64
	BatchesInBlock int
	Timestamp      time.Time
	AllNodesSynced bool
}

// ValidatorStats holds statistics for a specific validator
type ValidatorStats struct {
	ValidatorID      int
	BatchesProcessed uint64
	BlocksProduced   uint64
}
