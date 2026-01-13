# Local Nodes Contract Load Test

A comprehensive load testing suite for Evmos that distributes contracts, messages, and transactions across multiple validator nodes using their individual gRPC ports.

## Overview

This load test implements a production-like testing environment with:
- **4 separate validator processes** (ProductionNetwork)
- **100 funded users** submitting transactions
- **Round-robin distribution** across all validator nodes
- **4 transaction types**: Smart contracts (40%), Bank transfers (30%), Staking operations (20%), Raw EVM transactions (10%)
- **Comprehensive per-node statistics** tracking

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  LocalNodesContractLoadTestSuite                            │
│  - Manages ProductionNetwork (4 validators)                 │
│  - 100-user keyring                                         │
│  - Round-robin distribution                                 │
│  - Comprehensive statistics tracking                        │
└─────────────────────────────────────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
        ▼                   ▼                   ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│ Validator 0  │    │ Validator 1  │    │ Validator 2  │  ...
│ gRPC: 9090   │    │ gRPC: 9100   │    │ gRPC: 9110   │
│ RPC:  26657  │    │ RPC:  26667  │    │ RPC:  26677  │
└──────────────┘    └──────────────┘    └──────────────┘
```

## Files

### Core Implementation

1. **[load_stats.go](load_stats.go)** - Statistics tracking infrastructure
   - `TxTypeStats` - Per-transaction-type statistics
   - `LocalNodeStats` - Per-node statistics with latency tracking
   - `LocalLoadTestStats` - Global statistics coordinator
   - Formatted table output for comprehensive reporting

2. **[node_client.go](node_client.go)** - Per-node gRPC/RPC client wrapper
   - `NodeClient` - Manages connections to individual validator ports
   - Transaction submission via CometBFT RPC
   - Health checks and block height queries
   - `RoundRobinDistributor` - Ensures even load distribution

3. **[tx_builders.go](tx_builders.go)** - Transaction builders
   - `ContractTxBuilder` - Smart contract calls (BatchOrderBook)
   - `BankTxBuilder` - Bank transfers between users
   - `StakingTxBuilder` - Staking delegations to validators
   - `RawEVMTxBuilder` - Direct EVM transfers
   - Weighted random selection (40/30/20/10 distribution)

4. **[local_nodes_contract_load_test.go](local_nodes_contract_load_test.go)** - Main test suite
   - `LocalNodesContractLoadTestSuite` - Test orchestration
   - Custom genesis with 100 funded users
   - ProductionNetwork integration
   - Automated setup, execution, and teardown

## How to Run

```bash
cd tests/loadtest

# Run the basic load test (1,000 transactions)
go test -v -run TestLocalNodesContractLoad/TestBasicLoad

