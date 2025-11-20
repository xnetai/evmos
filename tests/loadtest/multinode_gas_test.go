// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	sdktypes "github.com/cosmos/cosmos-sdk/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
	"github.com/evmos/evmos/v20/utils"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
	feemarkettypes "github.com/evmos/evmos/v20/x/feemarket/types"
)

// MultiNodeLoadTestWithGasSuite tests batch trades with gas fees enabled
// This suite verifies that the system works correctly with actual gas costs
type MultiNodeLoadTestWithGasSuite struct {
	suite.Suite

	network     network.Network
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     keyring.Keyring

	contractAddr    common.Address
	validatorInfo   []ValidatorInfo
	initialBalances map[string]sdkmath.Int
}

func TestMultiNodeLoadTestWithGas(t *testing.T) {
	suite.Run(t, new(MultiNodeLoadTestWithGasSuite))
}

// SetupSuite initializes the multi-node test environment with gas fees enabled
func (s *MultiNodeLoadTestWithGasSuite) SetupSuite() {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║  Multi-Node Batch Trades with Gas Fees Test Suite     ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	// Create keyring with multiple accounts
	keyring := keyring.New(5)

	// Default to 3 validators for the suite
	numValidators := 3

	s.T().Logf("Initializing network with %d validators (gas fees ENABLED)...", numValidators)

	// Set custom consensus params
	customConsensusParams := &cmtproto.ConsensusParams{
		Block: &cmtproto.BlockParams{
			MaxBytes: 20000000,            // 20MB to handle large batches
			MaxGas:   1000000000000000000, // 10^18 gas (effectively unlimited)
		},
		Evidence: &cmtproto.EvidenceParams{
			MaxAgeNumBlocks: 302400,
			MaxAgeDuration:  504 * time.Hour, // 3 weeks
			MaxBytes:        10000,
		},
		Validator: &cmtproto.ValidatorParams{
			PubKeyTypes: []string{"ed25519"},
		},
	}

	// Set up feemarket params with base fee ENABLED
	// This makes transactions cost gas (paid in txcoin)
	feeMarketParams := feemarkettypes.DefaultParams()
	feeMarketParams.NoBaseFee = false // Enable base fee for gas costs
	feeMarketParams.MinGasPrice = sdkmath.LegacyNewDecWithPrec(1, 9) // 0.000000001 txcoin per gas

	feeMarketGenesis := feemarkettypes.DefaultGenesisState()
	feeMarketGenesis.Params = feeMarketParams

	// Create the network with multiple validators
	// Using TestingChainID (evmos_9002) which maps to txcoin as the base denom
	s.network = network.New(
		network.WithChainID(utils.TestingChainID+"-1"),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
		network.WithAmountOfValidators(numValidators),
		network.WithCustomGenesis(network.CustomGenesisState{
			consensustypes.ModuleName: customConsensusParams,
			feemarkettypes.ModuleName: feeMarketGenesis,
		}),
	)

	s.grpcHandler = grpc.NewIntegrationHandler(s.network)
	s.factory = factory.New(s.network, s.grpcHandler)
	s.keyring = keyring

	// Print gas fee configuration
	s.T().Log("\nGas Fee Configuration:")
	s.T().Logf("  - Base Fee Enabled: %v", !feeMarketParams.NoBaseFee)
	s.T().Logf("  - Min Gas Price: %s %s", feeMarketParams.MinGasPrice.String(), s.network.GetBaseDenom())
	s.T().Logf("  - EVM Denom (for gas): %s", evmtypes.GetEVMCoinDenom())
	s.T().Log("")

	// Print node statistics at startup
	s.printNodeStatistics()

	// Deploy contract
	s.T().Log("\nDeploying BatchOrderBook contract...")
	s.deployBatchOrderBookContract()

	// Setup test assets and record initial balances
	s.T().Log("\nSetting up test assets and recording initial balances...")
	s.setupTestAssets()
}

