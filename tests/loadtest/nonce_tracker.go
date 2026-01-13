// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)

package loadtest

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// NonceTracker tracks nonces for multiple accounts to avoid nonce conflicts
type NonceTracker struct {
	nonces map[common.Address]uint64
	mutex  sync.RWMutex
}

// NewNonceTracker creates a new NonceTracker
func NewNonceTracker() *NonceTracker {
	return &NonceTracker{
		nonces: make(map[common.Address]uint64),
	}
}

// GetAndIncrementNonce returns the current nonce for an address and increments it
func (nt *NonceTracker) GetAndIncrementNonce(addr common.Address) uint64 {
	nt.mutex.Lock()
	defer nt.mutex.Unlock()

	nonce := nt.nonces[addr]
	nt.nonces[addr] = nonce + 1
	return nonce
}

// GetNonce returns the current nonce for an address without incrementing
func (nt *NonceTracker) GetNonce(addr common.Address) uint64 {
	nt.mutex.RLock()
	defer nt.mutex.RUnlock()

	return nt.nonces[addr]
}

// SetNonce sets the nonce for an address
func (nt *NonceTracker) SetNonce(addr common.Address, nonce uint64) {
	nt.mutex.Lock()
	defer nt.mutex.Unlock()

	nt.nonces[addr] = nonce
}

// Reset resets all nonces to 0
func (nt *NonceTracker) Reset() {
	nt.mutex.Lock()
	defer nt.mutex.Unlock()

	nt.nonces = make(map[common.Address]uint64)
}
