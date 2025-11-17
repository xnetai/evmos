// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transferFrom(address sender, address recipient, uint256 amount) external returns (bool);
    function transfer(address recipient, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
    function approve(address spender, uint256 amount) external returns (bool);
}

/**
 * @title AssetOrderbook
 * @dev Orderbook for trading synthetic assets (TSLA, AAPL, GOOGLE) against XUSD
 * Price is calculated from the ratio of asset/xusd reserves
 */
contract AssetOrderbook {
    IERC20 public xusd;

    struct Asset {
        string symbol;
        uint256 reserveAsset;  // Asset token reserves
        uint256 reserveXUSD;   // XUSD reserves
        address tokenAddress;  // ERC20 token address for the asset
    }

    mapping(string => Asset) public assets;
    string[] public assetSymbols;

    event AssetAdded(string symbol, address tokenAddress, uint256 initialAssetReserve, uint256 initialXUSDReserve);
    event Buy(address indexed buyer, string symbol, uint256 xusdAmount, uint256 assetAmount, uint256 newPrice);
    event Sell(address indexed seller, string symbol, uint256 assetAmount, uint256 xusdAmount, uint256 newPrice);

    constructor(address _xusdAddress) {
        xusd = IERC20(_xusdAddress);
    }

    /**
     * @dev Add a new asset to the orderbook with initial liquidity
     */
    function addAsset(
        string memory symbol,
        address tokenAddress,
        uint256 initialAssetReserve,
        uint256 initialXUSDReserve
    ) external {
        require(assets[symbol].tokenAddress == address(0), "Asset already exists");
        require(tokenAddress != address(0), "Invalid token address");
        require(initialAssetReserve > 0 && initialXUSDReserve > 0, "Initial reserves must be > 0");

        // Transfer initial reserves from sender
        IERC20(tokenAddress).transferFrom(msg.sender, address(this), initialAssetReserve);
        xusd.transferFrom(msg.sender, address(this), initialXUSDReserve);

        assets[symbol] = Asset({
            symbol: symbol,
            reserveAsset: initialAssetReserve,
            reserveXUSD: initialXUSDReserve,
            tokenAddress: tokenAddress
        });

        assetSymbols.push(symbol);

        emit AssetAdded(symbol, tokenAddress, initialAssetReserve, initialXUSDReserve);
    }

    /**
     * @dev Get current price of asset in XUSD (how much XUSD per 1 asset token)
     */
    function getPrice(string memory symbol) public view returns (uint256) {
        Asset memory asset = assets[symbol];
        require(asset.tokenAddress != address(0), "Asset does not exist");
        require(asset.reserveAsset > 0, "No asset reserves");

        // Price = reserveXUSD / reserveAsset (in wei precision)
        return (asset.reserveXUSD * 1e18) / asset.reserveAsset;
    }

    /**
     * @dev Buy asset tokens with XUSD
     * Using constant product formula: reserveAsset * reserveXUSD = k
     */
    function buy(string memory symbol, uint256 xusdAmount) external returns (uint256) {
        Asset storage asset = assets[symbol];
        require(asset.tokenAddress != address(0), "Asset does not exist");
        require(xusdAmount > 0, "XUSD amount must be > 0");

        // Calculate asset amount out using constant product formula
        // assetOut = reserveAsset - (k / (reserveXUSD + xusdAmount))
        uint256 k = asset.reserveAsset * asset.reserveXUSD;
        uint256 newReserveXUSD = asset.reserveXUSD + xusdAmount;
        uint256 newReserveAsset = k / newReserveXUSD;
        uint256 assetAmount = asset.reserveAsset - newReserveAsset;

        require(assetAmount > 0, "Insufficient liquidity");
        require(assetAmount <= asset.reserveAsset, "Insufficient asset reserves");

        // Transfer XUSD from buyer
        xusd.transferFrom(msg.sender, address(this), xusdAmount);

        // Update reserves
        asset.reserveXUSD = newReserveXUSD;
        asset.reserveAsset = newReserveAsset;

        // Transfer asset to buyer
        IERC20(asset.tokenAddress).transfer(msg.sender, assetAmount);

        uint256 newPrice = getPrice(symbol);
        emit Buy(msg.sender, symbol, xusdAmount, assetAmount, newPrice);

        return assetAmount;
    }

    /**
     * @dev Sell asset tokens for XUSD
     * Using constant product formula: reserveAsset * reserveXUSD = k
     */
    function sell(string memory symbol, uint256 assetAmount) external returns (uint256) {
        Asset storage asset = assets[symbol];
        require(asset.tokenAddress != address(0), "Asset does not exist");
        require(assetAmount > 0, "Asset amount must be > 0");

        // Calculate XUSD amount out using constant product formula
        // xusdOut = reserveXUSD - (k / (reserveAsset + assetAmount))
        uint256 k = asset.reserveAsset * asset.reserveXUSD;
        uint256 newReserveAsset = asset.reserveAsset + assetAmount;
        uint256 newReserveXUSD = k / newReserveAsset;
        uint256 xusdAmount = asset.reserveXUSD - newReserveXUSD;

        require(xusdAmount > 0, "Insufficient liquidity");
        require(xusdAmount <= asset.reserveXUSD, "Insufficient XUSD reserves");

        // Transfer asset from seller
        IERC20(asset.tokenAddress).transferFrom(msg.sender, address(this), assetAmount);

        // Update reserves
        asset.reserveAsset = newReserveAsset;
        asset.reserveXUSD = newReserveXUSD;

        // Transfer XUSD to seller
        xusd.transfer(msg.sender, xusdAmount);

        uint256 newPrice = getPrice(symbol);
        emit Sell(msg.sender, symbol, assetAmount, xusdAmount, newPrice);

        return xusdAmount;
    }

    /**
     * @dev Get asset reserves
     */
    function getReserves(string memory symbol) external view returns (uint256 assetReserve, uint256 xusdReserve) {
        Asset memory asset = assets[symbol];
        require(asset.tokenAddress != address(0), "Asset does not exist");
        return (asset.reserveAsset, asset.reserveXUSD);
    }

    /**
     * @dev Get number of assets
     */
    function getAssetCount() external view returns (uint256) {
        return assetSymbols.length;
    }
}
