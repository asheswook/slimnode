package revfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// sha256d computes double SHA256 hash of data.
func sha256d(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// ParseRecord reads a single Record from r.
func ParseRecord(r io.Reader) (Record, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return Record{}, fmt.Errorf("record: read magic: %w", err)
	}
	if magic != MainnetMagic {
		return Record{}, fmt.Errorf("record: invalid magic %x", magic)
	}
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return Record{}, fmt.Errorf("record: read size: %w", err)
	}
	size := binary.LittleEndian.Uint32(sizeBuf[:])
	rawUndo := make([]byte, size)
	if _, err := io.ReadFull(r, rawUndo); err != nil {
		return Record{}, fmt.Errorf("record: read undo: %w", err)
	}
	var checksum [32]byte
	if _, err := io.ReadFull(r, checksum[:]); err != nil {
		return Record{}, fmt.Errorf("record: read checksum: %w", err)
	}
	bu, err := DeserializeBlockUndo(bytes.NewReader(rawUndo))
	if err != nil {
		return Record{}, fmt.Errorf("record: deserialize: %w", err)
	}
	return Record{Magic: MainnetMagic, BlockUndo: bu, Checksum: checksum, RawUndo: rawUndo}, nil
}

// ParseAllRecords reads all Records from r until EOF.
func ParseAllRecords(r io.Reader) ([]Record, error) {
	var records []Record
	for {
		rec, err := ParseRecord(r)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return records, nil
			}
			return records, fmt.Errorf("record: parse: %w", err)
		}
		records = append(records, rec)
	}
}

// SerializeRecord writes a Record to w with the given previous block hash.
func SerializeRecord(w io.Writer, rec Record, prevBlockHash [32]byte) error {
	var buf bytes.Buffer
	if err := SerializeBlockUndo(&buf, rec.BlockUndo); err != nil {
		return fmt.Errorf("record: serialize: %w", err)
	}
	serialized := buf.Bytes()
	if _, err := w.Write(MainnetMagic[:]); err != nil {
		return fmt.Errorf("record: write magic: %w", err)
	}
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(len(serialized)))
	if _, err := w.Write(sizeBuf[:]); err != nil {
		return fmt.Errorf("record: write size: %w", err)
	}
	if _, err := w.Write(serialized); err != nil {
		return fmt.Errorf("record: write undo: %w", err)
	}
	checksumInput := append(prevBlockHash[:], serialized...)
	checksum := sha256d(checksumInput)
	if _, err := w.Write(checksum[:]); err != nil {
		return fmt.Errorf("record: write checksum: %w", err)
	}
	return nil
}

// VerifyChecksum checks if the Record's checksum is valid for the given previous block hash.
func VerifyChecksum(rec Record, prevBlockHash [32]byte) bool {
	checksumInput := append(prevBlockHash[:], rec.RawUndo...)
	expected := sha256d(checksumInput)
	return expected == rec.Checksum
}
