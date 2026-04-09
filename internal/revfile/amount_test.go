package revfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressAmountKnownValues(t *testing.T) {
	tests := []struct {
		input    uint64
		expected uint64
	}{
		{0, 0},
		{1, 1},
		{100000000, 9},            // 1 BTC = 10^8 satoshis
		{5000000000, 50},          // 50 BTC - standard block reward
		{2100000000000000, 21000000}, // Max supply
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := CompressAmount(tt.input)
			assert.Equal(t, tt.expected, got, "CompressAmount(%d) = %d, want %d", tt.input, got, tt.expected)
		})
	}
}

func TestAmountRoundTrip(t *testing.T) {
	for n := uint64(0); n <= 100000; n++ {
		compressed := CompressAmount(n)
		decompressed := DecompressAmount(compressed)
		require.Equal(t, n, decompressed, "round-trip failed for %d: compress=%d, decompress=%d", n, compressed, decompressed)
	}
}

func TestAmountRoundTripLarge(t *testing.T) {
	// Test multiples of powers of 10 up to 21M BTC
	testValues := []uint64{
		1,
		10,
		100,
		1000,
		10000,
		100000,
		1000000,
		10000000,
		100000000,
		1000000000,
		10000000000,
		100000000000,
		1000000000000,
		10000000000000,
		100000000000000,
		1000000000000000,
		2100000000000000, // Max supply
	}

	for _, val := range testValues {
		compressed := CompressAmount(val)
		decompressed := DecompressAmount(compressed)
		assert.Equal(t, val, decompressed, "round-trip failed for %d: compress=%d, decompress=%d", val, compressed, decompressed)
	}
}

func TestAmountMaxSupply(t *testing.T) {
	maxSupply := uint64(2100000000000000)
	compressed := CompressAmount(maxSupply)
	decompressed := DecompressAmount(compressed)
	require.Equal(t, maxSupply, decompressed, "max supply round-trip failed: compress=%d, decompress=%d", compressed, decompressed)
}
