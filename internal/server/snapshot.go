package server

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CreateBlocksIndexSnapshot creates a tar.zst archive of indexDir.
// Prints the SHA-256 hash of the output file to stdout.
func CreateBlocksIndexSnapshot(indexDir, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	zstdWriter, err := zstd.NewWriter(outFile)
	if err != nil {
		return fmt.Errorf("create zstd writer: %w", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	if err := filepath.Walk(indexDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(indexDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		hdr := &tar.Header{
			Name:    relPath,
			Size:    info.Size(),
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header: %w", err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		if _, err := io.Copy(tarWriter, f); err != nil {
			return fmt.Errorf("copy file to tar: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk index dir: %w", err)
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := zstdWriter.Close(); err != nil {
		return fmt.Errorf("flush zstd: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	hashStr, err := sha256File(outputPath)
	if err != nil {
		return fmt.Errorf("sha256 output: %w", err)
	}
	fmt.Printf("SHA-256: %s\n", hashStr)
	return nil
}

// ExtractTarZst decompresses a zstd-compressed tar archive from r and extracts
// its contents into destDir. It includes path traversal protection.
func ExtractTarZst(r io.Reader, destDir string) error {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("create zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Path traversal protection
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("invalid tar entry path: %s", hdr.Name)
		}

		target := filepath.Join(destDir, cleanName)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", target, err)
			}
			f, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			f.Close()
		}
	}

	return nil
}
