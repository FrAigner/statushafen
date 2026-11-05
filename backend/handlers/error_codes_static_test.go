package handlers

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rest: Fehlerantworten tragen einen Fehlercode, das
// Frontend übersetzt ihn unter api.errors.<code>. Rohe Go-Fehlertexte
// (err.Error()) gehören nie in eine Antwort.

// Die Ratsche gilt nun für ALLE Handler- und Middleware-Dateien
// (vorher nur die bearbeiteten) und steht auf 0: Jede Fehlerantwort
// (gin.H mit "error") trägt einen "code". Neue Klartext-Fehler ohne Code
// schlagen den Test fehl.
func errorSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range []string{"*.go", "../middleware/*.go", "../main.go"} {
		m, err := filepath.Glob(pattern)
		require.NoError(t, err)
		for _, f := range m {
			if !strings.HasSuffix(f, "_test.go") {
				files = append(files, f)
			}
		}
	}
	require.NotEmpty(t, files)
	return files
}

// codeLuecken sind die wenigen Fehlerantworten ohne code, die sich in Tests nicht
// auslösen lassen (Zufallsquelle/JWT-Signatur versagt, OAuth-Austausch mit dem
// echten Provider) und deshalb vom Coverage-Gate nicht angefasst werden dürfen.
// Eine Ratsche: Steigt die Zahl, schlägt der Test fehl; sinkt sie, muss die
// Grenze im selben PR gesenkt werden (Folgeaufgabe).
var codeLuecken = map[string]int{
	"auth.go":       1,
	"auth_sso.go":   4,
	"webhook.go":    1,
	"page_order.go": 2,
}

// codeForStatusCodes sind die Codes, die codeForStatus (handlers und middleware)
// zurückgeben kann; sie stehen nicht als Literal in gin.H.
var codeForStatusCodes = []string{"invalid_request", "invalid_id", "unauthorized", "forbidden", "not_found", "conflict", "server_error"}

func parseHandlerFile(t *testing.T, name string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
	require.NoError(t, err)
	return f
}

func isGinH(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "gin" && sel.Sel.Name == "H"
}

func keyName(kv *ast.KeyValueExpr) string {
	lit, ok := kv.Key.(*ast.BasicLit)
	if !ok {
		return ""
	}
	s, _ := strconv.Unquote(lit.Value)
	return s
}

func TestFehlerantworten_TragenFehlercode(t *testing.T) {
	for _, name := range errorSourceFiles(t) {
		t.Run(name, func(t *testing.T) {
			without := 0
			ast.Inspect(parseHandlerFile(t, name), func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isGinH(lit) {
					return true
				}
				hasErr, hasCode := false, false
				for _, e := range lit.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					switch keyName(kv) {
					case "error":
						hasErr = true
					case "code":
						hasCode = true
					}
				}
				if hasErr && !hasCode {
					without++
				}
				return true
			})
			assert.Equal(t, codeLuecken[name], without, "Fehlerantworten ohne code in %s (Ratsche)", name)
		})
	}
}

func TestFehlerantworten_KeinRohesErrError(t *testing.T) {
	for _, name := range errorSourceFiles(t) {
		t.Run(name, func(t *testing.T) {
			ast.Inspect(parseHandlerFile(t, name), func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isGinH(lit) {
					return true
				}
				for _, e := range lit.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok || keyName(kv) != "error" {
						continue
					}
					call, ok := kv.Value.(*ast.CallExpr)
					if !ok {
						continue
					}
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" {
						assert.Fail(t, "roher err.Error() in einer Fehlerantwort", "%s", name)
					}
				}
				return true
			})
		})
	}
}

// collectCodes sammelt die Codes einer Datei: "code": "x" in gin.H sowie
// newCodedError("x", ...).
func collectCodes(f *ast.File, into map[string]bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if keyName(x) == "code" {
				if lit, ok := x.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					into[s] = true
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "newCodedError" && len(x.Args) > 0 {
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					into[s] = true
				}
			}
		}
		return true
	})
}

// Jeder vergebene Code der bearbeiteten Dateien hat eine deutsche und
// englische Übersetzung unter api.errors.
func TestFehlercodes_HabenUebersetzung(t *testing.T) {
	used := map[string]bool{}
	for _, name := range errorSourceFiles(t) {
		collectCodes(parseHandlerFile(t, name), used)
	}
	for _, c := range incidentValidationCodes { // Status-/Zeitprüfung (incident.go)
		used[c] = true
	}
	for _, c := range codeForStatusCodes {
		used[c] = true
	}
	for _, c := range []string{"tag_not_found", "tag_page_bound"} { // tag_access.go
		used[c] = true
	}
	require.NotEmpty(t, used)

	for _, lang := range []string{"de", "en"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "locales", lang+".json"))
		require.NoError(t, err)
		var doc struct {
			API struct {
				Errors map[string]string `json:"errors"`
			} `json:"api"`
		}
		require.NoError(t, json.Unmarshal(raw, &doc))
		var missing []string
		for code := range used {
			if doc.API.Errors[code] == "" {
				missing = append(missing, code)
			}
		}
		sort.Strings(missing)
		assert.Empty(t, missing, "Codes ohne api.errors.<code> in %s.json", lang)
	}
}
