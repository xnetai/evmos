// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// TxType represents the type of transaction
type TxType string

const (
	TxTypeContract TxType = "contract"
	TxTypeBank     TxType = "bank"
	TxTypeStaking  TxType = "staking"
	TxTypeRawEVM   TxType = "raw_evm"
)

// TxMetadata contains metadata about a submitted transaction
type TxMetadata struct {
	TxHash      string
	Type        TxType
	UserIndex   int
	NodeIndex   int
	SubmitTime  time.Time
	BlockHeight int64
	GasUsed     uint64
	GasWanted   uint64
	Success     bool
	Error       string
	Latency     time.Duration
}

// TxTypeStats tracks statistics for a specific transaction type
type TxTypeStats struct {
	Type           TxType
	Count          atomic.Uint64
	SuccessCount   atomic.Uint64
	ErrorCount     atomic.Uint64
	TotalGasUsed   atomic.Uint64
	TotalGasWanted atomic.Uint64

	// Error breakdown (requires mutex)
	Errors map[string]int
	mutex  sync.RWMutex
}

// NewTxTypeStats creates a new TxTypeStats instance
func NewTxTypeStats(txType TxType) *TxTypeStats {
	return &TxTypeStats{
		Type:   txType,
		Errors: make(map[string]int),
	}
}

// RecordError records an error occurrence (thread-safe)
func (s *TxTypeStats) RecordError(errMsg string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.Errors[errMsg]++
}

// GetErrorBreakdown returns a copy of the error breakdown (thread-safe)
func (s *TxTypeStats) GetErrorBreakdown() map[string]int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]int, len(s.Errors))
	for k, v := range s.Errors {
		result[k] = v
	}
	return result
}

// GetAvgGasUsed returns the average gas used per transaction
func (s *TxTypeStats) GetAvgGasUsed() uint64 {
	count := s.Count.Load()
	if count == 0 {
		return 0
	}
	return s.TotalGasUsed.Load() / count
}

// GetSuccessRate returns the success rate as a percentage
func (s *TxTypeStats) GetSuccessRate() float64 {
	count := s.Count.Load()
	if count == 0 {
		return 0
	}
	return float64(s.SuccessCount.Load()) / float64(count) * 100
}

// LocalNodeStats tracks statistics for a specific node
type LocalNodeStats struct {
	ValidatorIndex int

	// Per transaction type stats
	TypeStats map[TxType]*TxTypeStats

	// Latency tracking
	TotalLatency time.Duration
	MinLatency   time.Duration
	MaxLatency   time.Duration
	latencyCount atomic.Uint64

	// Block distribution (requires mutex)
	BlockHeights map[int64]int // block height -> tx count

	mutex sync.RWMutex
}

// NewLocalNodeStats creates a new LocalNodeStats instance
func NewLocalNodeStats(validatorIndex int) *LocalNodeStats {
	return &LocalNodeStats{
		ValidatorIndex: validatorIndex,
		TypeStats: map[TxType]*TxTypeStats{
			TxTypeContract: NewTxTypeStats(TxTypeContract),
			TxTypeBank:     NewTxTypeStats(TxTypeBank),
			TxTypeStaking:  NewTxTypeStats(TxTypeStaking),
			TxTypeRawEVM:   NewTxTypeStats(TxTypeRawEVM),
		},
		BlockHeights: make(map[int64]int),
		MinLatency:   time.Duration(1<<63 - 1), // Max duration
	}
}

// RecordTransaction records a transaction in the node statistics
func (s *LocalNodeStats) RecordTransaction(metadata TxMetadata) {
	typeStats := s.TypeStats[metadata.Type]

	// Update type-specific stats
	typeStats.Count.Add(1)
	if metadata.Success {
		typeStats.SuccessCount.Add(1)
	} else {
		typeStats.ErrorCount.Add(1)
		if metadata.Error != "" {
			typeStats.RecordError(metadata.Error)
		}
	}
	typeStats.TotalGasUsed.Add(metadata.GasUsed)
	typeStats.TotalGasWanted.Add(metadata.GasWanted)

	// Update latency stats (requires mutex for min/max)
	s.mutex.Lock()
	s.TotalLatency += metadata.Latency
	if metadata.Latency < s.MinLatency {
		s.MinLatency = metadata.Latency
	}
	if metadata.Latency > s.MaxLatency {
		s.MaxLatency = metadata.Latency
	}
	s.latencyCount.Add(1)

	// Update block distribution
	if metadata.BlockHeight > 0 {
		s.BlockHeights[metadata.BlockHeight]++
	}
	s.mutex.Unlock()
}

// GetTotalTxCount returns total transaction count for this node
func (s *LocalNodeStats) GetTotalTxCount() uint64 {
	var total uint64
	for _, typeStats := range s.TypeStats {
		total += typeStats.Count.Load()
	}
	return total
}

