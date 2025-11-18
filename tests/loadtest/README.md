# Batch Trades Load Test

This directory contains comprehensive load tests for the BatchOrderBook smart contract, designed to test high-throughput trading scenarios on the Evmos blockchain with multi-node configurations.

## Overview

The load test suite simulates high-volume trading environments with:
- **Basic Load Tests**: 1,000 trades per batch
- **Large Batch Tests**: 20,000, 30,000, and 100,000 trades per batch
- **Multi-Node Tests**: Testing with 3 and 10 validator nodes
- **Node Synchronization**: Verification that all nodes remain in sync
- **Performance Metrics**: Detailed throughput and timing statistics
- **Node Verification**: Using `netstat`/`ss` commands to verify node connectivity

## Components

### 1. BatchOrderBook Smart Contract

**Location**: `contracts/BatchOrderBook.sol`

The smart contract accepts batches of trades with the following structure:

```solidity
struct Trade {
    address trader;
    uint256 orderId;
    string symbol;
    uint256 price;
    uint256 amount;
    bool isBuy;
    uint256 timestamp;
}
```

**Key Features**:
- Batch processing of trades
- Event emission for each trade and batch
- Block-level statistics tracking
- Query methods for historical data

**Methods**:
- `executeBatchTrades(Trade[] memory trades)`: Submit a batch of trades
- `getStats()`: Get total batches, trades, and last block number
- `getBatchTrades(uint256 batchId)`: Retrieve trades for a specific batch
- `getTradesInBlock(uint256 blockNumber)`: Get trade count for a block

### 2. Load Test Suite

**Location**: `batch_trades_test.go`

The Go test suite implements:

#### Node Verification
- Checks for running nodes using `netstat` or `ss` commands
- Looks for common blockchain ports:
  - 26656: CometBFT P2P
  - 26657: CometBFT RPC
  - 8545: Ethereum JSON-RPC
  - 9090: Cosmos gRPC
  - And more...

#### Load Testing
- Configurable trades per batch (default: 1000)
- Configurable batches per second (default: 3)
- Configurable test duration (default: 10 seconds)
- Concurrent batch submissions
- Block production coordination

#### Statistics Tracking
- Total trades submitted
- Total batches submitted
- Success/failure rates
- Throughput metrics (trades/sec, batches/sec)
- Block-level statistics

### 3. Multi-Node Load Test Suite

**Location**: `multinode_test.go`

Tests batch trades with 3 validators:
- **20k Trades Test**: 20,000 trades per batch, 3 batches/sec, 3 seconds
- **30k Trades Test**: 30,000 trades per batch, 3 batches/sec, 3 seconds
- **100k Trades Test**: 100,000 trades per batch, 3 batches/sec, 3 seconds

**Features**:
- Comprehensive node statistics at startup
- Validator power and status display
- Node synchronization verification
- Per-block trade batch tracking
- Detailed timing metrics (submission time, block time)

### 4. Ten-Node Load Test Suite

**Location**: `tennode_test.go`

Tests batch trades with 10 validators:
- **20k Trades Test**: 20,000 trades per batch with 10 nodes
- **30k Trades Test**: 30,000 trades per batch with 10 nodes
- **100k Trades Test**: 100,000 trades per batch with 10 nodes

**Features**:
- Detailed validator table with power, status, and jailed state
- Consensus requirements calculation
- All-node synchronization checks
- Contract deployment verification across all nodes
- Per-validator statistics tracking

### 5. Contract Data

**Location**: `contract_data.go`

Provides:
- Contract loading utilities
- ABI definitions
- Deployment helpers

## Building

### Prerequisites

- Go 1.21 or higher
- Node.js and npm (for contract compilation)
- Python 3 (for compilation scripts)

### Compile the Smart Contract

```bash
# From the repository root
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --compile
```

This will compile all contracts including BatchOrderBook and place the artifacts in the appropriate directories.

### Build the Tests

```bash
# From the repository root
go test -c ./tests/loadtest
```

## Running the Tests

### Run All Load Tests

```bash
# From the repository root
go test -v ./tests/loadtest
```

### Run Basic Load Test (1,000 trades)

```bash
go test -v ./tests/loadtest -run TestBatchTradesLoadTest
```

### Run Multi-Node Tests (3 validators)

