package release

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	BinaryMember = "bin/worklease"
	ManMember    = "share/man/man1/worklease.1"
)

// CreateArchive packages the built binary and manual at stable install paths.
func CreateArchive(path, binary, manual string) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range []struct {
		path string
		name string
		mode int64
	}{{binary, BinaryMember, 0o755}, {manual, ManMember, 0o644}} {
		data, readErr := os.ReadFile(entry.path)
		if readErr != nil {
			return readErr
		}
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(data)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(data); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

// WriteChecksums writes an exact SHA-256 manifest for every archive.
func WriteChecksums(directory string) (string, error) {
	entries, err := filepath.Glob(filepath.Join(directory, "worklease-v*.tar.gz"))
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("release directory has no archives")
	}
	sort.Strings(entries)
	manifest := filepath.Join(directory, "checksums.txt")
	output, err := os.OpenFile(manifest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	for _, path := range entries {
		digest, digestErr := digest(path)
		if digestErr != nil {
			output.Close()
			return "", digestErr
		}
		fmt.Fprintf(output, "%s  %s\n", digest, filepath.Base(path))
	}
	return manifest, output.Close()
}

// VerifyChecksums requires the manifest to cover and match every archive.
func VerifyChecksums(directory string) error {
	manifest, err := os.Open(filepath.Join(directory, "checksums.txt"))
	if err != nil {
		return err
	}
	defer manifest.Close()
	archives, _ := filepath.Glob(filepath.Join(directory, "worklease-v*.tar.gz"))
	expected := make(map[string]bool, len(archives))
	for _, archive := range archives {
		expected[filepath.Base(archive)] = true
	}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 2 || filepath.Base(parts[1]) != parts[1] || !expected[parts[1]] || seen[parts[1]] {
			return fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		actual, err := digest(filepath.Join(directory, parts[1]))
		if err != nil || actual != parts[0] {
			return fmt.Errorf("checksum mismatch for %s", parts[1])
		}
		seen[parts[1]] = true
	}
	if err := scanner.Err(); err != nil || len(seen) != len(expected) || len(expected) == 0 {
		return fmt.Errorf("checksum manifest does not exactly cover archives")
	}
	return nil
}

func digest(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
