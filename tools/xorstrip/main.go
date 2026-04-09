// xorstrip strips XOR obfuscation from Bitcoin Core block files in-place.
//
// Detects each file's state via network magic bytes (f9beb4d9 for mainnet)
// and only converts files that are still obfuscated. Safe to re-run after
// interruption - already-converted files are skipped automatically.
//
// The --repair flag detects and fixes partially converted files (where
// xorstrip was interrupted mid-file). It scans each blk file in 8 MiB
// chunks for raw magic bytes; chunks without magic are still XOR'd and
// get stripped.
//
// Usage:
//
//	xorstrip --blocks-dir /data/bitcoin/blocks
//	xorstrip --blocks-dir /data/bitcoin/blocks --dry-run
//	xorstrip --blocks-dir /data/bitcoin/blocks --key cb37681bc512070e
//	xorstrip --blocks-dir /data/bitcoin/blocks --key cb37681bc512070e --repair
//	xorstrip --blocks-dir /data/bitcoin/blocks --key cb37681bc512070e --repair --dry-run
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var networkMagics = map[uint32]string{
	0xD9B4BEF9: "mainnet",
	0x0709110B: "testnet3",
	0x40CF030A: "signet",
	0xDAB5BFFA: "regtest",
	0x283F161C: "testnet4",
}

const (
	xorKeySize = 8
	bufSize    = 8 * 1024 * 1024
)

type fileState int

const (
	stateRaw fileState = iota
	stateObfuscated
	stateUnknown
)

// repairJob tracks a partially converted file that needs repair.
type repairJob struct {
	path   string
	offset int64 // byte offset where XOR'd data begins
}

func main() {
	blocksDir := flag.String("blocks-dir", "", "Path to Bitcoin Core blocks directory (required)")
	keyHex := flag.String("key", "", "XOR key in hex (use when xor.dat is already zeroed)")
	dryRun := flag.Bool("dry-run", false, "Detect and show file states without modifying")
	repair := flag.Bool("repair", false, "Detect and fix partially converted files (interrupted xorstrip)")
	workers := flag.Int("workers", runtime.NumCPU(), "Number of parallel workers")
	flag.Parse()

	if *blocksDir == "" {
		fmt.Fprintln(os.Stderr, "error: --blocks-dir is required")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(*blocksDir, *keyHex, *dryRun, *repair, *workers); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(blocksDir, keyHex string, dryRun, repair bool, workers int) error {
	key, err := loadKey(blocksDir, keyHex)
	if err != nil {
		return err
	}

	fmt.Printf("XOR key: %s\n", hex.EncodeToString(key))

	files, err := collectBlockFiles(blocksDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("No blk/rev files found.")
		return nil
	}

	var toConvert []string
	var toRepair []repairJob
	var rawCount, unknownCount int

	for _, path := range files {
		state, network := detectState(path, key)
		name := filepath.Base(path)
		switch state {
		case stateRaw:
			if repair && strings.HasPrefix(name, "blk") {
				boundary, ferr := findConversionBoundary(path)
				if ferr == nil && boundary >= 0 {
					toRepair = append(toRepair, repairJob{path: path, offset: boundary})
					fmt.Printf("  [PARTIAL]    %s - raw until %d MiB, XOR'd after\n", name, boundary/(1024*1024))
					continue
				}
			}
			rawCount++
			if dryRun {
				fmt.Printf("  [raw]        %s (%s)\n", name, network)
			}
		case stateObfuscated:
			toConvert = append(toConvert, path)
			if dryRun {
				fmt.Printf("  [obfuscated] %s (%s)\n", name, network)
			}
		case stateUnknown:
			unknownCount++
			fmt.Fprintf(os.Stderr, "  [UNKNOWN]    %s - magic mismatch, skipping\n", name)
		}
	}

	fmt.Printf("\n%d files: %d raw (skip), %d obfuscated (convert), %d partial (repair), %d unknown (skip)\n",
		len(files), rawCount, len(toConvert), len(toRepair), unknownCount)

	if dryRun {
		return nil
	}

	if len(toConvert) == 0 && len(toRepair) == 0 {
		fmt.Println("All files are already raw.")
		return zeroXorDat(blocksDir)
	}

	if unknownCount > 0 {
		fmt.Fprintf(os.Stderr, "\nWARNING: %d files have unknown state and will be skipped.\n", unknownCount)
	}

	start := time.Now()
	var totalBytes atomic.Int64
	var firstErr error
	var errOnce sync.Once

	if len(toConvert) > 0 {
		fmt.Printf("\nConverting %d files with %d workers...\n\n", len(toConvert), workers)

		var done atomic.Int32
		jobs := make(chan string, workers)
		var wg sync.WaitGroup

		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for path := range jobs {
					n, err := xorFileInPlace(path, key)
					if err != nil {
						errOnce.Do(func() {
							firstErr = fmt.Errorf("convert %s: %w", filepath.Base(path), err)
						})
						fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", filepath.Base(path), err)
						continue
					}
					totalBytes.Add(n)
					cur := done.Add(1)
					fmt.Printf("[%d/%d] %s (%d MiB) OK\n", cur, len(toConvert), filepath.Base(path), n/(1024*1024))
				}
			}()
		}

		for _, path := range toConvert {
			jobs <- path
		}
		close(jobs)
		wg.Wait()
	}

	if firstErr != nil {
		return firstErr
	}

	if len(toRepair) > 0 {
		fmt.Printf("\nRepairing %d partially converted files...\n\n", len(toRepair))

		var done atomic.Int32
		type repairResult struct {
			path   string
			offset int64
			n      int64
			err    error
		}
		repairJobs := make(chan repairJob, workers)
		results := make(chan repairResult, len(toRepair))
		var wg sync.WaitGroup

		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range repairJobs {
					n, err := xorFileFromOffset(job.path, key, job.offset)
					results <- repairResult{path: job.path, offset: job.offset, n: n, err: err}
					if err != nil {
						errOnce.Do(func() {
							firstErr = fmt.Errorf("repair %s: %w", filepath.Base(job.path), err)
						})
					} else {
						totalBytes.Add(n)
					}
					cur := done.Add(1)
					fmt.Printf("[%d/%d] REPAIR %s from offset %d MiB (%d MiB stripped)\n",
						cur, len(toRepair), filepath.Base(job.path), job.offset/(1024*1024), n/(1024*1024))
				}
			}()
		}

		for _, job := range toRepair {
			repairJobs <- job
		}
		close(repairJobs)
		wg.Wait()
		close(results)
	}

	if firstErr != nil {
		return firstErr
	}

	if err := zeroXorDat(blocksDir); err != nil {
		return err
	}

	elapsed := time.Since(start)
	total := float64(totalBytes.Load())
	if total > 0 && elapsed.Seconds() > 0 {
		fmt.Printf("\nDone: %.1f GB in %s (%.0f MB/s)\n",
			total/(1024*1024*1024), elapsed.Round(time.Second), total/(1024*1024)/elapsed.Seconds())
	} else {
		fmt.Println("\nDone.")
	}
	return nil
}

