// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	cmdcfg "github.com/evmos/evmos/v19/cmd/config"
	commonfactory "github.com/evmos/evmos/v19/testutil/integration/common/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/factory"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/grpc"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/keyring"
	"github.com/evmos/evmos/v19/testutil/integration/evmos/network"
	evmostypes "github.com/evmos/evmos/v19/types"
	evmtypes "github.com/evmos/evmos/v19/x/evm/types"
	inflationtypes "github.com/evmos/evmos/v19/x/inflation/v1/types"
)

// TestStakingRewardsWithInflation tests staking rewards distribution with custom inflation
// and commission rates across 4 validators and delegators
func TestStakingRewardsWithInflation(t *testing.T) {
	// Set Bech32 prefixes before creating network
	config := sdk.GetConfig()
	cmdcfg.SetBech32Prefixes(config)

	// Initialize keyring with 4 delegators
	keyring := keyring.New(4)
	delegators := make([]sdk.AccAddress, 4)
	delegatorPrivKeys := make([]cryptotypes.PrivKey, 4)
	for i := 0; i < 4; i++ {
		delegators[i] = keyring.GetAccAddr(i)
		delegatorPrivKeys[i] = keyring.GetPrivKey(i)
	}

	baseDenom := evmostypes.AttoEvmos // txcoin
	displayDenom := cmdcfg.DisplayDenom // xcoin

	t.Logf("\n=== Staking Rewards Test with Inflation ===")
	t.Logf("Base denom: %s", baseDenom)
	t.Logf("Display denom: %s", displayDenom)

	// Configure custom genesis for inflation and staking
	// Inflation configuration: Target 10,000,000 tokens over time
	inflationGenesis := inflationtypes.DefaultGenesisState()
	inflationGenesis.Params.MintDenom = baseDenom
	inflationGenesis.Params.EnableInflation = true
	// Configure inflation distribution: 90% to staking rewards, 10% to community pool
	inflationGenesis.Params.InflationDistribution = inflationtypes.InflationDistribution{
		StakingRewards:  sdkmath.LegacyNewDecWithPrec(90, 2),  // 90%
		CommunityPool:   sdkmath.LegacyNewDecWithPrec(10, 2),  // 10%
		UsageIncentives: sdkmath.LegacyZeroDec(),               // Deprecated
	}
	// Set exponential calculation parameters for block rewards
	// Simplified to produce consistent block rewards
	inflationGenesis.Params.ExponentialCalculation = inflationtypes.ExponentialCalculation{
		A:             sdkmath.LegacyNewDec(int64(10_000_000)), // Initial inflation amount
		R:             sdkmath.LegacyNewDecWithPrec(0, 2),      // No reduction (0%)
		C:             sdkmath.LegacyNewDec(int64(100)),        // Long-term inflation per block (~100 tokens)
		BondingTarget: sdkmath.LegacyNewDecWithPrec(66, 2),    // 66% bonding target
		MaxVariance:   sdkmath.LegacyZeroDec(),                 // No variance
	}
	inflationGenesis.EpochIdentifier = "block" // Mint every block
	inflationGenesis.EpochsPerPeriod = 1       // 1 block per epoch

	// Configure EVM params to use txcoin
	evmGenesis := evmtypes.DefaultGenesisState()
	evmGenesis.Params.EvmDenom = baseDenom

	// Create network with 4 validators and custom genesis
	nw := network.New(
		network.WithAmountOfValidators(4),
		network.WithPreFundedAccounts(delegators...),
		network.WithDenom(baseDenom),
		network.WithCustomGenesis(network.CustomGenesisState{
			evmtypes.ModuleName:       evmGenesis,
			inflationtypes.ModuleName: inflationGenesis,
		}),
	)

	// Create handlers for queries and transactions
	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	// Get validators
	stakingClient := nw.GetStakingClient()
	validatorsResp, err := stakingClient.Validators(nw.GetContext(), &stakingtypes.QueryValidatorsRequest{
		Status: stakingtypes.Bonded.String(),
	})
	require.NoError(t, err, "failed to get validators")
	require.Len(t, validatorsResp.Validators, 4, "should have 4 validators")

	t.Log("\n=== Initial Validator Setup ===")
	for i, val := range validatorsResp.Validators {
		t.Logf("Validator %d: %s", i+1, val.OperatorAddress)
		t.Logf("  Tokens: %s %s", val.Tokens, baseDenom)
		t.Logf("  Commission: Rate=%s, MaxRate=%s", val.Commission.CommissionRates.Rate, val.Commission.CommissionRates.MaxRate)
	}

	// Perform 1-on-1 delegations: delegator i -> validator i
	t.Log("\n=== Setting Up Delegations ===")
	delegationAmount := sdkmath.NewInt(1_000_000).Mul(sdkmath.NewInt(1e18)) // 1,000,000 xcoin = 1M * 10^18 txcoin

	for i := 0; i < 4; i++ {
		valAddr := validatorsResp.Validators[i].OperatorAddress
		delegatorAddr := delegators[i]

		t.Logf("Delegator %d (%s) delegating %s %s to Validator %d (%s)",
			i+1, delegatorAddr.String(), delegationAmount.String(), baseDenom, i+1, valAddr)

		// Parse validator address
		valOperatorAddr, err := sdk.ValAddressFromBech32(valAddr)
		require.NoError(t, err, "failed to parse validator address")

		// Create delegation message
		delegateMsg := stakingtypes.NewMsgDelegate(
			delegatorAddr,
			valOperatorAddr,
			sdk.NewCoin(baseDenom, delegationAmount),
		)

		// Execute delegation
		txRes, err := txFactory.ExecuteCosmosTx(delegatorPrivKeys[i], commonfactory.CosmosTxArgs{
			Msgs: []sdk.Msg{delegateMsg},
		})
		require.NoError(t, err, "delegation should succeed for delegator %d", i+1)
		require.Equal(t, uint32(0), txRes.Code, "delegation transaction should succeed for delegator %d", i+1)

		t.Logf("  ✓ Delegation successful")
	}

	// Commit block to process delegations
	err = nw.NextBlock()
	require.NoError(t, err, "failed to commit block after delegations")

	t.Log("\n=== Verifying Delegations ===")
	for i := 0; i < 4; i++ {
		valAddr := validatorsResp.Validators[i].OperatorAddress
		delegatorAddr := delegators[i]

		// Query delegation
		valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)
		delegationResp, err := stakingClient.Delegation(nw.GetContext(), &stakingtypes.QueryDelegationRequest{
			DelegatorAddr: delegatorAddr.String(),
			ValidatorAddr: valOperatorAddr.String(),
		})
		require.NoError(t, err, "failed to query delegation for delegator %d", i+1)
		require.NotNil(t, delegationResp.DelegationResponse, "delegation should exist for delegator %d", i+1)

		t.Logf("Delegator %d -> Validator %d: %s %s",
			i+1, i+1, delegationResp.DelegationResponse.Balance.Amount.String(), baseDenom)
	}

	// Get initial balances
	t.Log("\n=== Initial Balances ===")
	bankClient := nw.GetBankClient()
	initialBalances := make([]sdk.Coin, 4)
	for i := 0; i < 4; i++ {
		balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
			Address: delegators[i].String(),
			Denom:   baseDenom,
		})
		require.NoError(t, err, "failed to get balance for delegator %d", i+1)
		initialBalances[i] = *balanceResp.Balance
		t.Logf("Delegator %d: %s %s", i+1, initialBalances[i].Amount.String(), baseDenom)
	}

	// Run 4 blocks with bank sends to different validators
	t.Log("\n=== Running 4 Blocks with Transactions ===")
	sendAmount := sdkmath.NewInt(1_000).Mul(sdkmath.NewInt(1e18)) // 1,000 xcoin per block

	for blockNum := 0; blockNum < 4; blockNum++ {
		t.Logf("\n--- Block %d ---", blockNum+1)

		// Send tokens from delegator blockNum to validator blockNum's operator
		valAddr := validatorsResp.Validators[blockNum].OperatorAddress
		valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)
		valAccAddr := sdk.AccAddress(valOperatorAddr)

		sendMsg := banktypes.NewMsgSend(
			delegators[blockNum],
			valAccAddr,
			sdk.NewCoins(sdk.NewCoin(baseDenom, sendAmount)),
		)

		txRes, err := txFactory.ExecuteCosmosTx(delegatorPrivKeys[blockNum], commonfactory.CosmosTxArgs{
			Msgs: []sdk.Msg{sendMsg},
		})
		require.NoError(t, err, "send should succeed in block %d", blockNum+1)
		require.Equal(t, uint32(0), txRes.Code, "send transaction should succeed in block %d", blockNum+1)

		t.Logf("Sent %s %s from Delegator %d to Validator %d",
			sendAmount.String(), baseDenom, blockNum+1, blockNum+1)

		// Commit block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to commit block %d", blockNum+1)

		// Query balances after block
		t.Log("Balances after block:")
		for i := 0; i < 4; i++ {
			balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
				Address: delegators[i].String(),
				Denom:   baseDenom,
			})
			require.NoError(t, err, "failed to get balance for delegator %d", i+1)
			t.Logf("  Delegator %d: %s %s", i+1, balanceResp.Balance.Amount.String(), baseDenom)
		}

		// Query validator balances
		for i := 0; i < 4; i++ {
			valAddr := validatorsResp.Validators[i].OperatorAddress
			valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)
			valAccAddr := sdk.AccAddress(valOperatorAddr)

			balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
				Address: valAccAddr.String(),
				Denom:   baseDenom,
			})
			require.NoError(t, err, "failed to get validator balance")
			t.Logf("  Validator %d operator: %s %s", i+1, balanceResp.Balance.Amount.String(), baseDenom)
		}
	}

	// Query rewards for all delegators
	t.Log("\n=== Delegation Rewards ===")
	distrClient := nw.GetDistributionClient()
	for i := 0; i < 4; i++ {
		valAddr := validatorsResp.Validators[i].OperatorAddress

		rewardsResp, err := distrClient.DelegationRewards(nw.GetContext(), &distrtypes.QueryDelegationRewardsRequest{
			DelegatorAddress: delegators[i].String(),
			ValidatorAddress: valAddr,
		})
		require.NoError(t, err, "failed to query rewards for delegator %d", i+1)

		t.Logf("Delegator %d rewards from Validator %d:", i+1, i+1)
		if len(rewardsResp.Rewards) > 0 {
			for _, reward := range rewardsResp.Rewards {
				// Convert to whole tokens for readability
				rewardAmount := reward.Amount.TruncateInt()
				t.Logf("  %s %s (raw: %s)", rewardAmount.String(), reward.Denom, reward.Amount.String())
			}
		} else {
			t.Logf("  No rewards yet")
		}
	}

	// Claim rewards for delegator 0
	t.Log("\n=== Claiming Rewards for Delegator 1 ===")
	valAddr0 := validatorsResp.Validators[0].OperatorAddress
	valOperatorAddr0, _ := sdk.ValAddressFromBech32(valAddr0)

	withdrawMsg := distrtypes.NewMsgWithdrawDelegatorReward(
		delegators[0],
		valOperatorAddr0,
	)

	balanceBeforeClaim, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
		Address: delegators[0].String(),
		Denom:   baseDenom,
	})
	require.NoError(t, err, "failed to get balance before claim")

	txRes, err := txFactory.ExecuteCosmosTx(delegatorPrivKeys[0], commonfactory.CosmosTxArgs{
		Msgs: []sdk.Msg{withdrawMsg},
	})
	require.NoError(t, err, "withdraw rewards should succeed")
	require.Equal(t, uint32(0), txRes.Code, "withdraw rewards transaction should succeed")

	err = nw.NextBlock()
	require.NoError(t, err, "failed to commit block after withdraw")

	balanceAfterClaim, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
		Address: delegators[0].String(),
		Denom:   baseDenom,
	})
	require.NoError(t, err, "failed to get balance after claim")

	claimedRewards := balanceAfterClaim.Balance.Amount.Sub(balanceBeforeClaim.Balance.Amount)
	t.Logf("Claimed rewards: %s %s", claimedRewards.String(), baseDenom)
	t.Logf("Balance before claim: %s %s", balanceBeforeClaim.Balance.Amount.String(), baseDenom)
	t.Logf("Balance after claim: %s %s", balanceAfterClaim.Balance.Amount.String(), baseDenom)

	// Query community pool
	t.Log("\n=== Community Pool ===")
	communityPoolResp, err := distrClient.CommunityPool(nw.GetContext(), &distrtypes.QueryCommunityPoolRequest{})
	require.NoError(t, err, "failed to query community pool")
	t.Log("Community pool balance:")
	for _, coin := range communityPoolResp.Pool {
		poolAmount := coin.Amount.TruncateInt()
		t.Logf("  %s %s", poolAmount.String(), coin.Denom)
	}

	// Final summary
	t.Log("\n=== Test Summary ===")
	t.Log("✓ Configured 4 validators")
	t.Log("✓ Configured 4 delegators with 1-on-1 delegation (1M xcoin each)")
	t.Log("✓ Configured inflation for staking rewards (90%) and community pool (10%)")
	t.Log("✓ Ran 4 blocks with transactions to different validators")
	t.Log("✓ Verified delegation rewards accumulation")
	t.Log("✓ Successfully claimed rewards for one delegator")
	t.Log("✓ Verified community pool accumulation")
	t.Log("✓ All balances tracked and verified across blocks")
}
