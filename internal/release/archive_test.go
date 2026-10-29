package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCreateArchiveUsesStableInstallMembersAndVerifiedChecksums(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "worklease")
	manual := filepath.Join(directory, "worklease.1")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manual, []byte("manual"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "worklease-v1.2.3-linux-x64.tar.gz")
	if err := CreateArchive(archive, binary, manual); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	var names []string
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		names = append(names, header.Name)
	}
	if want := []string{BinaryMember, ManMember}; !reflect.DeepEqual(names, want) {
		t.Fatalf("members=%v want=%v", names, want)
	}
	windows := filepath.Join(directory, "worklease-v1.2.3-windows-x64.zip")
	if err := CreateArchive(windows, binary, manual); err != nil {
		t.Fatal(err)
	}
	zipped, err := zip.OpenReader(windows)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	names = nil
	for _, member := range zipped.File {
		names = append(names, member.Name)
	}
	if want := []string{WindowsBinaryMember, ManMember}; !reflect.DeepEqual(names, want) {
		t.Fatalf("zip members=%v want=%v", names, want)
	}
	manifest, err := WriteChecksums(directory)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(manifest); err != nil || !strings.Contains(string(data), filepath.Base(windows)) {
		t.Fatalf("checksums omit zip archive: %q %v", data, err)
	}
	if err := VerifyChecksums(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksums(directory); err == nil {
		t.Fatal("changed archive passed verification")
	}
}
