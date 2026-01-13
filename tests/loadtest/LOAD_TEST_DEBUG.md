# Load Test Debug Summary

## Current Status: ATTEMPT 1 FAILED (1 of 3)

### Problem Statement
Created a comprehensive Local Nodes Contract Load Test but all transactions are failing with:
- **Primary Error**: "failed to check sender balance: sender..." (521 occurrences)
- **Secondary Error**: "fee payer address: ..." (479 occurrences)
- **Success Rate**: 0% (0/1000 transactions succeeded)
- **Infrastructure**: ✅ Working perfectly (round-robin distribution, validators running, gRPC connections established)

### Root Cause
**ARCHITECTURAL MISMATCH**: Integration network and ProductionNetwork have completely separate genesis files.

1. **Integration Network** (used for building transactions):
   - Chain ID: `evmos_9002-1`
   - Has 100 funded user accounts from our keyring
   - Used by transaction factory to build and sign transactions

2. **ProductionNetwork** (used for executing transactions):
   - Chain ID: `evmos_9002-1` (same)
   - Created via `testnet init-files` with only validator accounts
   - Does NOT have the 100 user accounts from our keyring
   - Rejects all transactions because sender accounts don't exist

### What Works ✅
- ProductionNetwork starts successfully with 4 validators
- Contract deploys successfully
- All 4 gRPC clients connect successfully
- Round-robin distribution is perfect (250 txs/node = 25% each)
- Transaction type distribution matches expected (40/30/20/10)
- Statistics tracking is comprehensive and accurate
- Throughput is excellent (~700-800 txs/sec)

### What Fails ❌
- All 1000 transactions rejected because accounts don't exist in ProductionNetwork genesis
- Even using only user[0] fails (tried as quick fix)
- Integration network's funded accounts ≠ ProductionNetwork's funded accounts

## Files Created (All Working Except Account Funding)
1. `load_stats.go` - Thread-safe statistics tracking ✅
2. `node_client.go` - Per-node gRPC/RPC clients ✅
3. `tx_builders.go` - Transaction builders for 4 types ✅
4. `local_nodes_contract_load_test.go` - Main test suite ✅
5. `README_LOCAL_LOAD_TEST.md` - Documentation ✅

## Fixes Applied in ATTEMPT 1
1. Fixed chain ID mismatch (evmos_9000-1 → evmos_9002-1)
2. Fixed type name conflicts (renamed to LocalLoadTestStats, LocalNodeStats)
3. Fixed BroadcastTxSync field errors (removed unavailable fields)
4. Fixed CosmosTxArgs type (used commonfactory.CosmosTxArgs)
5. Fixed gas type conversion (int64 → uint64)
6. Fixed coin sorting (used WithOtherDenoms instead of custom genesis)
7. Tried using single user (still failed - account doesn't exist)

## ATTEMPT 2 Plan: Fund Accounts in ProductionNetwork Genesis

### Strategy
Modify ProductionNetwork's genesis files AFTER `testnet init-files` but BEFORE starting validators to include the 100 user accounts.

### Implementation Steps

1. **Add Genesis Modification Method to ProductionNetwork**
   ```go
   func (pn *ProductionNetwork) FundAccountsInGenesis(accounts []sdk.AccAddress, balances sdk.Coins) error
   ```

2. **Modify Setup Flow**
   ```go
   // Create keyring first
   keyring := keyring.New(100)

   // Create ProductionNetwork (but don't start yet)
   prodNet := NewProductionNetworkWithoutStart(numValidators)

   // Fund all 100 accounts in genesis
   prodNet.FundAccountsInGenesis(keyring.GetAllAccAddrs(), initialBalances)

   // NOW start validators
   prodNet.Start()
   ```

3. **Genesis File Modification**
   - Read each validator's genesis.json
   - Parse the bank module genesis state
   - Add 100 account balances
   - Update total supply
   - Sort coins properly (CRITICAL!)
   - Write back to genesis.json
   - Do this for ALL 4 validator nodes

### Critical Implementation Details

**Coin Sorting**: MUST be alphabetically sorted
```go
coins := sdk.NewCoins(
    sdk.NewCoin("abtc", amount),
    sdk.NewCoin("aeth", amount),
    sdk.NewCoin("asol", amount),
    sdk.NewCoin("txcoin", amount),
    sdk.NewCoin("xusd", amount),
).Sort()
```

**All Validators**: Must modify genesis for ALL validators, not just one

**Total Supply**: Must update bank module's total supply to include new accounts

### Alternative ATTEMPT 3 (If ATTEMPT 2 Fails)

**Complete Architectural Redesign**: Don't use integration network at all.

1. Extract validator keys from ProductionNetwork keyring-test directory
2. Load those keys into our test keyring
3. Use ONLY those accounts (the ones that actually exist)
4. Build transactions using ProductionNetwork's actual funded accounts

This requires:
- Reading keys from `$TESTNET_DIR/node*/evmosd/keyring-test/`
- Importing them into our keyring
- Using those 4 accounts instead of 100

## Test Results - ATTEMPT 1

```
Progress: 100.0% | 1000/1000 txs
Success: 0 (0.0%)
Errors: 1000 (100.0%)
Throughput: 718.36 txs/sec

Per-Node Distribution: ✅ PERFECT
- Node 0: 250 txs (25.0%)
- Node 1: 250 txs (25.0%)
- Node 2: 250 txs (25.0%)
- Node 3: 250 txs (25.0%)

Error Breakdown:
- "failed to check sender balance: sender...": 521 errors
- "fee payer address: ...": 479 errors
```

## User Request
"fix and run until test passed. revert the changes after 3 failed retries."

**Current Status**: Attempt 1 of 3 - FAILED
**Next**: Implement ATTEMPT 2 (fund accounts in genesis)
**Fallback**: ATTEMPT 3 (use validator accounts only)

## Key Learnings

1. **Integration network and ProductionNetwork are completely independent**
   - They have separate genesis files
   - Accounts funded in one don't exist in the other
   - Can't mix transaction building from one with execution in the other

2. **testnet init-files doesn't support custom accounts**
   - No --number-of-accounts flag
   - Only creates validator accounts
   - Must modify genesis files manually to add more accounts

3. **Chain ID matching is necessary but not sufficient**
   - Both networks now use evmos_9002-1
   - But account existence is the blocker

4. **All infrastructure works perfectly**
   - The load test framework is solid
   - Just needs accounts to actually exist in ProductionNetwork
