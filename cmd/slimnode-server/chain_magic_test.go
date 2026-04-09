package main

import (
	"testing"
)

func TestChainToNetworkMagic(t *testing.T) {
	tests := []struct {
		chain       string
		expectedMag uint32
		expectErr   bool
	}{
		// Regression: existing chains
		{"mainnet", 0xD9B4BEF9, false},
		{"testnet3", 0x0709110B, false},
		{"testnet", 0x0709110B, false},
		{"signet", 0x40CF030A, false},
		// New: regtest and testnet4
		{"regtest", 0xDAB5BFFA, false},
		{"testnet4", 0x283F161C, false},
		// Error case: unknown chain
		{"unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.chain, func(t *testing.T) {
			magic, err := chainToNetworkMagic(tt.chain)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for chain %q, got nil", tt.chain)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for chain %q: %v", tt.chain, err)
			}
			if magic != tt.expectedMag {
				t.Errorf("chain %q: expected magic 0x%08X, got 0x%08X", tt.chain, tt.expectedMag, magic)
			}
		})
	}
}
