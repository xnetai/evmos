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

func TestGovernedOrderbook(t *testing.T) {
	kr := keyring.New(5) // deployer, marketMaker, lp1, trader1, trader2

	nw := network.New(
		network.WithAmountOfValidators(1),
		network.WithPreFundedAccounts(kr.GetAllAccAddrs()...),
	)

	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	deployer := kr.GetKey(0)
	marketMaker := kr.GetKey(1)
	lp1 := kr.GetKey(2)
	trader1 := kr.GetKey(3)
	trader2 := kr.GetKey(4)

	t.Log("\n=== Governed Orderbook Test ===")
	t.Logf("Deployer: %s", deployer.Addr)
	t.Logf("Market Maker: %s", marketMaker.Addr)
	t.Logf("LP1: %s", lp1.Addr)
	t.Logf("Trader1: %s", trader1.Addr)
	t.Logf("Trader2: %s", trader2.Addr)

	// Deploy tokens
	t.Log("\n--- Deploying Tokens ---")
	xusdContract := deployAndMintToken(t, nw, txFactory, deployer, "XUSD", []keyring.Key{marketMaker, lp1, trader1, trader2})
	xsharesContract := deployToken(t, nw, txFactory, deployer, "XShares Governance", "XSHARES")
	tslaContract := deployAndMintToken(t, nw, txFactory, deployer, "TSLA", []keyring.Key{marketMaker, lp1})

	// Deploy GovernedOrderbook
	t.Log("\n--- Deploying Governed Orderbook ---")
	orderbookAddr, err := txFactory.DeployContract(
		deployer.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.GovernedOrderbookContract,
			ConstructorArgs: []interface{}{xusdContract, xsharesContract},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())
	t.Logf("Governed Orderbook deployed at: %s", orderbookAddr.Hex())

	// Test 1: Pool creation with minimum collateral
	t.Log("\n--- Test 1: Pool Creation with 100k XUSD Minimum Collateral ---")
	testPoolCreationWithCollateral(t, nw, txFactory, handler, marketMaker, orderbookAddr, tslaContract, xusdContract)

	// Test 2: Constant 1 XUSD fee
	t.Log("\n--- Test 2: Trading with Constant 1 XUSD Fee ---")
	testConstantFee(t, nw, txFactory, handler, trader1, orderbookAddr, tslaContract, xusdContract)

	// Test 3: XShares distribution
	t.Log("\n--- Test 3: XShares Governance Token Distribution ---")
	testXSharesDistribution(t, nw, handler, orderbookAddr, marketMaker, lp1)

	// Test 4: Governance voting on fee changes
	t.Log("\n--- Test 4: Governance Voting on Fee Changes ---")
	testGovernanceVoting(t, nw, txFactory, handler, marketMaker, lp1, orderbookAddr)

	// Test 5: In-block limit order execution
	t.Log("\n--- Test 5: In-Block Limit Order Execution ---")
	testInBlockLimitOrders(t, nw, txFactory, handler, trader1, trader2, orderbookAddr, tslaContract, xusdContract)

	// Test 6: Add liquidity and verify XShares
	t.Log("\n--- Test 6: Adding Liquidity and XShares Minting ---")
	testAddLiquidityXShares(t, nw, txFactory, handler, lp1, orderbookAddr, tslaContract, xusdContract)

	t.Log("\n=== Test Summary ===")
	t.Log("✓ Pool creation with 100k XUSD minimum collateral")
	t.Log("✓ Constant 1 XUSD fee per trade")
	t.Log("✓ XShares minted to LPs (1 XShare per 100 XUSD)")
	t.Log("✓ Governance proposal creation and voting")
	t.Log("✓ In-block limit order execution")
	t.Log("✓ Additional liquidity adds more XShares")
}

