# TxCoin Denomination Configuration Test Instructions

This branch contains changes to configure the Evmos chain to use **`txcoin`** as the base denomination and **`xcoin`** as the display denomination, along with comprehensive integration tests to verify the changes.

## Changes Made

### 1. Core Denomination Configuration
- **File**: `types/coin.go`
  - Base denom changed from `aevmos` to `txcoin`
  - Display denom changed from `evmos` to `xcoin`
  - Testnet base denom changed from `atevmos` to `ttxcoin`
  - Testnet display denom changed from `tevmos` to `txcoin`

### 2. Bank Metadata Configuration
- **File**: `testutil/integration/evmos/network/chain_id_modifiers.go`
  - Updated bank genesis metadata to use new denominations
  - Updated metadata name, symbol, and description

### 3. Comprehensive Test Suite
- **File**: `testutil/integration/evmos/network/denom_config_test.go`
  - `TestTxCoinDenomConfiguration`: Full integration test that:
    - Starts a local test network
    - Verifies denomination names (`txcoin`/`xcoin`)
    - Creates delegations from delegator to validator (operator)
    - Runs 10 blocks with transaction activity
    - Verifies fees are properly charged
    - Verifies balances for operator, delegator, and sender accounts
    - Verifies validator token increases
    - Verifies EVM denomination configuration
    - Verifies bank metadata is correct
  - `TestTxCoinWith18Decimals`: Verifies decimal precision

## How to Run the Test on Windows

### Prerequisites
1. **Install Go 1.21 or later**
   - Download from: https://go.dev/dl/
   - Verify installation: `go version`

2. **Install Git**
   - Download from: https://git-scm.com/download/win
   - Or use Git for Windows / GitHub Desktop

3. **Install Make (Optional but recommended)**
   - Install via Chocolatey: `choco install make`
   - Or use Git Bash which includes make

### Clone the Branch

```powershell
# Clone the repository
git clone https://github.com/xnetai/evmos.git
cd evmos

# Checkout the test branch
git fetch origin claude/test-txcoin-denom-config-01WVp937b3iAQoxDJ6mN1923
git checkout claude/test-txcoin-denom-config-01WVp937b3iAQoxDJ6mN1923
```

### Run the Test

#### Option 1: Using PowerShell
```powershell
# Set timeout to 30 minutes (1800 seconds)
$env:GO_TEST_TIMEOUT="30m"

# Run the comprehensive denomination config test
go test -v -timeout=30m ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration

# Run the decimals verification test
go test -v -timeout=30m ./testutil/integration/evmos/network -run TestTxCoinWith18Decimals

# Or run both tests
go test -v -timeout=30m ./testutil/integration/evmos/network -run "TestTxCoin"
```

#### Option 2: Using Git Bash or WSL
```bash
# Run the comprehensive denomination config test
timeout 1800 go test -v -timeout=30m ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration

# Run the decimals verification test
timeout 1800 go test -v -timeout=30m ./testutil/integration/evmos/network -run TestTxCoinWith18Decimals

# Or run both tests
timeout 1800 go test -v -timeout=30m ./testutil/integration/evmos/network -run "TestTxCoin"
```

#### Option 3: Using Make (if available)
```bash
# Create a custom make target in Makefile or run directly
make test-integration
```

### Expected Test Output

The test should output detailed progress information including:

