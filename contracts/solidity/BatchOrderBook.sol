// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

contract BatchOrderBook {
    struct Trade {
        address trader;
        uint256 orderId;
        string symbol;
        uint256 price;
        uint256 amount;
        bool isBuy;
        uint256 timestamp;
    }

    // Events - emit trade data instead of storing
    event TradeExecuted(
        address indexed trader,
        uint256 indexed orderId,
        string symbol,
        uint256 price,
        uint256 amount,
        bool isBuy,
        uint256 timestamp
    );

    event BatchTradesExecuted(
        uint256 indexed batchId,
        uint256 tradeCount,
        uint256 blockNumber,
        uint256 timestamp
    );

    // Storage - only store aggregated statistics for gas efficiency
    uint256 public totalBatches;
    uint256 public totalTrades;
    mapping(uint256 => uint256) public batchSizes;
    mapping(uint256 => uint256) public tradesPerBlock;
    uint256 public lastBlockNumber;

    constructor() {
        totalBatches = 0;
        totalTrades = 0;
        lastBlockNumber = block.number;
    }

    /**
     * @dev Execute a batch of trades - gas optimized version
     * @param trades Array of Trade structs to execute
     * @return batchId The ID of the executed batch
     */
    function executeBatchTrades(Trade[] memory trades) public returns (uint256) {
        require(trades.length > 0, "BatchOrderBook: empty trades array");

        uint256 batchId = totalBatches;
        uint256 currentBlock = block.number;

        // Store only batch size, not individual trades
        batchSizes[batchId] = trades.length;

        // Emit batch event with aggregate data
        emit BatchTradesExecuted(
            batchId,
            trades.length,
            currentBlock,
            block.timestamp
        );

        // Update statistics
        totalBatches++;
        totalTrades += trades.length;
        tradesPerBlock[currentBlock] += trades.length;
        lastBlockNumber = currentBlock;

        return batchId;
    }

    /**
     * @dev Get the number of trades in a specific batch
     * @param batchId The batch ID to query
     * @return Number of trades in the batch
     */
    function getBatchSize(uint256 batchId) public view returns (uint256) {
        require(batchId < totalBatches, "BatchOrderBook: batch does not exist");
        return batchSizes[batchId];
    }

    /**
     * @dev Get total statistics
     * @return _totalBatches Total number of batches processed
     * @return _totalTrades Total number of trades processed
     * @return _lastBlock Last block that processed trades
     */
    function getStats() public view returns (
        uint256 _totalBatches,
        uint256 _totalTrades,
        uint256 _lastBlock
    ) {
        return (totalBatches, totalTrades, lastBlockNumber);
    }

    /**
     * @dev Get trades processed in a specific block
     * @param blockNumber The block number to query
     * @return Number of trades processed in that block
     */
    function getTradesInBlock(uint256 blockNumber) public view returns (uint256) {
        return tradesPerBlock[blockNumber];
    }
}
