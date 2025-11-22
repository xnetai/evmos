// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	contractutils "github.com/evmos/xcoin/v20/contracts/utils"
	evmtypes "github.com/evmos/xcoin/v20/x/evm/types"
	"github.com/evmos/xcoin/v20/precompiles/testutil"
	"github.com/evmos/xcoin/v20/testutil/integration/evmos/factory"
)

// LoadBatchOrderBookContract loads the compiled BatchOrderBook contract from JSON
func LoadBatchOrderBookContract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("BatchOrderBook.json")
}

var (
	// BatchOrderBookContract contains the compiled bytecode and ABI for BatchOrderBook
	// This is loaded lazily to avoid initialization errors
	BatchOrderBookContract evmtypes.CompiledContract

	// BatchOrderBookABI is the ABI string for the BatchOrderBook contract
	// This will be populated when the contract is loaded
	BatchOrderBookABI string

	// defaultLogCheckArgs provides default arguments for log checking
	defaultLogCheckArgs testutil.LogCheckArgs
)

// init loads the contract at package initialization
func init() {
	var err error
	BatchOrderBookContract, err = LoadBatchOrderBookContract()
	if err != nil {
		// Log the error but don't panic to allow tests to handle it gracefully
		return
	}
	// ABIEvents expects a map of events from the ABI
	defaultLogCheckArgs = testutil.LogCheckArgs{
		ABIEvents: BatchOrderBookContract.ABI.Events,
	}
}

// ContractDeploymentData wraps the contract for deployment
func GetBatchOrderBookContractData() factory.ContractDeploymentData {
	return factory.ContractDeploymentData{
		Contract: BatchOrderBookContract,
	}
}
