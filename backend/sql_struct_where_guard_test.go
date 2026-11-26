package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Structural guard against struct conditions: with Where(&model) GORM omits
// every field with a zero value. A lookup of an identity with an empty
// subject or issuer then matches arbitrary rows and would have logged an attacker in
// as a foreign account. Conditions are therefore written as text with parameters
// ("a = ? AND b = ?"), every column explicit.
//
// Where, Not and Or with a struct as the first argument are forbidden: a
// variable or a field (&key, key) as well as a literal (&models.X{...}). One
// exception is listed below and has its reason: a literal whose field is never empty.
var allowedStructConditions = map[string]string{
	"handlers/oidc_login.go:Where:&models.StatusPageSSOConfig{OIDCEnabled: true}": "das einzige Feld ist das feste Literal true, kein Nullwert möglich",
}

func TestNoStructConditionsInQueries(t *testing.T) {
	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || (strings.HasPrefix(d.Name(), ".") && path != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Where" && sel.Sel.Name != "Not" && sel.Sel.Name != "Or") {
				return true
			}
			if isStructCondition(call.Args[0]) {
				arg := string(src[fset.Position(call.Args[0].Pos()).Offset:fset.Position(call.Args[0].End()).Offset])
				found = append(found, filepath.ToSlash(path)+":"+sel.Sel.Name+":"+arg)
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, site := range found {
		seen[site] = true
		reason, allowed := allowedStructConditions[site]
		assert.True(t, allowed, "Struktur-Bedingung %s: GORM ließe Nullwerte weg. Spalten ausdrücklich als Text mit Parametern vergleichen (siehe ssoIdentityScope)", site)
		assert.NotEmpty(t, reason)
	}
	for site := range allowedStructConditions {
		assert.True(t, seen[site], "Ausnahme %s wird nicht mehr gebraucht: aus der Liste streichen", site)
	}
}

// isStructCondition: &x, x.y pointers to variables, literals and pointers to them.
func isStructCondition(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		return v.Op == token.AND
	case *ast.CompositeLit:
		_, isMap := v.Type.(*ast.MapType)
		return !isMap // map conditions keep zero values
	}
	return false
}

// Control case: the checker detects the forbidden forms and lets text through.
func TestIsStructCondition(t *testing.T) {
	parse := func(src string) ast.Expr {
		e, err := parser.ParseExpr(src)
		require.NoError(t, err)
		return e
	}
	for src, want := range map[string]bool{
		"&key":                       true,
		"&models.X{A: 1}":            true,
		"models.X{A: 1}":             true,
		"map[string]any{\"a\": 0}":   false,
		"\"a = ? AND b = ?\"":        false,
		"someConstant":               false,
		"\"a = ?\" + col":            false,
		"&struct{ A int }{A: 1}":     true,
		"fmt.Sprintf(\"%s\", \"a\")": false,
	} {
		assert.Equal(t, want, isStructCondition(parse(src)), src)
	}
}
