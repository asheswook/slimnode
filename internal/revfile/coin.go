package revfile

import (
	"fmt"
	"io"
)

// SerializeCoin encodes a Coin to w using Bitcoin Core's format.
func SerializeCoin(w io.Writer, coin Coin) error {
	packed := uint64(coin.Height) * 2
	if coin.CoinBase {
		packed |= 1
	}
	if err := WriteVarInt(w, packed); err != nil {
		return fmt.Errorf("coin: write height: %w", err)
	}
	if coin.Height > 0 {
		if _, err := w.Write([]byte{0x00}); err != nil {
			return fmt.Errorf("coin: write dummy: %w", err)
		}
	}
	if err := WriteVarInt(w, CompressAmount(uint64(coin.Out.Value))); err != nil {
		return fmt.Errorf("coin: write amount: %w", err)
	}
	if err := WriteCompressedScript(w, coin.Out.ScriptPubKey); err != nil {
		return fmt.Errorf("coin: write script: %w", err)
	}
	return nil
}

// DeserializeCoin decodes a Coin from r using Bitcoin Core's format.
func DeserializeCoin(r io.Reader) (Coin, error) {
	packed, err := ReadVarInt(r)
	if err != nil {
		return Coin{}, fmt.Errorf("coin: read height: %w", err)
	}
	height := uint32(packed >> 1)
	coinbase := (packed & 1) == 1
	if height > 0 {
		buf := make([]byte, 1)
		if _, err := io.ReadFull(r, buf); err != nil {
			return Coin{}, fmt.Errorf("coin: read dummy: %w", err)
		}
		if buf[0] != 0x00 {
			return Coin{}, fmt.Errorf("coin: expected dummy 0x00, got 0x%02x", buf[0])
		}
	}
	compressed, err := ReadVarInt(r)
	if err != nil {
		return Coin{}, fmt.Errorf("coin: read amount: %w", err)
	}
	value := int64(DecompressAmount(compressed))
	script, err := ReadCompressedScript(r)
	if err != nil {
		return Coin{}, fmt.Errorf("coin: read script: %w", err)
	}
	return Coin{Height: height, CoinBase: coinbase, Out: TxOut{Value: value, ScriptPubKey: script}}, nil
}

// SerializeTxUndo encodes a TxUndo to w.
func SerializeTxUndo(w io.Writer, txundo TxUndo) error {
	if err := WriteCompactSize(w, uint64(len(txundo.Prevouts))); err != nil {
		return fmt.Errorf("txundo: write count: %w", err)
	}
	for _, coin := range txundo.Prevouts {
		if err := SerializeCoin(w, coin); err != nil {
			return fmt.Errorf("txundo: serialize coin: %w", err)
		}
	}
	return nil
}

// DeserializeTxUndo decodes a TxUndo from r.
func DeserializeTxUndo(r io.Reader) (TxUndo, error) {
	n, err := ReadCompactSize(r)
	if err != nil {
		return TxUndo{}, fmt.Errorf("txundo: read count: %w", err)
	}
	coins := make([]Coin, n)
	for i := range coins {
		coins[i], err = DeserializeCoin(r)
		if err != nil {
			return TxUndo{}, fmt.Errorf("txundo: deserialize coin: %w", err)
		}
	}
	return TxUndo{Prevouts: coins}, nil
}

// SerializeBlockUndo encodes a BlockUndo to w.
func SerializeBlockUndo(w io.Writer, bu BlockUndo) error {
	if err := WriteCompactSize(w, uint64(len(bu.TxUndos))); err != nil {
		return fmt.Errorf("blockundo: write count: %w", err)
	}
	for _, txundo := range bu.TxUndos {
		if err := SerializeTxUndo(w, txundo); err != nil {
			return fmt.Errorf("blockundo: serialize txundo: %w", err)
		}
	}
	return nil
}

// DeserializeBlockUndo decodes a BlockUndo from r.
func DeserializeBlockUndo(r io.Reader) (BlockUndo, error) {
	n, err := ReadCompactSize(r)
	if err != nil {
		return BlockUndo{}, fmt.Errorf("blockundo: read count: %w", err)
	}
	txundos := make([]TxUndo, n)
	for i := range txundos {
		txundos[i], err = DeserializeTxUndo(r)
		if err != nil {
			return BlockUndo{}, fmt.Errorf("blockundo: deserialize txundo: %w", err)
		}
	}
	return BlockUndo{TxUndos: txundos}, nil
}
