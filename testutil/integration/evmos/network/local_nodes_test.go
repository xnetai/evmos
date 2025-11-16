// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"fmt"
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	cmdcfg "github.com/evmos/evmos/v19/cmd/config"
	"github.com/evmos/evmos/v19/contracts"
	commonfactory "github.com/evmos/evmos/v19/testutil/integration/common/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/network"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/utils"
	evmostypes "github.com/evmos/evmos/v19/types"
	evmtypes "github.com/evmos/evmos/v19/x/evm/types"
)

// TestLocalNodesWithTxCoinDenom tests running local nodes with txcoin/xcoin denomination
func TestLocalNodesWithTxCoinDenom(t *testing.T) {
	// Set Bech32 prefixes before creating network
	config := sdk.GetConfig()
	cmdcfg.SetBech32Prefixes(config)
	cmdcfg.SetBip44CoinType(config)

	// Create keyring with test accounts
	keyring := keyring.New(3)
	delegatorAddr := keyring.GetAccAddr(0)
	delegatorPrivKey := keyring.GetPrivKey(0)
	senderAddr := keyring.GetAccAddr(1)
	senderPrivKey := keyring.GetPrivKey(1)

	// Create network with txcoin denomination
	// Configure EVM params to use txcoin as the EVM denomination
	evmGenesis := evmtypes.DefaultGenesisState()
	evmGenesis.Params.EvmDenom = evmostypes.AttoEvmos // Set EVM denom to txcoin

	nw := network.New(
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
		network.WithDenom(evmostypes.AttoEvmos), // Use txcoin as the base denomination
		network.WithCustomGenesis(network.CustomGenesisState{
			evmtypes.ModuleName: evmGenesis,
		}),
	)

	// Create handlers for queries and transactions
	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	t.Log("=== Testing XCoin Denomination Configuration ===")

	// Verify denomination constants
	baseDenom := evmostypes.AttoEvmos
	displayDenom := evmostypes.DisplayDenom
	t.Logf("Base denom: %s", baseDenom)
	t.Logf("Display denom: %s", displayDenom)
	require.Equal(t, "txcoin", baseDenom, "Base denomination should be txcoin")
	require.Equal(t, "xcoin", displayDenom, "Display denomination should be xcoin")

	// Verify Bech32 prefixes
	require.Equal(t, "xcoin", cmdcfg.Bech32Prefix, "Bech32 prefix should be xcoin")
	require.True(t, isXCoinAddress(delegatorAddr.String()), "Delegator address should start with xcoin1")
	require.True(t, isXCoinAddress(senderAddr.String()), "Sender address should start with xcoin1")

	// Get initial balances
	delegatorBalanceResp, err := handler.GetBalance(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get delegator balance")
	senderBalanceResp, err := handler.GetBalance(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get sender balance")

	t.Logf("Initial delegator balance: %s %s", delegatorBalanceResp.Balance.Amount, baseDenom)
	t.Logf("Initial sender balance: %s %s", senderBalanceResp.Balance.Amount, baseDenom)

	require.True(t, delegatorBalanceResp.Balance.Amount.GT(sdkmath.ZeroInt()), "delegator should have initial balance")
	require.True(t, senderBalanceResp.Balance.Amount.GT(sdkmath.ZeroInt()), "sender should have initial balance")

	// Get validators
	stakingClient := nw.GetStakingClient()
	validatorsResp, err := stakingClient.Validators(nw.GetContext(), &stakingtypes.QueryValidatorsRequest{
		Status: stakingtypes.Bonded.String(),
	})
	require.NoError(t, err, "failed to get validators")
	require.NotEmpty(t, validatorsResp.Validators, "should have at least one validator")

	validator := validatorsResp.Validators[0]
	valAddr := validator.OperatorAddress
	t.Logf("Validator operator address: %s", valAddr)
	require.True(t, isXCoinValidatorAddress(valAddr), "Validator address should start with xcoinvaloper1")

	t.Logf("Initial validator tokens: %s", validator.Tokens)

	// Test delegation
	t.Log("\n=== Testing Delegation ===")
	delegationAmount := sdkmath.NewInt(1_000_000_000_000_000_000) // 1 xcoin = 10^18 txcoin
	t.Logf("Delegating %s %s to validator %s", delegationAmount, baseDenom, valAddr)

	// Create delegation message
	valOperatorAddr, err := sdk.ValAddressFromBech32(valAddr)
	require.NoError(t, err, "failed to parse validator address")

	delegateMsg := stakingtypes.NewMsgDelegate(
		delegatorAddr,
		valOperatorAddr,
		sdk.NewCoin(baseDenom, delegationAmount),
	)

	txRes, err := txFactory.ExecuteCosmosTx(delegatorPrivKey, commonfactory.CosmosTxArgs{
		Msgs: []sdk.Msg{delegateMsg},
	})
	require.NoError(t, err, "delegation should succeed")
	if txRes.Code != 0 {
		t.Logf("Delegation failed with code %d: %s", txRes.Code, txRes.Log)
	}
	require.Equal(t, uint32(0), txRes.Code, "delegation transaction should succeed")

	// Commit the block to process the delegation
	err = nw.NextBlock()
	require.NoError(t, err, "failed to commit block")

	// Verify delegation
	delegationResp, err := stakingClient.Delegation(nw.GetContext(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddr.String(),
		ValidatorAddr: valAddr,
	})
	require.NoError(t, err, "failed to get delegation")
	require.NotNil(t, delegationResp.DelegationResponse, "delegation should exist")
	t.Logf("Delegation balance: %s", delegationResp.DelegationResponse.Balance)

	// Test bank transfer
	t.Log("\n=== Testing Bank Transfer ===")
	transferAmount := sdkmath.NewInt(500_000_000_000_000_000) // 0.5 xcoin
	t.Logf("Sending %s %s from sender to delegator", transferAmount, baseDenom)

	sendMsg := banktypes.NewMsgSend(
		senderAddr,
		delegatorAddr,
		sdk.NewCoins(sdk.NewCoin(baseDenom, transferAmount)),
	)

	txRes, err = txFactory.ExecuteCosmosTx(senderPrivKey, commonfactory.CosmosTxArgs{
		Msgs: []sdk.Msg{sendMsg},
	})
	require.NoError(t, err, "transfer should succeed")
	require.NotNil(t, txRes, "transaction response should not be nil")
	require.Equal(t, uint32(0), txRes.Code, "transaction should succeed with code 0")

	// Commit the block
	err = nw.NextBlock()
	require.NoError(t, err, "failed to commit block")

	// Verify balances after transfer
	newDelegatorBalance, err := handler.GetBalance(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get delegator balance")
	t.Logf("Delegator balance after transfer: %s %s", newDelegatorBalance.Balance.Amount, baseDenom)

	newSenderBalance, err := handler.GetBalance(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get sender balance")
	t.Logf("Sender balance after transfer: %s %s", newSenderBalance.Balance.Amount, baseDenom)

	// Run multiple blocks with transactions
	t.Log("\n=== Running 10 Blocks with Transactions ===")
	for i := 1; i <= 10; i++ {
		err = nw.NextBlock()
		require.NoError(t, err, fmt.Sprintf("failed to commit block %d", i))

		// Every 3 blocks, send a small transfer
		if i%3 == 0 {
			smallAmount := sdkmath.NewInt(100_000_000_000_000_000) // 0.1 xcoin
			sendMsg := banktypes.NewMsgSend(
				senderAddr,
				delegatorAddr,
				sdk.NewCoins(sdk.NewCoin(baseDenom, smallAmount)),
			)

			txRes, err := txFactory.ExecuteCosmosTx(senderPrivKey, commonfactory.CosmosTxArgs{
				Msgs: []sdk.Msg{sendMsg},
			})
			require.NoError(t, err, fmt.Sprintf("transfer in block %d should succeed", i))
			require.Equal(t, uint32(0), txRes.Code, fmt.Sprintf("transaction in block %d should succeed", i))
			t.Logf("Block %d: Sent %s %s from sender to delegator", i, smallAmount, baseDenom)
		}

		// Log block height
		ctx := nw.GetContext()
		t.Logf("Block %d committed, height: %d", i, ctx.BlockHeight())
	}

	// Verify EVM denomination
	t.Log("\n=== Verifying EVM Denomination ===")
	evmClient := nw.GetEvmClient()
	evmParamsResp, err := evmClient.Params(nw.GetContext(), &evmtypes.QueryParamsRequest{})
	require.NoError(t, err, "failed to get EVM params")
	evmDenom := evmParamsResp.Params.EvmDenom
	t.Logf("EVM coin denom: %s", evmDenom)
	require.Equal(t, baseDenom, evmDenom, "EVM denom should match base denom")

	// Final balance check
	t.Log("\n=== Final Balance Verification ===")
	finalDelegatorBalance, err := handler.GetBalance(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get final delegator balance")
	finalSenderBalance, err := handler.GetBalance(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get final sender balance")

	t.Logf("Final delegator balance: %s %s", finalDelegatorBalance.Balance.Amount, baseDenom)
	t.Logf("Final sender balance: %s %s", finalSenderBalance.Balance.Amount, baseDenom)

	// Verify staking params use correct denom
	stakingParamsResp, err := stakingClient.Params(nw.GetContext(), &stakingtypes.QueryParamsRequest{})
	require.NoError(t, err, "failed to get staking params")
	require.Equal(t, baseDenom, stakingParamsResp.Params.BondDenom, "Staking bond denom should be txcoin")
	t.Logf("Staking bond denom: %s", stakingParamsResp.Params.BondDenom)

	t.Log("\n=== Test Completed Successfully ===")
	t.Log("✓ Base denomination: txcoin")
	t.Log("✓ Display denomination: xcoin")
	t.Log("✓ Bech32 prefix: xcoin")
	t.Log("✓ Validator addresses: xcoinvaloper1...")
	t.Log("✓ Account addresses: xcoin1...")
	t.Log("✓ Delegations working")
	t.Log("✓ Bank transfers working")
	t.Log("✓ Multiple blocks processed")
	t.Log("✓ EVM denomination configured")
}

// isXCoinAddress checks if an address starts with xcoin1
func isXCoinAddress(addr string) bool {
	return len(addr) > 6 && addr[:6] == "xcoin1"
}

// isXCoinValidatorAddress checks if a validator address starts with xcoinvaloper1
func isXCoinValidatorAddress(addr string) bool {
	return len(addr) >= 13 && addr[:13] == "xcoinvaloper1"
}

// TestDeployFiveERC20Tokens tests deploying 5 ERC20 tokens as EVM contracts
func TestDeployFiveERC20Tokens(t *testing.T) {
	// Set Bech32 prefixes before creating network
	config := sdk.GetConfig()
	cmdcfg.SetBech32Prefixes(config)

	// Initialize keyring with 2 accounts
	keyring := keyring.New(2)
	deployerAddr := keyring.GetAddr(0)      // Ethereum address
	deployerPrivKey := keyring.GetPrivKey(0)
	userAddr := keyring.GetAddr(1)          // Ethereum address
	userPrivKey := keyring.GetPrivKey(1)

	// Create network with txcoin denomination
	// Configure EVM params to use txcoin as the EVM denomination
	evmGenesis := evmtypes.DefaultGenesisState()
	evmGenesis.Params.EvmDenom = evmostypes.AttoEvmos // Set EVM denom to txcoin

	nw := network.New(
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
		network.WithDenom(evmostypes.AttoEvmos), // Use txcoin as the base denomination
		network.WithCustomGenesis(network.CustomGenesisState{
			evmtypes.ModuleName: evmGenesis,
		}),
	)

	// Create handlers for queries and transactions
	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	t.Log("\n=== Testing ERC20 Token Deployment ===")

	// Define 5 different ERC20 tokens to deploy
	type tokenInfo struct {
		name     string
		symbol   string
		decimals uint8
	}

	tokens := []tokenInfo{
		{"Bitcoin Wrapped", "WBTC", 8},
		{"Ethereum Wrapped", "WETH", 18},
		{"USD Coin", "USDC", 6},
		{"Tether USD", "USDT", 6},
		{"Dai Stablecoin", "DAI", 18},
	}

	// Import contracts package for ERC20 contract
	var deployedContracts []struct {
		address common.Address
		token   tokenInfo
	}

	// Deploy each token
	for i, token := range tokens {
		t.Logf("\n--- Deploying Token %d/%d: %s (%s) ---", i+1, len(tokens), token.name, token.symbol)

		// Deploy the contract
		contractAddr, err := txFactory.DeployContract(
			deployerPrivKey,
			evmtypes.EvmTxArgs{},
			factory.ContractDeploymentData{
				Contract:        contracts.ERC20MinterBurnerDecimalsContract,
				ConstructorArgs: []interface{}{token.name, token.symbol, token.decimals},
			},
		)
		require.NoError(t, err, "failed to deploy %s contract", token.symbol)
		t.Logf("✓ Contract deployed at: %s", contractAddr.Hex())

		// Store deployed contract info
		deployedContracts = append(deployedContracts, struct {
			address common.Address
			token   tokenInfo
		}{contractAddr, token})

		// Advance block to finalize deployment
		err = nw.NextBlock()
		require.NoError(t, err, "failed to advance block after deployment")
	}

	t.Log("\n=== Testing ERC20 Token Functionality ===")

	// Test each deployed token
	for i, deployed := range deployedContracts {
		t.Logf("\n--- Testing Token %d/%d: %s (%s) at %s ---",
			i+1, len(deployedContracts), deployed.token.name, deployed.token.symbol, deployed.address.Hex())

		// 1. Check initial balance (should be 0)
		balance, err := utils.GetERC20Balance(nw, deployed.address, deployerAddr)
		require.NoError(t, err, "failed to get deployer balance for %s", deployed.token.symbol)
		require.Equal(t, common.Big0.Int64(), balance.Int64(), "deployer should have zero initial balance")
		t.Logf("✓ Initial deployer balance: %s %s", balance.String(), deployed.token.symbol)

		// 2. Mint tokens to deployer
		mintAmount := new(big.Int).Mul(big.NewInt(1_000_000), new(big.Int).Lsh(big.NewInt(1), uint(deployed.token.decimals))) // 1M tokens
		_, err = txFactory.ExecuteContractCall(
			deployerPrivKey,
			evmtypes.EvmTxArgs{
				To: &deployed.address,
			},
			factory.CallArgs{
				ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
				MethodName:  "mint",
				Args:        []interface{}{deployerAddr, mintAmount},
			},
		)
		require.NoError(t, err, "failed to mint %s tokens", deployed.token.symbol)
		t.Logf("✓ Minted %s tokens to deployer", mintAmount.String())

		// Advance block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to advance block after minting")

		// 3. Check balance after minting
		balance, err = utils.GetERC20Balance(nw, deployed.address, deployerAddr)
		require.NoError(t, err, "failed to get deployer balance after minting")
		require.Equal(t, mintAmount.String(), balance.String(), "deployer should have minted amount")
		t.Logf("✓ Deployer balance after mint: %s %s", balance.String(), deployed.token.symbol)

		// 4. Transfer tokens to user
		transferAmount := new(big.Int).Mul(big.NewInt(100_000), new(big.Int).Lsh(big.NewInt(1), uint(deployed.token.decimals))) // 100k tokens
		_, err = txFactory.ExecuteContractCall(
			deployerPrivKey,
			evmtypes.EvmTxArgs{
				To: &deployed.address,
			},
			factory.CallArgs{
				ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
				MethodName:  "transfer",
				Args:        []interface{}{userAddr, transferAmount},
			},
		)
		require.NoError(t, err, "failed to transfer %s tokens", deployed.token.symbol)
		t.Logf("✓ Transferred %s tokens to user", transferAmount.String())

		// Advance block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to advance block after transfer")

		// 5. Check user balance
		userBalance, err := utils.GetERC20Balance(nw, deployed.address, userAddr)
		require.NoError(t, err, "failed to get user balance")
		require.Equal(t, transferAmount.String(), userBalance.String(), "user should have transferred amount")
		t.Logf("✓ User balance: %s %s", userBalance.String(), deployed.token.symbol)

		// 6. Check deployer balance after transfer
		deployerBalance, err := utils.GetERC20Balance(nw, deployed.address, deployerAddr)
		require.NoError(t, err, "failed to get deployer balance after transfer")
		expectedBalance := new(big.Int).Sub(mintAmount, transferAmount)
		require.Equal(t, expectedBalance.String(), deployerBalance.String(), "deployer balance should be reduced by transfer amount")
		t.Logf("✓ Deployer balance after transfer: %s %s", deployerBalance.String(), deployed.token.symbol)

		// 7. User transfers back to deployer
		transferBack := new(big.Int).Mul(big.NewInt(10_000), new(big.Int).Lsh(big.NewInt(1), uint(deployed.token.decimals))) // 10k tokens
		_, err = txFactory.ExecuteContractCall(
			userPrivKey,
			evmtypes.EvmTxArgs{
				To: &deployed.address,
			},
			factory.CallArgs{
				ContractABI: contracts.ERC20MinterBurnerDecimalsContract.ABI,
				MethodName:  "transfer",
				Args:        []interface{}{deployerAddr, transferBack},
			},
		)
		require.NoError(t, err, "failed to transfer %s tokens back", deployed.token.symbol)
		t.Logf("✓ User transferred %s tokens back to deployer", transferBack.String())

		// Advance block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to advance block after transfer back")

		// 8. Verify final balances
		finalUserBalance, err := utils.GetERC20Balance(nw, deployed.address, userAddr)
		require.NoError(t, err, "failed to get final user balance")
		expectedUserBalance := new(big.Int).Sub(transferAmount, transferBack)
		require.Equal(t, expectedUserBalance.String(), finalUserBalance.String(), "user balance should be reduced by transfer back")
		t.Logf("✓ Final user balance: %s %s", finalUserBalance.String(), deployed.token.symbol)

		finalDeployerBalance, err := utils.GetERC20Balance(nw, deployed.address, deployerAddr)
		require.NoError(t, err, "failed to get final deployer balance")
		expectedFinalDeployerBalance := new(big.Int).Add(expectedBalance, transferBack)
		require.Equal(t, expectedFinalDeployerBalance.String(), finalDeployerBalance.String(), "deployer balance should increase by transfer back")
		t.Logf("✓ Final deployer balance: %s %s", finalDeployerBalance.String(), deployed.token.symbol)
	}

	t.Log("\n=== All Tests Completed Successfully ===")
	t.Logf("✓ Deployed %d ERC20 tokens", len(deployedContracts))
	t.Log("✓ All tokens tested for:")
	t.Log("  - Deployment")
	t.Log("  - Minting")
	t.Log("  - Transfers")
	t.Log("  - Balance queries")
}
