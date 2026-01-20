// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpcencoding "google.golang.org/grpc/encoding"

	"github.com/cosmos/gogoproto/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/evmos/evmos/v20/encoding"
	evmosgrpc "github.com/evmos/evmos/v20/testutil/integration/evmos/grpc"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
	feemarkettypes "github.com/evmos/evmos/v20/x/feemarket/types"
	infltypes "github.com/evmos/evmos/v20/x/inflation/v1/types"
)

// protoCodec is a custom codec that uses the cosmos encoding config
type protoCodec struct {
	codec sdktestutil.TestEncodingConfig
}

func (c protoCodec) Marshal(v any) ([]byte, error) {
	pm, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("message is not proto.Message: %T", v)
	}
	return c.codec.Codec.Marshal(pm)
}

func (c protoCodec) Unmarshal(data []byte, v any) error {
	pm, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("message is not proto.Message: %T", v)
	}
	return c.codec.Codec.Unmarshal(data, pm)
}

func (c protoCodec) Name() string {
	return "proto"
}

func init() {
	// Register the custom codec with the encoding config
	ec := encoding.MakeConfig()
	grpcencoding.RegisterCodec(protoCodec{codec: ec})
}

// ProductionGrpcHandler implements the grpc.Handler interface but connects to
// production network gRPC endpoints instead of integration network
type ProductionGrpcHandler struct {
	grpcConn *grpc.ClientConn
	grpcAddr string

	// Encoding config for unpacking account responses
	encodingConfig sdktestutil.TestEncodingConfig

	// Client interfaces
	authClient      authtypes.QueryClient
	bankClient      banktypes.QueryClient
	evmClient       evmtypes.QueryClient
	stakingClient   stakingtypes.QueryClient
	govClient       govtypes.QueryClient
	feemarketClient feemarkettypes.QueryClient
	inflationClient infltypes.QueryClient
	distrClient     distrtypes.QueryClient
}

var _ evmosgrpc.Handler = (*ProductionGrpcHandler)(nil)

// NewProductionGrpcHandler creates a grpc handler that connects to production network
func NewProductionGrpcHandler(grpcAddr string) (*ProductionGrpcHandler, error) {
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &ProductionGrpcHandler{
		grpcConn:        conn,
		grpcAddr:        grpcAddr,
		encodingConfig:  encoding.MakeConfig(),
		authClient:      authtypes.NewQueryClient(conn),
		bankClient:      banktypes.NewQueryClient(conn),
		evmClient:       evmtypes.NewQueryClient(conn),
		stakingClient:   stakingtypes.NewQueryClient(conn),
		govClient:       govtypes.NewQueryClient(conn),
		feemarketClient: feemarkettypes.NewQueryClient(conn),
		inflationClient: infltypes.NewQueryClient(conn),
		distrClient:     distrtypes.NewQueryClient(conn),
	}, nil
}

// GetAccount implements common grpc.Handler interface
func (h *ProductionGrpcHandler) GetAccount(address string) (sdk.AccountI, error) {
	res, err := h.authClient.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: address,
	})
	if err != nil {
		return nil, err
	}

	var acc sdk.AccountI
	if err := h.encodingConfig.InterfaceRegistry.UnpackAny(res.Account, &acc); err != nil {
		return nil, err
	}
	return acc, nil
}

// GetBalanceFromBank implements common grpc.Handler interface
func (h *ProductionGrpcHandler) GetBalanceFromBank(address sdk.AccAddress, denom string) (*banktypes.QueryBalanceResponse, error) {
	return h.bankClient.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: address.String(),
		Denom:   denom,
	})
}

// GetAllBalances implements common grpc.Handler interface
func (h *ProductionGrpcHandler) GetAllBalances(address sdk.AccAddress) (*banktypes.QueryAllBalancesResponse, error) {
	return h.bankClient.AllBalances(context.Background(), &banktypes.QueryAllBalancesRequest{
		Address: address.String(),
	})
}

// GetSpendableBalance implements common grpc.Handler interface
func (h *ProductionGrpcHandler) GetSpendableBalance(address sdk.AccAddress, denom string) (*banktypes.QuerySpendableBalanceByDenomResponse, error) {
	return h.bankClient.SpendableBalanceByDenom(context.Background(), &banktypes.QuerySpendableBalanceByDenomRequest{
		Address: address.String(),
		Denom:   denom,
	})
}

// GetTotalSupply implements common grpc.Handler interface
func (h *ProductionGrpcHandler) GetTotalSupply() (*banktypes.QueryTotalSupplyResponse, error) {
	return h.bankClient.TotalSupply(context.Background(), &banktypes.QueryTotalSupplyRequest{})
}

