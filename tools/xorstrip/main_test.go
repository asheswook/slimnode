package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	mainnetMagic = uint32(0xD9B4BEF9)
	testKey      = []byte{0xcb, 0x37, 0x68, 0x1b, 0xc5, 0x12, 0x07, 0x0e}
)

func bitcoinCoreXor(data []byte, key []byte, fileOffset int) {
	if len(key) == 0 {
		return
	}
	j := fileOffset % len(key)
	for i := range data {
		data[i] ^= key[j]
		j++
		if j == len(key) {
			j = 0
		}
	}
}

func makeRawBlkFile(t *testing.T, numBlocks int) []byte {
	t.Helper()
	var buf bytes.Buffer
	for range numBlocks {
		blockData := []byte("this is fake block payload data for testing purposes!!")
		var magic [4]byte
		binary.LittleEndian.PutUint32(magic[:], mainnetMagic)
		buf.Write(magic[:])
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(blockData)))
		buf.Write(size[:])
		buf.Write(blockData)
	}
	return buf.Bytes()
}

func TestXorMatchesBitcoinCore(t *testing.T) {
	raw := makeRawBlkFile(t, 5)

	obfuscated := make([]byte, len(raw))
	copy(obfuscated, raw)
	bitcoinCoreXor(obfuscated, testKey, 0)

	if bytes.Equal(raw, obfuscated) {
		t.Fatal("obfuscated data should differ from raw")
	}

	stripped := make([]byte, len(obfuscated))
	copy(stripped, obfuscated)
	for i := range stripped {
		stripped[i] ^= testKey[(int64(i))%int64(len(testKey))]
	}

	if !bytes.Equal(raw, stripped) {
		t.Fatal("our XOR formula does not reverse Bitcoin Core obfuscation")
	}
}

func TestXorFileInPlace(t *testing.T) {
	raw := makeRawBlkFile(t, 10)

	obfuscated := make([]byte, len(raw))
	copy(obfuscated, raw)
	bitcoinCoreXor(obfuscated, testKey, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, obfuscated, 0644); err != nil {
		t.Fatal(err)
	}

	n, err := xorFileInPlace(path, testKey)
	if err != nil {
		t.Fatalf("xorFileInPlace: %v", err)
	}
	if n != int64(len(obfuscated)) {
		t.Fatalf("processed %d bytes, expected %d", n, len(obfuscated))
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, result) {
		t.Fatalf("xorFileInPlace output does not match original raw data\nfirst 16 raw:    %x\nfirst 16 result: %x", raw[:16], result[:16])
	}
}

