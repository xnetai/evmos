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

	// SimpleOrderbookContract is the compiled SimpleOrderbook contract
	SimpleOrderbookContract evmtypes.CompiledContract
)

func init() {
	var err error

	// Load SimpleOrderbook
	if SimpleOrderbookContract, err = contractutils.ConvertHardhatBytesToCompiledContract(SimpleOrderbookJSON); err != nil {
		panic(err)
	}
}
