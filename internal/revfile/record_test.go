package revfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestBlockUndo() BlockUndo {
	p2pkh := append([]byte{0x76, 0xa9, 0x14}, append(bytes.Repeat([]byte{0x42}, 20), 0x88, 0xac)...)
	return BlockUndo{TxUndos: []TxUndo{
		{Prevouts: []Coin{{Height: 0, Out: TxOut{Value: 5000000000, ScriptPubKey: p2pkh}}}},
	}}
}

func TestParseRecord_SyntheticRoundTrip(t *testing.T) {
	prevHash := [32]byte{}
	copy(prevHash[:], bytes.Repeat([]byte{0xAB}, 32))
	rec := Record{Magic: MainnetMagic, BlockUndo: makeTestBlockUndo()}
	var buf bytes.Buffer
	require.NoError(t, SerializeRecord(&buf, rec, prevHash))
	parsed, err := ParseRecord(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, MainnetMagic, parsed.Magic)
	// Re-serialize and compare
	var buf2 bytes.Buffer
	require.NoError(t, SerializeRecord(&buf2, parsed, prevHash))
	assert.Equal(t, buf.Bytes(), buf2.Bytes())
}

func TestVerifyChecksum(t *testing.T) {
	prevHash := [32]byte{}
	copy(prevHash[:], bytes.Repeat([]byte{0xAB}, 32))
	rec := Record{Magic: MainnetMagic, BlockUndo: makeTestBlockUndo()}
	var buf bytes.Buffer
	require.NoError(t, SerializeRecord(&buf, rec, prevHash))
	parsed, err := ParseRecord(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.True(t, VerifyChecksum(parsed, prevHash))
	var wrongHash [32]byte
	copy(wrongHash[:], bytes.Repeat([]byte{0xFF}, 32))
	assert.False(t, VerifyChecksum(parsed, wrongHash))
}

func TestParseAllRecords(t *testing.T) {
	prevHash := [32]byte{}
	var buf bytes.Buffer
	for i := 0; i < 3; i++ {
		rec := Record{Magic: MainnetMagic, BlockUndo: makeTestBlockUndo()}
		require.NoError(t, SerializeRecord(&buf, rec, prevHash))
	}
	records, err := ParseAllRecords(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Len(t, records, 3)
	for _, r := range records {
		assert.Equal(t, MainnetMagic, r.Magic)
	}
}

func TestParseRecord_BadMagic(t *testing.T) {
	buf := bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	_, err := ParseRecord(buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic")
}

func TestParseRecord_Truncated(t *testing.T) {
	// magic + size(100) + only 10 bytes
	var buf bytes.Buffer
	buf.Write(MainnetMagic[:])
	buf.Write([]byte{100, 0, 0, 0}) // size = 100 LE
	buf.Write(make([]byte, 10))     // only 10 bytes, not 100
	_, err := ParseRecord(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
}