```
Testing with base denom: txcoin, display denom: xcoin
Initial delegator balance: 100000000000000000000000 txcoin
Initial sender balance: 100000000000000000000000 txcoin
Validator operator address: evmosvaloper1...
Initial validator tokens: 1000000000000000000

Delegation transaction broadcasted: 1000000000000000000 to validator

Running 10 blocks and tracking state...
Block 1 - Height: 2, Time: 2024-XX-XXTXX:XX:XXZ
Block 2 - Height: 3, Time: 2024-XX-XXTXX:XX:XXZ
Block 3 - Height: 4, Time: 2024-XX-XXTXX:XX:XXZ
  -> Transfer tx executed with gas used: XXXXX
...
Block 10 - Height: 13, Time: 2024-XX-XXTXX:XX:XXZ

Verifying final state...
Delegation verified: 1000000000000000000 txcoin
Final validator tokens: 1001000000000000000 (increased by 1000000000000000000)
Final delegator balance: XXXXXXXXXXXXXXX txcoin
Final sender balance: XXXXXXXXXXXXXXX txcoin
Sender balance change: XXXXXXXXXXXXX
Base fee: 875000000.000000000000000000
EVM denom verified: txcoin

Bank metadata verified:
  Base: txcoin
  Display: xcoin
  Name: XCoin
  Symbol: XCOIN

✓ All verifications passed successfully!
PASS
```

### What the Test Verifies

The comprehensive test verifies the following:

1. **Denomination Configuration**
   - Base denomination is correctly set to `txcoin`
   - Display denomination is correctly set to `xcoin`

2. **Network Functionality**
   - Local test network starts successfully with new denominations
   - Genesis state is properly configured

3. **Transaction Processing**
   - Delegations work correctly with new denom
   - Transfers execute successfully
   - Fees are properly calculated and charged in `txcoin`

4. **Account Balances**
   - Operator (validator) account balance tracking
   - Delegator account balance tracking
   - Sender account balance tracking
   - All balances denominated in `txcoin`

5. **Validator Operations**
   - Validator tokens increase correctly after delegation
   - Staking operations use correct denomination

6. **EVM Integration**
   - EVM denomination configuration matches base denom
   - EVM operations use `txcoin`

7. **Bank Module**
   - Bank metadata correctly shows `txcoin` as base
   - Bank metadata correctly shows `xcoin` as display
   - Denomination units properly configured with 18 decimals

8. **Block Production**
   - 10+ blocks are successfully produced
   - Block time progresses correctly
   - State transitions work properly

### Troubleshooting

#### Test Fails to Build
```powershell
# Clear Go module cache
go clean -modcache

# Download dependencies
go mod download

# Verify module integrity
go mod verify

# Try building again
go build ./...
```

#### Timeout Issues
If the test times out, increase the timeout:
```powershell
go test -v -timeout=60m ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration
```

#### Network Issues
If you encounter DNS or network issues downloading dependencies:
```powershell
# Set Go proxy
$env:GOPROXY="https://proxy.golang.org,direct"

# Or use a different proxy
$env:GOPROXY="https://goproxy.io,direct"
```

#### Permission Issues
Run PowerShell or Command Prompt as Administrator if you encounter permission errors.

### Reverting Changes

If the test fails and you need to revert to original denominations:

```bash
git restore types/coin.go testutil/integration/evmos/network/chain_id_modifiers.go
rm testutil/integration/evmos/network/denom_config_test.go
```

## Test Success Criteria

The test is considered successful if:
1. ✅ All test assertions pass without errors
2. ✅ Network starts with `txcoin`/`xcoin` denominations
3. ✅ 10+ blocks are produced successfully
4. ✅ Delegations, transfers, and fee payments work correctly
5. ✅ Operator, delegator, and sender balances are tracked correctly
6. ✅ Validator tokens increase by delegation amount
7. ✅ EVM configuration uses `txcoin`
8. ✅ Bank metadata shows correct `txcoin`/`xcoin` configuration
9. ✅ Test completes within 30 minutes

## Branch Information

- **Repository**: https://github.com/xnetai/evmos
- **Branch**: `claude/test-txcoin-denom-config-01WVp937b3iAQoxDJ6mN1923`
- **Base Branch**: `main`

## Additional Notes

- The test uses the integration test framework which creates an in-memory network
- No external node or blockchain is required
- All tests are self-contained and idempotent
- The test can be run multiple times without side effects
- Network state is reset between test runs

## Contact

If you encounter issues or have questions about the test, please refer to the Evmos documentation or create an issue in the repository.
