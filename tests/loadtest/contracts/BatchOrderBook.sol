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

    // Events
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

    // Storage
    mapping(uint256 => Trade[]) public batches;
    uint256 public totalBatches;
    uint256 public totalTrades;

    // Statistics
    mapping(uint256 => uint256) public tradesPerBlock;
    uint256 public lastBlockNumber;

    constructor() {
        totalBatches = 0;
        totalTrades = 0;
        lastBlockNumber = block.number;
    }

    /**
     * @dev Execute a batch of trades
     * @param trades Array of Trade structs to execute
     * @return batchId The ID of the executed batch
     */
    function executeBatchTrades(Trade[] memory trades) public returns (uint256) {
        require(trades.length > 0, "BatchOrderBook: empty trades array");

        uint256 batchId = totalBatches;
        uint256 currentBlock = block.number;

        // Store all trades in this batch
        for (uint256 i = 0; i < trades.length; i++) {
            batches[batchId].push(trades[i]);

            // Emit individual trade event
            emit TradeExecuted(
                trades[i].trader,
                trades[i].orderId,
                trades[i].symbol,
                trades[i].price,
                trades[i].amount,
                trades[i].isBuy,
                trades[i].timestamp
            );
        }

        // Update statistics
        totalBatches++;
        totalTrades += trades.length;
        tradesPerBlock[currentBlock] += trades.length;
        lastBlockNumber = currentBlock;

        // Emit batch event
        emit BatchTradesExecuted(
            batchId,
            trades.length,
            currentBlock,
            block.timestamp
        );

        return batchId;
    }

    /**
     * @dev Get all trades in a specific batch
     * @param batchId The batch ID to query
     * @return Array of trades in the batch
     */
    function getBatchTrades(uint256 batchId) public view returns (Trade[] memory) {
        require(batchId < totalBatches, "BatchOrderBook: batch does not exist");
        return batches[batchId];
    }

    /**
     * @dev Get the number of trades in a specific batch
     * @param batchId The batch ID to query
     * @return Number of trades in the batch
     */
    function getBatchSize(uint256 batchId) public view returns (uint256) {
        require(batchId < totalBatches, "BatchOrderBook: batch does not exist");
        return batches[batchId].length;
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
