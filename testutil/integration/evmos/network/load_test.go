// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"encoding/json"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/evmos/evmos/v19/contracts"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/network"
	evmtypes "github.com/evmos/evmos/v19/x/evm/types"
)

func TestMultiNodeLoadTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Configuration
	const (
		numValidators    = 4
		numSenders       = 10
		tradesPerBatch   = 10000
		numBatches       = 3
		blockTime        = 1 * time.Second
		assetsPerSender  = 3 // TSLA, AAPL, GOOGLE
	)

	t.Logf("\n=== Multi-Node Load Test ===")
	t.Logf("Configuration:")
	t.Logf("  Validators: %d", numValidators)
	t.Logf("  Senders: %d", numSenders)
	t.Logf("  Trades per batch: %d", tradesPerBatch)
	t.Logf("  Number of batches: %d", numBatches)
	t.Logf("  Target block time: %v", blockTime)
	t.Logf("  Total trades: %d", tradesPerBatch*numBatches)

	// Create keyring with senders
	kr := keyring.New(numSenders + 1) // +1 for deployer

	// Create network with multiple validators
	t.Log("\n--- Starting Multi-Node Network ---")
	nw := network.New(
		network.WithAmountOfValidators(numValidators),
		network.WithPreFundedAccounts(kr.GetAllAccAddrs()...),
	)

	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	deployer := kr.GetKey(0)
	t.Logf("✓ Network started with %d validators", numValidators)

	// Deploy contracts
	t.Log("\n--- Deploying Contracts ---")
	xusdContract := deployAndMintTokenForLoad(t, nw, txFactory, deployer, "XUSD", kr, numSenders)
	tslaContract := deployAndMintTokenForLoad(t, nw, txFactory, deployer, "TSLA", kr, numSenders)
	aaplContract := deployAndMintTokenForLoad(t, nw, txFactory, deployer, "AAPL", kr, numSenders)
	googleContract := deployAndMintTokenForLoad(t, nw, txFactory, deployer, "GOOGLE", kr, numSenders)

	// Deploy Simple Orderbook (simpler for high-throughput testing)
	orderbookAddr, err := txFactory.DeployContract(
		deployer.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.SimpleOrderbookContract,
			ConstructorArgs: []interface{}{xusdContract},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())
	t.Logf("✓ Orderbook deployed at: %s", orderbookAddr.Hex())

	// Setup pools for each asset
	t.Log("\n--- Setting Up Liquidity Pools ---")
	setupPool(t, nw, txFactory, deployer, orderbookAddr, tslaContract, xusdContract, "TSLA")
	setupPool(t, nw, txFactory, deployer, orderbookAddr, aaplContract, xusdContract, "AAPL")
	setupPool(t, nw, txFactory, deployer, orderbookAddr, googleContract, xusdContract, "GOOGLE")

	// Approve tokens for all senders
	t.Log("\n--- Approving Tokens for Senders ---")
	approveTokensForSenders(t, nw, txFactory, kr, numSenders, orderbookAddr,
		[]common.Address{xusdContract, tslaContract, aaplContract, googleContract})

	// Trade metrics
	var (
		totalTradesSubmitted atomic.Int64
		totalTradesSucceeded atomic.Int64
		totalTradesFailed    atomic.Int64
		blockTradeCounts     sync.Map // map[uint64]int64
		currentBlock         atomic.Uint64
	)

	// Start block monitor
	stopMonitor := make(chan struct{})
	go blockMonitor(nw, &currentBlock, stopMonitor)

	// Execute load test
	t.Log("\n=== Starting Load Test ===")
	startTime := time.Now()
	lastBlock := currentBlock.Load()

	for batch := 0; batch < numBatches; batch++ {
		batchStart := time.Now()
		t.Logf("\n--- Batch %d/%d ---", batch+1, numBatches)

		// Submit trades concurrently from all senders
		var wg sync.WaitGroup
		tradesPerSender := tradesPerBatch / numSenders

		for senderIdx := 1; senderIdx <= numSenders; senderIdx++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				sender := kr.GetKey(idx)
				assets := []common.Address{tslaContract, aaplContract, googleContract}

				for tradeNum := 0; tradeNum < tradesPerSender; tradeNum++ {
					// Alternate between buy and sell
					isBuy := tradeNum%2 == 0
					asset := assets[tradeNum%len(assets)]

					totalTradesSubmitted.Add(1)

					var err error
					if isBuy {
						// Buy with small amount to avoid depleting pool
						buyAmount := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18)) // 10 XUSD
						_, err = txFactory.ExecuteContractCall(
							sender.Priv,
							evmtypes.EvmTxArgs{To: &orderbookAddr},
							factory.CallArgs{
								ContractABI: contracts.SimpleOrderbookContract.ABI,
								MethodName:  "buy",
								Args:        []interface{}{asset, buyAmount},
							},
						)
					} else {
						// Sell small amount
						sellAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e17)) // 0.1 asset
						_, err = txFactory.ExecuteContractCall(
							sender.Priv,
							evmtypes.EvmTxArgs{To: &orderbookAddr},
							factory.CallArgs{
								ContractABI: contracts.SimpleOrderbookContract.ABI,
								MethodName:  "sell",
								Args:        []interface{}{asset, sellAmount},
							},
						)
					}

					if err != nil {
						totalTradesFailed.Add(1)
					} else {
						totalTradesSucceeded.Add(1)
						// Track trades by block
						block := currentBlock.Load()
						count, _ := blockTradeCounts.LoadOrStore(block, new(atomic.Int64))
						count.(*atomic.Int64).Add(1)
					}
				}
			}(senderIdx)
		}

		// Wait for batch to complete
		wg.Wait()
		batchDuration := time.Since(batchStart)

		// Advance block
		require.NoError(t, nw.NextBlock())
		currentBlock.Add(1)

		t.Logf("Batch %d completed in %v", batch+1, batchDuration)
		t.Logf("  Trades submitted: %d", totalTradesSubmitted.Load())
		t.Logf("  Trades succeeded: %d", totalTradesSucceeded.Load())
		t.Logf("  Trades failed: %d", totalTradesFailed.Load())

		// Wait for target block time
		time.Sleep(blockTime)
	}

	close(stopMonitor)
	totalDuration := time.Since(startTime)

	// Calculate final metrics
	t.Log("\n=== Load Test Results ===")
	t.Logf("Total duration: %v", totalDuration)
	t.Logf("Total trades submitted: %d", totalTradesSubmitted.Load())
	t.Logf("Total trades succeeded: %d", totalTradesSucceeded.Load())
	t.Logf("Total trades failed: %d", totalTradesFailed.Load())
	t.Logf("Success rate: %.2f%%", float64(totalTradesSucceeded.Load())/float64(totalTradesSubmitted.Load())*100)
	t.Logf("Average TPS: %.2f", float64(totalTradesSucceeded.Load())/totalDuration.Seconds())

	// Print trades per block
	t.Log("\n=== Trades Per Block ===")
	newBlock := currentBlock.Load()
	for block := lastBlock; block <= newBlock; block++ {
		if count, ok := blockTradeCounts.Load(block); ok {
			t.Logf("Block %d: %d trades", block, count.(*atomic.Int64).Load())
		} else {
			t.Logf("Block %d: 0 trades", block)
		}
	}

	// Verify final pool states
	t.Log("\n=== Final Pool States ===")
	tslaPrice, _ := getSimplePrice(t, nw, handler, orderbookAddr, tslaContract)
	aaplPrice, _ := getSimplePrice(t, nw, handler, orderbookAddr, aaplContract)
	googlePrice, _ := getSimplePrice(t, nw, handler, orderbookAddr, googleContract)

	t.Logf("TSLA price: %s XUSD", formatToken(tslaPrice))
	t.Logf("AAPL price: %s XUSD", formatToken(aaplPrice))
	t.Logf("GOOGLE price: %s XUSD", formatToken(googlePrice))

	t.Log("\n✓ Load test completed successfully")
}

