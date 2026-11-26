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

// Structure guard for the mail queues: every call to queueMail names
// a purpose as a text literal (each purpose has its own queue) and
// the keys by which the share is limited. Without its own purpose, two functions would share
// the slots; without keys, a single page
// or source could fill the queue alone. A new call trips this test here
// and belongs in the table together with its purpose and keys.
var expectedMailQueueCallSites = map[string]string{
	// file -> purpose and keys (in the order of the arguments)
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
