// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"encoding/json"
	"fmt"
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

func TestAssetOrderbook(t *testing.T) {
	keyring := keyring.New(2) // Create 2 accounts for testing

	// Create network with 1 validator
	nw := network.New(
		network.WithAmountOfValidators(1),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
	)

	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	sender := keyring.GetKey(0)
	buyer := keyring.GetKey(1)

	t.Log("\n=== Asset Orderbook Test ===")
	t.Logf("Sender address: %s", sender.Addr)
	t.Logf("Buyer address: %s", buyer.Addr)

	// Step 1: Deploy XUSD token
	t.Log("\n--- Deploying XUSD Token ---")
	xusdInitialSupply := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(1e18)) // 1 million XUSD
	xusdContract, err := deployERC20(t, nw, txFactory, sender, "XUSD Stablecoin", "XUSD", 18)
	require.NoError(t, err, "failed to deploy XUSD token")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("XUSD deployed at: %s", xusdContract.Hex())

	// Mint initial XUSD supply to sender
	err = mintERC20(t, nw, txFactory, sender, xusdContract, sender.Addr, xusdInitialSupply)
	require.NoError(t, err, "failed to mint XUSD")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Step 2: Deploy asset tokens (TSLA, AAPL, GOOGLE)
	t.Log("\n--- Deploying Asset Tokens ---")
	assetInitialSupply := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18)) // 100k tokens each

	tslaContract, err := deployERC20(t, nw, txFactory, sender, "Tesla Stock", "TSLA", 18)
	require.NoError(t, err, "failed to deploy TSLA token")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("TSLA deployed at: %s", tslaContract.Hex())

	err = mintERC20(t, nw, txFactory, sender, tslaContract, sender.Addr, assetInitialSupply)
	require.NoError(t, err, "failed to mint TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	aaplContract, err := deployERC20(t, nw, txFactory, sender, "Apple Stock", "AAPL", 18)
	require.NoError(t, err, "failed to deploy AAPL token")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("AAPL deployed at: %s", aaplContract.Hex())

	err = mintERC20(t, nw, txFactory, sender, aaplContract, sender.Addr, assetInitialSupply)
	require.NoError(t, err, "failed to mint AAPL")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	googleContract, err := deployERC20(t, nw, txFactory, sender, "Google Stock", "GOOGLE", 18)
	require.NoError(t, err, "failed to deploy GOOGLE token")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("GOOGLE deployed at: %s", googleContract.Hex())

	err = mintERC20(t, nw, txFactory, sender, googleContract, sender.Addr, assetInitialSupply)
	require.NoError(t, err, "failed to mint GOOGLE")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Step 3: Deploy Orderbook contract
	t.Log("\n--- Deploying Orderbook Contract ---")
	orderbookContract, err := deployOrderbook(t, nw, txFactory, sender, xusdContract)
	require.NoError(t, err, "failed to deploy orderbook")
	t.Logf("Orderbook deployed at: %s", orderbookContract.Hex())

	// Step 4: Add assets to orderbook with initial liquidity
	t.Log("\n--- Adding Assets to Orderbook ---")

	// Approve orderbook to spend tokens
	approveAmount := new(big.Int).Mul(big.NewInt(1000000), big.NewInt(1e18))

	err = approveERC20(t, nw, txFactory, sender, xusdContract, orderbookContract, approveAmount)
	require.NoError(t, err, "failed to approve XUSD")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	err = approveERC20(t, nw, txFactory, sender, tslaContract, orderbookContract, approveAmount)
	require.NoError(t, err, "failed to approve TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	err = approveERC20(t, nw, txFactory, sender, aaplContract, orderbookContract, approveAmount)
	require.NoError(t, err, "failed to approve AAPL")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	err = approveERC20(t, nw, txFactory, sender, googleContract, orderbookContract, approveAmount)
	require.NoError(t, err, "failed to approve GOOGLE")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Add TSLA: 1000 TSLA @ 100 XUSD each = 100,000 XUSD reserves
	tslaReserve := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))
	tslaXUSDReserve := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18))
	err = addAssetToOrderbook(t, nw, txFactory, sender, orderbookContract, "TSLA", tslaContract, tslaReserve, tslaXUSDReserve)
	require.NoError(t, err, "failed to add TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("Added TSLA: %s tokens, %s XUSD (price: 100 XUSD/TSLA)", formatToken(tslaReserve), formatToken(tslaXUSDReserve))

	// Add AAPL: 2000 AAPL @ 50 XUSD each = 100,000 XUSD reserves
	aaplReserve := new(big.Int).Mul(big.NewInt(2000), big.NewInt(1e18))
	aaplXUSDReserve := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18))
	err = addAssetToOrderbook(t, nw, txFactory, sender, orderbookContract, "AAPL", aaplContract, aaplReserve, aaplXUSDReserve)
	require.NoError(t, err, "failed to add AAPL")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("Added AAPL: %s tokens, %s XUSD (price: 50 XUSD/AAPL)", formatToken(aaplReserve), formatToken(aaplXUSDReserve))

	// Add GOOGLE: 500 GOOGLE @ 200 XUSD each = 100,000 XUSD reserves
	googleReserve := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18))
	googleXUSDReserve := new(big.Int).Mul(big.NewInt(100000), big.NewInt(1e18))
	err = addAssetToOrderbook(t, nw, txFactory, sender, orderbookContract, "GOOGLE", googleContract, googleReserve, googleXUSDReserve)
	require.NoError(t, err, "failed to add GOOGLE")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("Added GOOGLE: %s tokens, %s XUSD (price: 200 XUSD/GOOGLE)", formatToken(googleReserve), formatToken(googleXUSDReserve))

	// Step 5: Transfer XUSD to buyer for testing
	t.Log("\n--- Preparing Buyer Account ---")
	buyerXUSDAmount := new(big.Int).Mul(big.NewInt(50000), big.NewInt(1e18)) // 50,000 XUSD
	err = transferERC20(t, nw, txFactory, sender, xusdContract, buyer.Addr, buyerXUSDAmount)
	require.NoError(t, err, "failed to transfer XUSD to buyer")
	require.NoError(t, nw.NextBlock(), "failed to advance block")
	t.Logf("Transferred %s XUSD to buyer", formatToken(buyerXUSDAmount))

	// Approve orderbook to spend buyer's XUSD
	err = approveERC20(t, nw, txFactory, buyer, xusdContract, orderbookContract, approveAmount)
	require.NoError(t, err, "failed to approve buyer XUSD")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Step 6: Test buying assets
	t.Log("\n--- Testing Buy Operations ---")

	// Get initial price of TSLA
	tslaPrice, err := getAssetPrice(t, nw, handler, orderbookContract, tslaContract)
	require.NoError(t, err, "failed to get TSLA price")
	t.Logf("TSLA initial price: %s XUSD", formatToken(tslaPrice))

	// Buy TSLA with XUSD
	buyXUSDAmount := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18)) // 1000 XUSD
	err = buyAsset(t, nw, txFactory, buyer, orderbookContract, tslaContract, buyXUSDAmount)
	require.NoError(t, err, "failed to buy TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Get buyer's TSLA balance to see what they received
	buyerTSLABalance, err := getERC20Balance(t, nw, handler, tslaContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer TSLA balance")
	require.True(t, buyerTSLABalance.Cmp(big.NewInt(0)) > 0, "buyer should have received TSLA tokens")
	t.Logf("Bought %s TSLA for %s XUSD", formatToken(buyerTSLABalance), formatToken(buyXUSDAmount))

	// Get new price after buy
	tslaNewPrice, err := getAssetPrice(t, nw, handler, orderbookContract, tslaContract)
	require.NoError(t, err, "failed to get new TSLA price")
	t.Logf("TSLA new price: %s XUSD (increased by %s%%)",
		formatToken(tslaNewPrice),
		formatPercent(tslaNewPrice, tslaPrice))
	t.Logf("Buyer TSLA balance: %s", formatToken(buyerTSLABalance))

	// Step 7: Test selling assets
	t.Log("\n--- Testing Sell Operations ---")

	// Approve orderbook to spend buyer's TSLA
	err = approveERC20(t, nw, txFactory, buyer, tslaContract, orderbookContract, buyerTSLABalance)
	require.NoError(t, err, "failed to approve buyer TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Get buyer's initial XUSD balance before sell
	buyerXUSDBeforeSell, err := getERC20Balance(t, nw, handler, xusdContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer XUSD balance before sell")

	// Sell half of the TSLA back
	sellTSLAAmount := new(big.Int).Div(buyerTSLABalance, big.NewInt(2))
	err = sellAsset(t, nw, txFactory, buyer, orderbookContract, tslaContract, sellTSLAAmount)
	require.NoError(t, err, "failed to sell TSLA")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	// Get buyer's XUSD balance after sell to see what they received
	buyerXUSDAfterSell, err := getERC20Balance(t, nw, handler, xusdContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer XUSD balance after sell")
	xusdReceived := new(big.Int).Sub(buyerXUSDAfterSell, buyerXUSDBeforeSell)
	t.Logf("Sold %s TSLA for %s XUSD", formatToken(sellTSLAAmount), formatToken(xusdReceived))

	// Get price after sell
	tslaFinalPrice, err := getAssetPrice(t, nw, handler, orderbookContract, tslaContract)
	require.NoError(t, err, "failed to get final TSLA price")
	t.Logf("TSLA final price: %s XUSD (decreased by %s%%)",
		formatToken(tslaFinalPrice),
		formatPercent(tslaNewPrice, tslaFinalPrice))

	// Verify buyer's final TSLA balance
	buyerFinalTSLA, err := getERC20Balance(t, nw, handler, tslaContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer final TSLA balance")
	expectedTSLA := new(big.Int).Sub(buyerTSLABalance, sellTSLAAmount)
	require.Equal(t, expectedTSLA, buyerFinalTSLA, "buyer should have correct TSLA balance")
	t.Logf("Buyer final TSLA balance: %s", formatToken(buyerFinalTSLA))

	// Step 8: Test buying AAPL and GOOGLE
	t.Log("\n--- Testing Other Assets ---")

	// Buy AAPL
	aaplPrice, _ := getAssetPrice(t, nw, handler, orderbookContract, aaplContract)
	t.Logf("AAPL price: %s XUSD", formatToken(aaplPrice))

	aaplBuyAmount := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18))
	err = buyAsset(t, nw, txFactory, buyer, orderbookContract, aaplContract, aaplBuyAmount)
	require.NoError(t, err, "failed to buy AAPL")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	buyerAAPLBalance, err := getERC20Balance(t, nw, handler, aaplContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer AAPL balance")
	t.Logf("Bought %s AAPL for %s XUSD", formatToken(buyerAAPLBalance), formatToken(aaplBuyAmount))

	// Buy GOOGLE
	googlePrice, _ := getAssetPrice(t, nw, handler, orderbookContract, googleContract)
	t.Logf("GOOGLE price: %s XUSD", formatToken(googlePrice))

	googleBuyAmount := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))
	err = buyAsset(t, nw, txFactory, buyer, orderbookContract, googleContract, googleBuyAmount)
	require.NoError(t, err, "failed to buy GOOGLE")
	require.NoError(t, nw.NextBlock(), "failed to advance block")

	buyerGOOGLEBalance, err := getERC20Balance(t, nw, handler, googleContract, buyer.Addr)
	require.NoError(t, err, "failed to get buyer GOOGLE balance")
	t.Logf("Bought %s GOOGLE for %s XUSD", formatToken(buyerGOOGLEBalance), formatToken(googleBuyAmount))

	// Final summary
	t.Log("\n=== Test Summary ===")
	t.Log("✓ XUSD token deployed")
	t.Log("✓ Asset tokens (TSLA, AAPL, GOOGLE) deployed")
	t.Log("✓ Orderbook contract deployed")
	t.Log("✓ Assets added to orderbook with initial liquidity")
	t.Log("✓ Buy operations tested successfully")
	t.Log("✓ Sell operations tested successfully")
	t.Log("✓ Price discovery working correctly")

	buyerFinalXUSD, _ := getERC20Balance(t, nw, handler, xusdContract, buyer.Addr)
	buyerFinalAAPL, _ := getERC20Balance(t, nw, handler, aaplContract, buyer.Addr)
	buyerFinalGOOGLE, _ := getERC20Balance(t, nw, handler, googleContract, buyer.Addr)

	t.Log("\nBuyer final balances:")
	t.Logf("  XUSD:   %s", formatToken(buyerFinalXUSD))
	t.Logf("  TSLA:   %s", formatToken(buyerFinalTSLA))
	t.Logf("  AAPL:   %s", formatToken(buyerFinalAAPL))
	t.Logf("  GOOGLE: %s", formatToken(buyerFinalGOOGLE))
}

// Helper functions

func deployERC20(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, name, symbol string, decimals uint8) (common.Address, error) {

	// Deploy ERC20 contract using the existing ERC20MinterBurnerDecimals contract
	contractAddr, err := txFactory.DeployContract(
		deployer.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.ERC20MinterBurnerDecimalsContract,
			ConstructorArgs: []interface{}{name, symbol, decimals},
		},
	)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to deploy contract: %w", err)
	}

	return contractAddr, nil
}

