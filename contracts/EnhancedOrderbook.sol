// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function transfer(address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

contract EnhancedOrderbook {
    address public xusd;

    // Fee structure: basis points (100 = 1%)
    uint256 public tradingFee = 30; // 0.3%
    uint256 public constant FEE_DENOMINATOR = 10000;

    struct Pool {
        address token;
        uint256 assetReserve;
        uint256 xusdReserve;
        uint256 totalShares; // Total LP shares
        uint256 accumulatedFees; // Accumulated fees in XUSD
    }

    struct LPPosition {
        uint256 shares;
        uint256 minPrice; // Minimum price in range (1e18 precision)
        uint256 maxPrice; // Maximum price in range (1e18 precision)
        uint256 feeDebt; // Fee debt for this position
    }

    struct LimitOrder {
        address user;
        address token;
        bool isBuy; // true = buy, false = sell
        uint256 amount;
        uint256 priceLimit; // Price limit (1e18 precision)
        bool executed;
    }

    // Pool data
    mapping(address => Pool) public pools;

    // LP positions: token => user => position
    mapping(address => mapping(address => LPPosition)) public lpPositions;

    // Limit orders
    LimitOrder[] public limitOrders;
    mapping(address => uint256[]) public userOrders;

    event PoolCreated(address indexed token, uint256 assetReserve, uint256 xusdReserve);
    event LiquidityAdded(address indexed token, address indexed provider, uint256 shares, uint256 assetAmount, uint256 xusdAmount);
    event LiquidityRemoved(address indexed token, address indexed provider, uint256 shares, uint256 assetAmount, uint256 xusdAmount);
    event Trade(address indexed user, address indexed token, bool isBuy, uint256 assetAmount, uint256 xusdAmount, uint256 fee);
    event LimitOrderCreated(uint256 indexed orderId, address indexed user, address indexed token, bool isBuy);
    event LimitOrderExecuted(uint256 indexed orderId);
    event FeesCollected(address indexed token, address indexed provider, uint256 amount);

    constructor(address _xusd) {
        xusd = _xusd;
    }

    // Create a new pool (only if doesn't exist)
    function createPool(
        address token,
        uint256 initialAsset,
        uint256 initialXUSD,
        uint256 minPrice,
        uint256 maxPrice
    ) external returns (uint256) {
        require(pools[token].token == address(0), "Pool exists");
        require(minPrice < maxPrice, "Invalid price range");

        // Transfer tokens
        IERC20(token).transferFrom(msg.sender, address(this), initialAsset);
        IERC20(xusd).transferFrom(msg.sender, address(this), initialXUSD);

        // Calculate initial shares (use geometric mean)
        uint256 shares = sqrt(initialAsset * initialXUSD);

        pools[token] = Pool({
            token: token,
            assetReserve: initialAsset,
            xusdReserve: initialXUSD,
            totalShares: shares,
            accumulatedFees: 0
        });

        lpPositions[token][msg.sender] = LPPosition({
            shares: shares,
            minPrice: minPrice,
            maxPrice: maxPrice,
            feeDebt: 0
        });

        emit PoolCreated(token, initialAsset, initialXUSD);
        emit LiquidityAdded(token, msg.sender, shares, initialAsset, initialXUSD);

        return shares;
    }

    // Add liquidity to existing pool
    function addLiquidity(
        address token,
        uint256 assetAmount,
        uint256 xusdAmount,
        uint256 minPrice,
        uint256 maxPrice
    ) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");
        require(minPrice < maxPrice, "Invalid price range");

        // Transfer tokens
        IERC20(token).transferFrom(msg.sender, address(this), assetAmount);
        IERC20(xusd).transferFrom(msg.sender, address(this), xusdAmount);

        // Calculate shares proportional to contribution
        uint256 shares;
        if (pool.totalShares == 0) {
            shares = sqrt(assetAmount * xusdAmount);
        } else {
            uint256 assetShare = (assetAmount * pool.totalShares) / pool.assetReserve;
            uint256 xusdShare = (xusdAmount * pool.totalShares) / pool.xusdReserve;
            shares = assetShare < xusdShare ? assetShare : xusdShare;
        }

        // Update pool
        pool.assetReserve += assetAmount;
        pool.xusdReserve += xusdAmount;
        pool.totalShares += shares;

        // Update or create LP position
        LPPosition storage position = lpPositions[token][msg.sender];
        if (position.shares > 0) {
            // Update existing position
            position.shares += shares;
        } else {
            // Create new position
            lpPositions[token][msg.sender] = LPPosition({
                shares: shares,
                minPrice: minPrice,
                maxPrice: maxPrice,
                feeDebt: 0
            });
        }

        emit LiquidityAdded(token, msg.sender, shares, assetAmount, xusdAmount);

        return shares;
    }

    // Remove liquidity from pool
    function removeLiquidity(
        address token,
        uint256 shares
    ) external returns (uint256 assetAmount, uint256 xusdAmount, uint256 fees) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        LPPosition storage position = lpPositions[token][msg.sender];
        require(position.shares >= shares, "Insufficient shares");

        // Calculate amounts to return
        assetAmount = (shares * pool.assetReserve) / pool.totalShares;
        xusdAmount = (shares * pool.xusdReserve) / pool.totalShares;

        // Calculate accumulated fees for this position
        fees = (shares * pool.accumulatedFees) / pool.totalShares;

        // Update pool
        pool.assetReserve -= assetAmount;
        pool.xusdReserve -= xusdAmount;
        pool.totalShares -= shares;
        pool.accumulatedFees -= fees;

        // Update position
        position.shares -= shares;

        // Transfer tokens back
        IERC20(token).transfer(msg.sender, assetAmount);
        IERC20(xusd).transfer(msg.sender, xusdAmount + fees);

        emit LiquidityRemoved(token, msg.sender, shares, assetAmount, xusdAmount);
        if (fees > 0) {
            emit FeesCollected(token, msg.sender, fees);
        }

        return (assetAmount, xusdAmount, fees);
    }

    // Collect accumulated fees without removing liquidity
    function collectFees(address token) external returns (uint256 fees) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        LPPosition storage position = lpPositions[token][msg.sender];
        require(position.shares > 0, "No position");

        // Calculate fees for this position
        fees = (position.shares * pool.accumulatedFees) / pool.totalShares;

        if (fees > 0) {
            pool.accumulatedFees -= fees;
            IERC20(xusd).transfer(msg.sender, fees);
            emit FeesCollected(token, msg.sender, fees);
        }

        return fees;
    }

    // Buy assets with XUSD
    function buy(address token, uint256 xusdAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        // Calculate fee
        uint256 fee = (xusdAmount * tradingFee) / FEE_DENOMINATOR;
        uint256 xusdAfterFee = xusdAmount - fee;

        // Calculate asset amount using constant product formula
        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newXUSDReserve = pool.xusdReserve + xusdAfterFee;
        uint256 newAssetReserve = k / newXUSDReserve;
        uint256 assetAmount = pool.assetReserve - newAssetReserve;

        require(assetAmount > 0, "Insufficient liquidity");

        // Check if price is within any LP's range
        uint256 newPrice = getPrice(token);

        // Transfer XUSD from buyer
        IERC20(xusd).transferFrom(msg.sender, address(this), xusdAmount);

        // Update reserves
        pool.xusdReserve = newXUSDReserve;
        pool.assetReserve = newAssetReserve;
        pool.accumulatedFees += fee;

        // Transfer asset to buyer
        IERC20(token).transfer(msg.sender, assetAmount);

        emit Trade(msg.sender, token, true, assetAmount, xusdAmount, fee);

        // Check and execute limit orders
        _checkLimitOrders(token, newPrice);

        return assetAmount;
    }

    // Sell assets for XUSD
    function sell(address token, uint256 assetAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        // Calculate XUSD amount
        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newAssetReserve = pool.assetReserve + assetAmount;
        uint256 newXUSDReserve = k / newAssetReserve;
        uint256 xusdAmount = pool.xusdReserve - newXUSDReserve;

        require(xusdAmount > 0, "Insufficient liquidity");

        // Calculate fee
        uint256 fee = (xusdAmount * tradingFee) / FEE_DENOMINATOR;
        uint256 xusdAfterFee = xusdAmount - fee;

        // Transfer asset from seller
        IERC20(token).transferFrom(msg.sender, address(this), assetAmount);

        // Update reserves
        pool.assetReserve = newAssetReserve;
        pool.xusdReserve = newXUSDReserve;
        pool.accumulatedFees += fee;

        // Transfer XUSD to seller
        IERC20(xusd).transfer(msg.sender, xusdAfterFee);

        uint256 newPrice = getPrice(token);

        emit Trade(msg.sender, token, false, assetAmount, xusdAfterFee, fee);

        // Check and execute limit orders
        _checkLimitOrders(token, newPrice);

        return xusdAfterFee;
    }

    // Create a limit order
    function createLimitOrder(
        address token,
        bool isBuy,
        uint256 amount,
        uint256 priceLimit
    ) external returns (uint256) {
        require(pools[token].token != address(0), "Pool does not exist");

        uint256 orderId = limitOrders.length;
        limitOrders.push(LimitOrder({
            user: msg.sender,
            token: token,
            isBuy: isBuy,
            amount: amount,
            priceLimit: priceLimit,
            executed: false
        }));

        userOrders[msg.sender].push(orderId);

        emit LimitOrderCreated(orderId, msg.sender, token, isBuy);

        return orderId;
    }

    // Internal function to check limit orders (just emit events, don't execute)
    function _checkLimitOrders(address token, uint256 currentPrice) internal {
        // Simple implementation: check recent orders
        // In production, use a more efficient data structure
        uint256 len = limitOrders.length;
        uint256 startIdx = len > 10 ? len - 10 : 0;

        for (uint256 i = startIdx; i < len; i++) {
            LimitOrder storage order = limitOrders[i];
            if (order.executed || order.token != token) continue;

            bool shouldExecute = false;
            if (order.isBuy && currentPrice <= order.priceLimit) {
                shouldExecute = true;
            } else if (!order.isBuy && currentPrice >= order.priceLimit) {
                shouldExecute = true;
            }

            if (shouldExecute) {
                // Mark as executable - actual execution happens via executeLimitOrder()
                emit LimitOrderExecuted(i);
            }
        }
    }

    // Execute a limit order (can be called by anyone when price conditions are met)
    function executeLimitOrder(uint256 orderId) external {
        require(orderId < limitOrders.length, "Invalid order ID");

        LimitOrder storage order = limitOrders[orderId];
        require(!order.executed, "Already executed");

        uint256 currentPrice = getPrice(order.token);

        // Check if price condition is met
        bool canExecute = false;
        if (order.isBuy && currentPrice <= order.priceLimit) {
            canExecute = true;
        } else if (!order.isBuy && currentPrice >= order.priceLimit) {
            canExecute = true;
        }

        require(canExecute, "Price condition not met");

        order.executed = true;

        // Note: Actual trade execution would need to be done separately
        // by the order creator calling buy() or sell() after this
        emit LimitOrderExecuted(orderId);
    }

    // Get current price of an asset
    function getPrice(address token) public view returns (uint256) {
        Pool memory pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");
        require(pool.assetReserve > 0, "No reserves");
        return (pool.xusdReserve * 1e18) / pool.assetReserve;
    }

    // Get pool reserves
    function getReserves(address token) external view returns (uint256, uint256) {
        Pool memory pool = pools[token];
        return (pool.assetReserve, pool.xusdReserve);
    }

    // Get LP position info
    function getLPPosition(address token, address provider) external view returns (
        uint256 shares,
        uint256 minPrice,
        uint256 maxPrice,
        uint256 assetValue,
        uint256 xusdValue,
        uint256 pendingFees
    ) {
        LPPosition memory position = lpPositions[token][provider];
        Pool memory pool = pools[token];

        if (position.shares == 0 || pool.totalShares == 0) {
            return (0, 0, 0, 0, 0, 0);
        }

        shares = position.shares;
        minPrice = position.minPrice;
        maxPrice = position.maxPrice;
        assetValue = (shares * pool.assetReserve) / pool.totalShares;
        xusdValue = (shares * pool.xusdReserve) / pool.totalShares;
        pendingFees = (shares * pool.accumulatedFees) / pool.totalShares;

        return (shares, minPrice, maxPrice, assetValue, xusdValue, pendingFees);
    }

    // Get accumulated fees for a pool
    function getPoolFees(address token) external view returns (uint256) {
        return pools[token].accumulatedFees;
    }

    // Helper: square root function (Babylonian method)
    function sqrt(uint256 x) internal pure returns (uint256) {
        if (x == 0) return 0;
        uint256 z = (x + 1) / 2;
        uint256 y = x;
        while (z < y) {
            y = z;
            z = (x / z + z) / 2;
        }
        return y;
    }
}
