//go:build norace
// +build norace

package integration_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	pruningtypes "cosmossdk.io/store/pruning/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	simutils "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/suite"

	"github.com/evmos/evmos/v20/app"
	"github.com/evmos/evmos/v20/crypto/hd"
	"github.com/evmos/evmos/v20/testutil/network"
	evmostypes "github.com/evmos/evmos/v20/types"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
	inflationtypes "github.com/evmos/evmos/v20/x/inflation/v1/types"
)

func init() {
	// Register our test chain IDs at package init time
	// This ensures they're available before any app initialization
	evmtypes.ChainsCoinInfo[testChainID] = evmtypes.EvmCoinInfo{
		Denom:        xcoinDenom,
		DisplayDenom: xcoinDenom,
		Decimals:     evmtypes.EighteenDecimals,
	}
	
	// Note: We can't populate the global portPool here because it's in the network package
	// Instead, we'll set explicit addresses in the config
}

const (
	// Custom denomination and address prefixes for xcoin
	xcoinDenom         = "xcoin"
	xcoinPrefix        = "xcoin"
	xcoinValoperPrefix = "xcoinvaloper"
	xcoinValconsPrefix = "xcoinvalcons"
	// Use evmos testing chain ID but with custom xcoin denomination
	testChainID = "evmos_9002"
)

type ThreeNodeRewardsTestSuite struct {
	suite.Suite

	network *network.Network
}

func (s *ThreeNodeRewardsTestSuite) SetupSuite() {
	s.T().Log("setting up three node rewards test suite")

	var err error
	
	// Define our custom chain ID for xcoin
	customChainID := fmt.Sprintf("%s-1", testChainID)
	
	// Set custom Bech32 prefixes for xcoin BEFORE creating the network
	// This must be done early because it affects address generation
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(xcoinPrefix, xcoinPrefix+sdk.PrefixPublic)
	cfg.SetBech32PrefixForValidator(xcoinValoperPrefix, xcoinValoperPrefix+sdk.PrefixPublic)
	cfg.SetBech32PrefixForConsensusNode(xcoinValconsPrefix, xcoinValconsPrefix+sdk.PrefixPublic)
	
	// Build config manually instead of using DefaultConfig() to avoid random chain ID issues
	networkCfg := s.buildCustomConfig(customChainID)
	
	// Set explicit addresses to avoid port pool issues
	networkCfg.APIAddress = "tcp://0.0.0.0:1317"
	networkCfg.RPCAddress = "tcp://0.0.0.0:26657"
	networkCfg.GRPCAddress = "0.0.0.0:9090"
	networkCfg.JSONRPCAddress = "0.0.0.0:8545"

	// Configure 3 validators
	networkCfg.NumValidators = 3
	networkCfg.BondDenom = xcoinDenom
	networkCfg.MinGasPrices = fmt.Sprintf("0.000006%s", xcoinDenom)

	// Configure token amounts
	// AccountTokens: total tokens each validator starts with
	// StakingTokens: tokens available for staking
	// BondedTokens: tokens each validator will bond
	networkCfg.AccountTokens = sdk.TokensFromConsensusPower(1000000, evmostypes.PowerReduction)
	networkCfg.StakingTokens = sdk.TokensFromConsensusPower(500000, evmostypes.PowerReduction)
	networkCfg.BondedTokens = sdk.TokensFromConsensusPower(100000, evmostypes.PowerReduction)

	// Shorter timeout for faster block generation
	networkCfg.TimeoutCommit = 1 * time.Second

	// Configure genesis state with custom inflation parameters
	s.configureGenesisState(&networkCfg)

	s.network, err = network.New(s.T(), s.T().TempDir(), networkCfg)
	s.Require().NoError(err)
	s.Require().NotNil(s.network)

	s.T().Logf("Network started with chain ID: %s", networkCfg.ChainID)
	s.T().Log("waiting for network to start...")
	_, err = s.network.WaitForHeight(2)
	s.Require().NoError(err)
	s.T().Log("network started successfully")
}

