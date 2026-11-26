package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The admin form needs the real CNAME target before the domain is saved (it used to
// guess "edge.example.com"), the TXT value again when the form is reopened, a hint
// for apex domains (no CNAME allowed at the zone apex) and, after the TXT proof,
// whether the domain already points to the target.

// Tests never query real DNS: by default nothing resolves (stubDNS overrides it).
func init() {
	lookupCNAME = noCNAME
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
}

func stubDNS(t *testing.T, cname func(context.Context, string) (string, error), hosts map[string][]string) {
	t.Helper()
	origCNAME, origHost := lookupCNAME, lookupHost
	lookupCNAME = cname
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		if ips, ok := hosts[host]; ok {
			return ips, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { lookupCNAME, lookupHost = origCNAME, origHost })
}

func noCNAME(_ context.Context, host string) (string, error) {
	return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestIsApexDomain(t *testing.T) {
	assert.True(t, isApexDomain("aigner.online"))
	assert.True(t, isApexDomain("example.co.uk"))
	assert.False(t, isApexDomain("status.example.co.uk"))
	assert.False(t, isApexDomain("status.acme.example"))
}

func TestCustomDomainPointsToTarget(t *testing.T) {
	ctx := context.Background()
	t.Run("CNAME to the target (with trailing dot, any case)", func(t *testing.T) {
		stubDNS(t, func(context.Context, string) (string, error) { return "Edge.Example.com.", nil }, nil)
		assert.True(t, customDomainPointsToTarget(ctx, "status.acme.example", "edge.example.com"))
	})
	t.Run("same addresses as the target (ALIAS / CNAME flattening)", func(t *testing.T) {
		stubDNS(t, noCNAME, map[string][]string{"acme.example": {"203.0.113.9"}, "edge.example.com": {"203.0.113.8", "203.0.113.9"}})
		assert.True(t, customDomainPointsToTarget(ctx, "acme.example", "edge.example.com"))
	})
	t.Run("other addresses", func(t *testing.T) {
		stubDNS(t, func(context.Context, string) (string, error) { return "acme.example.", nil },
			map[string][]string{"acme.example": {"198.51.100.1"}, "edge.example.com": {"203.0.113.9"}})
		assert.False(t, customDomainPointsToTarget(ctx, "acme.example", "edge.example.com"))
	})
	t.Run("domain does not resolve", func(t *testing.T) {
		stubDNS(t, noCNAME, map[string][]string{"edge.example.com": {"203.0.113.9"}})
		assert.False(t, customDomainPointsToTarget(ctx, "acme.example", "edge.example.com"))
	})
	t.Run("target does not resolve", func(t *testing.T) {
		stubDNS(t, noCNAME, map[string][]string{"acme.example": {"203.0.113.9"}})
		assert.False(t, customDomainPointsToTarget(ctx, "acme.example", "edge.example.com"))
	})
}

func TestGetCustomDomainHandler(t *testing.T) {
	t.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "edge.example.com")
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	get := func(t *testing.T, id uint) map[string]any {
		t.Helper()
		return decodeJSON(t, doJSONRequestExpect(t, r, "GET", "/api/admin/pages/"+strconv.Itoa(int(id))+"/custom-domain", nil, http.StatusOK))
	}

	t.Run("without domain: target only", func(t *testing.T) {
		page := models.StatusPage{Name: "Leer", Slug: "leer"}
		require.NoError(t, db.Create(&page).Error)
		resp := get(t, page.ID)
		assert.Equal(t, "edge.example.com", resp["cname_target"])
		assert.Equal(t, "", resp["custom_domain"])
		assert.Equal(t, false, resp["apex_domain"])
		assert.NotContains(t, resp, "txt_record_value")
	})

	t.Run("pending apex domain: TXT value again and apex hint", func(t *testing.T) {
		page := models.StatusPage{Name: "Apex", Slug: "apex", CustomDomain: "acme.example", CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-1"}
		require.NoError(t, db.Create(&page).Error)
		resp := get(t, page.ID)
		assert.Equal(t, "acme.example", resp["custom_domain"])
		assert.Equal(t, "pending", resp["custom_domain_status"])
		assert.Equal(t, true, resp["apex_domain"])
		assert.Equal(t, "statushafen-verify=tok-1", resp["txt_record_value"])
	})

	t.Run("verified subdomain: no TXT value", func(t *testing.T) {
		page := models.StatusPage{Name: "Sub", Slug: "sub", CustomDomain: "status.acme.example", CustomDomainStatus: "verified", CustomDomainVerificationToken: "tok-2"}
		require.NoError(t, db.Create(&page).Error)
		resp := get(t, page.ID)
		assert.Equal(t, false, resp["apex_domain"])
		assert.NotContains(t, resp, "txt_record_value")
	})

	t.Run("unknown page", func(t *testing.T) {
		doJSONRequestExpect(t, r, "GET", "/api/admin/pages/99999/custom-domain", nil, http.StatusNotFound)
	})
}

func TestSetCustomDomainHandler_ReportsApexDomain(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&page).Error)
	resp := decodeJSON(t, doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain", map[string]any{"domain": "acme.example"}, http.StatusOK))
	assert.Equal(t, true, resp["apex_domain"])
}

func TestVerifyCustomDomainHandler_ReportsWhetherDomainPointsToTarget(t *testing.T) {
	t.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "edge.example.com")
	stubLookup(t, func(context.Context, string) ([]string, error) { return []string{"statushafen-verify=tok"}, nil })
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	verify := func(t *testing.T, slug, domain string) map[string]any {
		t.Helper()
		page := models.StatusPage{Name: slug, Slug: slug, CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok"}
		require.NoError(t, db.Create(&page).Error)
		return decodeJSON(t, doJSONRequestExpect(t, r, "POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil, http.StatusOK))
	}

	t.Run("points to the target", func(t *testing.T) {
		stubDNS(t, func(context.Context, string) (string, error) { return "edge.example.com.", nil }, nil)
		resp := verify(t, "ok", "status.ok.example")
		assert.Equal(t, true, resp["verified"])
		assert.Equal(t, true, resp["points_to_target"])
		assert.Equal(t, false, resp["apex_domain"])
	})

	t.Run("not yet pointing (still verified: TXT proves ownership)", func(t *testing.T) {
		stubDNS(t, func(context.Context, string) (string, error) { return "", errors.New("timeout") }, nil)
		resp := verify(t, "noch", "noch.example")
		assert.Equal(t, true, resp["verified"])
		assert.Equal(t, false, resp["points_to_target"])
		assert.Equal(t, true, resp["apex_domain"])
	})
}