// GetAvgLatency returns the average latency
func (s *LocalNodeStats) GetAvgLatency() time.Duration {
	count := s.latencyCount.Load()
	if count == 0 {
		return 0
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.TotalLatency / time.Duration(count)
}

// GetBlockDistribution returns a copy of the block distribution
func (s *LocalNodeStats) GetBlockDistribution() map[int64]int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[int64]int, len(s.BlockHeights))
	for k, v := range s.BlockHeights {
		result[k] = v
	}
	return result
}

// LocalLoadTestStats tracks comprehensive statistics across all nodes
type LocalLoadTestStats struct {
	StartTime         time.Time
	TotalTransactions atomic.Uint64
	TotalSuccess      atomic.Uint64
	TotalErrors       atomic.Uint64

	// Per-node statistics
	NodeStats map[int]*LocalNodeStats

	mutex sync.RWMutex
}

// NewLocalLoadTestStats creates a new LocalLoadTestStats instance
func NewLocalLoadTestStats(numNodes int) *LocalLoadTestStats {
	stats := &LocalLoadTestStats{
		StartTime: time.Now(),
		NodeStats: make(map[int]*LocalNodeStats, numNodes),
	}

	for i := 0; i < numNodes; i++ {
		stats.NodeStats[i] = NewLocalNodeStats(i)
	}

	return stats
}

// RecordTransaction records a transaction in the global and node-specific statistics
func (s *LocalLoadTestStats) RecordTransaction(nodeIdx int, txType TxType, metadata TxMetadata) {
	s.TotalTransactions.Add(1)
	if metadata.Success {
		s.TotalSuccess.Add(1)
	} else {
		s.TotalErrors.Add(1)
	}

	// Record in node-specific stats
	if nodeStats, ok := s.NodeStats[nodeIdx]; ok {
		nodeStats.RecordTransaction(metadata)
	}
}

// GetElapsedTime returns the elapsed time since test start
func (s *LocalLoadTestStats) GetElapsedTime() time.Duration {
	return time.Since(s.StartTime)
}

// GetThroughput returns the transactions per second
func (s *LocalLoadTestStats) GetThroughput() float64 {
	elapsed := s.GetElapsedTime().Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(s.TotalTransactions.Load()) / elapsed
}

// PrintPerNodeSummary prints a summary table of per-node statistics
func (s *LocalLoadTestStats) PrintPerNodeSummary() string {
	var output string

	output += "\n╔════════════════════════════════════════════════════════════════════════════════════╗\n"
	output += "║                              PER-NODE STATISTICS                                  ║\n"
	output += "╠═════════╤═════════╤══════════╤═══════════╤══════════╤═════════════╤═══════════════╣\n"
	output += "║ Node    │ Total   │ Contract │ Bank      │ Staking  │ Raw EVM     │ Success Rate  ║\n"
	output += "║ Index   │ Txs     │ Calls    │ Transfers │ Ops      │ Txs         │               ║\n"
	output += "╠═════════╪═════════╪══════════╪═══════════╪══════════╪═════════════╪═══════════════╣\n"

	totalTxs := s.TotalTransactions.Load()

	// Get sorted node indices
	nodeIndices := make([]int, 0, len(s.NodeStats))
	for idx := range s.NodeStats {
		nodeIndices = append(nodeIndices, idx)
	}
	sort.Ints(nodeIndices)

	var globalContract, globalBank, globalStaking, globalRawEVM uint64

	for _, idx := range nodeIndices {
		nodeStats := s.NodeStats[idx]
		nodeTotalTxs := nodeStats.GetTotalTxCount()
		nodePercentage := float64(0)
		if totalTxs > 0 {
			nodePercentage = float64(nodeTotalTxs) / float64(totalTxs) * 100
		}

		contractCount := nodeStats.TypeStats[TxTypeContract].Count.Load()
		bankCount := nodeStats.TypeStats[TxTypeBank].Count.Load()
		stakingCount := nodeStats.TypeStats[TxTypeStaking].Count.Load()
		rawEVMCount := nodeStats.TypeStats[TxTypeRawEVM].Count.Load()

		globalContract += contractCount
		globalBank += bankCount
		globalStaking += stakingCount
		globalRawEVM += rawEVMCount

		contractPct := float64(0)
		bankPct := float64(0)
		stakingPct := float64(0)
		rawEVMPct := float64(0)

		if nodeTotalTxs > 0 {
			contractPct = float64(contractCount) / float64(nodeTotalTxs) * 100
			bankPct = float64(bankCount) / float64(nodeTotalTxs) * 100
			stakingPct = float64(stakingCount) / float64(nodeTotalTxs) * 100
			rawEVMPct = float64(rawEVMCount) / float64(nodeTotalTxs) * 100
		}

		successRate := float64(0)
		if nodeTotalTxs > 0 {
			successCount := uint64(0)
			for _, typeStats := range nodeStats.TypeStats {
				successCount += typeStats.SuccessCount.Load()
			}
			successRate = float64(successCount) / float64(nodeTotalTxs) * 100
		}

		output += fmt.Sprintf("║ %-7d │ %-7d │ %-8d │ %-9d │ %-8d │ %-11d │ %-13.1f ║\n",
			idx,
			nodeTotalTxs,
			contractCount,
			bankCount,
			stakingCount,
			rawEVMCount,
			successRate,
		)
		output += fmt.Sprintf("║         │ (%.1f%%) │ (%.1f%%)  │ (%.1f%%)   │ (%.1f%%)  │ (%.1f%%)     │               ║\n",
			nodePercentage,
			contractPct,
			bankPct,
			stakingPct,
			rawEVMPct,
		)
		if idx < len(nodeIndices)-1 {
			output += "╠═════════╪═════════╪══════════╪═══════════╪══════════╪═════════════╪═══════════════╣\n"
		}
	}

	output += "╠═════════╪═════════╪══════════╪═══════════╪══════════╪═════════════╪═══════════════╣\n"
	output += fmt.Sprintf("║ TOTAL   │ %-7d │ %-8d │ %-9d │ %-8d │ %-11d │ %-13.1f ║\n",
		totalTxs,
		globalContract,
		globalBank,
		globalStaking,
		globalRawEVM,
		float64(s.TotalSuccess.Load())/float64(totalTxs)*100,
	)
	output += "╚═════════╧═════════╧══════════╧═══════════╧══════════╧═════════════╧═══════════════╝\n"

	return output
}

