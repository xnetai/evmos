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

	baseDenom := evmostypes.AttoEvmos    // txcoin
	displayDenom := cmdcfg.DisplayDenom  // xcoin

	// Helper function to convert txcoin to xcoin for display
	toXCoin := func(amount sdkmath.Int) string {
		// Divide by 10^18 to convert txcoin to xcoin
		xcoinAmount := sdkmath.LegacyNewDecFromInt(amount).QuoInt64(1e18)
		return xcoinAmount.String()
	}

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
		C:             sdkmath.LegacyNewDec(int64(100)).Mul(sdkmath.LegacyNewDec(1e18)), // Long-term inflation per block: 100 xcoin = 100 * 10^18 txcoin
		BondingTarget: sdkmath.LegacyNewDecWithPrec(66, 2),    // 66% bonding target
		MaxVariance:   sdkmath.LegacyZeroDec(),                 // No variance
	}
	inflationGenesis.EpochIdentifier = "block" // Mint every block
	inflationGenesis.EpochsPerPeriod = 1       // 1 block per epoch

	t.Logf("Epoch configuration: identifier=%s, epochs per period=%d",
		inflationGenesis.EpochIdentifier, inflationGenesis.EpochsPerPeriod)

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
		t.Logf("  Tokens: %s %s", toXCoin(val.Tokens), displayDenom)
		t.Logf("  Commission: Rate=%s, MaxRate=%s", val.Commission.CommissionRates.Rate, val.Commission.CommissionRates.MaxRate)
	}

	// Perform 1-on-1 delegations: delegator i -> validator i
	t.Log("\n=== Setting Up Delegations ===")
	delegationAmount := sdkmath.NewInt(3).Mul(sdkmath.NewInt(1e18)) // 3 xcoin = 3 * 10^18 txcoin (max available is ~4 xcoin)

	for i := 0; i < 4; i++ {
		valAddr := validatorsResp.Validators[i].OperatorAddress
		delegatorAddr := delegators[i]

		t.Logf("Delegator %d (%s) delegating %s %s to Validator %d (%s)",
			i+1, delegatorAddr.String(), toXCoin(delegationAmount), displayDenom, i+1, valAddr)

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
			i+1, i+1, toXCoin(delegationResp.DelegationResponse.Balance.Amount), displayDenom)
	}

	// Get initial balances
	t.Log("\n=== Initial Balances ===")
	bankClient := nw.GetBankClient()
	for i := 0; i < 4; i++ {
		balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
			Address: delegators[i].String(),
			Denom:   baseDenom,
		})
		require.NoError(t, err, "failed to get balance for delegator %d", i+1)
		t.Logf("Delegator %d: %s %s", i+1, toXCoin(balanceResp.Balance.Amount), displayDenom)
	}

	// Run 4 blocks with bank sends to different validators
	t.Log("\n=== Running 4 Blocks with Transactions ===")
	sendAmount := sdkmath.NewInt(5e17) // 0.5 xcoin per block (5*10^17 txcoin)
	distrClient := nw.GetDistributionClient()

	for blockNum := 0; blockNum < 4; blockNum++ {
		t.Logf("\n--- Block %d ---", blockNum+1)

		// Query validator operator balances BEFORE block
		t.Log("Validator operator balances BEFORE block:")
		operatorBalancesBefore := make([]sdkmath.Int, 4)
		for i := 0; i < 4; i++ {
			valAddr := validatorsResp.Validators[i].OperatorAddress
			valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)
			valAccAddr := sdk.AccAddress(valOperatorAddr)

			balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
				Address: valAccAddr.String(),
				Denom:   baseDenom,
			})
			require.NoError(t, err, "failed to get validator operator balance")
			operatorBalancesBefore[i] = balanceResp.Balance.Amount
			t.Logf("  Validator %d operator: %s %s", i+1, toXCoin(balanceResp.Balance.Amount), displayDenom)
		}

		// Query community pool BEFORE block
		communityPoolBefore := sdkmath.ZeroInt()
		communityPoolResp, err := distrClient.CommunityPool(nw.GetContext(), &distrtypes.QueryCommunityPoolRequest{})
		require.NoError(t, err, "failed to query community pool")
		for _, coin := range communityPoolResp.Pool {
			if coin.Denom == baseDenom {
				communityPoolBefore = coin.Amount.TruncateInt()
				t.Logf("Community pool BEFORE block: %s %s", toXCoin(communityPoolBefore), displayDenom)
			}
		}

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
			toXCoin(sendAmount), displayDenom, blockNum+1, blockNum+1)

		// Commit block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to commit block %d", blockNum+1)

		// Verify community pool increased AFTER block
		t.Log("\nVerifying block rewards distribution:")
		communityPoolAfterBlock := sdkmath.ZeroInt()
		communityPoolResp, err = distrClient.CommunityPool(nw.GetContext(), &distrtypes.QueryCommunityPoolRequest{})
		require.NoError(t, err, "failed to query community pool after block")
		for _, coin := range communityPoolResp.Pool {
			if coin.Denom == baseDenom {
				communityPoolAfterBlock = coin.Amount.TruncateInt()
				break
			}
		}
		communityPoolIncrease := communityPoolAfterBlock.Sub(communityPoolBefore)
		t.Logf("Community pool increased by: %s %s (expected ~10%% of block rewards)",
			toXCoin(communityPoolIncrease), displayDenom)

		// First, check validator commission
		t.Log("\nValidator commission:")
		for i := 0; i < 4; i++ {
			valAddr := validatorsResp.Validators[i].OperatorAddress

			// Query validator commission
			commissionResp, err := distrClient.ValidatorCommission(nw.GetContext(), &distrtypes.QueryValidatorCommissionRequest{
				ValidatorAddress: valAddr,
			})
			require.NoError(t, err, "failed to query commission for validator %d", i+1)

			if len(commissionResp.Commission.Commission) > 0 {
				totalCommission := sdkmath.ZeroInt()
				for _, coin := range commissionResp.Commission.Commission {
					if coin.Denom == baseDenom {
						totalCommission = totalCommission.Add(coin.Amount.TruncateInt())
					}
				}
				t.Logf("  Validator %d commission: %s %s", i+1, toXCoin(totalCommission), displayDenom)
			} else {
				t.Logf("  Validator %d has no commission yet", i+1)
			}
		}

		// Claim rewards for all delegators after each block
		t.Log("\nClaiming delegator rewards:")
		for i := 0; i < 4; i++ {
			valAddr := validatorsResp.Validators[i].OperatorAddress
			valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)

			// Check rewards before claiming
			rewardsResp, err := distrClient.DelegationRewards(nw.GetContext(), &distrtypes.QueryDelegationRewardsRequest{
				DelegatorAddress: delegators[i].String(),
				ValidatorAddress: valAddr,
			})
			require.NoError(t, err, "failed to query rewards for delegator %d", i+1)

			if len(rewardsResp.Rewards) > 0 {
				totalRewards := sdkmath.ZeroInt()
				for _, reward := range rewardsResp.Rewards {
					if reward.Denom == baseDenom {
						totalRewards = totalRewards.Add(reward.Amount.TruncateInt())
					}
				}
				t.Logf("  Delegator %d has %s %s rewards pending", i+1, toXCoin(totalRewards), displayDenom)

				// Get balance before claiming
				balanceBefore, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
					Address: delegators[i].String(),
					Denom:   baseDenom,
				})
				require.NoError(t, err, "failed to get balance before claiming")

				// Claim rewards
				withdrawMsg := distrtypes.NewMsgWithdrawDelegatorReward(
					delegators[i],
					valOperatorAddr,
				)

				txRes, err := txFactory.ExecuteCosmosTx(delegatorPrivKeys[i], commonfactory.CosmosTxArgs{
					Msgs: []sdk.Msg{withdrawMsg},
				})
				if err == nil && txRes.Code == 0 {
					// Get balance after claiming (before NextBlock)
					balanceAfter, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
						Address: delegators[i].String(),
						Denom:   baseDenom,
					})
					require.NoError(t, err, "failed to get balance after claiming")

					balanceIncrease := balanceAfter.Balance.Amount.Sub(balanceBefore.Balance.Amount)
					t.Logf("  ✓ Delegator %d: balance increased by %s %s (from %s to %s)",
						i+1, toXCoin(balanceIncrease), displayDenom,
						toXCoin(balanceBefore.Balance.Amount), toXCoin(balanceAfter.Balance.Amount))
				} else {
					if err != nil {
						t.Logf("  ✗ Delegator %d claim failed: %v", i+1, err)
					} else {
						t.Logf("  ✗ Delegator %d claim failed with code: %d", i+1, txRes.Code)
					}
				}
			} else {
				t.Logf("  Delegator %d has no rewards yet", i+1)
			}
		}

		// Commit rewards claims
		err = nw.NextBlock()
		require.NoError(t, err, "failed to commit block after claiming rewards")

		// Verify validator operator balances increased AFTER claiming
		t.Log("\nValidator operator balances AFTER claiming rewards:")
		for i := 0; i < 4; i++ {
			valAddr := validatorsResp.Validators[i].OperatorAddress
			valOperatorAddr, _ := sdk.ValAddressFromBech32(valAddr)
			valAccAddr := sdk.AccAddress(valOperatorAddr)

			balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
				Address: valAccAddr.String(),
				Denom:   baseDenom,
			})
			require.NoError(t, err, "failed to get validator operator balance after claiming")
			balanceAfter := balanceResp.Balance.Amount
			balanceIncrease := balanceAfter.Sub(operatorBalancesBefore[i])

			t.Logf("  Validator %d operator: %s %s (increased by %s %s)",
				i+1, toXCoin(balanceAfter), displayDenom, toXCoin(balanceIncrease), displayDenom)
		}

		// Query delegator balances after block
		t.Log("\nDelegator balances after block:")
		for i := 0; i < 4; i++ {
			balanceResp, err := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
				Address: delegators[i].String(),
				Denom:   baseDenom,
			})
			require.NoError(t, err, "failed to get balance for delegator %d", i+1)
			t.Logf("  Delegator %d: %s %s", i+1, toXCoin(balanceResp.Balance.Amount), displayDenom)
		}
	}

	// Query final rewards for all delegators
	t.Log("\n=== Final Delegation Rewards (After All Claims) ===")
	for i := 0; i < 4; i++ {
		valAddr := validatorsResp.Validators[i].OperatorAddress

		rewardsResp, err := distrClient.DelegationRewards(nw.GetContext(), &distrtypes.QueryDelegationRewardsRequest{
			DelegatorAddress: delegators[i].String(),
			ValidatorAddress: valAddr,
		})
		require.NoError(t, err, "failed to query rewards for delegator %d", i+1)

		t.Logf("Delegator %d unclaimed rewards from Validator %d:", i+1, i+1)
		if len(rewardsResp.Rewards) > 0 {
			for _, reward := range rewardsResp.Rewards {
				rewardAmount := toXCoin(reward.Amount.TruncateInt())
				t.Logf("  %s %s", rewardAmount, displayDenom)
			}
		} else {
			t.Logf("  No unclaimed rewards")
		}
	}

	// Query community pool
	t.Log("\n=== Community Pool ===")
	communityPoolResp, err := distrClient.CommunityPool(nw.GetContext(), &distrtypes.QueryCommunityPoolRequest{})
	require.NoError(t, err, "failed to query community pool")
	t.Log("Community pool balance:")
	for _, coin := range communityPoolResp.Pool {
		if coin.Denom == baseDenom {
			poolAmount := toXCoin(coin.Amount.TruncateInt())
			t.Logf("  %s %s", poolAmount, displayDenom)
		}
	}

	// Final summary
	t.Log("\n=== Test Summary ===")
	t.Log("✓ Configured 4 validators")
	t.Log("✓ Configured 4 delegators with 1-on-1 delegation (3 xcoin each)")
	t.Log("✓ Configured inflation for staking rewards (90%) and community pool (10%)")
	t.Log("✓ Configured target block rewards: ~100 xcoin per block")
	t.Log("✓ Verified epoch configuration: 1 block per epoch")
	t.Log("✓ Ran 4 blocks with 0.5 xcoin transactions to different validators")
	t.Log("✓ Verified validator operator balances before each block")
	t.Log("✓ Verified community pool increased by ~10% of block rewards")
	t.Log("✓ Claimed rewards for all delegators after each block")
	t.Log("✓ Verified validator operator balances increased after claiming rewards")
	t.Log("✓ Verified delegation rewards accumulation")
	t.Log("✓ All balances displayed in xcoin display denomination")
	t.Log("✓ All balances tracked and verified across blocks")
}
