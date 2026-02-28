package revfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompactSizeKnownValues(t *testing.T) {
	tests := []struct {
		value    uint64
		expected []byte
	}{
		{0, []byte{0x00}},
		{252, []byte{0xFC}},
		{253, []byte{0xFD, 0xFD, 0x00}},
		{255, []byte{0xFD, 0xFF, 0x00}},
		{65535, []byte{0xFD, 0xFF, 0xFF}},
		{65536, []byte{0xFE, 0x00, 0x00, 0x01, 0x00}},
		{0x100000000, []byte{0xFF, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
	}

	for _, tt := range tests {
		t.Run("encode", func(t *testing.T) {
			buf := new(bytes.Buffer)
			err := WriteCompactSize(buf, tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, buf.Bytes())
		})

		t.Run("decode", func(t *testing.T) {
			buf := bytes.NewReader(tt.expected)
			got, err := ReadCompactSize(buf)
			require.NoError(t, err)
			assert.Equal(t, tt.value, got)
		})
	}
}

func TestCompactSizeRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 252, 253, 65535, 65536, 0xFFFFFFFF, 0x100000000}

	for _, val := range values {
		t.Run("roundtrip", func(t *testing.T) {
			buf := new(bytes.Buffer)
			err := WriteCompactSize(buf, val)
			require.NoError(t, err)

			decoded, err := ReadCompactSize(buf)
			require.NoError(t, err)
			assert.Equal(t, val, decoded)
		})
	}
}

func TestCompactSizeErrorTruncated(t *testing.T) {
	// 0xFD requires 2 bytes after it, but only 1 is provided
	truncated := []byte{0xFD, 0x01}
	buf := bytes.NewReader(truncated)

	_, err := ReadCompactSize(buf)
	require.Error(t, err, "expected error when reading truncated CompactSize")
}
