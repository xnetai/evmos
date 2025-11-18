# Batch Trades Load Test

This directory contains load tests for the BatchOrderBook smart contract, designed to test high-throughput trading scenarios on the Evmos blockchain.

## Overview

The load test simulates a high-volume trading environment where:
- 1000 trades are aggregated into a single batch
- 3 batches are submitted per second
- Statistics are tracked per block and per second
- Node connectivity is verified using `netstat`/`ss` commands

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

### 3. Contract Data

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

### Run Specific Test

```bash
go test -v ./tests/loadtest -run TestBatchTradesLoad
```

### Run with Custom Parameters

To modify test parameters, edit the constants in `batch_trades_test.go`:

```go
const (
    tradesPerBatch  = 1000  // Number of trades per batch
    batchesPerSec   = 3     // Batches submitted per second
    testDurationSec = 10    // Test duration in seconds
)
```

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
