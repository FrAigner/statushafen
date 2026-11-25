package handlers

import (
	"encoding/json"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OIDC login errors reach the browser as ?oidc_error=<code> (oidcErrorURL) and the frontend
// translates them under auth.oidcErrors.<code>; an unknown code only shows the generic
// "unknown" text. Every code declared as an oidcError literal therefore needs a translation in
// both UI languages. "internal" is deliberately shown with the generic text.
func TestOIDCErrorCodes_HaveTranslations(t *testing.T) {
	used := map[string]bool{}
	for _, name := range errorSourceFiles(t) {
		ast.Inspect(parseHandlerFile(t, name), func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || len(lit.Elts) < 2 {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "oidcError" {
				return true
			}
			if s, ok := lit.Elts[1].(*ast.BasicLit); ok {
				if code, err := strconv.Unquote(s.Value); err == nil && code != "internal" {
					used[code] = true
				}
			}
			return true
		})
	}
	require.Contains(t, used, "provider_unknown", "the scan must find the oidcError literals")

	for _, lang := range []string{"de", "en"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "locales", lang+".json"))
		require.NoError(t, err)
		var doc struct {
			Auth struct {
				OIDCErrors map[string]string `json:"oidcErrors"`
			} `json:"auth"`
		}
		require.NoError(t, json.Unmarshal(raw, &doc))
		var missing []string
		for code := range used {
			if doc.Auth.OIDCErrors[code] == "" {
				missing = append(missing, code)
			}
		}
		sort.Strings(missing)
		assert.Empty(t, missing, "auth.oidcErrors.<code> missing in %s.json", lang)
	}
}