// Authz methods - stub implementations (not needed for load test)
func (h *ProductionGrpcHandler) GetAuthorizations(grantee, granter string) ([]authz.Authorization, error) {
	return nil, nil
}
func (h *ProductionGrpcHandler) GetAuthorizationsByGrantee(grantee string) ([]authz.Authorization, error) {
	return nil, nil
}
func (h *ProductionGrpcHandler) GetAuthorizationsByGranter(granter string) ([]authz.Authorization, error) {
	return nil, nil
}
func (h *ProductionGrpcHandler) GetGrants(grantee, granter string) ([]*authz.Grant, error) {
	return nil, nil
}
func (h *ProductionGrpcHandler) GetGrantsByGrantee(grantee string) ([]*authz.GrantAuthorization, error) {
	return nil, nil
}
func (h *ProductionGrpcHandler) GetGrantsByGranter(granter string) ([]*authz.GrantAuthorization, error) {
	return nil, nil
}

// Staking methods - stub implementations (not all needed)
func (h *ProductionGrpcHandler) GetDelegation(delegatorAddress string, validatorAddress string) (*stakingtypes.QueryDelegationResponse, error) {
	return h.stakingClient.Delegation(context.Background(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddress,
		ValidatorAddr: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetDelegatorDelegations(delegatorAddress string) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	return h.stakingClient.DelegatorDelegations(context.Background(), &stakingtypes.QueryDelegatorDelegationsRequest{
		DelegatorAddr: delegatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetValidatorDelegations(validatorAddress string) (*stakingtypes.QueryValidatorDelegationsResponse, error) {
	return h.stakingClient.ValidatorDelegations(context.Background(), &stakingtypes.QueryValidatorDelegationsRequest{
		ValidatorAddr: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetRedelegations(delegatorAddress, srcValidator, dstValidator string) (*stakingtypes.QueryRedelegationsResponse, error) {
	return h.stakingClient.Redelegations(context.Background(), &stakingtypes.QueryRedelegationsRequest{
		DelegatorAddr:    delegatorAddress,
		SrcValidatorAddr: srcValidator,
		DstValidatorAddr: dstValidator,
	})
}
func (h *ProductionGrpcHandler) GetValidatorUnbondingDelegations(validatorAddress string) (*stakingtypes.QueryValidatorUnbondingDelegationsResponse, error) {
	return h.stakingClient.ValidatorUnbondingDelegations(context.Background(), &stakingtypes.QueryValidatorUnbondingDelegationsRequest{
		ValidatorAddr: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetDelegatorUnbondingDelegations(delegatorAddress string) (*stakingtypes.QueryDelegatorUnbondingDelegationsResponse, error) {
	return h.stakingClient.DelegatorUnbondingDelegations(context.Background(), &stakingtypes.QueryDelegatorUnbondingDelegationsRequest{
		DelegatorAddr: delegatorAddress,
	})
}

// Distribution methods
func (h *ProductionGrpcHandler) GetDelegationTotalRewards(delegatorAddress string) (*distrtypes.QueryDelegationTotalRewardsResponse, error) {
	return h.distrClient.DelegationTotalRewards(context.Background(), &distrtypes.QueryDelegationTotalRewardsRequest{
		DelegatorAddress: delegatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetDelegationRewards(delegatorAddress string, validatorAddress string) (*distrtypes.QueryDelegationRewardsResponse, error) {
	return h.distrClient.DelegationRewards(context.Background(), &distrtypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetDelegatorWithdrawAddr(delegatorAddress string) (*distrtypes.QueryDelegatorWithdrawAddressResponse, error) {
	return h.distrClient.DelegatorWithdrawAddress(context.Background(), &distrtypes.QueryDelegatorWithdrawAddressRequest{
		DelegatorAddress: delegatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetValidatorCommission(validatorAddress string) (*distrtypes.QueryValidatorCommissionResponse, error) {
	return h.distrClient.ValidatorCommission(context.Background(), &distrtypes.QueryValidatorCommissionRequest{
		ValidatorAddress: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetValidatorOutstandingRewards(validatorAddress string) (*distrtypes.QueryValidatorOutstandingRewardsResponse, error) {
	return h.distrClient.ValidatorOutstandingRewards(context.Background(), &distrtypes.QueryValidatorOutstandingRewardsRequest{
		ValidatorAddress: validatorAddress,
	})
}
func (h *ProductionGrpcHandler) GetCommunityPool() (*distrtypes.QueryCommunityPoolResponse, error) {
	return h.distrClient.CommunityPool(context.Background(), &distrtypes.QueryCommunityPoolRequest{})
}

// EVM methods
func (h *ProductionGrpcHandler) GetEvmAccount(address common.Address) (*evmtypes.QueryAccountResponse, error) {
	return h.evmClient.Account(context.Background(), &evmtypes.QueryAccountRequest{
		Address: address.Hex(),
	})
}

func (h *ProductionGrpcHandler) EstimateGas(args []byte, GasCap uint64) (*evmtypes.EstimateGasResponse, error) {
	return h.evmClient.EstimateGas(context.Background(), &evmtypes.EthCallRequest{
		Args:   args,
		GasCap: GasCap,
	})
}

func (h *ProductionGrpcHandler) GetEvmParams() (*evmtypes.QueryParamsResponse, error) {
	return h.evmClient.Params(context.Background(), &evmtypes.QueryParamsRequest{})
}

func (h *ProductionGrpcHandler) GetEvmBaseFee() (*evmtypes.QueryBaseFeeResponse, error) {
	return h.evmClient.BaseFee(context.Background(), &evmtypes.QueryBaseFeeRequest{})
}

func (h *ProductionGrpcHandler) GetBalanceFromEVM(address sdk.AccAddress) (*evmtypes.QueryBalanceResponse, error) {
	return h.evmClient.Balance(context.Background(), &evmtypes.QueryBalanceRequest{
		Address: address.String(),
	})
}

// FeeMarket methods
func (h *ProductionGrpcHandler) GetBaseFee() (*feemarkettypes.QueryBaseFeeResponse, error) {
	return h.feemarketClient.BaseFee(context.Background(), &feemarkettypes.QueryBaseFeeRequest{})
}

func (h *ProductionGrpcHandler) GetFeeMarketParams() (*feemarkettypes.QueryParamsResponse, error) {
	return h.feemarketClient.Params(context.Background(), &feemarkettypes.QueryParamsRequest{})
}

// Gov methods
func (h *ProductionGrpcHandler) GetProposal(proposalID uint64) (*govtypes.QueryProposalResponse, error) {
	return h.govClient.Proposal(context.Background(), &govtypes.QueryProposalRequest{
		ProposalId: proposalID,
	})
}

func (h *ProductionGrpcHandler) GetGovParams(paramsType string) (*govtypes.QueryParamsResponse, error) {
	return h.govClient.Params(context.Background(), &govtypes.QueryParamsRequest{
		ParamsType: paramsType,
	})
}

// Inflation methods
func (h *ProductionGrpcHandler) GetPeriod() (*infltypes.QueryPeriodResponse, error) {
	return h.inflationClient.Period(context.Background(), &infltypes.QueryPeriodRequest{})
}

func (h *ProductionGrpcHandler) GetEpochMintProvision() (*infltypes.QueryEpochMintProvisionResponse, error) {
	return h.inflationClient.EpochMintProvision(context.Background(), &infltypes.QueryEpochMintProvisionRequest{})
}

func (h *ProductionGrpcHandler) GetSkippedEpochs() (*infltypes.QuerySkippedEpochsResponse, error) {
	return h.inflationClient.SkippedEpochs(context.Background(), &infltypes.QuerySkippedEpochsRequest{})
}

func (h *ProductionGrpcHandler) GetCirculatingSupply() (*infltypes.QueryCirculatingSupplyResponse, error) {
	return h.inflationClient.CirculatingSupply(context.Background(), &infltypes.QueryCirculatingSupplyRequest{})
}

func (h *ProductionGrpcHandler) GetInflationRate() (*infltypes.QueryInflationRateResponse, error) {
	return h.inflationClient.InflationRate(context.Background(), &infltypes.QueryInflationRateRequest{})
}

func (h *ProductionGrpcHandler) GetInflationParams() (*infltypes.QueryParamsResponse, error) {
	return h.inflationClient.Params(context.Background(), &infltypes.QueryParamsRequest{})
}

// Staking methods
func (h *ProductionGrpcHandler) GetStakingParams() (*stakingtypes.QueryParamsResponse, error) {
	return h.stakingClient.Params(context.Background(), &stakingtypes.QueryParamsRequest{})
}

func (h *ProductionGrpcHandler) GetBondedValidators() (*stakingtypes.QueryValidatorsResponse, error) {
	return h.stakingClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{
		Status: stakingtypes.BondStatusBonded,
	})
}

// Close closes the gRPC connection
func (h *ProductionGrpcHandler) Close() error {
	if h.grpcConn != nil {
		return h.grpcConn.Close()
	}
	return nil
}
