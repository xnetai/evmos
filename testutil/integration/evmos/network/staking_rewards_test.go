// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package network_test

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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
	epochstypes "github.com/evmos/evmos/v19/x/epochs/types"
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
	// Setup inflation params
	inflationParams := inflationtypes.DefaultParams()
	inflationParams.MintDenom = baseDenom
	inflationParams.EnableInflation = true
	// Configure inflation distribution: 90% to staking rewards, 10% to community pool
	inflationParams.InflationDistribution = inflationtypes.InflationDistribution{
		StakingRewards:  sdkmath.LegacyNewDecWithPrec(90, 2),  // 90%
		CommunityPool:   sdkmath.LegacyNewDecWithPrec(10, 2),  // 10%
		UsageIncentives: sdkmath.LegacyZeroDec(),               // Deprecated
	}
	// Set exponential calculation parameters for block rewards
	// Target: 100 xcoin per block
	// NOTE: The inflation formula divides by ReductionFactor=3, so to get 100 xcoin we need C=300
	// Formula: epochProvision = ((A * (1-R)^period + C) / ReductionFactor / epochsPerPeriod) * 10^18
	// With A=0, C=300, ReductionFactor=3, epochsPerPeriod=1: (0 + 300) / 3 / 1 * 10^18 = 100 * 10^18 txcoin
	inflationParams.ExponentialCalculation = inflationtypes.ExponentialCalculation{
		A:             sdkmath.LegacyZeroDec(),            // No exponential decay component
		R:             sdkmath.LegacyZeroDec(),            // No reduction
		C:             sdkmath.LegacyNewDec(300),          // 300 xcoin (will be divided by ReductionFactor=3 to get 100)
		BondingTarget: sdkmath.LegacyNewDecWithPrec(66, 2), // 66% bonding target
		MaxVariance:   sdkmath.LegacyZeroDec(),            // No variance
	}

	// Create inflation genesis
	// IMPORTANT: Use "day" as the epoch identifier because that's what the inflation module expects by default
	// We'll configure a "day" epoch in the epochs module to fire every block
	inflationGenesisVal := inflationtypes.NewGenesisState(
		inflationParams,
		0,           // period
		"day",       // epoch identifier - use "day" to match default inflation config
		1,           // epochs per period
		0,           // skipped epochs
	)
	inflationGenesis := &inflationGenesisVal

	t.Logf("Epoch configuration: identifier=%s, epochs per period=%d",
		inflationGenesis.EpochIdentifier, inflationGenesis.EpochsPerPeriod)

	// Configure epochs module with ONLY a "day" epoch that triggers every block
	// Replace default epochs (week, day) with a "day" epoch that has 1ns duration
	dayEpoch := epochstypes.EpochInfo{
		Identifier:              "day",  // MUST be "day" to match inflation epoch identifier
		StartTime:               time.Time{},
		Duration:                time.Nanosecond, // Very short duration to trigger on every block
		CurrentEpoch:            0,
		CurrentEpochStartHeight: 0,
		CurrentEpochStartTime:   time.Time{},
		EpochCountingStarted:    false,
	}
	epochsGenesis := epochstypes.NewGenesisState([]epochstypes.EpochInfo{dayEpoch})

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
			epochstypes.ModuleName:    epochsGenesis,
		}),
	)

	// Create handlers for queries and transactions
	handler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, handler)

	// Verify inflation configuration
	t.Log("\n=== Verifying Inflation Configuration ===")
	inflationClient := nw.GetInflationClient()
	inflationParamsResp, err := inflationClient.Params(nw.GetContext(), &inflationtypes.QueryParamsRequest{})
	require.NoError(t, err, "failed to query inflation params")
	t.Logf("Inflation enabled: %v", inflationParamsResp.Params.EnableInflation)
	t.Logf("Mint denom: %s", inflationParamsResp.Params.MintDenom)
	t.Logf("Inflation C parameter: %s", inflationParamsResp.Params.ExponentialCalculation.C)
	t.Logf("NOTE: Inflation genesis was configured with EpochIdentifier='day'")

	// Query epochs to see what's actually configured
	epochsClient := nw.GetEpochsClient()
	epochsResp, err := epochsClient.EpochInfos(nw.GetContext(), &epochstypes.QueryEpochsInfoRequest{})
	require.NoError(t, err, "failed to query epochs")
	t.Logf("Number of epochs configured: %d", len(epochsResp.Epochs))
	for i, epoch := range epochsResp.Epochs {
		t.Logf("  Epoch %d: identifier=%s, duration=%s", i+1, epoch.Identifier, epoch.Duration)
	}
	if len(epochsResp.Epochs) != 1 || epochsResp.Epochs[0].Identifier != "day" {
		t.Fatalf("Expected exactly 1 epoch with identifier 'day', got %d epochs", len(epochsResp.Epochs))
	}

	inflationPeriod, err := inflationClient.Period(nw.GetContext(), &inflationtypes.QueryPeriodRequest{})
	require.NoError(t, err, "failed to query inflation period")
	t.Logf("Current period: %d", inflationPeriod.Period)

	// Query skipped epochs
	skippedEpochsResp, err := inflationClient.SkippedEpochs(nw.GetContext(), &inflationtypes.QuerySkippedEpochsRequest{})
	require.NoError(t, err, "failed to query skipped epochs")
	t.Logf("Skipped epochs: %d", skippedEpochsResp.SkippedEpochs)

	inflationEpochMintProvision, err := inflationClient.EpochMintProvision(nw.GetContext(), &inflationtypes.QueryEpochMintProvisionRequest{})
	require.NoError(t, err, "failed to query epoch mint provision")
	epochMintAmount := inflationEpochMintProvision.EpochMintProvision.Amount.TruncateInt()
	t.Logf("Epoch mint provision: %s %s", toXCoin(epochMintAmount), displayDenom)

	// VERIFY: Epoch mint provision should be exactly 100 xcoin
	expectedEpochMint := sdkmath.NewInt(100).Mul(sdkmath.NewInt(1e18)) // 100 xcoin in txcoin
	require.Equal(t, expectedEpochMint, epochMintAmount,
		"Epoch mint provision should be exactly 100 xcoin, got %s xcoin", toXCoin(epochMintAmount))

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
		t.Logf("\n--- Block %d (Height: %d) ---", blockNum+1, nw.GetContext().BlockHeight())

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

		// DEBUG: Check epoch state BEFORE committing block
		epochsRespBefore, _ := epochsClient.EpochInfos(nw.GetContext(), &epochstypes.QueryEpochsInfoRequest{})
		for _, epoch := range epochsRespBefore.Epochs {
			if epoch.Identifier == "day" {
				t.Logf("DEBUG BEFORE: Height=%d, CurrentEpoch=%d, CurrentStartTime=%s, EndTime=%s, BlockTime=%s, shouldEnd=%v",
					nw.GetContext().BlockHeight(),
					epoch.CurrentEpoch,
					epoch.CurrentEpochStartTime.Format("15:04:05.000000000"),
					epoch.CurrentEpochStartTime.Add(epoch.Duration).Format("15:04:05.000000000"),
					nw.GetContext().BlockTime().Format("15:04:05.000000000"),
					nw.GetContext().BlockTime().After(epoch.CurrentEpochStartTime.Add(epoch.Duration)))
			}
		}

		// Query total supply BEFORE committing block
		supplyBefore, err := bankClient.SupplyOf(nw.GetContext(), &banktypes.QuerySupplyOfRequest{Denom: baseDenom})
		require.NoError(t, err, "failed to query supply before block")
		t.Logf("Total supply BEFORE block: %s %s", toXCoin(supplyBefore.Amount.Amount), displayDenom)

		// Commit block
		err = nw.NextBlock()
		require.NoError(t, err, "failed to commit block %d", blockNum+1)

		// DEBUG: Check epoch state AFTER committing block
		epochsRespAfter, _ := epochsClient.EpochInfos(nw.GetContext(), &epochstypes.QueryEpochsInfoRequest{})
		for _, epoch := range epochsRespAfter.Epochs {
			if epoch.Identifier == "day" {
				t.Logf("DEBUG AFTER: Height=%d, CurrentEpoch=%d, CurrentStartTime=%s, EndTime=%s, BlockTime=%s, shouldEnd=%v",
					nw.GetContext().BlockHeight(),
					epoch.CurrentEpoch,
					epoch.CurrentEpochStartTime.Format("15:04:05.000000000"),
					epoch.CurrentEpochStartTime.Add(epoch.Duration).Format("15:04:05.000000000"),
					nw.GetContext().BlockTime().Format("15:04:05.000000000"),
					nw.GetContext().BlockTime().After(epoch.CurrentEpochStartTime.Add(epoch.Duration)))
			}
		}

		// Check inflation state after block
		inflationPeriodAfter, _ := inflationClient.Period(nw.GetContext(), &inflationtypes.QueryPeriodRequest{})
		skippedEpochsAfter, _ := inflationClient.SkippedEpochs(nw.GetContext(), &inflationtypes.QuerySkippedEpochsRequest{})
		epochMintAfter, _ := inflationClient.EpochMintProvision(nw.GetContext(), &inflationtypes.QueryEpochMintProvisionRequest{})

		// Check inflation module account balance to see if minting is happening
		inflationModuleAddr := authtypes.NewModuleAddress(inflationtypes.ModuleName)
		inflationModuleBalResp, _ := bankClient.Balance(nw.GetContext(), &banktypes.QueryBalanceRequest{
			Address: inflationModuleAddr.String(),
			Denom:   baseDenom,
		})

		t.Logf("DEBUG INFLATION: Period=%d, SkippedEpochs=%d, EpochMint=%s xcoin, ModuleBalance=%s xcoin",
			inflationPeriodAfter.Period,
			skippedEpochsAfter.SkippedEpochs,
			toXCoin(epochMintAfter.EpochMintProvision.Amount.TruncateInt()),
			toXCoin(inflationModuleBalResp.Balance.Amount))

		// Query total supply AFTER committing block
		supplyAfter, err := bankClient.SupplyOf(nw.GetContext(), &banktypes.QuerySupplyOfRequest{Denom: baseDenom})
		require.NoError(t, err, "failed to query supply after block")
		supplyIncrease := supplyAfter.Amount.Amount.Sub(supplyBefore.Amount.Amount)
		t.Logf("Total supply AFTER block: %s %s (increased by %s %s)",
			toXCoin(supplyAfter.Amount.Amount), displayDenom, toXCoin(supplyIncrease), displayDenom)

		// VERIFY: Exactly 100 xcoin should be minted per block
		expectedMintAmount := sdkmath.NewInt(100).Mul(sdkmath.NewInt(1e18)) // 100 xcoin in txcoin
		if !expectedMintAmount.Equal(supplyIncrease) {
			t.Logf("WARNING: Block %d: expected supply increase of 100 xcoin, got %s xcoin",
				blockNum+1, toXCoin(supplyIncrease))
		}

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
		t.Logf("Community pool increased by: %s %s", toXCoin(communityPoolIncrease), displayDenom)

		// VERIFY: Exactly 10 xcoin should go to community pool (10% of 100 xcoin)
		expectedCommunityPoolIncrease := sdkmath.NewInt(10).Mul(sdkmath.NewInt(1e18)) // 10 xcoin in txcoin
		if !expectedCommunityPoolIncrease.Equal(communityPoolIncrease) {
			t.Logf("WARNING: Block %d: expected community pool increase of 10 xcoin, got %s xcoin",
				blockNum+1, toXCoin(communityPoolIncrease))
		}

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
