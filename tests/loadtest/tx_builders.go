// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"fmt"
	"math/big"
	"math/rand"

	sdkmath "cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/common"

	sdktypes "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/evmos/evmos/v20/testutil/integration/evmos/factory"
	commonfactory "github.com/evmos/evmos/v20/testutil/integration/common/factory"
	"github.com/evmos/evmos/v20/testutil/integration/evmos/keyring"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

// TransactionBuilder interface defines methods for building different transaction types
type TransactionBuilder interface {
	BuildTx(
		user keyring.Key,
		txFactory factory.TxFactory,
		extraData interface{},
	) ([]byte, TxMetadata, error)
	GetType() TxType
}

// ContractTxBuilder builds smart contract call transactions
type ContractTxBuilder struct {
	contractAddr common.Address
}

// NewContractTxBuilder creates a new ContractTxBuilder
func NewContractTxBuilder(contractAddr common.Address) *ContractTxBuilder {
	return &ContractTxBuilder{
		contractAddr: contractAddr,
	}
}

// BuildTx builds a contract call transaction
func (b *ContractTxBuilder) BuildTx(
	user keyring.Key,
	txFactory factory.TxFactory,
	extraData interface{},
) ([]byte, TxMetadata, error) {
	metadata := TxMetadata{
		Type:      TxTypeContract,
		UserIndex: -1, // Will be set by caller
	}

	// Generate simple trade count (10 trades per batch)
	tradeCount := big.NewInt(10)

	// Build contract call arguments
	callArgs := factory.CallArgs{
		ContractABI: BatchOrderBookContract.ABI,
		MethodName:  "executeBatchTrades",
		Args:        []interface{}{tradeCount},
	}

	// Calculate fee per trade (1 unit per trade)
	feePerTrade := big.NewInt(1)
	totalFee := new(big.Int).Mul(feePerTrade, tradeCount)

	// Build EVM transaction args
	txArgs := evmtypes.EvmTxArgs{
		To:        &b.contractAddr,
		GasLimit:  500000, // Sufficient for 10 trades
		GasFeeCap: big.NewInt(1000000000000), // 1000 gwei
		GasTipCap: big.NewInt(1000000000),    // 1 gwei
		Amount:    totalFee,
	}

	// Generate contract call input data
	txArgs, err := txFactory.GenerateContractCallArgs(txArgs, callArgs)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to generate contract call args: %w", err)
	}

	// Build and sign the transaction
	signedTx, err := txFactory.GenerateSignedEthTx(user.Priv, txArgs)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to generate signed eth tx: %w", err)
	}

	// Encode transaction
	txBytes, err := txFactory.EncodeTx(signedTx)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to encode tx: %w", err)
	}

	metadata.GasWanted = txArgs.GasLimit

	return txBytes, metadata, nil
}

// GetType returns the transaction type
func (b *ContractTxBuilder) GetType() TxType {
	return TxTypeContract
}

// BankTxBuilder builds bank transfer transactions
type BankTxBuilder struct {
	allKeys keyring.Keyring
}

// NewBankTxBuilder creates a new BankTxBuilder
func NewBankTxBuilder(allKeys keyring.Keyring) *BankTxBuilder {
	return &BankTxBuilder{
		allKeys: allKeys,
	}
}

// BuildTx builds a bank transfer transaction
func (b *BankTxBuilder) BuildTx(
	user keyring.Key,
	txFactory factory.TxFactory,
	extraData interface{},
) ([]byte, TxMetadata, error) {
	metadata := TxMetadata{
		Type:      TxTypeBank,
		UserIndex: -1,
	}

	// Select random receiver (different from sender)
	receiverIdx := rand.Intn(len(b.allKeys.GetKeys()))
	receiver := b.allKeys.GetAccAddr(receiverIdx)

	// Ensure receiver is different from sender
	for receiver.Equals(user.AccAddr) {
		receiverIdx = rand.Intn(len(b.allKeys.GetKeys()))
		receiver = b.allKeys.GetAccAddr(receiverIdx)
	}

	// Random amount between 1 and 1000 base denom units
	amount := sdkmath.NewInt(int64(rand.Intn(1000) + 1))
	coins := sdktypes.NewCoins(sdktypes.NewCoin("txcoin", amount))

	// Create MsgSend
	msg := banktypes.NewMsgSend(user.AccAddr, receiver, coins)

	// Build transaction
	gas := uint64(100000) // Standard gas for bank transfer
	gasPrice := sdkmath.NewInt(1000000000) // 1 gwei

	txArgs := commonfactory.CosmosTxArgs{
		Msgs:     []sdktypes.Msg{msg},
		Gas:      &gas,
		GasPrice: &gasPrice,
	}

	// Build and sign transaction
	signedTx, err := txFactory.BuildCosmosTx(user.Priv, txArgs)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to build cosmos tx: %w", err)
	}

	// Encode transaction
	txBytes, err := txFactory.EncodeTx(signedTx)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to encode tx: %w", err)
	}

	metadata.GasWanted = gas

	return txBytes, metadata, nil
}

