package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Strukturwächter: Jede Route, deren Handler (auch über Hilfsfunktionen) Mails
// an Abonnenten, Mitglieder oder Empfänger versendet, hat ein Limit je Client
// oder je Nutzer und Seite. Welche Handler Mails auslösen, ermittelt der Test
// aus dem Quelltext des Pakets handlers (Aufrufgraph nach Funktionsnamen bis
// deliverMail, dem einzigen Versandweg). Dann ruft er jede solche Route
// wiederholt als Global Admin auf und erwartet irgendwann 429. Ein neuer
// Handler mit Mailversand ohne Limit fällt damit auf, ohne dass jemand die
// Liste pflegen muss.

// handlersReachingMail liefert die Namen aller Funktionen in handlers, die
// deliverMail direkt oder indirekt aufrufen. Der Abgleich nach Namen kann zu
// viel melden (gleichnamige Methoden), nie zu wenig.
func handlersReachingMail(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("handlers/*.go")
	require.NoError(t, err)
	calls := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		require.NoError(t, err)
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if calls[fd.Name.Name] == nil {
				calls[fd.Name.Name] = map[string]bool{}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if ce, ok := n.(*ast.CallExpr); ok {
					switch fn := ce.Fun.(type) {
					case *ast.Ident:
						calls[fd.Name.Name][fn.Name] = true
					case *ast.SelectorExpr:
						calls[fd.Name.Name][fn.Sel.Name] = true
					}
				}
				return true
			})
		}
	}
	reach := map[string]bool{"deliverMail": true}
	for changed := true; changed; {
		changed = false
		for fn, callees := range calls {
			if reach[fn] {
				continue
			}
			for callee := range callees {
				if reach[callee] {
					reach[fn], changed = true, true
					break
				}
			}
		}
	}
	return reach
}

// routeHandlerName: "…/handlers.(*Handler).CreateIncidentHandler-fm" -> "CreateIncidentHandler".
func routeHandlerName(full string) string {
	name := full[strings.LastIndex(full, ".")+1:]
	return strings.TrimSuffix(name, "-fm")
}

type mailRouteFixture struct {
	r     *gin.Engine
	token string
	page  string
}

func setupMailRouteFixture(t *testing.T) mailRouteFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "Mail", Slug: "mail-limit"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Incident{Title: "V", StatusPageID: page.ID}).Error)
	require.NoError(t, db.Create(&models.GeneralNotice{Title: "A", StatusPageID: page.ID}).Error)
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "leser@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)
	admin := models.User{Email: "admin-limit@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, err := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	require.NoError(t, err)
	return mailRouteFixture{r: r, token: tok, page: strconv.FormatUint(uint64(page.ID), 10)}
}

func (f mailRouteFixture) call(method, path, addr string) int {
	path = strings.ReplaceAll(path, ":page_id", f.page)
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "1"
		}
	}
	req := httptest.NewRequest(method, strings.Join(parts, "/"), strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.RemoteAddr = addr
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w.Code
}

func TestRouter_EveryMailSendingRouteIsRateLimited(t *testing.T) {
	reach := handlersReachingMail(t)
	for _, known := range []string{"CreateIncidentHandler", "UpdateIncidentHandler", "AddIncidentUpdateHandler", "CreateNoticeHandler", "UpdateNoticeHandler", "UpdateSubscriberHandler"} {
		require.True(t, reach[known], "%s müsste Mails auslösen, der Aufrufgraph ist unvollständig", known)
	}
	f := setupMailRouteFixture(t)
	const maxRequests = 400 // über jeder Stundengrenze der Begrenzer

	var checked, offenders []string
	for i, ri := range f.r.Routes() {
		if ri.Method == http.MethodGet || ri.Method == http.MethodHead || ri.Method == http.MethodOptions {
			continue
		}
		if !reach[routeHandlerName(ri.Handler)] {
			continue
		}
		key := ri.Method + " " + ri.Path
		checked = append(checked, key)
		addr := "203.0.113." + strconv.Itoa(i%250+1) + ":4000" // eigene Adresse je Route
		limited := false
		for n := 0; n < maxRequests && !limited; n++ {
			limited = f.call(ri.Method, ri.Path, addr) == http.StatusTooManyRequests
		}
		if !limited {
			offenders = append(offenders, key)
		}
	}
	sort.Strings(checked)
	t.Logf("%d Routen mit Mailversand geprüft: %v", len(checked), checked)
	require.GreaterOrEqual(t, len(checked), 8, "Routen mit Mailversand nicht gefunden")
	assert.Empty(t, offenders, "Routen mit Mailversand ohne Limit")
}

// Angriff und Gegenprobe an den echten Routen: Nach 30 neuen Vorfällen je
// Stunde und Seite folgt 429 mit Retry-After; normale Nutzung bleibt frei.
func TestRouter_IncidentCreationIsLimitedPerPage(t *testing.T) {
	f := setupMailRouteFixture(t)
	post := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/pages/"+f.page+"/incidents", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		return w
	}
	// Normale Nutzung: 30 Anlagen (hier mit leerem Body, also 400) werden nie gedrosselt.
	for i := 0; i < 30; i++ {
		w := post("198.51.100.10:1")
		require.NotEqual(t, http.StatusTooManyRequests, w.Code, "Aufruf %d", i+1)
	}
	// Angriff: der 31. Aufruf scheitert, auch von einer anderen IP.
	w := post("198.51.100.99:1")
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.Greater(t, retry, 0)
	assert.LessOrEqual(t, retry, 3600)
}
