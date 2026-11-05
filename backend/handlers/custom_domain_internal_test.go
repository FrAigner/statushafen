package handlers

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Scheitert die Abfrage, ob eine andere Seite die Domain schon verifiziert
// trägt, wird nichts gespeichert (fail closed): 500 statt Verifizierung.
func TestVerifyCustomDomain_LiveCheckDBErrorFailsClosed(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	const domain = "status.acme.example"
	page := models.StatusPage{Name: "Acme", Slug: "acme-live-err", CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-live-err"}
	require.NoError(t, db.Create(&page).Error)
	stubLookup(t, func(_ context.Context, _ string) ([]string, error) {
		return []string{"statushafen-verify=tok-live-err"}, nil
	})

	// Die erste Abfrage auf status_pages lädt die Seite, die zweite ist der Live-Check.
	removeFail := injectDBError(t, db, "query", "status_pages", 1)
	w := doJSONRequest(r, "POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil)

	removeFail()
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	assertCustomDomainState(t, db, page.ID, domain, "pending")
}

func TestWarnIfCustomDomainCheckUnprotected(t *testing.T) {
	capture := func(t *testing.T) *bytes.Buffer {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })
		return &buf
	}

	t.Run("Produktion ohne Token warnt", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("CADDY_INTERNAL_TOKEN", "")
		buf := capture(t)
		WarnIfCustomDomainCheckUnprotected()
		assert.Contains(t, buf.String(), "CADDY_INTERNAL_TOKEN nicht gesetzt")
	})
	t.Run("Produktion mit Token schweigt", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("CADDY_INTERNAL_TOKEN", "token-example")
		buf := capture(t)
		WarnIfCustomDomainCheckUnprotected()
		assert.Empty(t, buf.String())
		assert.NotContains(t, buf.String(), "token-example")
	})
	t.Run("Entwicklung ohne Token schweigt", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("CADDY_INTERNAL_TOKEN", "")
		buf := capture(t)
		WarnIfCustomDomainCheckUnprotected()
		assert.Empty(t, buf.String())
	})
}

// Fehlerpfade der Domain-Endpunkte: ungültige Seiten-ID, keine Domain
// eingetragen, fehlender Host bzw. fehlende Domain bei den öffentlichen und
// internen Abfragen. Alle antworten ohne Seiteneffekt.
func TestCustomDomainEndpoints_InputErrors(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("CADDY_INTERNAL_TOKEN", "")
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "Ohne", Slug: "ohne-domain"}
	require.NoError(t, db.Create(&page).Error)
	lookup := func(context.Context, string) ([]string, error) {
		t.Error("kein DNS-Lookup erwartet")
		return nil, nil
	}
	stubLookup(t, lookup)
	pid := strconv.Itoa(int(page.ID))

	cases := []struct {
		name, method, path string
		wantStatus         int
		wantBody           string
	}{
		{"Löschen mit ungültiger ID", "DELETE", "/api/admin/pages/abc/custom-domain", http.StatusBadRequest, `"code":"invalid_id"`},
		{"Prüfen mit ungültiger ID", "POST", "/api/admin/pages/abc/custom-domain/verify", http.StatusBadRequest, `"code":"invalid_id"`},
		{"Prüfen ohne eingetragene Domain", "POST", "/api/admin/pages/" + pid + "/custom-domain/verify", http.StatusBadRequest, `"code":"custom_domain_missing"`},
		{"Auflösung ohne Host", "GET", "/api/public/resolve-domain", http.StatusNotFound, `"code":"page_not_found"`},
		{"Auflösung mit ungültigem Host", "GET", "/api/public/resolve-domain?host=../etc", http.StatusNotFound, `"code":"page_not_found"`},
		{"Interner Check ohne Domain", "GET", "/api/internal/custom-domain-check", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSONRequest(r, tc.method, tc.path, nil)
			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			if tc.wantBody != "" {
				assert.Contains(t, w.Body.String(), tc.wantBody)
			}
		})
	}
}
