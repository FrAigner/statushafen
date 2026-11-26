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

// normalizeDomain lets only real hostnames through: lowercase, no
// trailing dot (otherwise "example.com." bypasses uniqueness), no
// wildcards, no IP literals, no empty or invalid labels.
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
		"example.com.",                       // trailing dot
		"*.example.com",                      // Wildcard
		"foo.*.example.com",                  // wildcard in the middle
		"127.0.0.1",                          // IPv4 literal
		"8.8.8.8",                            // IPv4 literal
		"[::1]",                              // IPv6 literal
		"::1",                                // IPv6 literal
		"a..b.example",                       // empty label
		".example.com",                       // leading dot
		"-a.example.com",                     // label starts with hyphen
		"a-.example.com",                     // label ends with hyphen
		"a_b.example.com",                    // Unterstrich
		"exa mple.com",                       // Leerzeichen
		"https://example.com",                // Schema
		"example.com/pfad",                   // Pfad
		"example.com:8443",                   // Port
		"user@example.com",                   // Userinfo
		"localhost",                          // no dot
		"bücher.example",                     // non-ASCII (only punycode allowed)
		"example.123",                        // purely numeric TLD
		strings.Repeat("a", 64) + ".example", // label too long
		strings.Repeat("a.", 130) + "com",    // total name too long
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

	// Control case: the same spelling without error is saved and lowercased.
	doJSONRequestExpect(t, r, "PUT", path, map[string]string{"domain": "STATUS.Victim-Brand.example"}, http.StatusOK)
	assertCustomDomainState(t, db, page.ID, "status.victim-brand.example", "pending")
}

// An unproven entry (pending) does not reserve the domain: whoever
// really controls the domain sets it on their page and proves it via
// TXT. Setting it also does not reveal whether another page already
// carries the domain.
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

	// The rightful owner gets no 409 and can register the domain.
	w := doJSONRequestExpect(t, r, "PUT", pathOf(owner, ""), map[string]string{"domain": domain}, http.StatusOK)
	token, _ := decodeJSON(t, w)["verification_token"].(string)
	require.NotEmpty(t, token)
	assert.NotContains(t, w.Body.String(), "domain_taken")

	// Only the owner can set the TXT record: their token is found.
	stubLookup(t, func(_ context.Context, host string) ([]string, error) {
		return []string{"statushafen-verify=" + token}, nil
	})
	resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", pathOf(owner, "/verify"), nil, http.StatusOK))
	assert.Equal(t, true, resp["verified"])
	assertCustomDomainState(t, db, owner.ID, domain, "verified")

	// The squatter cannot: their token is not in DNS. And even with a
	// valid proof they do not displace the verified domain (see below).
	resp = decodeJSON(t, doJSONRequestExpect(t, r, "POST", pathOf(squatter, "/verify"), nil, http.StatusOK))
	assert.Equal(t, false, resp["verified"])
	assertCustomDomainState(t, db, squatter.ID, domain, "check_failed")
}

// Verified domains stay protected: a second page can register them
// (the response reveals nothing), but not verify them, as long as the first one carries them as
// verified or active. Resolution stays with the first page.
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

			// Registering does not reveal the occupancy.
			w := doJSONRequestExpect(t, r, "PUT", otherPath, map[string]string{"domain": domain}, http.StatusOK)
			token, _ := decodeJSON(t, w)["verification_token"].(string)
			assertCustomDomainState(t, db, other.ID, domain, "pending")

			// Even with a valid TXT proof, the domain of the other page is not taken over.
			stubLookup(t, func(_ context.Context, _ string) ([]string, error) {
				return []string{"statushafen-verify=" + token}, nil
			})
			w = doJSONRequest(r, "POST", otherPath+"/verify", nil)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"domain_taken"`)
			assertCustomDomainState(t, db, other.ID, domain, "pending")
			assertCustomDomainState(t, db, holder.ID, domain, live)

			// Resolution continues to belong to the owner.
			resp := decodeJSON(t, doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host="+domain, nil, http.StatusOK))
			assert.Equal(t, "holder", resp["slug"])
		})
	}
}
