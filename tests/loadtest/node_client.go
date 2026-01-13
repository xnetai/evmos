// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/evmos/evmos/v20/x/evm/types"
)

// NodeClient wraps gRPC and RPC connections to a specific validator node
type NodeClient struct {
	ValidatorIndex int
	GRPCAddr       string
	RPCAddr        string
	APIAddr        string
	JSONRPCAddr    string

	// gRPC connection and clients
	grpcConn      *grpc.ClientConn
	evmClient     evmtypes.QueryClient
	bankClient    banktypes.QueryClient
	stakingClient stakingtypes.QueryClient

	// CometBFT RPC client for transaction broadcasting
	rpcClient *rpchttp.HTTP

	// Statistics
	stats *LocalNodeStats

	mutex sync.RWMutex
}

// NewNodeClient creates a new NodeClient for a specific validator
func NewNodeClient(validator *ValidatorProcess) (*NodeClient, error) {
	grpcAddr := fmt.Sprintf("localhost:%d", validator.GRPCPort)
	rpcAddr := fmt.Sprintf("http://localhost:%d", validator.RPCPort)
	apiAddr := fmt.Sprintf("http://localhost:%d", validator.APIPort)
	jsonRPCAddr := fmt.Sprintf("http://localhost:%d", validator.JSONRPCPort)

	// Create gRPC connection
	grpcConn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", grpcAddr, err)
	}

	// Create CometBFT RPC client
	rpcClient, err := rpchttp.New(rpcAddr, "/websocket")
	if err != nil {
		grpcConn.Close()
		return nil, fmt.Errorf("failed to create RPC client to %s: %w", rpcAddr, err)
	}

	// Start the RPC client
	if err := rpcClient.Start(); err != nil {
		grpcConn.Close()
		return nil, fmt.Errorf("failed to start RPC client: %w", err)
	}

	client := &NodeClient{
		ValidatorIndex: validator.Index,
		GRPCAddr:       grpcAddr,
		RPCAddr:        rpcAddr,
		APIAddr:        apiAddr,
		JSONRPCAddr:    jsonRPCAddr,
		grpcConn:       grpcConn,
		evmClient:      evmtypes.NewQueryClient(grpcConn),
		bankClient:     banktypes.NewQueryClient(grpcConn),
		stakingClient:  stakingtypes.NewQueryClient(grpcConn),
		rpcClient:      rpcClient,
		stats:          NewLocalNodeStats(validator.Index),
	}

	return client, nil
}

// SubmitTx submits a transaction to this specific node via RPC
func (nc *NodeClient) SubmitTx(txBytes []byte) (*abcitypes.ExecTxResult, error) {
	startTime := time.Now()

	// Create CometBFT transaction
	tx := cmttypes.Tx(txBytes)

	// Broadcast transaction asynchronously (don't wait for block inclusion)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := nc.rpcClient.BroadcastTxAsync(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast tx to node %d: %w", nc.ValidatorIndex, err)
	}

	// BroadcastTxAsync just returns the hash, transaction will be processed later
	// Return success with Code 0
	execResult := &abcitypes.ExecTxResult{
		Code: 0,
		Data: nil,
		Log:  fmt.Sprintf("tx broadcasted with hash: %s", result.Hash.String()),
	}

	// Calculate latency
	_ = time.Since(startTime)

	return execResult, nil
}

// GetBlockHeight queries the current block height from this node
func (nc *NodeClient) GetBlockHeight() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := nc.rpcClient.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get status from node %d: %w", nc.ValidatorIndex, err)
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// GetBlock queries a specific block from this node
func (nc *NodeClient) GetBlock(height int64) (*coretypes.ResultBlock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	block, err := nc.rpcClient.Block(ctx, &height)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d from node %d: %w", height, nc.ValidatorIndex, err)
	}

	return block, nil
}

// QueryTx queries a transaction by hash from this node
func (nc *NodeClient) QueryTx(txHash []byte) (*coretypes.ResultTx, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := nc.rpcClient.Tx(ctx, txHash, false)
	if err != nil {
		return nil, fmt.Errorf("failed to query tx from node %d: %w", nc.ValidatorIndex, err)
	}

	return result, nil
}

// WaitForBlockHeight waits until this node reaches or exceeds the specified block height
func (nc *NodeClient) WaitForBlockHeight(targetHeight int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for node %d to reach height %d", nc.ValidatorIndex, targetHeight)
		case <-ticker.C:
			height, err := nc.GetBlockHeight()
			if err != nil {
				continue // Retry on error
			}
			if height >= targetHeight {
				return nil
			}
		}
	}
}

// GetStats returns the statistics for this node
func (nc *NodeClient) GetStats() *LocalNodeStats {
	nc.mutex.RLock()
	defer nc.mutex.RUnlock()
	return nc.stats
}

// Close closes the gRPC and RPC connections
func (nc *NodeClient) Close() error {
	var errs []error

	if nc.rpcClient != nil {
		if err := nc.rpcClient.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop RPC client: %w", err))
		}
	}

	if nc.grpcConn != nil {
		if err := nc.grpcConn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close gRPC connection: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing node client %d: %v", nc.ValidatorIndex, errs)
	}

	return nil
}

// IsHealthy checks if the node is healthy and responding
func (nc *NodeClient) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := nc.rpcClient.Status(ctx)
	return err == nil
}

// RoundRobinDistributor distributes transactions across nodes in round-robin fashion
type RoundRobinDistributor struct {
	currentNode uint32
	nodeCount   uint32
	mutex       sync.Mutex
}

// NewRoundRobinDistributor creates a new round-robin distributor
func NewRoundRobinDistributor(nodeCount int) *RoundRobinDistributor {
	return &RoundRobinDistributor{
		currentNode: 0,
		nodeCount:   uint32(nodeCount),
	}
}

// GetNextNode returns the index of the next node in round-robin order
func (r *RoundRobinDistributor) GetNextNode() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	node := r.currentNode % r.nodeCount
	r.currentNode++

	return int(node)
}

// Reset resets the distributor to start from node 0
func (r *RoundRobinDistributor) Reset() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.currentNode = 0
}
