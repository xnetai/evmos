// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	grpchandler "github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v20/testutil/integration/common/factory"
	testkeyring "github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/network"
	evmostypes "github.com/evmos/evmos/v20/types"
	"github.com/evmos/evmos/v20/utils"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

// TestTxCoinDenomConfiguration tests that the network properly uses 'txcoin' as base denom
// and 'xcoin' as display denom, verifying transactions, fees, and balances over 10 blocks.
func TestTxCoinDenomConfiguration(t *testing.T) {
	// Create keyring with 2 accounts: one will be delegator, other for transactions
	keyring := testkeyring.New(2)
	delegatorAddr := keyring.GetAccAddr(0)
	delegatorPrivKey := keyring.GetPrivKey(0)
	senderAddr := keyring.GetAccAddr(1)
	senderPrivKey := keyring.GetPrivKey(1)

	// Create a network with the testing chain ID (which uses mainnet denoms)
	chainID := utils.TestingChainID + "-1"

	opts := []network.ConfigOption{
		network.WithChainID(chainID),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
	}

	nw := network.New(opts...)
	handler := grpchandler.NewIntegrationHandler(nw)

	// Get base denom from types package (should be 'txcoin' after our changes)
	baseDenom := evmostypes.BaseDenom
	displayDenom := evmostypes.DisplayDenom

	fmt.Printf("Testing with base denom: %s, display denom: %s\n", baseDenom, displayDenom)

	// Verify base denom is 'txcoin' and display denom is 'xcoin'
	require.Equal(t, "txcoin", baseDenom, "base denom should be 'txcoin'")
	require.Equal(t, "xcoin", displayDenom, "display denom should be 'xcoin'")

	// ------------------------------------------------------------------------------------
	// 1. Initial State Verification
	// ------------------------------------------------------------------------------------

	// Get initial balances
	delegatorBalanceResp, err := handler.GetBalanceFromBank(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get delegator balance")
	initialDelegatorBalance := delegatorBalanceResp.Balance.Amount
	fmt.Printf("Initial delegator balance: %s %s\n", initialDelegatorBalance.String(), baseDenom)

	senderBalanceResp, err := handler.GetBalanceFromBank(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get sender balance")
	initialSenderBalance := senderBalanceResp.Balance.Amount
	fmt.Printf("Initial sender balance: %s %s\n", initialSenderBalance.String(), baseDenom)

	// Get validators (operators)
	validators := nw.GetValidators()
	require.NotEmpty(t, validators, "should have at least one validator")

	validator := validators[0]
	valAddr := sdk.ValAddress(validator.OperatorAddress)
	fmt.Printf("Validator operator address: %s\n", valAddr.String())

	// Get initial validator info
	validatorsResp, err := handler.GetBondedValidators()
	require.NoError(t, err, "failed to get validators")
	require.NotEmpty(t, validatorsResp.Validators, "should have validators")
	initialValTokens := validatorsResp.Validators[0].Tokens
	fmt.Printf("Initial validator tokens: %s\n", initialValTokens.String())

	// ------------------------------------------------------------------------------------
	// 2. Create a Delegation
	// ------------------------------------------------------------------------------------

	delegationAmount := math.NewInt(1_000_000_000_000_000_000) // 1 xcoin in base units (1e18 txcoin)

	txFactory := factory.New(nw, handler)
	err = txFactory.Delegate(
		delegatorPrivKey,
		valAddr.String(),
		sdk.NewCoin(baseDenom, delegationAmount),
	)
	require.NoError(t, err, "failed to delegate")
	fmt.Printf("Delegation transaction completed: %s to validator\n", delegationAmount.String())

	// ------------------------------------------------------------------------------------
	// 3. Run 10 blocks and track state changes
	// ------------------------------------------------------------------------------------

	fmt.Println("\nRunning 10 blocks and tracking state...")

	for i := 1; i <= 10; i++ {
		// Commit next block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to commit block %d", i)

		ctx := nw.GetContext()
		blockHeight := ctx.BlockHeight()
		blockTime := ctx.BlockTime()

		fmt.Printf("Block %d - Height: %d, Time: %s\n", i, blockHeight, blockTime.Format(time.RFC3339))

		// Every 3 blocks, send a transaction to generate fees
		if i%3 == 0 {
			transferAmount := math.NewInt(100_000_000_000_000_000) // 0.1 xcoin

			sendMsg := banktypes.NewMsgSend(
				senderAddr,
				delegatorAddr,
				sdk.NewCoins(sdk.NewCoin(baseDenom, transferAmount)),
			)

			txRes, err := txFactory.ExecuteCosmosTx(senderPrivKey, factory.CosmosTxArgs{
				Msgs: []sdk.Msg{sendMsg},
			})
			require.NoError(t, err, "failed to execute transfer tx at block %d", i)
			require.Equal(t, uint32(0), txRes.Code, "transfer tx failed with code %d: %s", txRes.Code, txRes.Log)

			fmt.Printf("  -> Transfer tx executed with gas used: %d\n", txRes.GasUsed)

			// Commit the transaction
			err = nw.NextBlock()
			require.NoError(t, err, "failed to commit transaction at block %d", i)
		}
	}

	// ------------------------------------------------------------------------------------
	// 4. Final State Verification
	// ------------------------------------------------------------------------------------

	fmt.Println("\nVerifying final state...")

	// Check delegation was created
	delResp, err := handler.GetDelegation(delegatorAddr.String(), valAddr.String())
	require.NoError(t, err, "failed to get delegation")
	require.NotNil(t, delResp.DelegationResponse, "delegation should exist")
	require.Equal(t, delegationAmount.String(), delResp.DelegationResponse.Balance.Amount.String(),
		"delegation amount should match")
	fmt.Printf("Delegation verified: %s %s\n",
		delResp.DelegationResponse.Balance.Amount.String(),
		delResp.DelegationResponse.Balance.Denom)

	// Get final validator info
	finalValidatorsResp, err := handler.GetBondedValidators()
	require.NoError(t, err, "failed to get final validators")
	require.NotEmpty(t, finalValidatorsResp.Validators, "should have validators")
	finalValTokens := finalValidatorsResp.Validators[0].Tokens
	fmt.Printf("Final validator tokens: %s (increased by %s)\n",
		finalValTokens.String(),
		finalValTokens.Sub(initialValTokens).String())

	// Validator tokens should have increased by the delegation amount
	require.True(t, finalValTokens.GT(initialValTokens),
		"validator tokens should have increased")

	// Get final balances
	finalDelegatorResp, err := handler.GetBalanceFromBank(delegatorAddr, baseDenom)
	require.NoError(t, err, "failed to get final delegator balance")
	finalDelegatorBalance := finalDelegatorResp.Balance.Amount
	fmt.Printf("Final delegator balance: %s %s\n", finalDelegatorBalance.String(), baseDenom)

	finalSenderResp, err := handler.GetBalanceFromBank(senderAddr, baseDenom)
	require.NoError(t, err, "failed to get final sender balance")
	finalSenderBalance := finalSenderResp.Balance.Amount
	fmt.Printf("Final sender balance: %s %s\n", finalSenderBalance.String(), baseDenom)

	// Delegator balance should have decreased (delegation + fees) but increased from received transfers
	fmt.Printf("Delegator balance change: %s\n", finalDelegatorBalance.Sub(initialDelegatorBalance).String())

	// Sender balance should have decreased (transfers + fees)
	require.True(t, finalSenderBalance.LT(initialSenderBalance),
		"sender balance should have decreased after transfers and fees")
	fmt.Printf("Sender balance change: %s\n", finalSenderBalance.Sub(initialSenderBalance).String())

	// ------------------------------------------------------------------------------------
	// 5. Verify Fee Market and EVM Configuration
	// ------------------------------------------------------------------------------------

	// Check base fee is properly denominated
	baseFeeResp, err := handler.GetBaseFee()
	require.NoError(t, err, "failed to get base fee")
	require.NotNil(t, baseFeeResp.BaseFee, "base fee should be set")
	fmt.Printf("Base fee: %s\n", baseFeeResp.BaseFee.String())

	// Verify EVM denom configuration
	evmDenom := evmtypes.GetEVMCoinDenom()
	require.Equal(t, baseDenom, evmDenom,
		"EVM denom should match base denom")
	fmt.Printf("EVM denom verified: %s\n", evmDenom)

	// ------------------------------------------------------------------------------------
	// 6. Verify Bank Metadata
	// ------------------------------------------------------------------------------------

	bankClient := nw.GetBankClient()
	metadataResp, err := bankClient.DenomMetadata(nw.GetContext(),
		&banktypes.QueryDenomMetadataRequest{Denom: baseDenom})
	require.NoError(t, err, "failed to get denom metadata")

	require.Equal(t, baseDenom, metadataResp.Metadata.Base,
		"metadata base should be txcoin")
	require.Equal(t, displayDenom, metadataResp.Metadata.Display,
		"metadata display should be xcoin")
	require.Len(t, metadataResp.Metadata.DenomUnits, 2,
		"should have 2 denom units")

	fmt.Printf("Bank metadata verified:\n")
	fmt.Printf("  Base: %s\n", metadataResp.Metadata.Base)
	fmt.Printf("  Display: %s\n", metadataResp.Metadata.Display)
	fmt.Printf("  Name: %s\n", metadataResp.Metadata.Name)
	fmt.Printf("  Symbol: %s\n", metadataResp.Metadata.Symbol)

	fmt.Println("\n✓ All verifications passed successfully!")
}

// TestTxCoinWith18Decimals verifies that txcoin uses 18 decimals correctly
func TestTxCoinWith18Decimals(t *testing.T) {
	keyring := testkeyring.New(1)

	opts := []network.ConfigOption{
		network.WithChainID(utils.TestingChainID + "-1"),
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
	}

	nw := network.New(opts...)
	handler := grpchandler.NewIntegrationHandler(nw)

	baseDenom := evmostypes.BaseDenom

	// Check EVM balance (should always be 18 decimals)
	evmBalResp, err := handler.GetBalanceFromEVM(keyring.GetAccAddr(0))
	require.NoError(t, err, "failed to get EVM balance")
	require.Equal(t,
		network.GetInitialAmount(evmtypes.EighteenDecimals).String(),
		evmBalResp.Balance,
		"EVM balance should be in 18 decimals",
	)

	// Check bank balance (should match the base denom decimals)
	bankBalResp, err := handler.GetBalanceFromBank(keyring.GetAccAddr(0), baseDenom)
	require.NoError(t, err, "failed to get bank balance")
	require.Equal(t,
		network.GetInitialAmount(evmtypes.EighteenDecimals).String(),
		bankBalResp.Balance.Amount.String(),
		"Bank balance should be in 18 decimals for txcoin",
	)

	fmt.Printf("✓ Decimals verification passed!\n")
	fmt.Printf("  EVM Balance: %s\n", evmBalResp.Balance)
	fmt.Printf("  Bank Balance: %s %s\n", bankBalResp.Balance.Amount.String(), baseDenom)
}