func TestDetectState(t *testing.T) {
	raw := makeRawBlkFile(t, 1)

	obfuscated := make([]byte, len(raw))
	copy(obfuscated, raw)
	bitcoinCoreXor(obfuscated, testKey, 0)

	dir := t.TempDir()

	rawPath := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(rawPath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	obfPath := filepath.Join(dir, "blk00001.dat")
	if err := os.WriteFile(obfPath, obfuscated, 0644); err != nil {
		t.Fatal(err)
	}

	garbagePath := filepath.Join(dir, "blk00002.dat")
	if err := os.WriteFile(garbagePath, []byte{0xDE, 0xAD, 0xBE, 0xEF}, 0644); err != nil {
		t.Fatal(err)
	}

	state, network := detectState(rawPath, testKey)
	if state != stateRaw || network != "mainnet" {
		t.Fatalf("raw file: got state=%d network=%q, want stateRaw mainnet", state, network)
	}

	state, network = detectState(obfPath, testKey)
	if state != stateObfuscated || network != "mainnet" {
		t.Fatalf("obfuscated file: got state=%d network=%q, want stateObfuscated mainnet", state, network)
	}

	state, _ = detectState(garbagePath, testKey)
	if state != stateUnknown {
		t.Fatalf("garbage file: got state=%d, want stateUnknown", state)
	}
}

func TestDoubleXorIsIdempotent(t *testing.T) {
	raw := makeRawBlkFile(t, 3)

	obfuscated := make([]byte, len(raw))
	copy(obfuscated, raw)
	bitcoinCoreXor(obfuscated, testKey, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, obfuscated, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := xorFileInPlace(path, testKey); err != nil {
		t.Fatal(err)
	}

	if _, err := xorFileInPlace(path, testKey); err != nil {
		t.Fatal(err)
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(obfuscated, result) {
		t.Fatal("double XOR should return to obfuscated state")
	}
}

func TestXorLargeFileChunkBoundary(t *testing.T) {
	size := bufSize + 12345
	raw := make([]byte, size)
	binary.LittleEndian.PutUint32(raw[:4], mainnetMagic)
	for i := 4; i < size; i++ {
		raw[i] = byte(i % 251)
	}

	obfuscated := make([]byte, size)
	copy(obfuscated, raw)
	bitcoinCoreXor(obfuscated, testKey, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, obfuscated, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := xorFileInPlace(path, testKey); err != nil {
		t.Fatal(err)
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, result) {
		for i := range raw {
			if raw[i] != result[i] {
				t.Fatalf("mismatch at byte %d (chunk boundary at %d): raw=0x%02x got=0x%02x",
					i, bufSize, raw[i], result[i])
			}
		}
	}
}

func makePartialFile(t *testing.T, rawChunks int, totalChunks int) ([]byte, []byte) {
	t.Helper()
	size := totalChunks * bufSize
	raw := make([]byte, size)
	for i := 0; i < totalChunks; i++ {
		off := i * bufSize
		binary.LittleEndian.PutUint32(raw[off:], mainnetMagic)
		binary.LittleEndian.PutUint32(raw[off+4:], 100)
		for j := 8; j < bufSize; j++ {
			raw[off+j] = byte((off + j) % 251)
		}
	}

	partial := make([]byte, size)
	copy(partial, raw)
	bitcoinCoreXor(partial, testKey, 0)
	copy(partial[:rawChunks*bufSize], raw[:rawChunks*bufSize])

	return raw, partial
}

func TestFindConversionBoundary_FullyRaw(t *testing.T) {
	raw := makeRawBlkFile(t, 100)
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	boundary, err := findConversionBoundary(path)
	if err != nil {
		t.Fatal(err)
	}
	if boundary != -1 {
		t.Fatalf("expected -1 for fully raw file, got %d", boundary)
	}
}

func TestFindConversionBoundary_Partial(t *testing.T) {
	_, partial := makePartialFile(t, 2, 4)
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, partial, 0644); err != nil {
		t.Fatal(err)
	}

	boundary, err := findConversionBoundary(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := int64(2 * bufSize)
	if boundary != expected {
		t.Fatalf("expected boundary at %d, got %d", expected, boundary)
	}
}

func TestXorFileFromOffset(t *testing.T) {
	raw, partial := makePartialFile(t, 3, 5)
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, partial, 0644); err != nil {
		t.Fatal(err)
	}

	startOffset := int64(3 * bufSize)
	n, err := xorFileFromOffset(path, testKey, startOffset)
	if err != nil {
		t.Fatalf("xorFileFromOffset: %v", err)
	}
	expectedBytes := int64(2 * bufSize)
	if n != expectedBytes {
		t.Fatalf("expected %d bytes processed, got %d", expectedBytes, n)
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, result) {
		for i := range raw {
			if raw[i] != result[i] {
				t.Fatalf("mismatch at byte %d: raw=0x%02x got=0x%02x", i, raw[i], result[i])
			}
		}
	}
}

func TestRepairEndToEnd(t *testing.T) {
	raw, partial := makePartialFile(t, 2, 4)
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, partial, 0644); err != nil {
		t.Fatal(err)
	}

	if err := run(dir, "cb37681bc512070e", false, true, 1); err != nil {
		t.Fatalf("run --repair: %v", err)
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, result) {
		for i := range raw {
			if raw[i] != result[i] {
				t.Fatalf("mismatch at byte %d: raw=0x%02x got=0x%02x", i, raw[i], result[i])
			}
		}
	}
}

func TestRepairDryRun(t *testing.T) {
	_, partial := makePartialFile(t, 2, 4)
	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, partial, 0644); err != nil {
		t.Fatal(err)
	}

	before, _ := os.ReadFile(path)

	if err := run(dir, "cb37681bc512070e", true, true, 1); err != nil {
		t.Fatalf("run --repair --dry-run: %v", err)
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run should not modify files")
	}
}

func TestRepairSkipsFullyRawFiles(t *testing.T) {
	raw := make([]byte, 2*bufSize)
	for i := 0; i < 2; i++ {
		off := i * bufSize
		binary.LittleEndian.PutUint32(raw[off:], mainnetMagic)
		binary.LittleEndian.PutUint32(raw[off+4:], 100)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "blk00000.dat")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	if err := run(dir, "cb37681bc512070e", false, true, 1); err != nil {
		if !strings.Contains(err.Error(), "") {
			t.Fatalf("run --repair on fully raw: %v", err)
		}
	}

	result, _ := os.ReadFile(path)
	if !bytes.Equal(raw, result) {
		t.Fatal("repair should not modify fully raw files")
	}
}

func TestRepairWithMixedFiles(t *testing.T) {
	dir := t.TempDir()

	raw1, partial := makePartialFile(t, 1, 3)
	if err := os.WriteFile(filepath.Join(dir, "blk00000.dat"), partial, 0644); err != nil {
		t.Fatal(err)
	}

	raw2 := make([]byte, bufSize)
	binary.LittleEndian.PutUint32(raw2, mainnetMagic)
	obf2 := make([]byte, bufSize)
	copy(obf2, raw2)
	bitcoinCoreXor(obf2, testKey, 0)
	if err := os.WriteFile(filepath.Join(dir, "blk00001.dat"), obf2, 0644); err != nil {
		t.Fatal(err)
	}

	if err := run(dir, "cb37681bc512070e", false, true, 2); err != nil {
		t.Fatalf("run --repair mixed: %v", err)
	}

	result1, _ := os.ReadFile(filepath.Join(dir, "blk00000.dat"))
	if !bytes.Equal(raw1, result1) {
		t.Fatal("partial file not repaired correctly")
	}

	result2, _ := os.ReadFile(filepath.Join(dir, "blk00001.dat"))
	if !bytes.Equal(raw2, result2) {
		t.Fatal("fully obfuscated file not converted correctly")
	}
}

func TestLoadKeyFormats(t *testing.T) {
	dir8 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir8, "xor.dat"), testKey, 0644); err != nil {
		t.Fatal(err)
	}
	key8, err := loadKey(dir8, "")
	if err != nil {
		t.Fatalf("8-byte format: %v", err)
	}
	if !bytes.Equal(key8, testKey) {
		t.Fatalf("8-byte format: got %x, want %x", key8, testKey)
	}

	dir9 := t.TempDir()
	prefixed := append([]byte{0x08}, testKey...)
	if err := os.WriteFile(filepath.Join(dir9, "xor.dat"), prefixed, 0644); err != nil {
		t.Fatal(err)
	}
	key9, err := loadKey(dir9, "")
	if err != nil {
		t.Fatalf("9-byte format: %v", err)
	}
	if !bytes.Equal(key9, testKey) {
		t.Fatalf("9-byte format: got %x, want %x", key9, testKey)
	}

	keyHex, err := loadKey(t.TempDir(), "cb37681bc512070e")
	if err != nil {
		t.Fatalf("--key flag: %v", err)
	}
	if !bytes.Equal(keyHex, testKey) {
		t.Fatalf("--key flag: got %x, want %x", keyHex, testKey)
	}
}
