package handlers

import (
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

// Strukturwächter für die Mail-Warteschlangen: Jeder Aufruf von queueMail nennt
// einen Zweck als Textliteral (jeder Zweck hat eine eigene Warteschlange) und
// die Schlüssel, nach denen der Anteil begrenzt wird. Ohne eigenen Zweck teilten
// sich zwei Funktionen die Plätze, ohne Schlüssel könnte eine einzelne Seite
// oder Quelle die Warteschlange allein füllen. Ein neuer Aufruf schlägt hier an
// und gehört samt Zweck und Schlüsseln in die Tabelle.
var expectedMailQueueCallSites = map[string]string{
	// Datei -> Zweck und Schlüssel (in der Reihenfolge der Argumente)
	"subscriber_confirm.go": "subscription_confirmation: mailPageKey, mailSourceKey",
	"page_order.go":         "order_confirmation: mailSourceKey",
	"page_invitation.go":    "page_invitation: mailPageKey",
}

func TestQueueMailCallSitesHavePurposeAndSourceKeys(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	found := map[string]string{}
	purposes := map[string]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "mail_queue.go" {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "queueMail" {
				return true
			}
			require.GreaterOrEqual(t, len(call.Args), 2, "%s: queueMail braucht Zweck und Auftrag", name)
			lit, ok := call.Args[0].(*ast.BasicLit)
			require.True(t, ok && lit.Kind == token.STRING, "%s: der Zweck von queueMail ist ein Textliteral", name)
			purpose, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)

			var keys []string
			for _, arg := range call.Args[2:] {
				keyCall, ok := arg.(*ast.CallExpr)
				require.True(t, ok, "%s: ein Schlüssel ist ein Aufruf von mailPageKey oder mailSourceKey", name)
				ident, ok := keyCall.Fun.(*ast.Ident)
				require.True(t, ok, "%s: Schlüsselfunktion", name)
				keys = append(keys, ident.Name)
			}
			assert.NotContains(t, found, name, "%s: genau ein queueMail-Aufruf je Datei erwartet", name)
			found[name] = purpose + ": " + strings.Join(keys, ", ")
			if other, dup := purposes[purpose]; dup {
				t.Errorf("Zweck %q wird in %s und %s verwendet: jeder Zweck braucht eine eigene Warteschlange", purpose, other, name)
			}
			purposes[purpose] = name
			return true
		})
	}

	var names []string
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		assert.Equal(t, expectedMailQueueCallSites[name], found[name], "%s: Zweck und Schlüssel von queueMail", name)
	}
	for name, want := range expectedMailQueueCallSites {
		assert.Equal(t, want, found[name], "%s: erwarteter queueMail-Aufruf fehlt", name)
	}
}
