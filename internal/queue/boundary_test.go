package queue

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The queue is a leaf consumer: authority and protocol packages must not
// acquire a dependency on the queue or its terminal renderer.
func TestQueueImportBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	for _, name := range []string{"authority", "lease", "mcp", "server", "store"} {
		directory := filepath.Join(root, name)
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				importPath, _ := strconv.Unquote(spec.Path.Value)
				if strings.HasPrefix(importPath, "github.com/brettinternet/worklease/internal/queue") ||
					strings.HasPrefix(importPath, "github.com/charmbracelet/bubbletea") ||
					strings.HasPrefix(importPath, "github.com/charmbracelet/lipgloss") ||
					strings.HasPrefix(importPath, "github.com/charmbracelet/bubbles") {
					t.Errorf("%s imports queue/TUI package %s", path, importPath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
