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
		ast.Inspect(f, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && path == "cli/root.go" && fn.Recv == nil && fn.Name.Name == "Execute" {
				return false
			}
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

// FetchBookDetails runs requests on several goroutines, so the cookie map is
// shared. cookiesMu guards it, and the only code allowed to touch the map is the
// three functions that take the lock; everything else reads cookieSnapshot().
func TestCookieMapIsOnlyTouchedUnderItsLock(t *testing.T) {
	allowed := map[string]bool{"Logout": true, "cookieSnapshot": true, "SetCookies": true}
	for path, f := range goFiles(t) {
		if !strings.HasPrefix(path, "zlib/") {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || allowed[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "cookies" {
					t.Errorf("%s: %s reads Client.cookies without cookiesMu. Call c.cookieSnapshot() to read it or c.SetCookies() to replace it", path, fn.Name.Name)
				}
				return true
			})
		}
	}
}

// A full-screen page that builds its own tea.View skips the theme background and
// foreground, which left a light theme unreadable on a dark terminal on every
// screen but the theme picker. screenView and fullScreenView in tui_layout.go are
// the one place views are made; update_progress.go is the inline progress line
// that must not paint the terminal.
func TestViewsAreOnlyBuiltByTheLayoutHelpers(t *testing.T) {
	allowed := map[string]bool{"cli/tui_layout.go": true, "cli/update_progress.go": true}
	for path, f := range goFiles(t) {
		if !strings.HasPrefix(path, "cli/") || allowed[path] {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if isSelector(n.Fun, "tea", "NewView") {
					t.Errorf("%s: tea.NewView skips the theme background. Return screenView(...) or fullScreenView(...) from tui_layout.go", path)
				}
			case *ast.CompositeLit:
				if isSelector(n.Type, "tea", "View") {
					t.Errorf("%s: a tea.View literal skips the theme background. Return screenView(...) or fullScreenView(...) from tui_layout.go", path)
				}
			}
			return true
		})
	}
}

// The catalogue, AI, file and WeRead packages talk to third parties, so they
// must have no way to reach the Z-Library session: they import neither the
// Z-Library client nor the session store. internal/cli hands them what they need
// (a proxy, a key the user typed for that service) and nothing else.
func TestThirdPartyPackagesCannotSeeTheSession(t *testing.T) {
	forbidden := []string{"github.com/heartleo/zlib/internal/zlib", "github.com/heartleo/zlib/internal/config"}
	for path, f := range goFiles(t) {
		switch {
		case strings.HasPrefix(path, "metasource/"), strings.HasPrefix(path, "aimatch/"),
			strings.HasPrefix(path, "ebookmeta/"), strings.HasPrefix(path, "weread/"):
		default:
			continue
		}
		for _, imp := range f.Imports {
			for _, bad := range forbidden {
				if strings.Trim(imp.Path.Value, `"`) == bad {
					t.Errorf("%s imports %s, which holds the Z-Library session. Pass the value this package needs in as an argument or option from internal/cli", path, bad)
				}
			}
		}
	}
}

var (
	narrowTypes = map[string]bool{"int8": true, "int16": true, "int32": true, "uint8": true, "uint16": true, "uint32": true}
	parseFuncs  = map[string]bool{"ParseInt": true, "ParseUint": true, "Atoi": true}
)

// narrowingWithoutBounds reports each conversion of a parsed number to a type that
// cannot hold every value of the parse, in a function that does not compare the
// number with both a lower and an upper bound. A value read from a file or a server is then truncated
// silently: PDF integers and WeRead sizes were converted this way. The fix is a
// range check before the conversion, or parsing with the target bit size so an
// out-of-range value is an error.
func narrowingWithoutBounds(fset *token.FileSet, f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		parsed := map[string]bool{}
		lessThan, greaterThan := map[string]bool{}, map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Rhs) == 1 && len(n.Lhs) >= 1 {
					if call, ok := n.Rhs[0].(*ast.CallExpr); ok && isParse(call) {
						if id, ok := n.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
							parsed[id.Name] = true
						}
					}
				}
			case *ast.BinaryExpr:
				switch n.Op {
				case token.LSS, token.GTR, token.LEQ, token.GEQ:
					// x < c and c > x both say "x is below something".
					leftIsLess := n.Op == token.LSS || n.Op == token.LEQ
					for i, side := range []ast.Expr{n.X, n.Y} {
						below := leftIsLess == (i == 0)
						ast.Inspect(side, func(m ast.Node) bool {
							if id, ok := m.(*ast.Ident); ok {
								if below {
									lessThan[id.Name] = true
								} else {
									greaterThan[id.Name] = true
								}
							}
							return true
						})
					}
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || !narrowTypes[id.Name] {
				return true
			}
			ast.Inspect(call.Args[0], func(m ast.Node) bool {
				if v, ok := m.(*ast.Ident); ok && parsed[v.Name] && !(lessThan[v.Name] && greaterThan[v.Name]) {
					out = append(out, fset.Position(call.Pos()).String()+": "+id.Name+"("+v.Name+") truncates a parsed number without a lower and an upper bound. Compare "+v.Name+" with both bounds first, or parse with the target bit size so an out-of-range value is an error")
				}
				return true
			})
			return true
		})
	}
	return out
}

func isParse(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "strconv" && parseFuncs[sel.Sel.Name]
}

func TestParsedNumbersAreRangeCheckedBeforeNarrowing(t *testing.T) {
	fset := token.NewFileSet()
	for path := range goFiles(t) {
		f, err := parser.ParseFile(fset, filepath.Join("..", path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range narrowingWithoutBounds(fset, f) {
			t.Error(msg)
		}
	}
}

// The checker is shown against the mistake it exists for: a 64-bit parse
// converted straight to a 16-bit field.
func TestNarrowingCheckerCatchesTheOriginalMistake(t *testing.T) {
	check := func(src string) []string {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", "package p\nimport \"strconv\"\n"+src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return narrowingWithoutBounds(fset, f)
	}
	bad := `func f(s string) uint16 { n, _ := strconv.ParseInt(s, 10, 64); return uint16(n) }`
	if got := check(bad); len(got) != 1 {
		t.Fatalf("the unchecked conversion was not reported: %v", got)
	}
	oneSided := `func f(s string) uint8 { n, _ := strconv.Atoi(s); if n > 0 { return uint8(n) }; return 0 }`
	if got := check(oneSided); len(got) != 1 {
		t.Fatalf("a one-sided bound was accepted, but it still truncates everything above 255: %v", got)
	}
	good := `func f(s string) uint16 { n, _ := strconv.ParseInt(s, 10, 64); if n < 0 || n > 65535 { return 0 }; return uint16(n) }`
	if got := check(good); len(got) != 0 {
		t.Fatalf("a range-checked conversion was reported: %v", got)
	}
}
