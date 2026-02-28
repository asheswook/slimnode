package revfile

import (
	"fmt"
	"io"
)

// Bitcoin script opcodes used in standard scripts.
const (
	opDUP         = 0x76
	opHASH160     = 0xa9
	opEQUALVERIFY = 0x88
	opCHECKSIG    = 0xac
	opEQUAL       = 0x87
)

// nSpecialScripts is the count of specially-encoded script types in ScriptCompression.
// Values 0x00–0x05 are reserved; non-special scripts encode as varint(len+6).
const nSpecialScripts = 6

// WriteCompressedScript writes the compressed form of script to w.
// Uses Bitcoin Core's ScriptCompression with nSpecialScripts=6.
//
// Special cases (written as nSize byte + payload):
//   0x00 P2PKH: [0x00][20-byte hash160]                 (21 bytes)
//   0x01 P2SH:  [0x01][20-byte hash160]                 (21 bytes)
//   0x02 P2PK compressed 02: [0x02][32-byte x-coord]    (33 bytes)
//   0x03 P2PK compressed 03: [0x03][32-byte x-coord]    (33 bytes)
//   0x04 P2PK uncompressed even-y: [0x04][32-byte x]    (33 bytes)
//   0x05 P2PK uncompressed odd-y:  [0x05][32-byte x]    (33 bytes)
//
// All other scripts: WriteVarInt(len+6) then raw script bytes.
func WriteCompressedScript(w io.Writer, script []byte) error {
	// Case 0x00: P2PKH — OP_DUP OP_HASH160 0x14 [20B] OP_EQUALVERIFY OP_CHECKSIG
	if len(script) == 25 &&
		script[0] == opDUP &&
		script[1] == opHASH160 &&
		script[2] == 0x14 &&
		script[23] == opEQUALVERIFY &&
		script[24] == opCHECKSIG {
		out := make([]byte, 21)
		out[0] = 0x00
		copy(out[1:], script[3:23])
		_, err := w.Write(out)
		return err
	}

	// Case 0x01: P2SH — OP_HASH160 0x14 [20B] OP_EQUAL
	if len(script) == 23 &&
		script[0] == opHASH160 &&
		script[1] == 0x14 &&
		script[22] == opEQUAL {
		out := make([]byte, 21)
		out[0] = 0x01
		copy(out[1:], script[2:22])
		_, err := w.Write(out)
		return err
	}

	// Case 0x02/0x03: P2PK with compressed pubkey — 0x21 [0x02|0x03 + 32B] OP_CHECKSIG
	if len(script) == 35 &&
		script[0] == 0x21 &&
		(script[1] == 0x02 || script[1] == 0x03) &&
		script[34] == opCHECKSIG {
		out := make([]byte, 33)
		out[0] = script[1] // 0x02 or 0x03
		copy(out[1:], script[2:34])
		_, err := w.Write(out)
		return err
	}

	// Case 0x04/0x05: P2PK with uncompressed pubkey — 0x41 [0x04 + 32B x + 32B y] OP_CHECKSIG
	if len(script) == 67 &&
		script[0] == 0x41 &&
		script[1] == 0x04 &&
		script[66] == opCHECKSIG {
		// 0x04 for even y, 0x05 for odd y
		nSize := byte(0x04 + (script[65] & 1))
		out := make([]byte, 33)
		out[0] = nSize
		copy(out[1:], script[2:34]) // x-coord only
		_, err := w.Write(out)
		return err
	}

	// Case 0x04/0x05 internal representation: 33-byte synthetic form produced by
	// ReadCompressedScript for uncompressed P2PK. Format: [0x04|0x05][32B x-coord].
	// This avoids a secp256k1 dependency while preserving round-trip correctness.
	if len(script) == 33 && (script[0] == 0x04 || script[0] == 0x05) {
		_, err := w.Write(script[:33])
		return err
	}

	// Non-special: varint(len+6) followed by raw script bytes.
	if err := WriteVarInt(w, uint64(len(script))+nSpecialScripts); err != nil {
		return fmt.Errorf("revfile: write compressed script size: %w", err)
	}
	_, err := w.Write(script)
	return err
}

// ReadCompressedScript reads a compressed script from r and returns the original script.
// Uses Bitcoin Core's ScriptCompression with nSpecialScripts=6.
//
// Note: for nSize 0x04/0x05 (uncompressed P2PK), full secp256k1 point decompression
// is not performed. The raw compressed representation ([nSize][x-coord]) is returned.
func ReadCompressedScript(r io.Reader) ([]byte, error) {
	nSize, err := ReadVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("revfile: read compressed script nSize: %w", err)
	}

	switch nSize {
	case 0x00: // P2PKH — reconstruct OP_DUP OP_HASH160 0x14 [20B] OP_EQUALVERIFY OP_CHECKSIG
		hash160 := make([]byte, 20)
		if _, err := io.ReadFull(r, hash160); err != nil {
			return nil, fmt.Errorf("revfile: read P2PKH hash160: %w", err)
		}
		script := make([]byte, 0, 25)
		script = append(script, opDUP, opHASH160, 0x14)
		script = append(script, hash160...)
		script = append(script, opEQUALVERIFY, opCHECKSIG)
		return script, nil

	case 0x01: // P2SH — reconstruct OP_HASH160 0x14 [20B] OP_EQUAL
		hash160 := make([]byte, 20)
		if _, err := io.ReadFull(r, hash160); err != nil {
			return nil, fmt.Errorf("revfile: read P2SH hash160: %w", err)
		}
		script := make([]byte, 0, 23)
		script = append(script, opHASH160, 0x14)
		script = append(script, hash160...)
		script = append(script, opEQUAL)
		return script, nil

	case 0x02, 0x03: // P2PK compressed — reconstruct 0x21 [prefix + 32B] OP_CHECKSIG
		xCoord := make([]byte, 32)
		if _, err := io.ReadFull(r, xCoord); err != nil {
			return nil, fmt.Errorf("revfile: read P2PK compressed x-coord: %w", err)
		}
		script := make([]byte, 0, 35)
		script = append(script, 0x21, byte(nSize))
		script = append(script, xCoord...)
		script = append(script, opCHECKSIG)
		return script, nil

	case 0x04, 0x05: // P2PK uncompressed — return compressed form (no secp256k1 dep)
		xCoord := make([]byte, 32)
		if _, err := io.ReadFull(r, xCoord); err != nil {
			return nil, fmt.Errorf("revfile: read P2PK uncompressed x-coord: %w", err)
		}
		result := make([]byte, 33)
		result[0] = byte(nSize)
		copy(result[1:], xCoord)
		return result, nil

	default: // Non-special: nSize encodes len+6, read raw script
		if nSize < nSpecialScripts {
			return nil, fmt.Errorf("revfile: invalid compressed script nSize: %d", nSize)
		}
		scriptLen := nSize - nSpecialScripts
		script := make([]byte, scriptLen)
		if _, err := io.ReadFull(r, script); err != nil {
			return nil, fmt.Errorf("revfile: read raw script: %w", err)
		}
		return script, nil
	}
}