// PrintDetailedNodeStats prints detailed statistics for a specific node
func (s *LocalLoadTestStats) PrintDetailedNodeStats(nodeIdx int) string {
	nodeStats, ok := s.NodeStats[nodeIdx]
	if !ok {
		return fmt.Sprintf("Node %d not found\n", nodeIdx)
	}

	var output string

	output += fmt.Sprintf("\n╔════════════════════════════════════════════════════════════════════════════════════╗\n")
	output += fmt.Sprintf("║                    NODE %d DETAILED STATISTICS                                     ║\n", nodeIdx)
	output += "╠═══════════════════╤════════╤═════════╤══════════╤═════════════════╤══════════════╣\n"
	output += "║ Transaction Type  │ Count  │ Success │ Errors   │ Avg Gas Used    │ Success Rate ║\n"
	output += "╠═══════════════════╪════════╪═════════╪══════════╪═════════════════╪══════════════╣\n"

	txTypes := []TxType{TxTypeContract, TxTypeBank, TxTypeStaking, TxTypeRawEVM}
	txTypeNames := map[TxType]string{
		TxTypeContract: "Contract Calls",
		TxTypeBank:     "Bank Transfers",
		TxTypeStaking:  "Staking Ops",
		TxTypeRawEVM:   "Raw EVM Txs",
	}

	var totalCount, totalSuccess, totalErrors uint64
	var totalGasUsed uint64

	for _, txType := range txTypes {
		typeStats := nodeStats.TypeStats[txType]
		count := typeStats.Count.Load()
		success := typeStats.SuccessCount.Load()
		errors := typeStats.ErrorCount.Load()
		avgGas := typeStats.GetAvgGasUsed()
		successRate := typeStats.GetSuccessRate()

		totalCount += count
		totalSuccess += success
		totalErrors += errors
		totalGasUsed += typeStats.TotalGasUsed.Load()

		output += fmt.Sprintf("║ %-17s │ %-6d │ %-7d │ %-8d │ %-15s │ %-12.1f ║\n",
			txTypeNames[txType],
			count,
			success,
			errors,
			formatNumber(avgGas),
			successRate,
		)
	}

	avgGasTotal := uint64(0)
	if totalCount > 0 {
		avgGasTotal = totalGasUsed / totalCount
	}
	totalSuccessRate := float64(0)
	if totalCount > 0 {
		totalSuccessRate = float64(totalSuccess) / float64(totalCount) * 100
	}

	output += "╠═══════════════════╪════════╪═════════╪══════════╪═════════════════╪══════════════╣\n"
	output += fmt.Sprintf("║ %-17s │ %-6d │ %-7d │ %-8d │ %-15s │ %-12.1f ║\n",
		"TOTAL",
		totalCount,
		totalSuccess,
		totalErrors,
		formatNumber(avgGasTotal),
		totalSuccessRate,
	)
	output += "╚═══════════════════╧════════╧═════════╧══════════╧═════════════════╧══════════════╝\n"

	// Latency statistics
	avgLatency := nodeStats.GetAvgLatency()
	output += fmt.Sprintf("\nLatency Statistics:\n")
	output += fmt.Sprintf("  Min: %v | Max: %v | Avg: %v\n",
		nodeStats.MinLatency.Round(time.Millisecond),
		nodeStats.MaxLatency.Round(time.Millisecond),
		avgLatency.Round(time.Millisecond),
	)

	// Block distribution (top 10 blocks)
	blockDist := nodeStats.GetBlockDistribution()
	if len(blockDist) > 0 {
		output += fmt.Sprintf("\nBlock Distribution (top 10 blocks):\n")

		type blockCount struct {
			height int64
			count  int
		}
		blocks := make([]blockCount, 0, len(blockDist))
		for height, count := range blockDist {
			blocks = append(blocks, blockCount{height, count})
		}
		sort.Slice(blocks, func(i, j int) bool {
			return blocks[i].height < blocks[j].height
		})

		displayCount := len(blocks)
		if displayCount > 10 {
			displayCount = 10
		}
		for i := 0; i < displayCount; i++ {
			output += fmt.Sprintf("  Block %d: %d txs\n", blocks[i].height, blocks[i].count)
		}
		if len(blocks) > 10 {
			output += fmt.Sprintf("  ... and %d more blocks\n", len(blocks)-10)
		}
	}

	return output
}

