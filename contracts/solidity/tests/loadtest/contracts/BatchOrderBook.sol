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
        uint256 timestamp,
        uint256 totalFees
    );

    // Storage
    mapping(uint256 => Trade[]) public batches;
    uint256 public totalBatches;
    uint256 public totalTrades;
    uint256 public totalFeesCollected;

    // Statistics
    mapping(uint256 => uint256) public tradesPerBlock;
    mapping(uint256 => uint256) public feesPerBlock;
    uint256 public lastBlockNumber;

    // Fee constants
    uint256 public constant FEE_PER_TRADE = 1; // 1 unit (1xcoin in base denom)

    constructor() {
        totalBatches = 0;
        totalTrades = 0;
        totalFeesCollected = 0;
        lastBlockNumber = block.number;
    }

    /**
     * @dev Execute a batch of trades (optimized version - takes only trade count)
     * @param tradeCount Number of trades in this batch
     * @return batchId The ID of the executed batch
     */
    function executeBatchTrades(uint256 tradeCount) public payable returns (uint256) {
        require(tradeCount > 0, "BatchOrderBook: tradeCount must be > 0");

        // Calculate expected fees
        uint256 expectedFees = tradeCount * FEE_PER_TRADE;
        require(msg.value >= expectedFees, "BatchOrderBook: insufficient fees");

        uint256 batchId = totalBatches;
        uint256 currentBlock = block.number;

        // Update statistics
        totalBatches++;
        totalTrades += tradeCount;
        totalFeesCollected += expectedFees;
        tradesPerBlock[currentBlock] += tradeCount;
        feesPerBlock[currentBlock] += expectedFees;
        lastBlockNumber = currentBlock;

        // Emit batch event with fees
        emit BatchTradesExecuted(
            batchId,
            tradeCount,
            currentBlock,
            block.timestamp,
            expectedFees
        );

        // Return excess fees if any
        if (msg.value > expectedFees) {
            payable(msg.sender).transfer(msg.value - expectedFees);
        }

        return batchId;
    }

    /**
     * @dev Get total statistics
     * @return _totalBatches Total number of batches processed
     * @return _totalTrades Total number of trades processed
     * @return _totalFees Total fees collected
     * @return _lastBlock Last block that processed trades
     */
    function getStats() public view returns (
        uint256 _totalBatches,
        uint256 _totalTrades,
        uint256 _totalFees,
        uint256 _lastBlock
    ) {
        return (totalBatches, totalTrades, totalFeesCollected, lastBlockNumber);
    }

    /**
     * @dev Get trades processed in a specific block
     * @param blockNumber The block number to query
     * @return Number of trades processed in that block
     */
    function getTradesInBlock(uint256 blockNumber) public view returns (uint256) {
        return tradesPerBlock[blockNumber];
    }

    /**
     * @dev Get fees collected in a specific block
     * @param blockNumber The block number to query
     * @return Fees collected in that block
     */
    function getFeesInBlock(uint256 blockNumber) public view returns (uint256) {
        return feesPerBlock[blockNumber];
    }

    /**
     * @dev Get the fee per trade constant
     * @return Fee per trade in base denom units
     */
    function getFeePerTrade() public pure returns (uint256) {
        return FEE_PER_TRADE;
    }
}