func loadKey(blocksDir, keyHex string) ([]byte, error) {
	if keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return nil, fmt.Errorf("invalid --key hex: %w", err)
		}
		if len(key) != xorKeySize {
			return nil, fmt.Errorf("--key must be %d bytes (%d hex chars)", xorKeySize, xorKeySize*2)
		}
		return key, nil
	}

	xorPath := filepath.Join(blocksDir, "xor.dat")
	data, err := os.ReadFile(xorPath)
	if err != nil {
		return nil, fmt.Errorf("read xor.dat: %w\nuse --key to provide key manually", err)
	}

	switch len(data) {
	case xorKeySize:
	case xorKeySize + 1:
		if data[0] == byte(xorKeySize) {
			data = data[1:]
		} else {
			return nil, fmt.Errorf("xor.dat: unexpected %d-byte format", len(data))
		}
	default:
		return nil, fmt.Errorf("xor.dat has %d bytes, expected %d or %d", len(data), xorKeySize, xorKeySize+1)
	}

	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, fmt.Errorf("xor.dat is all zeros (key already cleared)\nuse --key to provide the original key")
	}

	return data, nil
}

func detectState(path string, key []byte) (fileState, string) {
	f, err := os.Open(path)
	if err != nil {
		return stateUnknown, ""
	}
	defer f.Close()

	var buf [4]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return stateUnknown, ""
	}

	magic := binary.LittleEndian.Uint32(buf[:])
	if network, ok := networkMagics[magic]; ok {
		return stateRaw, network
	}

	var xored [4]byte
	for i := range xored {
		xored[i] = buf[i] ^ key[i]
	}
	xoredMagic := binary.LittleEndian.Uint32(xored[:])
	if network, ok := networkMagics[xoredMagic]; ok {
		return stateObfuscated, network
	}

	return stateUnknown, ""
}

func zeroXorDat(blocksDir string) error {
	xorPath := filepath.Join(blocksDir, "xor.dat")
	data, err := os.ReadFile(xorPath)
	if err != nil {
		return nil
	}

	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil
	}

	if err := os.WriteFile(xorPath, make([]byte, len(data)), 0644); err != nil {
		return fmt.Errorf("zero xor.dat: %w", err)
	}
	fmt.Println("xor.dat zeroed out.")
	return nil
}

func collectBlockFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != dir {
			return fs.SkipDir
		}
		name := d.Name()
		if (strings.HasPrefix(name, "blk") || strings.HasPrefix(name, "rev")) && strings.HasSuffix(name, ".dat") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func xorFileInPlace(path string, key []byte) (int64, error) {
	return xorFileFromOffset(path, key, 0)
}

func xorFileFromOffset(path string, key []byte, startOffset int64) (int64, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, bufSize)
	offset := startOffset

	for {
		n, err := f.ReadAt(buf[:], offset)
		if n > 0 {
			chunk := buf[:n]
			for i := range chunk {
				chunk[i] ^= key[(offset+int64(i))%int64(len(key))]
			}
			if _, werr := f.WriteAt(chunk, offset); werr != nil {
				return offset - startOffset, werr
			}
			offset += int64(n)
		}
		if err != nil {
			break
		}
	}

	return offset - startOffset, f.Sync()
}

var rawMagic = []byte{0xf9, 0xbe, 0xb4, 0xd9}

func findConversionBoundary(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return -1, err
	}

	buf := make([]byte, bufSize)
	for offset := int64(0); offset < info.Size(); offset += int64(bufSize) {
		n, err := f.ReadAt(buf, offset)
		if n == 0 && err != nil {
			break
		}
		if !bytes.Contains(buf[:n], rawMagic) {
			return offset, nil
		}
	}
	return -1, nil
}
