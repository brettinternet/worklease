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

// This source-level guard complements the provider read-only tests. Queue
// claim lifecycle methods are confined to the dedicated owner controller;
// provider writes and guarded operations remain unavailable.
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
					if !queueClaimAuthorityMutation(path, selector) {
						t.Errorf("%s calls forbidden mutation %s", path, selector.Sel.Name)
					}
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

func queueClaimAuthorityMutation(path string, call *ast.SelectorExpr) bool {
	name := filepath.Base(path)
	if name == "queue_claim.go" && call.Sel.Name == "Acquire" {
		api, ok := call.X.(*ast.SelectorExpr)
		if !ok || api.Sel.Name != "API" {
			return false
		}
		backend, ok := api.X.(*ast.SelectorExpr)
		if !ok || backend.Sel.Name != "backend" {
			return false
		}
		controller, ok := backend.X.(*ast.Ident)
		return ok && controller.Name == "c"
	}
	if name != "queue_lifecycle.go" || call.Sel.Name != "Heartbeat" && call.Sel.Name != "Release" {
		return false
	}
	api, ok := call.X.(*ast.SelectorExpr)
	if !ok || api.Sel.Name != "API" {
		return false
	}
	backend, ok := api.X.(*ast.SelectorExpr)
	if !ok || backend.Sel.Name != "backend" {
		return false
	}
	controller, ok := backend.X.(*ast.SelectorExpr)
	if !ok || controller.Sel.Name != "controller" {
		return false
	}
	owner, ok := controller.X.(*ast.Ident)
	return ok && owner.Name == "l"
}
