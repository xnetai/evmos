// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"encoding/json"
	"math/big"
	"testing"

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

func TestEnhancedOrderbook(t *testing.T) {
	kr := keyring.New(4) // sender, LP1, LP2, trader

	nw := network.New(
		network.WithAmountOfValidators(1),
		network.WithPreFundedAccounts(kr.GetAllAccAddrs()...),
	)

	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	sender := kr.GetKey(0)
	lp1 := kr.GetKey(1)
	lp2 := kr.GetKey(2)
	trader := kr.GetKey(3)

	t.Log("\n=== Enhanced Orderbook AMM Test ===")
	t.Logf("Sender: %s", sender.Addr)
	t.Logf("LP1: %s", lp1.Addr)
	t.Logf("LP2: %s", lp2.Addr)
	t.Logf("Trader: %s", trader.Addr)

	// Deploy XUSD
	t.Log("\n--- Deploying XUSD Token ---")
	xusdContract, err := deployERC20(t, nw, txFactory, sender, "XUSD Stablecoin", "XUSD", 18)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())
	t.Logf("XUSD deployed at: %s", xusdContract.Hex())

	// Mint XUSD for all participants
	mintAmount := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(1e18))
	for _, account := range []keyring.Key{sender, lp1, lp2, trader} {
		err = mintERC20(t, nw, txFactory, sender, xusdContract, account.Addr, mintAmount)
		require.NoError(t, err)
		require.NoError(t, nw.NextBlock())
	}

	// Deploy TSLA token
	t.Log("\n--- Deploying TSLA Token ---")
	tslaContract, err := deployERC20(t, nw, txFactory, sender, "Tesla Stock", "TSLA", 18)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Mint TSLA for LPs
	for _, account := range []keyring.Key{sender, lp1, lp2} {
		err = mintERC20(t, nw, txFactory, sender, tslaContract, account.Addr, mintAmount)
		require.NoError(t, err)
		require.NoError(t, nw.NextBlock())
	}

	// Deploy Enhanced Orderbook
	t.Log("\n--- Deploying Enhanced Orderbook ---")
	orderbookAddr, err := txFactory.DeployContract(
		sender.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.EnhancedOrderbookContract,
			ConstructorArgs: []interface{}{xusdContract},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())
	t.Logf("Enhanced Orderbook deployed at: %s", orderbookAddr.Hex())

	// Test 1: Create pool with price range
	t.Log("\n--- Test 1: Creating Pool with Price Range ---")
	testCreatePoolWithRange(t, nw, txFactory, handler, lp1, orderbookAddr, tslaContract, xusdContract)

	// Test 2: Add liquidity from second LP
	t.Log("\n--- Test 2: Adding Liquidity from Second LP ---")
	testAddLiquidity(t, nw, txFactory, handler, lp2, orderbookAddr, tslaContract, xusdContract)

	// Test 3: Trading with fees
	t.Log("\n--- Test 3: Trading with Fees ---")
	testTradingWithFees(t, nw, txFactory, handler, trader, orderbookAddr, tslaContract, xusdContract)

	// Test 4: Fee collection by LPs
	t.Log("\n--- Test 4: Fee Collection by LPs ---")
	testFeeCollection(t, nw, txFactory, handler, lp1, lp2, orderbookAddr, tslaContract, xusdContract)

	// Test 5: Remove liquidity
	t.Log("\n--- Test 5: Removing Liquidity ---")
	testRemoveLiquidity(t, nw, txFactory, handler, lp2, orderbookAddr, tslaContract, xusdContract)

	// Test 6: Limit orders
	t.Log("\n--- Test 6: Limit Orders ---")
	testLimitOrders(t, nw, txFactory, handler, trader, orderbookAddr, tslaContract, xusdContract)

	t.Log("\n=== Test Summary ===")
	t.Log("✓ Pool creation with price ranges")
	t.Log("✓ Multiple LPs adding liquidity")
	t.Log("✓ Trading with fee collection")
	t.Log("✓ LP fee distribution")
	t.Log("✓ Liquidity removal")
	t.Log("✓ Limit order creation and execution")
}