```bash
# Run all multi-node tests (20k, 30k, 100k trades)
go test -v ./tests/loadtest -run TestMultiNodeLoadTest

# Run specific batch size
go test -v ./tests/loadtest -run TestMultiNodeLoadTest/TestBatchSize20k
go test -v ./tests/loadtest -run TestMultiNodeLoadTest/TestBatchSize30k
go test -v ./tests/loadtest -run TestMultiNodeLoadTest/TestBatchSize100k
```

### Run Ten-Node Tests (10 validators)

```bash
# Run all ten-node tests (20k, 30k, 100k trades)
go test -v ./tests/loadtest -run TestTenNodeLoadTest

# Run specific batch size
go test -v ./tests/loadtest -run TestTenNodeLoadTest/TestTenNodeBatchSize20k
go test -v ./tests/loadtest -run TestTenNodeLoadTest/TestTenNodeBatchSize30k
go test -v ./tests/loadtest -run TestTenNodeLoadTest/TestTenNodeBatchSize100k
```

### Run with Custom Parameters

To modify test parameters, edit the constants in the respective test files:

**`batch_trades_test.go`** (Basic test):
```go
const (
    tradesPerBatch  = 1000  // Number of trades per batch
    batchesPerSec   = 3     // Batches submitted per second
    testDurationSec = 10    // Test duration in seconds
)
```

**`multinode_test.go`** and **`tennode_test.go`** (Multi-node tests):
- Tests are parameterized by batch size (20k, 30k, 100k)
- Fixed at 3 batches/sec, 3 seconds duration
- Validator count set in SetupSuite (3 or 10 nodes)

## Test Output

The test provides detailed output including:

1. **Node Verification**:
   ```
   === Verifying Nodes Status ===
   Using netstat command to verify nodes
   ✓ Found node listening on port :26656
   ✓ Found node listening on port :26657
   ...
   === Node Verification Complete ===
   ```

2. **Configuration**:
   ```
   Configuration:
     - Trades per batch: 1000
     - Batches per second: 3
     - Test duration: 10 seconds
     - Expected total trades: 30000
   ```

3. **Progress Updates**:
   ```
   Progress: 3 batches, 3000 trades, 3 success, 0 errors
   Progress: 6 batches, 6000 trades, 6 success, 0 errors
   ...
   ```

4. **Final Statistics**:
   ```
   === Load Test Statistics ===
   Duration: 10.5s
   Total Batches Submitted: 30
   Total Trades Submitted: 30000
   Successful Batches: 30
   Failed Batches: 0
   Success Rate: 100.00%

   Throughput:
     - Batches/sec: 2.86
     - Trades/sec: 2857.14
   === Statistics Complete ===
   ```

5. **Contract State Verification**:
   ```
   === Verifying Contract State ===
   Contract state verified successfully
   === Verification Complete ===
   ```

### Multi-Node Test Output Examples

**Node Statistics (10 Validators)**:
```
╔════════════════════════════════════════════════════════════════════════════════╗
║                    Detailed Node Statistics & Verification                    ║
╚════════════════════════════════════════════════════════════════════════════════╝

Network Configuration:
  - Chain ID: evmos_9000-1
  - Total Validators: 10
  - Block Height: 1
  - Block Time: 2025-11-18 12:00:00

Validator Details:
┌─────┬──────────────────────────────────────────────┬──────────┬────────────┬──────────┐
│ ID  │ Validator Address                            │ Power    │ Status     │ Jailed   │
├─────┼──────────────────────────────────────────────┼──────────┼────────────┼──────────┤
│ 1   │ evmosvaloper1abc...                          │ 1000000  │ Bonded     │ No       │
│ 2   │ evmosvaloper1def...                          │ 1000000  │ Bonded     │ No       │
...
└─────┴──────────────────────────────────────────────┴──────────┴────────────┴──────────┘

Network Summary:
  - Active Validators: 10 / 10
  - Total Voting Power: 10000000
  - Average Power per Validator: 1000000.00

╔════════════════════════════════════════════════════════════════════════════════╗
║                  ✓ All 10 Nodes Verified and Synchronized                     ║
╚════════════════════════════════════════════════════════════════════════════════╝
```

