// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function transfer(address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

contract GovernedOrderbook {
    address public xusd;
    address public xshares; // Governance token

    // Fee structure - constant 1 XUSD per trade (votable)
    uint256 public tradingFee = 1e18; // 1 XUSD
    bool public useConstantFee = true; // If false, use percentage fee
    uint256 public percentageFee = 30; // 0.3% as fallback
    uint256 public constant FEE_DENOMINATOR = 10000;

    // Asset listing requirements
    uint256 public minMarketMakerCollateral = 100000e18; // 100,000 XUSD

    // Governance settings
    uint256 public proposalCount;
    uint256 public constant VOTING_PERIOD = 3 days;
    uint256 public constant MIN_XSHARES_TO_PROPOSE = 1000e18; // 1000 XShares

    struct Pool {
        address token;
        uint256 assetReserve;
        uint256 xusdReserve;
        uint256 totalShares;
        uint256 accumulatedFees;
        address marketMaker; // Who provided initial collateral
        uint256 marketMakerCollateral; // XUSD locked
        bool active;
    }

    struct LPPosition {
        uint256 shares;
        uint256 minPrice;
        uint256 maxPrice;
        uint256 feeDebt;
    }

    struct LimitOrder {
        address user;
        address token;
        bool isBuy;
        uint256 amount;
        uint256 priceLimit;
        bool executed;
    }

    struct Proposal {
        uint256 id;
        address proposer;
        ProposalType proposalType;
        uint256 newValue;
        address targetAddress; // For asset proposals
        uint256 startTime;
        uint256 endTime;
        uint256 votesFor;
        uint256 votesAgainst;
        bool executed;
        mapping(address => bool) hasVoted;
    }

    enum ProposalType {
        ChangeTradingFee,
        ChangeMinCollateral,
        ToggleFeeType,
        ApproveNewAsset
    }

    // State
    mapping(address => Pool) public pools;
    mapping(address => mapping(address => LPPosition)) public lpPositions;
    LimitOrder[] public limitOrders;
    mapping(address => uint256[]) public userOrders;
    mapping(uint256 => Proposal) public proposals;

    // XShares distribution tracking
    mapping(address => uint256) public xsharesBalance;
    uint256 public totalXShares;

    event PoolCreated(address indexed token, address indexed marketMaker, uint256 collateral);
    event LiquidityAdded(address indexed token, address indexed provider, uint256 shares);
    event LiquidityRemoved(address indexed token, address indexed provider, uint256 shares);
    event Trade(address indexed user, address indexed token, bool isBuy, uint256 assetAmount, uint256 xusdAmount, uint256 fee);
    event LimitOrderCreated(uint256 indexed orderId, address indexed user, address indexed token);
    event LimitOrderExecuted(uint256 indexed orderId);
    event FeesCollected(address indexed token, address indexed provider, uint256 amount);
    event ProposalCreated(uint256 indexed proposalId, ProposalType proposalType, address proposer);
    event Voted(uint256 indexed proposalId, address indexed voter, bool support, uint256 weight);
    event ProposalExecuted(uint256 indexed proposalId);
    event XSharesMinted(address indexed recipient, uint256 amount);

    constructor(address _xusd, address _xshares) {
        xusd = _xusd;
        xshares = _xshares;
    }

    // Create pool with minimum collateral requirement
    function createPool(
        address token,
        uint256 initialAsset,
        uint256 initialXUSD,
        uint256 minPrice,
        uint256 maxPrice
    ) external returns (uint256) {
        require(pools[token].token == address(0), "Pool exists");
        require(minPrice < maxPrice, "Invalid price range");
        require(initialXUSD >= minMarketMakerCollateral, "Insufficient collateral");

        // Transfer tokens
        IERC20(token).transferFrom(msg.sender, address(this), initialAsset);
        IERC20(xusd).transferFrom(msg.sender, address(this), initialXUSD);

        uint256 shares = sqrt(initialAsset * initialXUSD);

        pools[token] = Pool({
            token: token,
            assetReserve: initialAsset,
            xusdReserve: initialXUSD,
            totalShares: shares,
            accumulatedFees: 0,
            marketMaker: msg.sender,
            marketMakerCollateral: initialXUSD,
            active: true
        });

        lpPositions[token][msg.sender] = LPPosition({
            shares: shares,
            minPrice: minPrice,
            maxPrice: maxPrice,
            feeDebt: 0
        });

        // Mint XShares to market maker based on collateral
        uint256 xsharesToMint = initialXUSD / 100; // 1 XShare per 100 XUSD
        _mintXShares(msg.sender, xsharesToMint);

        emit PoolCreated(token, msg.sender, initialXUSD);
        emit LiquidityAdded(token, msg.sender, shares);

        return shares;
    }

    function addLiquidity(
        address token,
        uint256 assetAmount,
        uint256 xusdAmount,
        uint256 minPrice,
        uint256 maxPrice
    ) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.active, "Pool not active");
        require(minPrice < maxPrice, "Invalid price range");

        IERC20(token).transferFrom(msg.sender, address(this), assetAmount);
        IERC20(xusd).transferFrom(msg.sender, address(this), xusdAmount);

        uint256 shares;
        if (pool.totalShares == 0) {
            shares = sqrt(assetAmount * xusdAmount);
        } else {
            uint256 assetShare = (assetAmount * pool.totalShares) / pool.assetReserve;
            uint256 xusdShare = (xusdAmount * pool.totalShares) / pool.xusdReserve;
            shares = assetShare < xusdShare ? assetShare : xusdShare;
        }

        pool.assetReserve += assetAmount;
        pool.xusdReserve += xusdAmount;
        pool.totalShares += shares;

        LPPosition storage position = lpPositions[token][msg.sender];
        if (position.shares > 0) {
            position.shares += shares;
        } else {
            lpPositions[token][msg.sender] = LPPosition({
                shares: shares,
                minPrice: minPrice,
                maxPrice: maxPrice,
                feeDebt: 0
            });
        }

        // Mint XShares proportional to contribution
        uint256 xsharesToMint = xusdAmount / 100;
        _mintXShares(msg.sender, xsharesToMint);

        emit LiquidityAdded(token, msg.sender, shares);

        return shares;
    }

    function removeLiquidity(
        address token,
        uint256 shares
    ) external returns (uint256 assetAmount, uint256 xusdAmount, uint256 fees) {
        Pool storage pool = pools[token];
        require(pool.active, "Pool not active");

        LPPosition storage position = lpPositions[token][msg.sender];
        require(position.shares >= shares, "Insufficient shares");

        assetAmount = (shares * pool.assetReserve) / pool.totalShares;
        xusdAmount = (shares * pool.xusdReserve) / pool.totalShares;
        fees = (shares * pool.accumulatedFees) / pool.totalShares;

        pool.assetReserve -= assetAmount;
        pool.xusdReserve -= xusdAmount;
        pool.totalShares -= shares;
        pool.accumulatedFees -= fees;
        position.shares -= shares;

        IERC20(token).transfer(msg.sender, assetAmount);
        IERC20(xusd).transfer(msg.sender, xusdAmount + fees);

        emit LiquidityRemoved(token, msg.sender, shares);
        if (fees > 0) {
            emit FeesCollected(token, msg.sender, fees);
        }

        return (assetAmount, xusdAmount, fees);
    }

    function collectFees(address token) external returns (uint256 fees) {
        Pool storage pool = pools[token];
        require(pool.active, "Pool not active");

        LPPosition storage position = lpPositions[token][msg.sender];
        require(position.shares > 0, "No position");

        fees = (position.shares * pool.accumulatedFees) / pool.totalShares;

        if (fees > 0) {
            pool.accumulatedFees -= fees;
            IERC20(xusd).transfer(msg.sender, fees);
            emit FeesCollected(token, msg.sender, fees);
        }

        return fees;
    }

    function buy(address token, uint256 xusdAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.active, "Pool not active");

        // Calculate fee (constant 1 XUSD or percentage)
        uint256 fee = useConstantFee ? tradingFee : (xusdAmount * percentageFee) / FEE_DENOMINATOR;
        uint256 xusdAfterFee = xusdAmount - fee;

        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newXUSDReserve = pool.xusdReserve + xusdAfterFee;
        uint256 newAssetReserve = k / newXUSDReserve;
        uint256 assetAmount = pool.assetReserve - newAssetReserve;

        require(assetAmount > 0, "Insufficient liquidity");

        IERC20(xusd).transferFrom(msg.sender, address(this), xusdAmount);

        pool.xusdReserve = newXUSDReserve;
        pool.assetReserve = newAssetReserve;
        pool.accumulatedFees += fee;

        IERC20(token).transfer(msg.sender, assetAmount);

        emit Trade(msg.sender, token, true, assetAmount, xusdAmount, fee);

        // Execute pending limit orders in same block
        uint256 newPrice = getPrice(token);
        _executePendingLimitOrders(token, newPrice);

        return assetAmount;
    }

    function sell(address token, uint256 assetAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.active, "Pool not active");

        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newAssetReserve = pool.assetReserve + assetAmount;
        uint256 newXUSDReserve = k / newAssetReserve;
        uint256 xusdAmount = pool.xusdReserve - newXUSDReserve;

        require(xusdAmount > 0, "Insufficient liquidity");

        uint256 fee = useConstantFee ? tradingFee : (xusdAmount * percentageFee) / FEE_DENOMINATOR;
        uint256 xusdAfterFee = xusdAmount - fee;

        IERC20(token).transferFrom(msg.sender, address(this), assetAmount);

        pool.assetReserve = newAssetReserve;
        pool.xusdReserve = newXUSDReserve;
        pool.accumulatedFees += fee;

        IERC20(xusd).transfer(msg.sender, xusdAfterFee);

        emit Trade(msg.sender, token, false, assetAmount, xusdAfterFee, fee);

        uint256 newPrice = getPrice(token);
        _executePendingLimitOrders(token, newPrice);

        return xusdAfterFee;
    }

    function createLimitOrder(
        address token,
        bool isBuy,
        uint256 amount,
        uint256 priceLimit
    ) external returns (uint256) {
        require(pools[token].active, "Pool not active");

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

        emit LimitOrderCreated(orderId, msg.sender, token);

        return orderId;
    }

    // Execute all pending limit orders for a token when price is touched
    function _executePendingLimitOrders(address token, uint256 currentPrice) internal {
        uint256 len = limitOrders.length;

        for (uint256 i = 0; i < len; i++) {
            LimitOrder storage order = limitOrders[i];
            if (order.executed || order.token != token) continue;

            bool shouldExecute = false;
            if (order.isBuy && currentPrice <= order.priceLimit) {
                shouldExecute = true;
            } else if (!order.isBuy && currentPrice >= order.priceLimit) {
                shouldExecute = true;
            }

            if (shouldExecute) {
                order.executed = true;
                emit LimitOrderExecuted(i);
                // Note: Actual trade needs to be executed by order owner
                // This just marks it as executable
            }
        }
    }

    // Governance: Create proposal
    function createProposal(
        ProposalType proposalType,
        uint256 newValue,
        address targetAddress
    ) external returns (uint256) {
        require(xsharesBalance[msg.sender] >= MIN_XSHARES_TO_PROPOSE, "Insufficient XShares");

        uint256 proposalId = proposalCount++;
        Proposal storage proposal = proposals[proposalId];

        proposal.id = proposalId;
        proposal.proposer = msg.sender;
        proposal.proposalType = proposalType;
        proposal.newValue = newValue;
        proposal.targetAddress = targetAddress;
        proposal.startTime = block.timestamp;
        proposal.endTime = block.timestamp + VOTING_PERIOD;
        proposal.executed = false;

        emit ProposalCreated(proposalId, proposalType, msg.sender);

        return proposalId;
    }

    // Governance: Vote on proposal
    function vote(uint256 proposalId, bool support) external {
        Proposal storage proposal = proposals[proposalId];
        require(block.timestamp >= proposal.startTime, "Voting not started");
        require(block.timestamp <= proposal.endTime, "Voting ended");
        require(!proposal.hasVoted[msg.sender], "Already voted");

        uint256 votingPower = xsharesBalance[msg.sender];
        require(votingPower > 0, "No voting power");

        proposal.hasVoted[msg.sender] = true;

        if (support) {
            proposal.votesFor += votingPower;
        } else {
            proposal.votesAgainst += votingPower;
        }

        emit Voted(proposalId, msg.sender, support, votingPower);
    }

    // Governance: Execute proposal
    function executeProposal(uint256 proposalId) external {
        Proposal storage proposal = proposals[proposalId];
        require(block.timestamp > proposal.endTime, "Voting not ended");
        require(!proposal.executed, "Already executed");
        require(proposal.votesFor > proposal.votesAgainst, "Proposal rejected");

        proposal.executed = true;

        if (proposal.proposalType == ProposalType.ChangeTradingFee) {
            tradingFee = proposal.newValue;
        } else if (proposal.proposalType == ProposalType.ChangeMinCollateral) {
            minMarketMakerCollateral = proposal.newValue;
        } else if (proposal.proposalType == ProposalType.ToggleFeeType) {
            useConstantFee = !useConstantFee;
        } else if (proposal.proposalType == ProposalType.ApproveNewAsset) {
            // Asset approval logic
            pools[proposal.targetAddress].active = true;
        }

        emit ProposalExecuted(proposalId);
    }

    // Internal: Mint XShares governance tokens
    function _mintXShares(address recipient, uint256 amount) internal {
        xsharesBalance[recipient] += amount;
        totalXShares += amount;
        emit XSharesMinted(recipient, amount);
    }

    // View functions
    function getPrice(address token) public view returns (uint256) {
        Pool memory pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");
        require(pool.assetReserve > 0, "No reserves");
        return (pool.xusdReserve * 1e18) / pool.assetReserve;
    }

    function getReserves(address token) external view returns (uint256, uint256) {
        Pool memory pool = pools[token];
        return (pool.assetReserve, pool.xusdReserve);
    }

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

    function getPoolFees(address token) external view returns (uint256) {
        return pools[token].accumulatedFees;
    }

    function getProposal(uint256 proposalId) external view returns (
        address proposer,
        ProposalType proposalType,
        uint256 newValue,
        address targetAddress,
        uint256 startTime,
        uint256 endTime,
        uint256 votesFor,
        uint256 votesAgainst,
        bool executed
    ) {
        Proposal storage proposal = proposals[proposalId];
        return (
            proposal.proposer,
            proposal.proposalType,
            proposal.newValue,
            proposal.targetAddress,
            proposal.startTime,
            proposal.endTime,
            proposal.votesFor,
            proposal.votesAgainst,
            proposal.executed
        );
    }

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
