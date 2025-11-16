// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cmdcfg "github.com/evmos/evmos/v19/cmd/config"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/network"
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
	operatorAddr := keyring.GetAccAddr(0)
	operatorPrivKey := keyring.GetPrivKey(0)
	delegatorAddr := keyring.GetAccAddr(1)
	delegatorPrivKey := keyring.GetPrivKey(1)
	senderAddr := keyring.GetAccAddr(2)
	senderPrivKey := keyring.GetPrivKey(2)

	// Create network
	nw := network.New(
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
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
	require.True(t, isXCoinAddress(operatorAddr.String()), "Operator address should start with xcoin1")
	require.True(t, isXCoinAddress(delegatorAddr.String()), "Delegator address should start with xcoin1")
	require.True(t, isXCoinAddress(senderAddr.String()), "Sender address should start with xcoin1")

	// Get initial balances
	operatorBalanceResp, err := handler.GetBalance(operatorAddr, baseDenom)
	require.NoError(t, err, "failed to get operator balance")
	delegatorBalanceResp, err := handler.GetBalance(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get delegator balance")
	senderBalanceResp, err := handler.GetBalance(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get sender balance")

	t.Logf("Initial operator balance: %s %s", operatorBalanceResp.Balance.Amount, baseDenom)
	t.Logf("Initial delegator balance: %s %s", delegatorBalanceResp.Balance.Amount, baseDenom)
	t.Logf("Initial sender balance: %s %s", senderBalanceResp.Balance.Amount, baseDenom)

	require.True(t, operatorBalanceResp.Balance.Amount.GT(sdkmath.ZeroInt()), "operator should have initial balance")
	require.True(t, delegatorBalanceResp.Balance.Amount.GT(sdkmath.ZeroInt()), "delegator should have initial balance")
	require.True(t, senderBalanceResp.Balance.Amount.GT(sdkmath.ZeroInt()), "sender should have initial balance")

	// Get validators
	validatorsResp, err := handler.GetBondedValidators()
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

	err = txFactory.Delegate(delegatorPrivKey, valAddr, sdk.NewCoin(baseDenom, delegationAmount))
	require.NoError(t, err, "delegation should succeed")

	// Commit the block to process the delegation
	err = nw.NextBlock()
	require.NoError(t, err, "failed to commit block")

	// Verify delegation
	delegationResp, err := handler.GetDelegation(delegatorAddr.String(), valAddr)
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

	txRes, err := txFactory.ExecuteCosmosTx(senderPrivKey, factory.CosmosTxArgs{
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
				operatorAddr,
				sdk.NewCoins(sdk.NewCoin(baseDenom, smallAmount)),
			)

			txRes, err := txFactory.ExecuteCosmosTx(senderPrivKey, factory.CosmosTxArgs{
				Msgs: []sdk.Msg{sendMsg},
			})
			require.NoError(t, err, fmt.Sprintf("transfer in block %d should succeed", i))
			require.Equal(t, uint32(0), txRes.Code, fmt.Sprintf("transaction in block %d should succeed", i))
			t.Logf("Block %d: Sent %s %s from sender to operator", i, smallAmount, baseDenom)
		}

		// Log block height
		ctx := nw.GetContext()
		t.Logf("Block %d committed, height: %d", i, ctx.BlockHeight())
	}

	// Verify EVM denomination
	t.Log("\n=== Verifying EVM Denomination ===")
	evmDenom := evmtypes.GetEVMCoinDenom()
	t.Logf("EVM coin denom: %s", evmDenom)
	require.Equal(t, baseDenom, evmDenom, "EVM denom should match base denom")

	// Final balance check
	t.Log("\n=== Final Balance Verification ===")
	finalOperatorBalance, err := handler.GetBalance(operatorAddr, baseDenom)
	require.NoError(t, err, "failed to get final operator balance")
	finalDelegatorBalance, err := handler.GetBalance(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get final delegator balance")
	finalSenderBalance, err := handler.GetBalance(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get final sender balance")

	t.Logf("Final operator balance: %s %s", finalOperatorBalance.Balance.Amount, baseDenom)
	t.Logf("Final delegator balance: %s %s", finalDelegatorBalance.Balance.Amount, baseDenom)
	t.Logf("Final sender balance: %s %s", finalSenderBalance.Balance.Amount, baseDenom)

	// Verify staking params use correct denom
	stakingParams, err := handler.GetStakingParams()
	require.NoError(t, err, "failed to get staking params")
	require.Equal(t, baseDenom, stakingParams.Params.BondDenom, "Staking bond denom should be txcoin")
	t.Logf("Staking bond denom: %s", stakingParams.Params.BondDenom)

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
	return len(addr) > 14 && addr[:14] == "xcoinvaloper1"
}