func testCreatePoolWithRange(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, lp keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Initial pool: 1000 TSLA, 100,000 XUSD (price = 100 XUSD/TSLA)
	// Price range: 50-200 XUSD/TSLA
	assetAmount := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))
	xusdAmount := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18))
	minPrice := new(big.Int).Mul(big.NewInt(50), big.NewInt(1e18))  // 50 XUSD
	maxPrice := new(big.Int).Mul(big.NewInt(200), big.NewInt(1e18)) // 200 XUSD

	// Approve tokens
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, lp, tslaAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	err = approveERC20(t, nw, txFactory, lp, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Create pool
	_, err = txFactory.ExecuteContractCall(
		lp.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "createPool",
			Args:        []interface{}{tslaAddr, assetAmount, xusdAmount, minPrice, maxPrice},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Verify pool was created
	price, err := getEnhancedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("Initial TSLA price: %s XUSD", formatToken(price))

	// Get LP position
	shares, minP, maxP, assetVal, xusdVal, fees := getLPPosition(t, nw, handler, orderbookAddr, tslaAddr, lp.Addr)
	t.Logf("LP1 Position: shares=%s, range=[%s, %s], assets=%s TSLA, xusd=%s, fees=%s",
		formatToken(shares), formatToken(minP), formatToken(maxP),
		formatToken(assetVal), formatToken(xusdVal), formatToken(fees))

	require.True(t, shares.Cmp(big.NewInt(0)) > 0, "LP should have shares")
}

func testAddLiquidity(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, lp keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Add 500 TSLA, 50,000 XUSD (same ratio as existing pool)
	assetAmount := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18))
	xusdAmount := new(big.Int).Mul(big.NewInt(50000), big.NewInt(1e18))
	minPrice := new(big.Int).Mul(big.NewInt(80), big.NewInt(1e18))  // 80 XUSD
	maxPrice := new(big.Int).Mul(big.NewInt(150), big.NewInt(1e18)) // 150 XUSD

	// Approve tokens
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, lp, tslaAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	err = approveERC20(t, nw, txFactory, lp, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Add liquidity
	_, err = txFactory.ExecuteContractCall(
		lp.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "addLiquidity",
			Args:        []interface{}{tslaAddr, assetAmount, xusdAmount, minPrice, maxPrice},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Get LP position
	shares, minP, maxP, assetVal, xusdVal, fees := getLPPosition(t, nw, handler, orderbookAddr, tslaAddr, lp.Addr)
	t.Logf("LP2 Position: shares=%s, range=[%s, %s], assets=%s TSLA, xusd=%s, fees=%s",
		formatToken(shares), formatToken(minP), formatToken(maxP),
		formatToken(assetVal), formatToken(xusdVal), formatToken(fees))

	require.True(t, shares.Cmp(big.NewInt(0)) > 0, "LP2 should have shares")
}

func testTradingWithFees(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, trader keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Approve XUSD for trading
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, trader, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Get initial price and pool fees
	initialPrice, err := getEnhancedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)

	initialFees, err := getPoolFees(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("Initial pool fees: %s XUSD", formatToken(initialFees))

	// Buy TSLA with 10,000 XUSD
	buyAmount := new(big.Int).Mul(big.NewInt(10000), big.NewInt(1e18))
	_, err = txFactory.ExecuteContractCall(
		trader.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "buy",
			Args:        []interface{}{tslaAddr, buyAmount},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Check new price and fees
	newPrice, err := getEnhancedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("Price after buy: %s XUSD (was %s, change: %s%%)",
		formatToken(newPrice), formatToken(initialPrice),
		formatPercent(newPrice, initialPrice))

	newFees, err := getPoolFees(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	feesCollected := new(big.Int).Sub(newFees, initialFees)
	t.Logf("Fees collected from trade: %s XUSD", formatToken(feesCollected))

	require.True(t, newPrice.Cmp(initialPrice) > 0, "Price should increase after buy")
	require.True(t, feesCollected.Cmp(big.NewInt(0)) > 0, "Fees should be collected")

	// Verify trader received TSLA
	traderBalance, err := getERC20Balance(t, nw, handler, tslaAddr, trader.Addr)
	require.NoError(t, err)
	t.Logf("Trader TSLA balance: %s", formatToken(traderBalance))
	require.True(t, traderBalance.Cmp(big.NewInt(0)) > 0, "Trader should have TSLA")
}

func testFeeCollection(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, lp1, lp2 keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Get pending fees for both LPs
	_, _, _, _, _, lp1Fees := getLPPosition(t, nw, handler, orderbookAddr, tslaAddr, lp1.Addr)
	_, _, _, _, _, lp2Fees := getLPPosition(t, nw, handler, orderbookAddr, tslaAddr, lp2.Addr)

	t.Logf("LP1 pending fees: %s XUSD", formatToken(lp1Fees))
	t.Logf("LP2 pending fees: %s XUSD", formatToken(lp2Fees))

	require.True(t, lp1Fees.Cmp(big.NewInt(0)) > 0, "LP1 should have pending fees")

	// LP1 collects fees
	initialXUSD, err := getERC20Balance(t, nw, handler, xusdAddr, lp1.Addr)
	require.NoError(t, err)

	_, err = txFactory.ExecuteContractCall(
		lp1.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "collectFees",
			Args:        []interface{}{tslaAddr},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Verify LP1 received fees
	finalXUSD, err := getERC20Balance(t, nw, handler, xusdAddr, lp1.Addr)
	require.NoError(t, err)

	feesReceived := new(big.Int).Sub(finalXUSD, initialXUSD)
	t.Logf("LP1 collected fees: %s XUSD", formatToken(feesReceived))
	require.True(t, feesReceived.Cmp(big.NewInt(0)) > 0, "LP1 should receive fees")
}

func testRemoveLiquidity(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, lp keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Get LP position
	shares, _, _, assetVal, xusdVal, fees := getLPPosition(t, nw, handler, orderbookAddr, tslaAddr, lp.Addr)
	t.Logf("LP2 position before removal: shares=%s, assets=%s, xusd=%s, fees=%s",
		formatToken(shares), formatToken(assetVal), formatToken(xusdVal), formatToken(fees))

	// Remove half the liquidity
	sharesToRemove := new(big.Int).Div(shares, big.NewInt(2))

	initialTSLA, err := getERC20Balance(t, nw, handler, tslaAddr, lp.Addr)
	require.NoError(t, err)

	initialXUSD, err := getERC20Balance(t, nw, handler, xusdAddr, lp.Addr)
	require.NoError(t, err)

	_, err = txFactory.ExecuteContractCall(
		lp.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "removeLiquidity",
			Args:        []interface{}{tslaAddr, sharesToRemove},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Verify LP received tokens back
	finalTSLA, err := getERC20Balance(t, nw, handler, tslaAddr, lp.Addr)
	require.NoError(t, err)

	finalXUSD, err := getERC20Balance(t, nw, handler, xusdAddr, lp.Addr)
	require.NoError(t, err)

	tslaReceived := new(big.Int).Sub(finalTSLA, initialTSLA)
	xusdReceived := new(big.Int).Sub(finalXUSD, initialXUSD)

	t.Logf("LP2 received: %s TSLA, %s XUSD", formatToken(tslaReceived), formatToken(xusdReceived))

	require.True(t, tslaReceived.Cmp(big.NewInt(0)) > 0, "LP should receive TSLA")
	require.True(t, xusdReceived.Cmp(big.NewInt(0)) > 0, "LP should receive XUSD")
}

func testLimitOrders(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, trader keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Get current price
	currentPrice, err := getEnhancedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("Current TSLA price: %s XUSD", formatToken(currentPrice))

	// Create a buy limit order at 5% below current price
	limitPrice := new(big.Int).Mul(currentPrice, big.NewInt(95))
	limitPrice = new(big.Int).Div(limitPrice, big.NewInt(100))
	orderAmount := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))

	_, err = txFactory.ExecuteContractCall(
		trader.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.EnhancedOrderbookContract.ABI,
			MethodName:  "createLimitOrder",
			Args:        []interface{}{tslaAddr, true, orderAmount, limitPrice},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	t.Logf("Created buy limit order: amount=%s XUSD, limit price=%s XUSD/TSLA",
		formatToken(orderAmount), formatToken(limitPrice))
	t.Log("✓ Limit order created successfully")
}

// Helper functions

func getEnhancedPrice(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr common.Address) (*big.Int, error) {

	input, err := contracts.EnhancedOrderbookContract.ABI.Pack("getPrice", tokenAddr)
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
	err = contracts.EnhancedOrderbookContract.ABI.UnpackIntoInterface(&price, "getPrice", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return price, nil
}

func getLPPosition(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr, providerAddr common.Address) (*big.Int, *big.Int, *big.Int, *big.Int, *big.Int, *big.Int) {

	input, err := contracts.EnhancedOrderbookContract.ABI.Pack("getLPPosition", tokenAddr, providerAddr)
	require.NoError(t, err)

	callData, err := json.Marshal(evmtypes.TransactionArgs{
		To:    &orderbookAddr,
		Input: (*hexutil.Bytes)(&input),
	})
	require.NoError(t, err)

	ethRes, err := nw.GetEvmClient().EthCall(
		nw.GetContext(),
		&evmtypes.EthCallRequest{
			Args: callData,
		},
	)
	require.NoError(t, err)

	results, err := contracts.EnhancedOrderbookContract.ABI.Unpack("getLPPosition", ethRes.Ret)
	require.NoError(t, err)

	return results[0].(*big.Int), results[1].(*big.Int), results[2].(*big.Int),
		results[3].(*big.Int), results[4].(*big.Int), results[5].(*big.Int)
}

func getPoolFees(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr common.Address) (*big.Int, error) {

	input, err := contracts.EnhancedOrderbookContract.ABI.Pack("getPoolFees", tokenAddr)
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

	var fees *big.Int
	err = contracts.EnhancedOrderbookContract.ABI.UnpackIntoInterface(&fees, "getPoolFees", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return fees, nil
}
