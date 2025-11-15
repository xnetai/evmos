# XCoin Denomination Configuration Test Instructions

This branch contains changes to configure the XCoin chain (formerly Evmos) to use **`txcoin`** as the base denomination and **`xcoin`** as the display denomination, with the binary renamed from `evmosd` to **`xcoind`**, along with comprehensive integration tests to verify the changes.

## Changes Made

### 1. Binary Renamed
- **Changed**: `evmosd` → `xcoind`
- **Files**:
  - Renamed `cmd/evmosd/` → `cmd/xcoind/`
  - Updated `Makefile` to build `xcoind` binary
  - Updated version name from `evmos` to `xcoin`

### 2. Core Denomination Configuration
- **File**: `types/coin.go`
  - Base denom changed from `aevmos` to `txcoin`
  - Display denom changed from `evmos` to `xcoin`
  - Testnet base denom changed from `atevmos` to `ttxcoin`
  - Testnet display denom changed from `tevmos` to `txcoin`

### 3. Bank Metadata Configuration
- **File**: `testutil/integration/evmos/network/chain_id_modifiers.go`
  - Updated bank genesis metadata to use new denominations
  - Updated metadata name, symbol, and description

### 4. Comprehensive Test Suite
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

**IMPORTANT: Clear build cache first to avoid CGO issues**

```powershell
# Clear Go build cache
go clean -cache -modcache -testcache
```

#### Option 1: Using PowerShell (Recommended)

```powershell
# Set environment variables (run separately)
$env:PATH = "C:\ProgramData\mingw64\mingw64\bin;" + $env:PATH
$env:CGO_ENABLED = "1"
$env:CC = "gcc"

# Verify GCC is available
gcc --version

# Run the comprehensive denomination test
go test -v ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration -timeout 10m

# Run the decimals verification test
go test -v ./testutil/integration/evmos/network -run TestTxCoinWith18Decimals -timeout 10m

# Or run both tests together
go test -v ./testutil/integration/evmos/network -run "TestTxCoin" -timeout 10m
```

#### Option 2: Using Git Bash or WSL

```bash
# Clear cache
go clean -cache -modcache -testcache

# Set environment and run
export CGO_ENABLED=1
export CC=gcc

# Run the test
go test -v ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration -timeout 10m
```

#### Option 3: Single Command (PowerShell)

```powershell
# After clearing cache
$env:CGO_ENABLED="1"; $env:CC="gcc"; go test -v ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration -timeout 10m
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

#### CGO Signature Error (Most Common Issue)

If you see an error like:
```
assignment mismatch: 2 variables but btc_ecdsa.SignCompact returns 1 value
```

This means CGO isn't properly enabled. **Follow these steps in order:**

1. **Verify GCC is installed and in PATH:**
   ```powershell
   gcc --version
   # Should show: gcc (MinGW-W64 x86_64...) or similar
   ```

   If not found, install MinGW-w64:
   - Download from: https://www.mingw-w64.org/downloads/
   - Or via Chocolatey: `choco install mingw`
   - Add to PATH: `C:\ProgramData\mingw64\mingw64\bin`

2. **Clear ALL caches:**
   ```powershell
   go clean -cache
   go clean -modcache
   go clean -testcache
   Remove-Item -Recurse -Force $env:TEMP\go-build -ErrorAction SilentlyContinue
   ```

3. **Set environment variables in same PowerShell session:**
   ```powershell
   $env:CGO_ENABLED = "1"
   $env:CC = "gcc"
   $env:PATH = "C:\ProgramData\mingw64\mingw64\bin;" + $env:PATH

   # Verify they're set
   echo "CGO_ENABLED: $env:CGO_ENABLED"
   echo "CC: $env:CC"
   gcc --version
   ```

4. **Run the test in the SAME PowerShell session:**
   ```powershell
   go test -v ./testutil/integration/evmos/network -run TestTxCoinDenomConfiguration -timeout 10m
   ```

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
