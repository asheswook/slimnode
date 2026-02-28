package revfile

import (
	"fmt"
	"io"
)

// WriteVarInt encodes n using Bitcoin Core's MSB-continuation VARINT format.
// Each byte stores 7 bits of data with bit 7 as a continuation flag.
// Values are encoded big-endian with each group-minus-one stored to avoid redundant encodings.
func WriteVarInt(w io.Writer, n uint64) error {
	var buf [10]byte
	i := 9
	
	// Write the last byte (lowest 7 bits, no continuation bit yet)
	buf[i] = byte(n & 0x7F)
	
	// Process remaining bytes
	for n >>= 7; n > 0; n >>= 7 {
		i--
		n--
		buf[i] = byte(n&0x7F) | 0x80
	}
	
	_, err := w.Write(buf[i:])
	return err
}

// ReadVarInt decodes a Bitcoin Core MSB-continuation VARINT from r.
// Returns the decoded uint64 value or an error if reading fails.
func ReadVarInt(r io.Reader) (uint64, error) {
	var n uint64
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, fmt.Errorf("revfile: read varint: %w", err)
		}
		n = (n << 7) | uint64(buf[0]&0x7F)
		if buf[0]&0x80 == 0 {
			return n, nil
		}
		n++
	}
}