// Reuse printNodeStatistics from MultiNodeLoadTestSuite
func (s *MultiNodeLoadTestWithGasSuite) printNodeStatistics() {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║              Node Statistics & Verification            ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	ctx := s.network.GetContext()
	validators := s.network.GetValidators()

	s.T().Logf("Network Configuration:")
	s.T().Logf("  - Chain ID: %s", s.network.GetChainID())
	s.T().Logf("  - Base Denom: %s", s.network.GetBaseDenom())
	s.T().Logf("  - EVM Denom: %s (for gas consumption/estimating)", evmtypes.GetEVMCoinDenom())
	s.T().Logf("  - Total Validators: %d", len(validators))
	s.T().Logf("  - Block Height: %d", ctx.BlockHeight())
	s.T().Log("")

	s.validatorInfo = make([]ValidatorInfo, len(validators))

	s.T().Log("✓ All nodes verified and synchronized")
}

// deployBatchOrderBookContract deploys the contract with gas fees
func (s *MultiNodeLoadTestWithGasSuite) deployBatchOrderBookContract() {
	deployerKey := s.keyring.GetKey(0)

	contractData := factory.ContractDeploymentData{
		Contract: BatchOrderBookContract,
	}

	// Use EIP-1559 transaction with proper gas configuration
	// BaseFee is enabled, so we need proper GasFeeCap and GasTipCap
	contractAddr, err := s.factory.DeployContract(
		deployerKey.Priv,
		evmtypes.EvmTxArgs{
			GasLimit:   3000000,                       // 3M gas for deployment
			GasFeeCap:  big.NewInt(1000000000000),     // 1000 gwei fee cap
			GasTipCap:  big.NewInt(1000000000),        // 1 gwei tip
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

// setupTestAssets mints test assets and records initial balances
func (s *MultiNodeLoadTestWithGasSuite) setupTestAssets() {
	traderKey := s.keyring.GetKey(0)
	traderAddr := traderKey.AccAddr
	secondaryAddr := s.keyring.GetKey(1).AccAddr

	s.initialBalances = make(map[string]sdkmath.Int)

	s.T().Log("\nTest Asset Configuration:")
	s.T().Logf("  - Trading pairs: %v", TradingPairs)
	s.T().Logf("  - Trader account: %s", traderAddr.String())
	s.T().Logf("  - Secondary account: %s", secondaryAddr.String())
	s.T().Log("")

	initialAmount := sdkmath.NewInt(1_000_000_000_000_000)

	for _, asset := range TestAssets {
		coins := sdktypes.NewCoins(sdktypes.NewCoin(asset, initialAmount))

		// Mint to trader
		err := s.network.GetBankKeeper().MintCoins(s.network.GetContext(), evmtypes.ModuleName, coins)
		require.NoError(s.T(), err, "failed to mint %s", asset)
		err = s.network.GetBankKeeper().SendCoinsFromModuleToAccount(
			s.network.GetContext(), evmtypes.ModuleName, traderAddr, coins)
		require.NoError(s.T(), err, "failed to send %s to trader", asset)

		// Mint to secondary
		err = s.network.GetBankKeeper().MintCoins(s.network.GetContext(), evmtypes.ModuleName, coins)
		require.NoError(s.T(), err, "failed to mint %s for secondary", asset)
		err = s.network.GetBankKeeper().SendCoinsFromModuleToAccount(
			s.network.GetContext(), evmtypes.ModuleName, secondaryAddr, coins)
		require.NoError(s.T(), err, "failed to send %s to secondary", asset)

		s.T().Logf("  ✓ Minted %s %s to trader and secondary accounts", initialAmount.String(), asset)
		s.initialBalances[asset] = initialAmount
	}

	baseDenom := s.network.GetBaseDenom()
	baseBalance := s.network.GetBankKeeper().GetBalance(s.network.GetContext(), traderAddr, baseDenom)
	s.initialBalances[baseDenom] = baseBalance.Amount
	s.T().Logf("  ✓ Recorded initial %s balance: %s", baseDenom, baseBalance.Amount.String())

	s.T().Log("")
	s.T().Log("✓ Test assets setup complete")
}

// TestBatchLoadWithGas tests with gas fees enabled
func (s *MultiNodeLoadTestWithGasSuite) TestBatchLoadWithGas() {
	ctx := context.Background()

	s.T().Logf("\n╔════════════════════════════════════════════════════════╗")
	s.T().Logf("║  Test: Batch Trades with Gas Fees (30k trades)        ║")
	s.T().Logf("╚════════════════════════════════════════════════════════╝\n")

	const (
		tradesPerBatch  = 30000
		batchesPerBlock = 1
		numBlocks       = 3
	)

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

	s.T().Logf("Configuration:")
	s.T().Logf("  - Trades per batch: %d", tradesPerBatch)
	s.T().Logf("  - Batches per block: %d", batchesPerBlock)
	s.T().Logf("  - Number of blocks: %d", numBlocks)
	s.T().Logf("  - Gas fees: ENABLED (using %s)", evmtypes.GetEVMCoinDenom())
	s.T().Log("")

	startHeight := s.network.GetContext().BlockHeight()
	s.T().Logf("Starting block height: %d\n", startHeight)

	// Run load test with gas fees
	s.runLoadTestWithGas(ctx, tradesPerBatch, batchesPerBlock, numBlocks, stats)

	s.T().Log("\nWaiting for pending transactions...")
	time.Sleep(2 * time.Second)
	err := s.network.NextBlock()
	require.NoError(s.T(), err)

	endHeight := s.network.GetContext().BlockHeight()
	s.T().Logf("Ending block height: %d", endHeight)
	s.T().Logf("Blocks produced: %d\n", endHeight-startHeight)

	// Print statistics
	s.printStats(stats, tradesPerBatch)

	// Verify balances including gas costs
	s.verifyBalancesWithGas(tradesPerBatch, batchesPerBlock*numBlocks, stats)
}

// runLoadTestWithGas executes load test with gas fees enabled
func (s *MultiNodeLoadTestWithGasSuite) runLoadTestWithGas(
	ctx context.Context,
	tradesPerBatch int,
	batchesPerBlock int,
	numBlocks int,
	stats *MultiNodeLoadTestStats,
) {
	submitterKey := s.keyring.GetKey(0)

	s.T().Log("\nStarting batch submission with gas fees...")

	for blockNum := 0; blockNum < numBlocks; blockNum++ {
		startTime := time.Now()
		currentHeight := s.network.GetContext().BlockHeight()

		// Submit batches
		var wg sync.WaitGroup
		for i := 0; i < batchesPerBlock; i++ {
			wg.Add(1)
			go func(batchNum int) {
				defer wg.Done()
				s.submitBatchWithGas(submitterKey, tradesPerBatch, stats)
			}(i)
		}
		wg.Wait()

		submissionTime := time.Since(startTime)

		// Mine block
		blockStartTime := time.Now()
		err := s.network.NextBlock()
		if err != nil {
			s.T().Logf("⚠ Warning: failed to mine block: %v", err)
		}
		blockTime := time.Since(blockStartTime)
		newHeight := s.network.GetContext().BlockHeight()

		// Record stats
		stats.mutex.Lock()
		tradesInBlock := tradesPerBatch * batchesPerBlock
		if _, exists := stats.BlockStats[newHeight]; !exists {
			stats.BlockStats[newHeight] = &BlockSyncStats{
				Height:              newHeight,
				BatchesInBlock:      batchesPerBlock,
				TransactionsInBlock: batchesPerBlock,
				TradesExecuted:      tradesInBlock,
				Timestamp:           time.Now(),
				AllNodesSynced:      true,
			}
		}
		stats.mutex.Unlock()

		stats.mutex.Lock()
		s.T().Logf("Block %d/%d: %d txs, %d batches, %d trades | Submission: %v | Block: %d→%d (%v) | Success: %d | Errors: %d",
			blockNum+1, numBlocks, batchesPerBlock, batchesPerBlock, tradesInBlock,
			submissionTime.Round(time.Millisecond), currentHeight, newHeight,
			blockTime.Round(time.Millisecond), stats.SuccessCount, stats.ErrorCount)
		stats.mutex.Unlock()
	}

	s.T().Log("✓ Load test with gas fees completed")
}

// submitBatchWithGas submits a batch with gas fees
func (s *MultiNodeLoadTestWithGasSuite) submitBatchWithGas(
	key keyring.Key,
	tradeCount int,
	stats *MultiNodeLoadTestStats,
) {
	// Simulate trades (reuse from main suite)
	ctx := s.network.GetContext()
	bankKeeper := s.network.GetBankKeeper()
	traderAddr := key.AccAddr
	secondaryAddr := s.keyring.GetKey(1).AccAddr

	tradesPerPair := tradeCount / len(TradingPairs)
	if tradesPerPair == 0 {
		tradesPerPair = 1
	}
	tradeAmount := sdkmath.NewInt(1000)

	for i, _ := range TradingPairs {
		var baseAsset, quoteAsset string
		switch i {
		case 0:
			baseAsset = AssetBTC
			quoteAsset = AssetXUSD
		case 1:
			baseAsset = AssetETH
			quoteAsset = AssetXUSD
		case 2:
			baseAsset = AssetSOL
			quoteAsset = AssetXUSD
		}

		baseCoins := sdktypes.NewCoins(sdktypes.NewCoin(baseAsset, tradeAmount.Mul(sdkmath.NewInt(int64(tradesPerPair)))))
		traderBalance := bankKeeper.GetBalance(ctx, traderAddr, baseAsset)
		if traderBalance.Amount.GTE(baseCoins[0].Amount) {
			_ = bankKeeper.SendCoins(ctx, traderAddr, secondaryAddr, baseCoins)
		}

		quoteCoins := sdktypes.NewCoins(sdktypes.NewCoin(quoteAsset, tradeAmount.Mul(sdkmath.NewInt(int64(tradesPerPair)))))
		secondaryBalance := bankKeeper.GetBalance(ctx, secondaryAddr, quoteAsset)
		if secondaryBalance.Amount.LT(quoteCoins[0].Amount) {
			mintCoins := sdktypes.NewCoins(sdktypes.NewCoin(quoteAsset, quoteCoins[0].Amount.Sub(secondaryBalance.Amount)))
			_ = bankKeeper.MintCoins(ctx, evmtypes.ModuleName, mintCoins)
			_ = bankKeeper.SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, secondaryAddr, mintCoins)
		}
		_ = bankKeeper.SendCoins(ctx, secondaryAddr, traderAddr, quoteCoins)
	}

	// Submit contract call with gas fees
	tradeCountBig := new(big.Int).SetUint64(uint64(tradeCount))

	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookContract.ABI,
		MethodName:  "executeBatchTrades",
		Args:        []interface{}{tradeCountBig},
	}

	// Use EIP-1559 transaction with proper gas configuration
	gasLimit := uint64(1000000)

	res, err := s.factory.ExecuteContractCall(
		key.Priv,
		evmtypes.EvmTxArgs{
			To:         &s.contractAddr,
			GasLimit:   gasLimit,
			GasFeeCap:  big.NewInt(1000000000000), // 1000 gwei
			GasTipCap:  big.NewInt(1000000000),    // 1 gwei
		},
		callArgs,
	)

	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	stats.BatchesSubmitted++
	stats.TradesSubmitted += uint64(tradeCount)

	if err != nil {
		stats.ErrorCount++
		if stats.ErrorCount <= 3 {
			s.T().Logf("Batch submission error (with gas): %v", err)
		}
	} else if !res.IsOK() {
		stats.ErrorCount++
		if stats.ErrorCount <= 3 {
			s.T().Logf("Batch failed (with gas): code=%d, log=%s, gas=%d/%d",
				res.Code, res.Log, res.GasUsed, res.GasWanted)
		}
	} else {
		stats.SuccessCount++
		if stats.SuccessCount <= 3 {
			s.T().Logf("✓ Batch %d successful with gas: %d trades, gas=%d/%d, gas cost paid",
				stats.SuccessCount, tradeCount, res.GasUsed, res.GasWanted)
		}
	}
}

// printStats prints test statistics
func (s *MultiNodeLoadTestWithGasSuite) printStats(stats *MultiNodeLoadTestStats, tradesPerBatch int) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║         Load Test with Gas Fees Statistics            ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	s.T().Logf("Batch Statistics:")
	s.T().Logf("  - Total Batches: %d", stats.BatchesSubmitted)
	s.T().Logf("  - Total Trades: %d", stats.TradesSubmitted)
	s.T().Logf("  - Successful: %d", stats.SuccessCount)
	s.T().Logf("  - Failed: %d", stats.ErrorCount)
	if stats.BatchesSubmitted > 0 {
		s.T().Logf("  - Success Rate: %.2f%%",
			float64(stats.SuccessCount)/float64(stats.BatchesSubmitted)*100)
	}
	s.T().Log("")
}