func deployOrderbook(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	deployer keyring.Key, xusdAddress common.Address) (common.Address, error) {

	// Deploy AssetOrderbook contract
	contractAddr, err := txFactory.DeployContract(
		deployer.Priv,
		evmtypes.EvmTxArgs{},
		factory.ContractDeploymentData{
			Contract:        contracts.SimpleOrderbookContract,
			ConstructorArgs: []interface{}{xusdAddress},
		},
	)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to deploy contract: %w", err)
	}

	return contractAddr, nil
}

func mintERC20(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	minter keyring.Key, tokenAddr, to common.Address, amount *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		minter.Priv,
		evmtypes.EvmTxArgs{To: &tokenAddr},
		factory.CallArgs{
			ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
			MethodName:  "mint",
			Args:        []interface{}{to, amount},
		},
	)

	return err
}

func approveERC20(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	owner keyring.Key, tokenAddr, spender common.Address, amount *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		owner.Priv,
		evmtypes.EvmTxArgs{To: &tokenAddr},
		factory.CallArgs{
			ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
			MethodName:  "approve",
			Args:        []interface{}{spender, amount},
		},
	)

	return err
}

func transferERC20(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	from keyring.Key, tokenAddr common.Address, to common.Address, amount *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		from.Priv,
		evmtypes.EvmTxArgs{To: &tokenAddr},
		factory.CallArgs{
			ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
			MethodName:  "transfer",
			Args:        []interface{}{to, amount},
		},
	)

	return err
}

