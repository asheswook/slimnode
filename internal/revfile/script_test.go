package revfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptCompressionP2PKH(t *testing.T) {
	hash160 := make([]byte, 20)
	for i := range hash160 {
		hash160[i] = byte(i + 1)
	}

	// Construct P2PKH script: OP_DUP OP_HASH160 OP_PUSH20 [20B] OP_EQUALVERIFY OP_CHECKSIG
	p2pkh := []byte{opDUP, opHASH160, 0x14}
	p2pkh = append(p2pkh, hash160...)
	p2pkh = append(p2pkh, opEQUALVERIFY, opCHECKSIG)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2pkh)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 21, "P2PKH compressed size must be 21 bytes")
	assert.Equal(t, byte(0x00), compressed[0], "P2PKH nSize must be 0x00")
	assert.Equal(t, hash160, compressed[1:], "P2PKH hash160 must be preserved")

	// Round-trip: decompress and verify identical to original
	got, err := ReadCompressedScript(bytes.NewReader(compressed))
	require.NoError(t, err)
	assert.Equal(t, p2pkh, got, "P2PKH round-trip must produce identical script")
}

func TestScriptCompressionP2SH(t *testing.T) {
	hash160 := make([]byte, 20)
	for i := range hash160 {
		hash160[i] = byte(i + 5)
	}

	// Construct P2SH script: OP_HASH160 OP_PUSH20 [20B] OP_EQUAL
	p2sh := []byte{opHASH160, 0x14}
	p2sh = append(p2sh, hash160...)
	p2sh = append(p2sh, opEQUAL)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2sh)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 21, "P2SH compressed size must be 21 bytes")
	assert.Equal(t, byte(0x01), compressed[0], "P2SH nSize must be 0x01")
	assert.Equal(t, hash160, compressed[1:], "P2SH hash160 must be preserved")

	// Round-trip
	got, err := ReadCompressedScript(bytes.NewReader(compressed))
	require.NoError(t, err)
	assert.Equal(t, p2sh, got, "P2SH round-trip must produce identical script")
}

func TestScriptCompressionP2PKCompressed02(t *testing.T) {
	xCoord := make([]byte, 32)
	for i := range xCoord {
		xCoord[i] = byte(i + 1)
	}

	// Construct P2PK with compressed 02 key: OP_PUSH33 [0x02 + 32B] OP_CHECKSIG
	p2pk := []byte{0x21, 0x02}
	p2pk = append(p2pk, xCoord...)
	p2pk = append(p2pk, opCHECKSIG)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2pk)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 33, "P2PK 02 compressed size must be 33 bytes")
	assert.Equal(t, byte(0x02), compressed[0], "P2PK 02 nSize must be 0x02")
	assert.Equal(t, xCoord, compressed[1:], "P2PK 02 x-coord must be preserved")

	// Round-trip
	got, err := ReadCompressedScript(bytes.NewReader(compressed))
	require.NoError(t, err)
	assert.Equal(t, p2pk, got, "P2PK 02 round-trip must produce identical script")
}

func TestScriptCompressionP2PKCompressed03(t *testing.T) {
	xCoord := make([]byte, 32)
	for i := range xCoord {
		xCoord[i] = byte(i + 7)
	}

	// Construct P2PK with compressed 03 key: OP_PUSH33 [0x03 + 32B] OP_CHECKSIG
	p2pk := []byte{0x21, 0x03}
	p2pk = append(p2pk, xCoord...)
	p2pk = append(p2pk, opCHECKSIG)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2pk)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 33, "P2PK 03 compressed size must be 33 bytes")
	assert.Equal(t, byte(0x03), compressed[0], "P2PK 03 nSize must be 0x03")
	assert.Equal(t, xCoord, compressed[1:], "P2PK 03 x-coord must be preserved")

	// Round-trip
	got, err := ReadCompressedScript(bytes.NewReader(compressed))
	require.NoError(t, err)
	assert.Equal(t, p2pk, got, "P2PK 03 round-trip must produce identical script")
}

func TestScriptCompressionP2PKUncompressed04(t *testing.T) {
	xCoord := make([]byte, 32)
	yCoord := make([]byte, 32)
	for i := range xCoord {
		xCoord[i] = byte(i + 1)
		yCoord[i] = byte(i + 2)
	}
	yCoord[31] = 0x02 // even y → nSize 0x04

	p2pk := []byte{0x41, 0x04}
	p2pk = append(p2pk, xCoord...)
	p2pk = append(p2pk, yCoord...)
	p2pk = append(p2pk, opCHECKSIG)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2pk)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 33, "P2PK 04 compressed size must be 33 bytes")
	assert.Equal(t, byte(0x04), compressed[0], "P2PK even-y nSize must be 0x04")
	assert.Equal(t, xCoord, compressed[1:], "P2PK 04 x-coord must be preserved")
}

func TestScriptCompressionP2PKUncompressed05(t *testing.T) {
	xCoord := make([]byte, 32)
	yCoord := make([]byte, 32)
	for i := range xCoord {
		xCoord[i] = byte(i + 3)
		yCoord[i] = byte(i + 4)
	}
	yCoord[31] = 0x03 // odd y → nSize 0x05

	p2pk := []byte{0x41, 0x04}
	p2pk = append(p2pk, xCoord...)
	p2pk = append(p2pk, yCoord...)
	p2pk = append(p2pk, opCHECKSIG)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, p2pk)
	require.NoError(t, err)

	compressed := buf.Bytes()
	assert.Len(t, compressed, 33, "P2PK 05 compressed size must be 33 bytes")
	assert.Equal(t, byte(0x05), compressed[0], "P2PK odd-y nSize must be 0x05")
	assert.Equal(t, xCoord, compressed[1:], "P2PK 05 x-coord must be preserved")
}

func TestScriptCompressionP2PKUncompressedSyntheticRoundTrip(t *testing.T) {
	xCoord := make([]byte, 32)
	for i := range xCoord {
		xCoord[i] = byte(i + 10)
	}

	synthetic := append([]byte{0x04}, xCoord...)

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, synthetic)
	require.NoError(t, err)

	assert.Equal(t, synthetic, buf.Bytes(), "synthetic 0x04 form must round-trip through WriteCompressedScript")
}

func TestScriptNonStandard(t *testing.T) {
	// OP_RETURN + 4 bytes of data — not a special script type
	script := []byte{0x6a, 0x04, 0xde, 0xad, 0xbe, 0xef}

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, script)
	require.NoError(t, err)

	data := buf.Bytes()
	r := bytes.NewReader(data)

	// The varint prefix must encode len(script)+6 = 12
	nSize, err := ReadVarInt(r)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(script))+nSpecialScripts, nSize, "non-standard nSize must be len+6")

	// Remaining bytes must be the raw script
	rest := make([]byte, len(script))
	_, err = io.ReadFull(r, rest)
	require.NoError(t, err)
	assert.Equal(t, script, rest, "non-standard raw bytes must match original script")
}

func TestScriptNonStandardRoundTrip(t *testing.T) {
	// Arbitrary non-standard script (OP_RETURN + 8 bytes)
	script := []byte{0x6a, 0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	var buf bytes.Buffer
	err := WriteCompressedScript(&buf, script)
	require.NoError(t, err)

	got, err := ReadCompressedScript(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, script, got, "non-standard round-trip must produce identical script")
}