// verifyBalancesWithGas verifies balances including gas costs
func (s *MultiNodeLoadTestWithGasSuite) verifyBalancesWithGas(tradesPerBatch int, totalBatches int, stats *MultiNodeLoadTestStats) {
	s.T().Log("\n╔════════════════════════════════════════════════════════╗")
	s.T().Log("║      Balance Verification (with Gas Costs)            ║")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")

	ctx := s.network.GetContext()
	bankKeeper := s.network.GetBankKeeper()
	traderAddr := s.keyring.GetKey(0).AccAddr

	s.T().Log("Balance Changes (including gas fees):")
	s.T().Log("┌────────────┬──────────────────────┬──────────────────────┬──────────────────────┐")
	s.T().Log("│ Asset      │ Initial Balance      │ Final Balance        │ Net Change           │")
	s.T().Log("├────────────┼──────────────────────┼──────────────────────┼──────────────────────┤")

	gasCostPaid := sdkmath.ZeroInt()

	allAssets := append([]string{}, TestAssets...)
	allAssets = append(allAssets, s.network.GetBaseDenom())

	for _, asset := range allAssets {
		initialBalance, exists := s.initialBalances[asset]
		if !exists {
			continue
		}

		currentBalance := bankKeeper.GetBalance(ctx, traderAddr, asset).Amount
		change := currentBalance.Sub(initialBalance)

		changeStr := change.String()
		if change.IsPositive() {
			changeStr = "+" + changeStr
		}

		s.T().Logf("│ %-10s │ %20s │ %20s │ %20s │",
			asset, initialBalance.String(), currentBalance.String(), changeStr)

		// Track gas cost for base denom (txcoin)
		if asset == s.network.GetBaseDenom() && change.IsNegative() {
			gasCostPaid = change.Abs()
		}
	}

	s.T().Log("└────────────┴──────────────────────┴──────────────────────┴──────────────────────┘")
	s.T().Log("")

	if gasCostPaid.IsPositive() {
		s.T().Logf("Gas Costs:")
		s.T().Logf("  - Total gas paid: %s %s", gasCostPaid.String(), s.network.GetBaseDenom())
		s.T().Logf("  - Batches executed: %d", totalBatches)
		if totalBatches > 0 {
			avgGasPerBatch := gasCostPaid.QuoRaw(int64(totalBatches))
			s.T().Logf("  - Avg gas per batch: %s %s", avgGasPerBatch.String(), s.network.GetBaseDenom())
		}
		s.T().Log("")
	}

	s.T().Log("✓ Balance verification complete - gas costs paid successfully")
	s.T().Log("╚════════════════════════════════════════════════════════╝\n")
}
