package release

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
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
	if _, err := WriteChecksums(directory); err != nil {
		t.Fatal(err)
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
