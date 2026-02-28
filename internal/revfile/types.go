// Package revfile implements Bitcoin Core rev file (block undo data) parsing and serialization.
package revfile

// TxOut represents a transaction output with its value and script.
type TxOut struct {
	Value        int64
	ScriptPubKey []byte
}

// Coin represents a spent coin (previous transaction output).
type Coin struct {
	Height   uint32
	CoinBase bool
	Out      TxOut
}

// TxUndo represents the undo data for a single transaction.
type TxUndo struct {
	Prevouts []Coin
}

// BlockUndo represents the undo data for an entire block.
type BlockUndo struct {
	TxUndos []TxUndo
}

// Record represents a complete rev file record with magic, block undo data, checksum, and raw bytes.
type Record struct {
	Magic     [4]byte
	BlockUndo BlockUndo
	Checksum  [32]byte
	RawUndo   []byte
}

// Constants for rev file format.
const (
	StorageHeaderSize = 8
	ChecksumSize     = 32
)

// MainnetMagic is the Bitcoin mainnet network magic number.
var MainnetMagic = [4]byte{0xf9, 0xbe, 0xb4, 0xd9}