// GetType returns the transaction type
func (b *BankTxBuilder) GetType() TxType {
	return TxTypeBank
}

// StakingTxBuilder builds staking operation transactions
type StakingTxBuilder struct {
	validators []stakingtypes.Validator
}

// NewStakingTxBuilder creates a new StakingTxBuilder
func NewStakingTxBuilder(validators []stakingtypes.Validator) *StakingTxBuilder {
	return &StakingTxBuilder{
		validators: validators,
	}
}

// BuildTx builds a staking delegation transaction
func (b *StakingTxBuilder) BuildTx(
	user keyring.Key,
	txFactory factory.TxFactory,
	extraData interface{},
) ([]byte, TxMetadata, error) {
	metadata := TxMetadata{
		Type:      TxTypeStaking,
		UserIndex: -1,
	}

	if len(b.validators) == 0 {
		return nil, metadata, fmt.Errorf("no validators available for staking")
	}

	// Select random validator
	valIdx := rand.Intn(len(b.validators))
	validator := b.validators[valIdx]

	// Random delegation amount between 100 and 10000 base denom units
	amount := sdkmath.NewInt(int64(rand.Intn(9900) + 100))
	coin := sdktypes.NewCoin("txcoin", amount)

	// Create MsgDelegate
	msg := stakingtypes.NewMsgDelegate(
		user.AccAddr.String(),
		validator.GetOperator(),
		coin,
	)

	// Build transaction
	gas := uint64(400000) // Standard gas for delegation
	gasPrice := sdkmath.NewInt(1000000000) // 1 gwei

	txArgs := commonfactory.CosmosTxArgs{
		Msgs:     []sdktypes.Msg{msg},
		Gas:      &gas,
		GasPrice: &gasPrice,
	}

	// Build and sign transaction
	signedTx, err := txFactory.BuildCosmosTx(user.Priv, txArgs)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to build cosmos tx: %w", err)
	}

	// Encode transaction
	txBytes, err := txFactory.EncodeTx(signedTx)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to encode tx: %w", err)
	}

	metadata.GasWanted = gas

	return txBytes, metadata, nil
}

// GetType returns the transaction type
func (b *StakingTxBuilder) GetType() TxType {
	return TxTypeStaking
}

// RawEVMTxBuilder builds raw EVM transfer transactions
type RawEVMTxBuilder struct {
	allKeys keyring.Keyring
}

// NewRawEVMTxBuilder creates a new RawEVMTxBuilder
func NewRawEVMTxBuilder(allKeys keyring.Keyring) *RawEVMTxBuilder {
	return &RawEVMTxBuilder{
		allKeys: allKeys,
	}
}

// BuildTx builds a raw EVM transfer transaction
func (b *RawEVMTxBuilder) BuildTx(
	user keyring.Key,
	txFactory factory.TxFactory,
	extraData interface{},
) ([]byte, TxMetadata, error) {
	metadata := TxMetadata{
		Type:      TxTypeRawEVM,
		UserIndex: -1,
	}

	// Select random receiver (different from sender)
	receiverIdx := rand.Intn(len(b.allKeys.GetKeys()))
	receiver := b.allKeys.GetAddr(receiverIdx)

	// Ensure receiver is different from sender
	for receiver == user.Addr {
		receiverIdx = rand.Intn(len(b.allKeys.GetKeys()))
		receiver = b.allKeys.GetAddr(receiverIdx)
	}

	// Random amount between 1 and 1000 wei
	amount := big.NewInt(int64(rand.Intn(1000) + 1))

	// Build EVM transaction args
	txArgs := evmtypes.EvmTxArgs{
		To:        &receiver,
		Amount:    amount,
		GasLimit:  21000, // Standard gas for EVM transfer
		GasFeeCap: big.NewInt(1000000000000), // 1000 gwei
		GasTipCap: big.NewInt(1000000000),    // 1 gwei
	}

	// Build and sign the transaction
	signedTx, err := txFactory.GenerateSignedEthTx(user.Priv, txArgs)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to generate signed eth tx: %w", err)
	}

	// Encode transaction
	txBytes, err := txFactory.EncodeTx(signedTx)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to encode tx: %w", err)
	}

	metadata.GasWanted = txArgs.GasLimit

	return txBytes, metadata, nil
}

// GetType returns the transaction type
func (b *RawEVMTxBuilder) GetType() TxType {
	return TxTypeRawEVM
}

// SelectTransactionBuilder selects a transaction builder based on weighted distribution
// 40% Contract, 30% Bank, 20% Staking, 10% Raw EVM
func SelectTransactionBuilder(
	contractBuilder *ContractTxBuilder,
	bankBuilder *BankTxBuilder,
	stakingBuilder *StakingTxBuilder,
	rawEVMBuilder *RawEVMTxBuilder,
) TransactionBuilder {
	r := rand.Intn(100)

	if r < 40 {
		return contractBuilder
	} else if r < 70 {
		return bankBuilder
	} else if r < 90 {
		return stakingBuilder
	}
	return rawEVMBuilder
}
