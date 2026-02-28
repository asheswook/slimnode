package revfile

import (
	"encoding/binary"
	"fmt"
	"io"
)

// WriteCompactSize encodes n as Bitcoin CompactSize into w.
// Encoding rules:
// - n <= 0xFC → 1 byte: n
// - 0xFD <= n <= 0xFFFF → 3 bytes: 0xFD + uint16 LE
// - 0x10000 <= n <= 0xFFFFFFFF → 5 bytes: 0xFE + uint32 LE
// - n > 0xFFFFFFFF → 9 bytes: 0xFF + uint64 LE
func WriteCompactSize(w io.Writer, n uint64) error {
	if n <= 0xFC {
		_, err := w.Write([]byte{byte(n)})
		return err
	}

	if n <= 0xFFFF {
		if _, err := w.Write([]byte{0xFD}); err != nil {
			return err
		}
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(n))
		_, err := w.Write(buf)
		return err
	}

	if n <= 0xFFFFFFFF {
		if _, err := w.Write([]byte{0xFE}); err != nil {
			return err
		}
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(n))
		_, err := w.Write(buf)
		return err
	}

	if _, err := w.Write([]byte{0xFF}); err != nil {
		return err
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, n)
	_, err := w.Write(buf)
	return err
}

// ReadCompactSize decodes a Bitcoin CompactSize from r.
func ReadCompactSize(r io.Reader) (uint64, error) {
	firstByte := make([]byte, 1)
	if _, err := io.ReadFull(r, firstByte); err != nil {
		return 0, fmt.Errorf("revfile: read first byte: %w", err)
	}

	switch firstByte[0] {
	case 0xFD:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, fmt.Errorf("revfile: read uint16: %w", err)
		}
		return uint64(binary.LittleEndian.Uint16(buf)), nil

	case 0xFE:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, fmt.Errorf("revfile: read uint32: %w", err)
		}
		return uint64(binary.LittleEndian.Uint32(buf)), nil

	case 0xFF:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, fmt.Errorf("revfile: read uint64: %w", err)
		}
		return binary.LittleEndian.Uint64(buf), nil

	default:
		return uint64(firstByte[0]), nil
	}
}