func deployAndMintTokenForLoad(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, symbol string, kr keyring.Keyring, numSenders int) common.Address {

	tokenAddr, err := deployERC20(t, nw, txFactory, deployer, symbol+" Token", symbol, 18)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Mint large amounts for load testing
	// For XUSD, mint 1B tokens (enough for 3 pools × 100M each + extra for trading)
	// For other tokens, 100M is sufficient
	mintAmount := new(big.Int).Mul(big.NewInt(100000000), big.NewInt(1e18)) // 100M tokens
	if symbol == "XUSD" {
		mintAmount = new(big.Int).Mul(big.NewInt(1000000000), big.NewInt(1e18)) // 1B XUSD
	}

	// Mint for deployer (for pool creation)
	err = mintERC20(t, nw, txFactory, deployer, tokenAddr, deployer.Addr, mintAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Mint for all senders
	for i := 1; i <= numSenders; i++ {
		sender := kr.GetKey(i)
		err = mintERC20(t, nw, txFactory, deployer, tokenAddr, sender.Addr, mintAmount)
		require.NoError(t, err)
		require.NoError(t, nw.NextBlock())
	}

	return tokenAddr
}

func setupPool(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, orderbookAddr, tokenAddr, xusdAddr common.Address, symbol string) {

	// Large liquidity for load testing
	assetAmount := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(1e18)) // 1M tokens
	xusdAmount := new(big.Int).Mul(big.NewInt(100000000), big.NewInt(1e18)) // 100M XUSD

	// Approve
	approveAmount := new(big.Int).Mul(big.NewInt(1000000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, deployer, tokenAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	err = approveERC20(t, nw, txFactory, deployer, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Create pool
	_, err = txFactory.ExecuteContractCall(
		deployer.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.SimpleOrderbookContract.ABI,
			MethodName:  "addPool",
			Args:        []interface{}{tokenAddr, assetAmount, xusdAmount},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	t.Logf("✓ Pool created for %s", symbol)
}

func approveTokensForSenders(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	kr keyring.Keyring, numSenders int, spender common.Address, tokens []common.Address) {

	approveAmount := new(big.Int).Mul(big.NewInt(1000000000), big.NewInt(1e18))

	for i := 1; i <= numSenders; i++ {
		sender := kr.GetKey(i)
		for _, token := range tokens {
			err := approveERC20(t, nw, txFactory, sender, token, spender, approveAmount)
			require.NoError(t, err)
		}
		require.NoError(t, nw.NextBlock())
	}

	t.Logf("✓ Approved tokens for %d senders", numSenders)
}

func blockMonitor(nw *network.IntegrationNetwork, currentBlock *atomic.Uint64, stop chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Update current block height
			// Note: In real implementation, query actual block height from network
			// For now, we track it via our own counter
		}
	}
}

func getSimplePrice(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr common.Address) (*big.Int, error) {

	input, err := contracts.SimpleOrderbookContract.ABI.Pack("getPrice", tokenAddr)
	if err != nil {
		return nil, err
	}

	callData, err := json.Marshal(evmtypes.TransactionArgs{
		To:    &orderbookAddr,
		Input: (*hexutil.Bytes)(&input),
	})
	if err != nil {
		return nil, err
	}

	ethRes, err := nw.GetEvmClient().EthCall(
		nw.GetContext(),
		&evmtypes.EthCallRequest{
			Args: callData,
		},
	)
	if err != nil {
		return nil, err
	}

	var price *big.Int
	err = contracts.SimpleOrderbookContract.ABI.UnpackIntoInterface(&price, "getPrice", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return price, nil
}
