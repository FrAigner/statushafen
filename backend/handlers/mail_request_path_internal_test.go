package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Structure guard: an HTTP handler (function with *gin.Context) must not send
// a mail directly. SMTP sending can hang until the overall deadline and
// would reveal through response time whether a mail is sent for an address. Mails
// run through queueMail (bounded background queue); sending whose
// result determines the response would need a justified exception here.
var blockingMailFuncs = map[string]bool{
	"deliverMail":                  true,
	"sendMailWithTimeout":          true,
	"sendAlertEmail":               true,
	"sendOrderConfirmationEmail":   true,
	"sendSubscriptionConfirmation": true,
	"sendOutageMail":               true,
	"sendOutageMails":              true,
	"sendExternalIncidentMail":     true,
}

// blockingMailCalls returns "function: call" for every direct mail send
// in a function with a *gin.Context parameter, except calls inside
// a function literal that is passed to queueMail.
func blockingMailCalls(file *ast.File) []string {
	var found []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !hasGinContextParam(fn) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calleeName(call)
			if name == "queueMail" {
				return false // sending in the background job is intended
			}
			if blockingMailFuncs[name] {
				found = append(found, fn.Name.Name+": "+name)
			}
			return true
		})
	}
	return found
}

func hasGinContextParam(fn *ast.FuncDecl) bool {
	for _, p := range fn.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Context" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "gin" {
				return true
			}
		}
	}
	return false
}

func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func TestBlockingMailCalls_CheckerFlagsDirectCallsOnly(t *testing.T) {
	const src = `package x
import "github.com/gin-gonic/gin"
func (h *H) Direkt(c *gin.Context) { h.sendOrderConfirmationEmail(cfg, order) }
func (h *H) Gequeued(c *gin.Context) { h.queueMail("x", func() { h.sendOrderConfirmationEmail(cfg, order) }) }
func (h *H) OhneKontext() { h.deliverMail(cfg, "a", nil, t) }
`
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"Direkt: sendOrderConfirmationEmail"}, blockingMailCalls(file))
}

// blockingMailExceptions: handlers whose response is the result of the send.
var blockingMailExceptions = map[string]string{
	"subscriber_test_message.go sendSubscriberTestMail: deliverMail": "test message: the admin waits for the result; authenticated, 5/min, admin-entered address",
}

func TestHandlers_SendNoMailInsideRequest(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var offenders []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err, name)
		for _, o := range blockingMailCalls(file) {
			if _, ok := blockingMailExceptions[name+" "+o]; !ok {
				offenders = append(offenders, name+" "+o)
			}
		}
	}
	sort.Strings(offenders)
	assert.Empty(t, offenders, "Mail in der Anfrage senden blockiert sie bis zur SMTP-Frist; über h.queueMail versenden")
}

// Only smtp.go speaks SMTP: the dial timeout and overall deadline live there. A
// second call to smtp.SendMail/smtp.Dial elsewhere would bypass both.
func TestSMTP_OnlyUsedInSMTPFile(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == `"net/smtp"` && filepath.ToSlash(path) != "../handlers/smtp.go" {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "net/smtp nur in handlers/smtp.go (Timeouts)")
}
