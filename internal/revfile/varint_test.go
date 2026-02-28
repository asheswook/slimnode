package revfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVarIntKnownValues(t *testing.T) {
	tests := []struct {
		value    uint64
		expected []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7F}},
		{128, []byte{0x80, 0x00}},
		{255, []byte{0x80, 0x7F}},
		{16383, []byte{0xFE, 0x7F}},
		{16384, []byte{0xFF, 0x00}},
	}

	for _, tt := range tests {
		t.Run("encode", func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteVarInt(buf, tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, buf.Bytes(), "value %d", tt.value)
		})

		t.Run("decode", func(t *testing.T) {
			buf := bytes.NewReader(tt.expected)
			got, err := ReadVarInt(buf)
			require.NoError(t, err)
			assert.Equal(t, tt.value, got, "bytes %v", tt.expected)
		})
	}
}

func TestVarIntRoundTrip(t *testing.T) {
	values := []uint64{
		0,
		1,
		127,
		128,
		255,
		256,
		16383,
		16384,
		1<<32 - 1,
		1<<64 - 1,
	}

	for _, val := range values {
		t.Run("roundtrip", func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteVarInt(buf, val)
			require.NoError(t, err)

			got, err := ReadVarInt(bytes.NewReader(buf.Bytes()))
			require.NoError(t, err)
			assert.Equal(t, val, got, "value %d", val)
		})
	}
}

func TestVarIntErrorEmptyInput(t *testing.T) {
	buf := bytes.NewReader([]byte{})
	_, err := ReadVarInt(buf)
	require.Error(t, err)
}
