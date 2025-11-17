// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package contracts

import (
	_ "embed"

	contractutils "github.com/evmos/evmos/v19/contracts/utils"
	evmtypes "github.com/evmos/evmos/v19/x/evm/types"
)

var (
	//go:embed SimpleOrderbook.json
	SimpleOrderbookJSON []byte

	//go:embed EnhancedOrderbook.json
	EnhancedOrderbookJSON []byte

	// SimpleOrderbookContract is the compiled SimpleOrderbook contract
	SimpleOrderbookContract evmtypes.CompiledContract

	// EnhancedOrderbookContract is the compiled EnhancedOrderbook contract
	EnhancedOrderbookContract evmtypes.CompiledContract
)

func init() {
	var err error

	// Load SimpleOrderbook
	if SimpleOrderbookContract, err = contractutils.ConvertHardhatBytesToCompiledContract(SimpleOrderbookJSON); err != nil {
		panic(err)
	}

	// Load EnhancedOrderbook
	if EnhancedOrderbookContract, err = contractutils.ConvertHardhatBytesToCompiledContract(EnhancedOrderbookJSON); err != nil {
		panic(err)
	}
}
