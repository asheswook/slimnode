package revfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestP2PKH() []byte {
	hash := bytes.Repeat([]byte{0x42}, 20)
	s := []byte{0x76, 0xa9, 0x14}
	s = append(s, hash...)
	return append(s, 0x88, 0xac)
}

func TestSerializeCoin_HeightZero(t *testing.T) {
	coin := Coin{Height: 0, CoinBase: false, Out: TxOut{Value: 5000000000, ScriptPubKey: makeTestP2PKH()}}
	var buf bytes.Buffer
	require.NoError(t, SerializeCoin(&buf, coin))
	// Verify no dummy byte: first varint is 0 (height=0, coinbase=false)
	data := buf.Bytes()
	assert.Equal(t, byte(0x00), data[0]) // VARINT(0) = 0x00
	// Deserialize and compare
	got, err := DeserializeCoin(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, coin.Height, got.Height)
	assert.Equal(t, coin.CoinBase, got.CoinBase)
	assert.Equal(t, coin.Out.Value, got.Out.Value)
}

func TestSerializeCoin_HeightNonZero(t *testing.T) {
	coin := Coin{Height: 100000, CoinBase: true, Out: TxOut{Value: 5000000000, ScriptPubKey: makeTestP2PKH()}}
	var buf bytes.Buffer
	require.NoError(t, SerializeCoin(&buf, coin))
	got, err := DeserializeCoin(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, coin.Height, got.Height)
	assert.Equal(t, coin.CoinBase, got.CoinBase)
	assert.Equal(t, coin.Out.Value, got.Out.Value)
}

func TestSerializeTxUndo(t *testing.T) {
	txundo := TxUndo{Prevouts: []Coin{
		{Height: 0, Out: TxOut{Value: 1000000, ScriptPubKey: makeTestP2PKH()}},
		{Height: 500000, CoinBase: false, Out: TxOut{Value: 2000000, ScriptPubKey: makeTestP2PKH()}},
	}}
	var buf bytes.Buffer
	require.NoError(t, SerializeTxUndo(&buf, txundo))
	got, err := DeserializeTxUndo(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, got.Prevouts, 2)
	assert.Equal(t, txundo.Prevouts[0].Out.Value, got.Prevouts[0].Out.Value)
	assert.Equal(t, txundo.Prevouts[1].Height, got.Prevouts[1].Height)
}

func TestSerializeBlockUndo(t *testing.T) {
	bu := BlockUndo{TxUndos: []TxUndo{
		{Prevouts: []Coin{{Height: 0, Out: TxOut{Value: 1000, ScriptPubKey: makeTestP2PKH()}}}},
		{Prevouts: []Coin{{Height: 1000, Out: TxOut{Value: 2000, ScriptPubKey: makeTestP2PKH()}}}},
	}}
	var buf bytes.Buffer
	require.NoError(t, SerializeBlockUndo(&buf, bu))
	got, err := DeserializeBlockUndo(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, got.TxUndos, 2)
}

func TestBlockUndoRoundTrip(t *testing.T) {
	bu := BlockUndo{TxUndos: []TxUndo{
		{Prevouts: []Coin{
			{Height: 0, CoinBase: false, Out: TxOut{Value: 5000000000, ScriptPubKey: makeTestP2PKH()}},
			{Height: 840000, CoinBase: true, Out: TxOut{Value: 625000000, ScriptPubKey: makeTestP2PKH()}},
		}},
		{Prevouts: []Coin{
			{Height: 100, Out: TxOut{Value: 100000, ScriptPubKey: makeTestP2PKH()}},
		}},
	}}
	var buf1 bytes.Buffer
	require.NoError(t, SerializeBlockUndo(&buf1, bu))
	got, err := DeserializeBlockUndo(bytes.NewReader(buf1.Bytes()))
	require.NoError(t, err)
	var buf2 bytes.Buffer
	require.NoError(t, SerializeBlockUndo(&buf2, got))
	assert.Equal(t, buf1.Bytes(), buf2.Bytes())
}
