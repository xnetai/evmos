// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function transfer(address to, uint256 amount) external returns (bool);
}

contract SimpleOrderbook {
    address public xusd;

    struct Pool {
        address token;
        uint256 assetReserve;
        uint256 xusdReserve;
    }

    // Use address as key instead of string to avoid complex encoding
    mapping(address => Pool) public pools;

    constructor(address _xusd) {
        xusd = _xusd;
    }

    function addPool(
        address token,
        uint256 initialAsset,
        uint256 initialXUSD
    ) external {
        require(pools[token].token == address(0), "Pool exists");

        // Transfer tokens to this contract
        IERC20(token).transferFrom(msg.sender, address(this), initialAsset);
        IERC20(xusd).transferFrom(msg.sender, address(this), initialXUSD);

        pools[token] = Pool({
            token: token,
            assetReserve: initialAsset,
            xusdReserve: initialXUSD
        });
    }

    function buy(address token, uint256 xusdAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        // Calculate asset amount using constant product formula
        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newXUSDReserve = pool.xusdReserve + xusdAmount;
        uint256 newAssetReserve = k / newXUSDReserve;
        uint256 assetAmount = pool.assetReserve - newAssetReserve;

        // Transfer XUSD from buyer
        IERC20(xusd).transferFrom(msg.sender, address(this), xusdAmount);

        // Update reserves
        pool.xusdReserve = newXUSDReserve;
        pool.assetReserve = newAssetReserve;

        // Transfer asset to buyer
        IERC20(token).transfer(msg.sender, assetAmount);

        return assetAmount;
    }

    function sell(address token, uint256 assetAmount) external returns (uint256) {
        Pool storage pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");

        // Calculate XUSD amount
        uint256 k = pool.assetReserve * pool.xusdReserve;
        uint256 newAssetReserve = pool.assetReserve + assetAmount;
        uint256 newXUSDReserve = k / newAssetReserve;
        uint256 xusdAmount = pool.xusdReserve - newXUSDReserve;

        // Transfer asset from seller
        IERC20(token).transferFrom(msg.sender, address(this), assetAmount);

        // Update reserves
        pool.assetReserve = newAssetReserve;
        pool.xusdReserve = newXUSDReserve;

        // Transfer XUSD to seller
        IERC20(xusd).transfer(msg.sender, xusdAmount);

        return xusdAmount;
    }

    function getPrice(address token) external view returns (uint256) {
        Pool memory pool = pools[token];
        require(pool.token != address(0), "Pool does not exist");
        require(pool.assetReserve > 0, "No reserves");
        return (pool.xusdReserve * 1e18) / pool.assetReserve;
    }

    function getReserves(address token) external view returns (uint256, uint256) {
        Pool memory pool = pools[token];
        return (pool.assetReserve, pool.xusdReserve);
    }
}
