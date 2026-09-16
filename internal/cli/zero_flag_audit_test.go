package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	urfave "github.com/urfave/cli/v3"
)

func TestZeroFlagAuditCoversEveryLeafCommand(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "docs", "cli-zero-flag-audit.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string][2]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) != 5 {
			t.Fatalf("malformed zero-flag audit row: %q", line)
		}
		path := strings.Trim(strings.TrimSpace(columns[1]), "`")
		behavior, guidance := strings.TrimSpace(columns[2]), strings.TrimSpace(columns[3])
		if path == "" || behavior == "" || guidance == "" {
			t.Fatalf("incomplete zero-flag audit row: %q", line)
		}
		if _, duplicate := documented[path]; duplicate {
			t.Fatalf("duplicate zero-flag audit row for %q", path)
		}
		documented[path] = [2]string{behavior, guidance}
	}
	root := NewRootCommand("test", "unknown", "unknown", nil, nil)
	registered := map[string]bool{}
	var walk func(*urfave.Command, string)
	walk = func(command *urfave.Command, prefix string) {
		path := strings.TrimSpace(prefix + " " + command.Name)
		if len(command.Commands) == 0 {
			registered[path] = true
			if _, ok := documented[path]; !ok {
				t.Errorf("zero-flag audit is missing %q", path)
			}
			return
		}
		for _, child := range command.Commands {
			walk(child, path)
		}
	}
	for _, command := range root.Commands {
		walk(command, "")
	}
	for path := range documented {
		if !registered[path] {
			t.Errorf("zero-flag audit documents unregistered leaf %q", path)
		}
	}
}
