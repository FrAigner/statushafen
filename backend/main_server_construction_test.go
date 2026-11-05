package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Strukturwächter: Der HTTP-Server entsteht nur über newHTTPServer (mit allen
// Zeitgrenzen). Ein weiteres &http.Server{...}-Literal oder
// http.ListenAndServe/ListenAndServeTLS in main.go liefe ohne ReadHeader-,
// Lese-, Schreib- und Leerlauf-Grenzen (Slowloris).
func serverConstructionOutsideHelper(t *testing.T, src string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0)
	require.NoError(t, err)
	var offenders []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name == "newHTTPServer" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := v.Type.(*ast.SelectorExpr); ok && isPkg(sel.X, "http") && sel.Sel.Name == "Server" {
					offenders = append(offenders, fn.Name.Name+": http.Server{}")
				}
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok && isPkg(sel.X, "http") &&
					(sel.Sel.Name == "ListenAndServe" || sel.Sel.Name == "ListenAndServeTLS") {
					offenders = append(offenders, fn.Name.Name+": http."+sel.Sel.Name)
				}
			}
			return true
		})
	}
	return offenders
}

func isPkg(x ast.Expr, name string) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Name == name
}

func TestMain_ServerOnlyBuiltByNewHTTPServer(t *testing.T) {
	raw, err := readFileForTest("main.go")
	require.NoError(t, err)
	assert.Empty(t, serverConstructionOutsideHelper(t, raw))
	assert.Contains(t, raw, "newHTTPServer(", "main() muss den Server über newHTTPServer bauen")
}

func TestServerConstructionOutsideHelper_FlagsDirectUse(t *testing.T) {
	const bad = `package main
import "net/http"
func main() { srv := &http.Server{Addr: ":1"}; _ = srv }
func andere() { _ = http.ListenAndServe(":2", nil) }
func newHTTPServer() *http.Server { return &http.Server{} }
`
	assert.Equal(t, []string{"main: http.Server{}", "andere: http.ListenAndServe"}, serverConstructionOutsideHelper(t, bad))
}

func readFileForTest(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}