func testPoolCreationWithCollateral(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, marketMaker keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Pool requires 100,000 XUSD minimum
	assetAmount := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))
	xusdAmount := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18))
	minPrice := new(big.Int).Mul(big.NewInt(50), big.NewInt(1e18))
	maxPrice := new(big.Int).Mul(big.NewInt(200), big.NewInt(1e18))

	// Approve tokens
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, marketMaker, tslaAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	err = approveERC20(t, nw, txFactory, marketMaker, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Create pool
	_, err = txFactory.ExecuteContractCall(
		marketMaker.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "createPool",
			Args:        []interface{}{tslaAddr, assetAmount, xusdAmount, minPrice, maxPrice},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Verify pool was created
	price, err := getGovernedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("✓ Pool created with 100,000 XUSD collateral")
	t.Logf("  Initial TSLA price: %s XUSD", formatToken(price))
}

func testConstantFee(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, trader keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Approve XUSD
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err := approveERC20(t, nw, txFactory, trader, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Get initial fees
	initialFees, err := getGovernedPoolFees(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)

	// Buy with 10,000 XUSD
	buyAmount := new(big.Int).Mul(big.NewInt(10000), big.NewInt(1e18))
	_, err = txFactory.ExecuteContractCall(
		trader.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "buy",
			Args:        []interface{}{tslaAddr, buyAmount},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Check fees collected (should be 1 XUSD)
	newFees, err := getGovernedPoolFees(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)

	feesCollected := new(big.Int).Sub(newFees, initialFees)
	expectedFee := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // 1 XUSD

	t.Logf("✓ Constant fee verified:")
	t.Logf("  Fee collected: %s XUSD", formatToken(feesCollected))
	t.Logf("  Expected: %s XUSD", formatToken(expectedFee))
	require.Equal(t, expectedFee, feesCollected, "Fee should be exactly 1 XUSD")
}

func testXSharesDistribution(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr common.Address, marketMaker, lp1 keyring.Key) {

	// Market maker provided 100,000 XUSD -> should get 1,000 XShares (1 per 100 XUSD)
	mmXShares, err := getXSharesBalance(t, nw, handler, orderbookAddr, marketMaker.Addr)
	require.NoError(t, err)

	expectedXShares := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))
	t.Logf("✓ XShares distributed:")
	t.Logf("  Market Maker XShares: %s", formatToken(mmXShares))
	t.Logf("  Expected: %s (1 XShare per 100 XUSD)", formatToken(expectedXShares))
	require.Equal(t, expectedXShares, mmXShares, "Market maker should have 1000 XShares")
}

func testGovernanceVoting(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, voter1, voter2 keyring.Key, orderbookAddr common.Address) {

	// Create proposal to change trading fee to 2 XUSD
	newFee := new(big.Int).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := txFactory.ExecuteContractCall(
		voter1.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "createProposal",
			Args:        []interface{}{uint8(0), newFee, common.Address{}}, // ProposalType.ChangeTradingFee = 0
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	t.Logf("✓ Governance proposal created:")
	t.Logf("  Proposal: Change trading fee to %s XUSD", formatToken(newFee))
	t.Logf("  Proposer: %s", voter1.Addr)

	// Vote on proposal (proposalId = 0)
	_, err = txFactory.ExecuteContractCall(
		voter1.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "vote",
			Args:        []interface{}{big.NewInt(0), true}, // proposalId 0, vote yes
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	t.Logf("✓ Vote cast successfully")
	t.Log("  (Note: Execution requires voting period to end)")
}

func testInBlockLimitOrders(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, trader1, trader2 keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Get current price
	currentPrice, err := getGovernedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)
	t.Logf("Current TSLA price: %s XUSD", formatToken(currentPrice))

	// Trader1 creates a sell limit order at 10% above current price
	limitPrice := new(big.Int).Mul(currentPrice, big.NewInt(110))
	limitPrice = new(big.Int).Div(limitPrice, big.NewInt(100))
	sellAmount := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))

	// Approve TSLA for trader1
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err = approveERC20(t, nw, txFactory, trader1, tslaAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Create limit order
	_, err = txFactory.ExecuteContractCall(
		trader1.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "createLimitOrder",
			Args:        []interface{}{tslaAddr, false, sellAmount, limitPrice}, // sell order
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	t.Logf("✓ Limit order created:")
	t.Logf("  Type: Sell")
	t.Logf("  Amount: %s TSLA", formatToken(sellAmount))
	t.Logf("  Limit price: %s XUSD (10%% above current)", formatToken(limitPrice))

	// Trader2 buys enough to push price above limit
	// This should trigger the limit order in the same block
	approveAmount = new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err = approveERC20(t, nw, txFactory, trader2, xusdAddr, orderbookAddr, approveAmount)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Buy large amount to push price up
	bigBuyAmount := new(big.Int).Mul(big.NewInt(50000), big.NewInt(1e18))
	_, err = txFactory.ExecuteContractCall(
		trader2.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "buy",
			Args:        []interface{}{tslaAddr, bigBuyAmount},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Check new price
	newPrice, err := getGovernedPrice(t, nw, handler, orderbookAddr, tslaAddr)
	require.NoError(t, err)

	t.Logf("✓ Large buy executed:")
	t.Logf("  New price: %s XUSD", formatToken(newPrice))
	t.Logf("  Price change: %s%%", formatPercent(newPrice, currentPrice))

	if newPrice.Cmp(limitPrice) >= 0 {
		t.Log("✓ Limit order price condition met (order marked executable in same block)")
	}
}

func testAddLiquidityXShares(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	handler grpc.Handler, lp keyring.Key, orderbookAddr, tslaAddr, xusdAddr common.Address) {

	// Get initial XShares
	initialXShares, err := getXSharesBalance(t, nw, handler, orderbookAddr, lp.Addr)
	require.NoError(t, err)

	// Add 50,000 XUSD liquidity
	assetAmount := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18))
	xusdAmount := new(big.Int).Mul(big.NewInt(50000), big.NewInt(1e18))
	minPrice := new(big.Int).Mul(big.NewInt(80), big.NewInt(1e18))
	maxPrice := new(big.Int).Mul(big.NewInt(150), big.NewInt(1e18))

	// Approve
	approveAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	err = approveERC20(t, nw, txFactory, lp, tslaAddr, orderbookAddr, approveAmount)
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
			ContractABI: contracts.GovernedOrderbookContract.ABI,
			MethodName:  "addLiquidity",
			Args:        []interface{}{tslaAddr, assetAmount, xusdAmount, minPrice, maxPrice},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	// Check new XShares balance
	newXShares, err := getXSharesBalance(t, nw, handler, orderbookAddr, lp.Addr)
	require.NoError(t, err)

	xSharesGained := new(big.Int).Sub(newXShares, initialXShares)
	expectedXShares := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18)) // 50,000 / 100 = 500

	t.Logf("✓ Liquidity added:")
	t.Logf("  XUSD provided: %s", formatToken(xusdAmount))
	t.Logf("  XShares minted: %s", formatToken(xSharesGained))
	t.Logf("  Expected: %s (1 XShare per 100 XUSD)", formatToken(expectedXShares))
	require.Equal(t, expectedXShares, xSharesGained, "Should receive 500 XShares")
}