# Run the intensive load test (10,000 transactions) - currently skipped
# First, remove the Skip() line in TestIntensiveLoad, then:
go test -v -run TestLocalNodesContractLoad/TestIntensiveLoad -timeout 30m
```

## Test Flow

### 1. Setup (Automated)
- Create 100-user keyring
- Initialize integration network for transaction building
- Start ProductionNetwork with 4 validators (separate processes)
- Deploy BatchOrderBook contract
- Create gRPC clients for each validator's port
- Initialize round-robin distributor and statistics tracker

### 2. Execution
- Submit transactions in round-robin fashion across nodes
- Real-time progress reporting every 100 transactions
- Per-node distribution updates every 500 transactions
- All transactions signed and properly formatted

### 3. Teardown
- Print comprehensive final statistics
- Per-node detailed breakdowns
- Error analysis (if any)
- Clean up all resources (close connections, stop validators)

## Transaction Types

### 1. Contract Calls (40%)
Execute batch trades on the deployed BatchOrderBook contract:
- 10 trades per batch
- Gas limit: 500,000
- Calls `executeBatchTrades()` method

### 2. Bank Transfers (30%)
Random token transfers between users:
- Amount: 1-1000 txcoin
- Random sender/receiver pairs
- Gas limit: 100,000

### 3. Staking Operations (20%)
Delegation to random validators:
- Amount: 100-10,000 txcoin
- Random validator selection
- Gas limit: 400,000

### 4. Raw EVM Transactions (10%)
Direct EVM transfers:
- Amount: 1-1000 wei
- Random sender/receiver pairs
- Gas limit: 21,000

## User Accounts

All 100 users are funded at genesis with:
- **1,000,000,000 txcoin** - For gas fees
- **100,000 abtc** - Test Bitcoin asset
- **100,000 aeth** - Test Ethereum asset
- **100,000 asol** - Test Solana asset
- **100,000 xusd** - Test USD stablecoin

## Validator Ports

Each validator runs on its own set of ports:

| Validator | RPC Port | P2P Port | gRPC Port | API Port | JSON-RPC Port |
|-----------|----------|----------|-----------|----------|---------------|
| 0         | 26657    | 26656    | 9090      | 1317     | 8545          |
| 1         | 26667    | 26666    | 9100      | 1327     | 8555          |
| 2         | 26677    | 26676    | 9110      | 1337     | 8565          |
| 3         | 26687    | 26686    | 9120      | 1347     | 8575          |

## Statistics Output

### Per-Node Summary
```
╔════════════════════════════════════════════════════════════════════════╗
║                    PER-NODE STATISTICS                                ║
╠═════════╤═════════╤══════════╤═══════════╤══════════╤═════════════════╣
║ Node    │ Total   │ Contract │ Bank      │ Staking  │ Raw EVM        ║
║ Index   │ Txs     │ Calls    │ Transfers │ Ops      │ Txs            ║
╠═════════╪═════════╪══════════╪═══════════╪══════════╪═════════════════╣
║ 0       │ 250     │ 100      │ 75        │ 50       │ 25             ║
║         │ (25.0%) │ (40.0%)  │ (30.0%)   │ (20.0%)  │ (10.0%)        ║
...
```

### Detailed Per-Node Statistics
Each node gets a detailed breakdown showing:
- Transaction counts by type
- Success/error rates
- Average gas used per type
- Latency statistics (min/max/avg)
- Block distribution
- Error categorization

### Error Breakdown
If errors occur, a comprehensive breakdown shows:
- Error messages and counts
- Which nodes were affected
- Error distribution by type

## Verification

The test automatically verifies:
- ✓ All 100 users funded correctly
- ✓ Contract deploys successfully
- ✓ All 4 validators start and listen on correct ports
- ✓ gRPC clients connect successfully
- ✓ Round-robin distribution is even (±5% tolerance)
- ✓ Transaction type distribution matches expected ratios (40/30/20/10)
- ✓ Transaction success rates
- ✓ Per-node statistics accuracy

## Performance Metrics

The test tracks and reports:
- **Throughput**: Transactions per second
- **Latency**: Min/max/average per node
- **Gas Usage**: Average per transaction type
- **Success Rate**: Overall and per-node
- **Distribution**: Even distribution across nodes
- **Block Distribution**: Which blocks contain which transactions

## Troubleshooting

### Binary Not Found
If you see "Neither xcoind nor evmosd found in PATH", run:
```bash
make build
```

### Port Already in Use
If validators fail to start due to port conflicts:
```bash
# Find and kill processes using the ports
lsof -ti:9090,9100,9110,9120,26657,26667,26677,26687 | xargs kill -9
```

### Slow Performance
For faster testing, reduce the number of transactions:
```go
numTxs := 100  // Instead of 1000
```

### Test Timeout
For large tests (10k+ transactions), increase the timeout:
```bash
go test -v -run TestLocalNodesContractLoad -timeout 30m
```

## Implementation Notes

### Round-Robin Distribution
- Uses atomic counter for thread-safe node selection
- Guarantees even distribution across all nodes
- Each transaction goes to next node in sequence

### Statistics Thread Safety
- Atomic operations for simple counters
- Mutex protection for complex data structures (maps, latency tracking)
- No data races or corruption

### Connection Management
- One gRPC connection per validator
- One RPC connection per validator
- All connections properly closed in teardown
- Automatic reconnection on failure

### Transaction Building
- Uses integration network utilities for proper signing
- All transactions properly formatted with gas/fees
- Nonce management handled automatically
- Proper encoding for network submission

## Extension Points

To add new transaction types:

1. Create a new builder implementing `TransactionBuilder`
2. Update `SelectTransactionBuilder()` with new weight
3. Add new `TxType` constant
4. Update statistics display logic

To change distribution ratios:
```go
// In SelectTransactionBuilder()
r := rand.Intn(100)
if r < 50 { return contractBuilder }      // 50%
else if r < 75 { return bankBuilder }     // 25%
else if r < 90 { return stakingBuilder }  // 15%
return rawEVMBuilder                      // 10%
```

## License

Copyright Tharsis Labs Ltd.(Evmos)
SPDX-License-Identifier:ENCL-1.0