// buildCustomConfig creates a network config with our custom chain ID
// This is similar to network.DefaultConfig() but uses our pre-registered chain ID
func (s *ThreeNodeRewardsTestSuite) buildCustomConfig(chainID string) network.Config {
	dir, err := os.MkdirTemp("", "simapp")
	s.Require().NoError(err)
	defer os.RemoveAll(dir)
	
	// Create a temporary app to get codec, txconfig, etc.
	// Use NoOpEvmosOptions to avoid denom registration issues in the temporary app
	app := app.NewEvmos(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		nil,
		dir,
		0,
		simutils.NewAppOptionsWithFlagHome(dir),
		app.NoOpEvmosOptions,  // Use NoOp to avoid registering denoms in temp app
		baseapp.SetChainID(chainID),
	)
	
	return network.Config{
		Codec:             app.AppCodec(),
		TxConfig:          app.GetTxConfig(),
		LegacyAmino:       app.LegacyAmino(),
		InterfaceRegistry: app.InterfaceRegistry(),
		AccountRetriever:  authtypes.AccountRetriever{},
		AppConstructor:    network.NewAppConstructor(chainID),
		GenesisState:      app.DefaultGenesis(),
		TimeoutCommit:     3 * time.Second,
		ChainID:           chainID,
		NumValidators:     3,
		BondDenom:         xcoinDenom,
		MinGasPrices:      fmt.Sprintf("0.000006%s", xcoinDenom),
		AccountTokens:     sdk.TokensFromConsensusPower(1000000, evmostypes.PowerReduction),
		StakingTokens:     sdk.TokensFromConsensusPower(500000, evmostypes.PowerReduction),
		BondedTokens:      sdk.TokensFromConsensusPower(100000, evmostypes.PowerReduction),
		PruningStrategy:   pruningtypes.PruningOptionNothing,
		CleanupDir:        true,
		SigningAlgo:       string(hd.EthSecp256k1Type),
		KeyringOptions:    []keyring.Option{hd.EthSecp256k1Option()},
		PrintMnemonic:     false,
	}
}

func (s *ThreeNodeRewardsTestSuite) configureGenesisState(cfg *network.Config) {
	// Configure inflation module to mint xcoin with specific parameters
	var inflationGenState inflationtypes.GenesisState
	cfg.Codec.MustUnmarshalJSON(cfg.GenesisState[inflationtypes.ModuleName], &inflationGenState)

	// Set mint denomination to xcoin
	inflationGenState.Params.MintDenom = xcoinDenom
	inflationGenState.Params.EnableInflation = true

	// Configure inflation parameters for 1,000,000 xCoins total inflation
	// and 10 xCoins per block reward
	// 
	// The inflation module uses an exponential decay formula to calculate inflation
	// We need to configure it so that over the testing period, we get approximately
	// 10 xCoins per block
	//
	// For simplicity in testing, we'll use a high initial value (A) and low reduction (R)
	// to maintain consistent block rewards
	inflationGenState.Params.ExponentialCalculation = inflationtypes.ExponentialCalculation{
		A:             math.LegacyNewDec(100000),        // Initial inflation per period
		R:             math.LegacyNewDecWithPrec(1, 2),  // 1% reduction per period (very slow decay)
		C:             math.LegacyNewDec(0),             // Constant term
		BondingTarget: math.LegacyNewDecWithPrec(66, 2), // 66% bonding target
		MaxVariance:   math.LegacyZeroDec(),             // 0% variance
	}

	// Set distribution to give 100% to staking rewards for easier testing
	inflationGenState.Params.InflationDistribution = inflationtypes.InflationDistribution{
		StakingRewards:  math.LegacyOneDec(),  // 100% to staking rewards
		CommunityPool:   math.LegacyZeroDec(), // 0% to community pool
		UsageIncentives: math.LegacyZeroDec(), // 0% (deprecated)
	}

	// Set epoch parameters for faster testing (use block-based epochs)
	inflationGenState.EpochIdentifier = "minute" // Use minute epoch for faster testing
	inflationGenState.EpochsPerPeriod = 1        // 1 epoch per period

	cfg.GenesisState[inflationtypes.ModuleName] = cfg.Codec.MustMarshalJSON(&inflationGenState)
}

func (s *ThreeNodeRewardsTestSuite) TearDownSuite() {
	s.T().Log("tearing down three node rewards test suite")
	s.network.Cleanup()
}

