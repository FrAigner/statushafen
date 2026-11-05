package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// normalizeDomain lässt nur echte Hostnamen durch: Kleinschreibung, kein
// abschließender Punkt (sonst umgeht "example.com." die Eindeutigkeit), keine
// Wildcards, keine IP-Literale, keine leeren oder ungültigen Labels.
func TestNormalizeDomain_Strikt(t *testing.T) {
	valid := map[string]string{
		"status.example.com":                 "status.example.com",
		"  STATUS.Example.COM \t":            "status.example.com",
		"a-b.example.co.uk":                  "a-b.example.co.uk",
		"xn--bcher-kva.example":              "xn--bcher-kva.example",
		"s1.example.org":                     "s1.example.org",
		strings.Repeat("a", 63) + ".example": strings.Repeat("a", 63) + ".example",
	}
	for in, want := range valid {
		got, ok := normalizeDomain(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	got, ok := normalizeDomain("   ")
	assert.True(t, ok, "leer löscht die Domain")
	assert.Equal(t, "", got)

	invalid := []string{
		"example.com.",                       // abschließender Punkt
		"*.example.com",                      // Wildcard
		"foo.*.example.com",                  // Wildcard mittendrin
		"127.0.0.1",                          // IPv4-Literal
		"8.8.8.8",                            // IPv4-Literal
		"[::1]",                              // IPv6-Literal
		"::1",                                // IPv6-Literal
		"a..b.example",                       // leeres Label
		".example.com",                       // führender Punkt
		"-a.example.com",                     // Label beginnt mit Bindestrich
		"a-.example.com",                     // Label endet mit Bindestrich
		"a_b.example.com",                    // Unterstrich
		"exa mple.com",                       // Leerzeichen
		"https://example.com",                // Schema
		"example.com/pfad",                   // Pfad
		"example.com:8443",                   // Port
		"user@example.com",                   // Userinfo
		"localhost",                          // kein Punkt
		"bücher.example",                     // Nicht-ASCII (nur Punycode zulässig)
		"example.123",                        // rein numerische TLD
		strings.Repeat("a", 64) + ".example", // Label zu lang
		strings.Repeat("a.", 130) + "com",    // Gesamtname zu lang
		"example.com\x00.evil.test",          // Steuerzeichen
	}
	for _, in := range invalid {
		_, ok := normalizeDomain(in)
		assert.False(t, ok, "muss abgelehnt werden: %q", in)
	}
}

func TestSetCustomDomainHandler_RejectsMalformedDomains(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "Acme", Slug: "acme-strict"}
	require.NoError(t, db.Create(&page).Error)
	path := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/custom-domain"

	for _, d := range []string{"STATUS.Victim-Brand.example.", "*.victim-brand.example", "127.0.0.1", "a..b.example"} {
		w := doJSONRequest(r, "PUT", path, map[string]string{"domain": d})
		assert.Equal(t, http.StatusBadRequest, w.Code, d)
		assert.Contains(t, w.Body.String(), `"code":"domain_invalid"`, d)
	}
	assertCustomDomainState(t, db, page.ID, "", "")

	// Gegenprobe: dieselbe Schreibweise ohne Fehler wird gespeichert und klein geschrieben.
	doJSONRequestExpect(t, r, "PUT", path, map[string]string{"domain": "STATUS.Victim-Brand.example"}, http.StatusOK)
	assertCustomDomainState(t, db, page.ID, "status.victim-brand.example", "pending")
}

// Eine unbewiesene Eintragung (pending) reserviert die Domain nicht: Wer die
// Domain wirklich kontrolliert, setzt sie auf seiner Seite und weist sie per
// TXT nach. Das Setzen verrät außerdem nicht, ob eine andere Seite die Domain
// schon trägt.
func TestCustomDomain_PendingReservierungBlockiertNicht(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))

	squatter := models.StatusPage{Name: "Squatter", Slug: "squatter"}
	owner := models.StatusPage{Name: "Owner", Slug: "owner"}
	require.NoError(t, db.Create(&squatter).Error)
	require.NoError(t, db.Create(&owner).Error)
	pathOf := func(p models.StatusPage, suffix string) string {
		return "/api/admin/pages/" + strconv.Itoa(int(p.ID)) + "/custom-domain" + suffix
	}
	const domain = "status.victim-brand.example"

	doJSONRequestExpect(t, r, "PUT", pathOf(squatter, ""), map[string]string{"domain": domain}, http.StatusOK)

	// Der rechtmäßige Inhaber bekommt keinen 409 und kann die Domain eintragen.
	w := doJSONRequestExpect(t, r, "PUT", pathOf(owner, ""), map[string]string{"domain": domain}, http.StatusOK)
	token, _ := decodeJSON(t, w)["verification_token"].(string)
	require.NotEmpty(t, token)
	assert.NotContains(t, w.Body.String(), "domain_taken")

	// Nur der Inhaber kann den TXT-Eintrag setzen: Sein Token wird gefunden.
	stubLookup(t, func(_ context.Context, host string) ([]string, error) {
		return []string{"statushafen-verify=" + token}, nil
	})
	resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", pathOf(owner, "/verify"), nil, http.StatusOK))
	assert.Equal(t, true, resp["verified"])
	assertCustomDomainState(t, db, owner.ID, domain, "verified")

	// Der Squatter kann es nicht: sein Token steht nicht im DNS. Und selbst mit
	// gültigem Nachweis verdrängt er die verifizierte Domain nicht (siehe unten).
	resp = decodeJSON(t, doJSONRequestExpect(t, r, "POST", pathOf(squatter, "/verify"), nil, http.StatusOK))
	assert.Equal(t, false, resp["verified"])
	assertCustomDomainState(t, db, squatter.ID, domain, "check_failed")
}