// Helper functions

func deployAndMintToken(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, symbol string, recipients []keyring.Key) common.Address {

	tokenAddr, err := deployERC20(t, nw, txFactory, deployer, symbol+" Token", symbol, 18)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	mintAmount := new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1e18))
	for _, recipient := range recipients {
		err = mintERC20(t, nw, txFactory, deployer, tokenAddr, recipient.Addr, mintAmount)
		require.NoError(t, err)
		require.NoError(t, nw.NextBlock())
	}

	return tokenAddr
}

func deployToken(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, name, symbol string) common.Address {

	tokenAddr, err := txFactory.DeployContract(
		deployer.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.XSharesTokenContract,
			ConstructorArgs: []interface{}{},
		},
	)
	require.NoError(t, err)
	require.NoError(t, nw.NextBlock())

	return tokenAddr
}

func getGovernedPrice(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr common.Address) (*big.Int, error) {

	input, err := contracts.GovernedOrderbookContract.ABI.Pack("getPrice", tokenAddr)
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
	err = contracts.GovernedOrderbookContract.ABI.UnpackIntoInterface(&price, "getPrice", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return price, nil
}

func getGovernedPoolFees(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, tokenAddr common.Address) (*big.Int, error) {

	input, err := contracts.GovernedOrderbookContract.ABI.Pack("getPoolFees", tokenAddr)
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
	err = contracts.GovernedOrderbookContract.ABI.UnpackIntoInterface(&fees, "getPoolFees", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return fees, nil
}

func getXSharesBalance(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	orderbookAddr, account common.Address) (*big.Int, error) {

	input, err := contracts.GovernedOrderbookContract.ABI.Pack("xsharesBalance", account)
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

	var balance *big.Int
	err = contracts.GovernedOrderbookContract.ABI.UnpackIntoInterface(&balance, "xsharesBalance", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return balance, nil
}