**Progress with Timing** (Multi-node):
```
Sec 1 | Batches: 3 (60000 trades) | Submit:  125ms | Block: 1→2 ( 50ms) | ✓ 3 | ✗ 0 | Nodes: 10 synced
Sec 2 | Batches: 3 (60000 trades) | Submit:  118ms | Block: 2→3 ( 48ms) | ✓ 6 | ✗ 0 | Nodes: 10 synced
Sec 3 | Batches: 3 (60000 trades) | Submit:  122ms | Block: 3→4 ( 51ms) | ✓ 9 | ✗ 0 | Nodes: 10 synced
```

**Node Synchronization Verification**:
```
╔════════════════════════════════════════════════════════════════════════════════╗
║                 10-Node Synchronization Verification                          ║
╚════════════════════════════════════════════════════════════════════════════════╝

Network State:
  - Current Height: 4
  - Block Time: 2025-11-18 12:00:03
  - Total Validators: 10

Per-Validator Synchronization:
  ✓ Validator  1 synced at height 4 (Power: 1000000)
  ✓ Validator  2 synced at height 4 (Power: 1000000)
  ...
  ✓ Validator 10 synced at height 4 (Power: 1000000)

Block Processing Summary:
  ✓ Block 2: 3 batches, all 10 nodes synced
  ✓ Block 3: 3 batches, all 10 nodes synced
  ✓ Block 4: 3 batches, all 10 nodes synced

✓ All 10 nodes verified in sync with trade batches
╚════════════════════════════════════════════════════════════════════════════════╝
```

**Final Statistics** (100k trades test):
```
╔════════════════════════════════════════════════════════════════════════════════╗
║                        10-Node Load Test Results                              ║
╚════════════════════════════════════════════════════════════════════════════════╝

Test Duration: 3.125s

Performance Metrics:
  - Total Batches: 9
  - Total Trades: 900000
  - Success Rate: 100.00% (9/9)
  - Failed Batches: 0

Throughput Analysis:
  - Batches/sec: 2.88
  - Trades/sec: 288000.00
  - Avg Batch Size: 100000 trades
  - Blocks Produced: 3
  - Avg Block Time: 1041.67 ms

Network Statistics:
  - Validators: 10
  - All Nodes Synced: ✓ Yes
  - Consensus Maintained: ✓ Yes

╚════════════════════════════════════════════════════════════════════════════════╝
```

## Architecture

### Test Flow

1. **Setup Phase**:
   - Verify nodes are running
   - Initialize network with validators
   - Deploy BatchOrderBook contract

2. **Execution Phase**:
   - Generate mock trade data
   - Submit batches concurrently
   - Coordinate block production
   - Track statistics

3. **Verification Phase**:
   - Wait for pending transactions
   - Query contract state
   - Verify expected results
   - Print final statistics

### Trade Generation

Mock trades are generated with:
- Sequential order IDs
- Multiple trading pairs (BTC/USD, ETH/USD, etc.)
- Varying prices and amounts
- Alternating buy/sell orders
- Current timestamps

## Performance Considerations

### Gas Estimation

The contract uses efficient storage patterns:
- Batched storage writes
- Event-based indexing
- Minimal storage reads

### Throughput Targets

Expected performance:
- 1000 trades/batch = ~500k-1M gas per transaction
- 3 batches/second = ~1.5-3M gas/second
- 10 second test = 30k trades total

### Scalability

To increase load:
1. Increase `tradesPerBatch` (more trades per transaction)
2. Increase `batchesPerSec` (more transactions per second)
3. Increase `testDurationSec` (longer test duration)
4. Add more concurrent submitters

## Monitoring

The test automatically tracks:
- Transaction success/failure rates
- Block production timing
- Gas usage patterns
- Throughput metrics

## Troubleshooting

### Contract Compilation Errors

```bash
# Clean and recompile
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --clean
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --compile
```

### Test Failures

Common issues:
1. **Gas estimation failures**: Increase gas limits in test configuration
2. **Timeout errors**: Increase test timeouts or reduce load
3. **Contract not found**: Ensure contract is compiled before running tests
4. **Node verification warnings**: Expected in integration test environments

### Node Verification Skipped

If neither `netstat` nor `ss` is available:
- Test will log a warning and continue
- This is expected in some container environments
- Node verification is informational only

## Future Enhancements

Potential improvements:
- [ ] Add metrics export (Prometheus format)
- [ ] Support for multiple concurrent traders
- [ ] Historical data analysis
- [ ] Stress testing modes
- [ ] Performance regression testing
- [ ] Integration with CI/CD pipelines

## License

Copyright Tharsis Labs Ltd.(Evmos)
SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)