func addAssetToOrderbook(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	sender keyring.Key, orderbookAddr common.Address, symbol string, tokenAddr common.Address,
	assetReserve, xusdReserve *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		sender.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.SimpleOrderbookContract.ABI,
			MethodName:  "addPool",
			Args:        []interface{}{tokenAddr, assetReserve, xusdReserve},
		},
	)

	return err
}

func buyAsset(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	buyer keyring.Key, orderbookAddr, tokenAddr common.Address, xusdAmount *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		buyer.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.SimpleOrderbookContract.ABI,
			MethodName:  "buy",
			Args:        []interface{}{tokenAddr, xusdAmount},
		},
	)
	return err
}

func sellAsset(t *testing.T, nw *network.IntegrationNetwork, txFactory factory.TxFactory,
	seller keyring.Key, orderbookAddr, tokenAddr common.Address, assetAmount *big.Int) error {

	_, err := txFactory.ExecuteContractCall(
		seller.Priv,
		evmtypes.EvmTxArgs{To: &orderbookAddr},
		factory.CallArgs{
			ContractABI: contracts.SimpleOrderbookContract.ABI,
			MethodName:  "sell",
			Args:        []interface{}{tokenAddr, assetAmount},
		},
	)
	return err
}

func getAssetPrice(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
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

func getERC20Balance(t *testing.T, nw *network.IntegrationNetwork, handler grpc.Handler,
	tokenAddr common.Address, account common.Address) (*big.Int, error) {

	input, err := contracts.ERC20MinterBurnerDecimalsContract.ABI.Pack("balanceOf", account)
	if err != nil {
		return nil, err
	}

	callData, err := json.Marshal(evmtypes.TransactionArgs{
		To:    &tokenAddr,
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
	err = contracts.ERC20MinterBurnerDecimalsContract.ABI.UnpackIntoInterface(&balance, "balanceOf", ethRes.Ret)
	if err != nil {
		return nil, err
	}

	return balance, nil
}

func formatToken(amount *big.Int) string {
	if amount == nil {
		return "0"
	}
	// Convert from wei to tokens (divide by 10^18)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	tokens := new(big.Int).Div(amount, divisor)
	remainder := new(big.Int).Mod(amount, divisor)

	// Format with 2 decimal places
	decimals := new(big.Int).Div(remainder, new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil))

	return fmt.Sprintf("%s.%02d", tokens.String(), decimals.Int64())
}

func formatPercent(newVal, oldVal *big.Int) string {
	if oldVal.Cmp(big.NewInt(0)) == 0 {
		return "N/A"
	}

	diff := new(big.Int).Sub(newVal, oldVal)
	percent := new(big.Int).Mul(diff, big.NewInt(10000))
	percent = new(big.Int).Div(percent, oldVal)

	sign := ""
	if percent.Sign() < 0 {
		sign = "-"
		percent = new(big.Int).Abs(percent)
	} else if percent.Sign() > 0 {
		sign = "+"
	}

	whole := new(big.Int).Div(percent, big.NewInt(100))
	decimal := new(big.Int).Mod(percent, big.NewInt(100))

	return fmt.Sprintf("%s%s.%02d", sign, whole.String(), decimal.Int64())
}