// PrintErrorBreakdown prints error breakdown across all nodes
func (s *LocalLoadTestStats) PrintErrorBreakdown() string {
	var output string

	// Collect all errors across all nodes
	type errorInfo struct {
		message string
		count   int
		nodes   []int
	}

	errorMap := make(map[string]*errorInfo)

	for nodeIdx, nodeStats := range s.NodeStats {
		for _, typeStats := range nodeStats.TypeStats {
			errors := typeStats.GetErrorBreakdown()
			for errMsg, count := range errors {
				if ei, ok := errorMap[errMsg]; ok {
					ei.count += count
					ei.nodes = append(ei.nodes, nodeIdx)
				} else {
					errorMap[errMsg] = &errorInfo{
						message: errMsg,
						count:   count,
						nodes:   []int{nodeIdx},
					}
				}
			}
		}
	}

	if len(errorMap) == 0 {
		return "\nNo errors recorded.\n"
	}

	output += "\n╔════════════════════════════════════════════════════════════════════════════════════╗\n"
	output += "║                                ERROR BREAKDOWN                                     ║\n"
	output += "╠═══════════════════════════════════════════╤════════╤═══════════════════════════════╣\n"
	output += "║ Error Message                             │ Count  │ Nodes Affected                ║\n"
	output += "╠═══════════════════════════════════════════╪════════╪═══════════════════════════════╣\n"

	// Sort errors by count (descending)
	errors := make([]*errorInfo, 0, len(errorMap))
	for _, ei := range errorMap {
		errors = append(errors, ei)
	}
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].count > errors[j].count
	})

	for _, ei := range errors {
		truncatedMsg := ei.message
		if len(truncatedMsg) > 41 {
			truncatedMsg = truncatedMsg[:38] + "..."
		}

		nodeStr := fmt.Sprintf("%v", ei.nodes)
		if len(nodeStr) > 29 {
			nodeStr = nodeStr[:26] + "..."
		}

		output += fmt.Sprintf("║ %-41s │ %-6d │ %-29s ║\n",
			truncatedMsg,
			ei.count,
			nodeStr,
		)
	}

	output += "╚═══════════════════════════════════════════╧════════╧═══════════════════════════════╝\n"

	return output
}

// PrintSummary prints a comprehensive summary of the load test
func (s *LocalLoadTestStats) PrintSummary() string {
	var output string

	elapsed := s.GetElapsedTime()
	throughput := s.GetThroughput()

	output += "\n╔════════════════════════════════════════════════════════════════════════════════════╗\n"
	output += "║                          LOAD TEST SUMMARY                                         ║\n"
	output += "╚════════════════════════════════════════════════════════════════════════════════════╝\n"

	output += fmt.Sprintf("\nTotal Transactions: %d\n", s.TotalTransactions.Load())
	output += fmt.Sprintf("Successful: %d (%.1f%%)\n",
		s.TotalSuccess.Load(),
		float64(s.TotalSuccess.Load())/float64(s.TotalTransactions.Load())*100,
	)
	output += fmt.Sprintf("Failed: %d (%.1f%%)\n",
		s.TotalErrors.Load(),
		float64(s.TotalErrors.Load())/float64(s.TotalTransactions.Load())*100,
	)
	output += fmt.Sprintf("\nElapsed Time: %v\n", elapsed.Round(time.Millisecond))
	output += fmt.Sprintf("Throughput: %.2f txs/sec\n", throughput)

	return output
}

// formatNumber formats a number with thousand separators
func formatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	// Add commas every 3 digits from the right
	var result string
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}