// Verifizierte Domains bleiben geschützt: Eine zweite Seite kann sie eintragen
// (die Antwort verrät nichts), aber nicht verifizieren, solange die erste sie
// verifiziert oder aktiv trägt. Die Auflösung bleibt bei der ersten Seite.
func TestCustomDomain_VerifizierteDomainBleibtGeschuetzt(t *testing.T) {
	for _, live := range []string{"verified", "active"} {
		t.Run(live, func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := setupCustomDomainRouter(NewHandler(db))
			const domain = "status.acme.example"
			holder := models.StatusPage{Name: "Holder", Slug: "holder", CustomDomain: domain, CustomDomainStatus: live, CustomDomainVerificationToken: "holder-token"}
			other := models.StatusPage{Name: "Other", Slug: "other"}
			require.NoError(t, db.Create(&holder).Error)
			require.NoError(t, db.Create(&other).Error)
			otherPath := "/api/admin/pages/" + strconv.Itoa(int(other.ID)) + "/custom-domain"

			// Eintragen verrät die Belegung nicht.
			w := doJSONRequestExpect(t, r, "PUT", otherPath, map[string]string{"domain": domain}, http.StatusOK)
			token, _ := decodeJSON(t, w)["verification_token"].(string)
			assertCustomDomainState(t, db, other.ID, domain, "pending")

			// Auch mit gültigem TXT-Nachweis wird die Domain der anderen Seite nicht übernommen.
			stubLookup(t, func(_ context.Context, _ string) ([]string, error) {
				return []string{"statushafen-verify=" + token}, nil
			})
			w = doJSONRequest(r, "POST", otherPath+"/verify", nil)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"domain_taken"`)
			assertCustomDomainState(t, db, other.ID, domain, "pending")
			assertCustomDomainState(t, db, holder.ID, domain, live)

			// Die Auflösung gehört weiter dem Inhaber.
			resp := decodeJSON(t, doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host="+domain, nil, http.StatusOK))
			assert.Equal(t, "holder", resp["slug"])
		})
	}
}
