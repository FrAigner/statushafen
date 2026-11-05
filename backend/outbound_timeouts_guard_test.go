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

// Strukturwächter: Jeder ausgehende Verbindungsaufbau hat eine Zeitgrenze.
// Ein net.Dialer oder http.Client ohne Timeout hält bei einem hängenden Ziel
// (SMTP, Webhook, Identitätsanbieter, externe Statusseite) Goroutinen und
// Anfragen beliebig lange offen. Der Test prüft in allen Quelldateien, dass
// jedes net.Dialer- und http.Client-Literal ein Timeout setzt.
func literalsWithoutTimeout(t *testing.T, name, src string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	require.NoError(t, err)
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		isDialer := pkg.Name == "net" && sel.Sel.Name == "Dialer"
		isClient := pkg.Name == "http" && sel.Sel.Name == "Client"
		if !isDialer && !isClient {
			return true
		}
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Timeout" {
					return true
				}
			}
		}
		found = append(found, name+": "+pkg.Name+"."+sel.Sel.Name+" ohne Timeout")
		return true
	})
	return found
}

func TestOutbound_DialersAndClientsHaveTimeouts(t *testing.T) {
	var offenders []string
	for path := range nonTestGoFiles(t) {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		offenders = append(offenders, literalsWithoutTimeout(t, path, string(raw))...)
	}
	assert.Empty(t, offenders)
}

func TestOutbound_CheckerFlagsMissingTimeout(t *testing.T) {
	const src = `package x
import ("net"; "net/http")
func a() { _ = &net.Dialer{} }
func b() { _ = &http.Client{Transport: nil} }
func c() { _ = &net.Dialer{Timeout: 1}; _ = &http.Client{Timeout: 2} }
`
	assert.Equal(t, []string{"x.go: net.Dialer ohne Timeout", "x.go: http.Client ohne Timeout"}, literalsWithoutTimeout(t, "x.go", src))
}
