package queue

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This source-level guard complements the status-only authority fake and
// read-only provider subprocess: newly added queue paths cannot invoke a
// claim lifecycle/guard method or introduce provider write commands unnoticed.
func TestQueueHasNoMutationCallPath(t *testing.T) {
	forbidden := map[string]bool{
		"Acquire": true, "Heartbeat": true, "Checkpoint": true,
		"Transfer": true, "Release": true, "BeginOperation": true,
		"RenewOperation": true, "CompleteOperation": true, "Reconcile": true,
		"ReconcileAtCurrentRevision": true, "ReplaceFile": true, "Execute": true,
	}
	for _, directory := range []string{".", filepath.Join("..", "cli")} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || directory != "." && !strings.HasPrefix(entry.Name(), "queue_") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && forbidden[selector.Sel.Name] {
					t.Errorf("%s calls forbidden mutation %s", path, selector.Sel.Name)
				}
				// Backlog.md's CLI is a read-only source in this slice.
				if strings.HasSuffix(path, "backlog.go") {
					var args []string
					for _, arg := range call.Args {
						if literal, ok := arg.(*ast.BasicLit); ok && literal.Kind == token.STRING {
							value, _ := strconv.Unquote(literal.Value)
							args = append(args, value)
						}
					}
					joined := " " + strings.Join(args, " ") + " "
					for _, command := range []string{" task edit ", " task create ", " task archive ", " task delete ", " git push ", " git commit "} {
						if strings.Contains(joined, command) {
							t.Errorf("%s invokes provider mutation %q", path, command)
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