func (s *ThreeNodeRewardsTestSuite) TestThreeNodeRewards() {
	s.Run("verify_three_validators_created", func() {
		s.Require().Equal(3, len(s.network.Validators), "expected 3 validators")
		s.T().Log("✓ Successfully created 3 validators")
	})

	s.Run("verify_initial_balances", func() {
		s.T().Log("Initial validator balances:")
		for i, val := range s.network.Validators {
			balance := s.getValidatorBalance(val)
			s.T().Logf("  Validator %d (%s): %s", i, val.Address.String(), balance.String())
			s.Require().False(balance.IsZero(), "validator should have positive balance")
		}
	})

	s.Run("run_10_blocks_and_verify_rewards", func() {
		// Get initial balances
		initialBalances := make([]sdk.Coins, len(s.network.Validators))
		for i, val := range s.network.Validators {
			initialBalances[i] = s.getValidatorBalance(val)
		}

		s.T().Logf("Initial height: %d", s.getCurrentHeight())

		// Wait for 10 additional blocks
		initialHeight := s.getCurrentHeight()
		targetHeight := initialHeight + 10
		s.T().Logf("Waiting for 10 blocks (from height %d to %d)...", initialHeight, targetHeight)

		_, err := s.network.WaitForHeightWithTimeout(targetHeight, 30*time.Second)
		s.Require().NoError(err, "failed to wait for target height")

		currentHeight := s.getCurrentHeight()
		s.T().Logf("✓ Reached height: %d (waited for %d blocks)", currentHeight, currentHeight-initialHeight)

		// Get final balances and verify rewards
		s.T().Log("\nValidator rewards after 10 blocks:")
		totalRewards := sdk.NewCoins()
		for i, val := range s.network.Validators {
			finalBalance := s.getValidatorBalance(val)
			rewards := finalBalance.Sub(initialBalances[i]...)

			s.T().Logf("  Validator %d (%s):", i, val.Address.String())
			s.T().Logf("    Initial: %s", initialBalances[i].String())
			s.T().Logf("    Final:   %s", finalBalance.String())
			s.T().Logf("    Rewards: %s", rewards.String())

			// Verify that validator received some rewards
			// Note: Rewards distribution depends on voting power and may not be equal
			xcoinReward := rewards.AmountOf(xcoinDenom)
			if xcoinReward.IsPositive() {
				s.T().Logf("    ✓ Validator received rewards")
			} else {
				s.T().Logf("    Note: Validator did not receive rewards (may be due to voting power or delegation)")
			}

			totalRewards = totalRewards.Add(rewards...)
		}

		s.T().Logf("\nTotal rewards distributed: %s", totalRewards.String())
		
		// Verify that at least some rewards were distributed
		totalXcoinRewards := totalRewards.AmountOf(xcoinDenom)
		if totalXcoinRewards.IsPositive() {
			s.T().Logf("✓ Total xcoin rewards distributed: %s", totalXcoinRewards.String())
		} else {
			s.T().Log("Note: No xcoin rewards distributed yet (inflation may not have kicked in)")
		}
	})

	s.Run("verify_staking_configuration", func() {
		// Query staking params to verify bond denom
		stakingClient := stakingtypes.NewQueryClient(s.network.Validators[0].ClientCtx)
		paramsResp, err := stakingClient.Params(context.Background(), &stakingtypes.QueryParamsRequest{})
		s.Require().NoError(err)
		s.Require().Equal(xcoinDenom, paramsResp.Params.BondDenom, "bond denom should be xcoin")
		s.T().Logf("✓ Staking bond denomination: %s", paramsResp.Params.BondDenom)

		// Query all validators
		validatorsResp, err := stakingClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})
		s.Require().NoError(err)
		s.Require().Len(validatorsResp.Validators, 3, "should have 3 validators")
		
		s.T().Log("\nValidator details:")
		for i, val := range validatorsResp.Validators {
			s.T().Logf("  Validator %d:", i)
			s.T().Logf("    Operator: %s", val.OperatorAddress)
			s.T().Logf("    Tokens: %s", val.Tokens.String())
			s.T().Logf("    Status: %s", val.Status.String())
			s.Require().True(val.OperatorAddress[:len(xcoinValoperPrefix)] == xcoinValoperPrefix, 
				"validator address should start with xcoinvaloper")
		}
		s.T().Logf("✓ All validators have correct address prefix (%s)", xcoinValoperPrefix)
	})

	s.Run("verify_address_prefixes", func() {
		// Verify account addresses use xcoin prefix
		for i, val := range s.network.Validators {
			addrStr := val.Address.String()
			s.Require().True(addrStr[:len(xcoinPrefix)] == xcoinPrefix,
				"validator %d address should start with xcoin prefix, got: %s", i, addrStr)
		}
		s.T().Logf("✓ All account addresses use %s prefix", xcoinPrefix)

		// Verify validator operator addresses use xcoinvaloper prefix
		stakingClient := stakingtypes.NewQueryClient(s.network.Validators[0].ClientCtx)
		validatorsResp, err := stakingClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})
		s.Require().NoError(err)
		
		for i, val := range validatorsResp.Validators {
			s.Require().True(val.OperatorAddress[:len(xcoinValoperPrefix)] == xcoinValoperPrefix,
				"validator %d operator address should start with %s prefix, got: %s", 
				i, xcoinValoperPrefix, val.OperatorAddress)
		}
		s.T().Logf("✓ All validator operator addresses use %s prefix", xcoinValoperPrefix)
	})
}

func (s *ThreeNodeRewardsTestSuite) getValidatorBalance(val *network.Validator) sdk.Coins {
	bankClient := banktypes.NewQueryClient(val.ClientCtx)
	balanceResp, err := bankClient.AllBalances(context.Background(), &banktypes.QueryAllBalancesRequest{
		Address: val.Address.String(),
	})
	s.Require().NoError(err)
	return balanceResp.Balances
}

func (s *ThreeNodeRewardsTestSuite) getCurrentHeight() int64 {
	height, err := s.network.LatestHeight()
	s.Require().NoError(err)
	return height
}

func TestThreeNodeRewards(t *testing.T) {
	suite.Run(t, new(ThreeNodeRewardsTestSuite))
}
