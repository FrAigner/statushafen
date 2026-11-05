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

// Strukturwächter gegen Struktur-Bedingungen: GORM lässt bei Where(&modell)
// jedes Feld mit Nullwert weg. Eine Suche nach einer Identität mit leerem
// Subject oder Issuer trifft dann beliebige Zeilen und meldete einen Angreifer
// als fremdes Konto an. Bedingungen stehen deshalb als Text mit Parametern
// ("a = ? AND b = ?"), jede Spalte ausdrücklich.
//
// Verboten sind Where, Not und Or mit einer Struktur als erstem Argument: eine
// Variable oder ein Feld (&key, key) sowie ein Literal (&models.X{...}). Eine
// Ausnahme steht unten und hat ihren Grund: ein Literal, dessen Feld nie leer ist.
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

// isStructCondition: &x, x.y-Zeiger auf Variablen, Literale und Zeiger darauf.
func isStructCondition(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		return v.Op == token.AND
	case *ast.CompositeLit:
		_, isMap := v.Type.(*ast.MapType)
		return !isMap // Map-Bedingungen behalten Nullwerte
	}
	return false
}

// Gegenprobe: Der Prüfer erkennt die verbotenen Formen und lässt Text zu.
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
