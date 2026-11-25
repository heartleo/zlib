// Package guard holds architecture tests: rules about how the code is built
// that a reviewer would otherwise have to remember. It has no non-test code.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectExempt lists the http.Client literals that follow redirects on
// purpose. A client that carries a credential must not: see zlib.checkRedirect,
// which strips the remix-* headers when a redirect leaves the allowlist.
var redirectExempt = map[string]string{
	"cli/update_release.go": "release assets redirect to GitHub's CDN and the request carries no credential",
}

func goFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(filepath.ToSlash(path), "../")] = f
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// Every http.Client says what it does on a redirect. The default follows them
// all and forwards what it can, which is how credentials reached other hosts.
func TestEveryHTTPClientDecidesRedirects(t *testing.T) {
	for path, f := range goFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if !isSelector(n.Type, "http", "Client") {
					return true
				}
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "CheckRedirect" {
							return true
						}
					}
				}
				if _, ok := redirectExempt[path]; !ok {
					t.Errorf("%s: an http.Client without CheckRedirect follows every redirect. Set CheckRedirect (zlib.checkRedirect for a client that carries a credential, or http.ErrUseLastResponse to refuse), or add the file to redirectExempt in internal/guard with the reason", path)
				}
			case *ast.SelectorExpr:
				if isSelector(n, "http", "DefaultClient") || isSelector(n, "http", "Get") || isSelector(n, "http", "Post") {
					t.Errorf("%s: http.%s uses the shared default client with no timeout or redirect policy. Build a request with http.NewRequestWithContext and send it through a client that sets Timeout and CheckRedirect", path, n.Sel.Name)
				}
			}
			return true
		})
	}
}

// A command returns its error and Execute turns it into an exit code (see
// docs/usage.md#exit-codes). An os.Exit deeper down skips deferred cleanup such
// as removing a .part file and bypasses the code table.
func TestOnlyExecuteExits(t *testing.T) {
	for path, f := range goFiles(t) {
		if path == "cli/root.go" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && (isSelector(call.Fun, "os", "Exit") || isSelector(call.Fun, "log", "Fatal") || isSelector(call.Fun, "log", "Fatalf")) {
				t.Errorf("%s: exits the process directly. Return the error from RunE and let cli.Execute pick the exit code", path)
			}
			return true
		})
	}
}

// The binary entry must stay anchored: a bare "zlib" would also ignore
// plugins/zlib/, the generated plugin the marketplace installs.
func TestGitignoreAnchorsTheBinary(t *testing.T) {
	data, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	var anchored bool
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case "zlib", "zlib/", "**/zlib":
			t.Errorf(".gitignore has %q, which also ignores plugins/zlib/. Write /zlib", strings.TrimSpace(line))
		case "/zlib":
			anchored = true
		}
	}
	if !anchored {
		t.Error(".gitignore has no /zlib entry for the built binary")
	}
}
