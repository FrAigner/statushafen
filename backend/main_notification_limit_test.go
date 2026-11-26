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

// Structural guard: every route whose handler (also via helper functions) sends mails
// to subscribers, members or recipients has a limit per client
// or per user and page. The test determines which handlers trigger mails
// from the source of the handlers package (call graph by function name up to
// deliverMail, the only delivery path). Then it calls each such route
// repeatedly as Global Admin and expects a 429 at some point. A new
// handler that sends mail without a limit is thus caught without anyone having to
// maintain the list.

// handlersReachingMail returns the names of all functions in handlers that
// call deliverMail directly or indirectly. Matching by name can report
// too many (methods with the same name), never too few.
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
	const maxRequests = 400 // the limiter above every hour boundary

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
		addr := "203.0.113." + strconv.Itoa(i%250+1) + ":4000" // own address per route
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

// Attack and control case on the real routes: after 30 new incidents per
// hour and page, 429 with Retry-After follows; normal use stays free.
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
	// Normal use: 30 creations (here with an empty body, hence 400) are never throttled.
	for i := 0; i < 30; i++ {
		w := post("198.51.100.10:1")
		require.NotEqual(t, http.StatusTooManyRequests, w.Code, "Aufruf %d", i+1)
	}
	// Attack: the 31st call fails, even from a different IP.
	w := post("198.51.100.99:1")
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.Greater(t, retry, 0)
	assert.LessOrEqual(t, retry, 3600)
}
