package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/publicsuffix"
)

// lookupTXT is declared as a package variable instead of a direct resolver call
// so that tests can simulate a DNS response (including real *net.DNSError values)
// without needing real network lookups. The context
// carries the deadline (previously there was no timeout and a hanging resolver
// blocked the request).
var lookupTXT = net.DefaultResolver.LookupTXT

// lookupCNAME and lookupHost check after the TXT proof whether the domain already
// points to the CNAME target (package variables for the same reason as lookupTXT).
var (
	lookupCNAME = net.DefaultResolver.LookupCNAME
	lookupHost  = net.DefaultResolver.LookupHost
)

// isApexDomain reports whether the domain is the registrable domain itself
// (e.g. example.org, example.co.uk). DNS allows no CNAME there; such a domain
// needs ALIAS/ANAME or "CNAME flattening" at the DNS provider, or a subdomain.
func isApexDomain(domain string) bool {
	apex, err := publicsuffix.EffectiveTLDPlusOne(domain)
	return err == nil && apex == domain
}

// customDomainPointsToTarget reports whether the domain already reaches the CNAME
// target: either its CNAME chain ends at the target, or it resolves to one of
// the target's addresses (ALIAS/ANAME and CNAME flattening hide the CNAME).
// Every lookup error counts as "not yet".
func customDomainPointsToTarget(ctx context.Context, domain, target string) bool {
	if cname, err := lookupCNAME(ctx, domain); err == nil && strings.EqualFold(strings.TrimSuffix(cname, "."), target) {
		return true
	}
	got, err := lookupHost(ctx, domain)
	if err != nil {
		return false
	}
	want, err := lookupHost(ctx, target)
	if err != nil {
		return false
	}
	for _, ip := range got {
		if slices.Contains(want, ip) {
			return true
		}
	}
	return false
}

// customDomainLookupTimeout limits a single TXT lookup (a variable
// so the timeout test does not wait 5 s).
var customDomainLookupTimeout = 5 * time.Second

// customDomainTXTChallenge returns the expected TXT record value for the
// ownership proof of a domain (standard procedure as with Google
// Search Console/Mailgun: TXT proves DNS control, independent of the later
// configured CNAME).
func customDomainTXTChallenge(token string) string {
	return "statushafen-verify=" + token
}

// customDomainCNAMETarget returns the hostname that customers must point
// their CNAME to (the shared reverse proxy/edge server of the
// installation). Configurable via CUSTOM_DOMAIN_CNAME_TARGET so that
// several environments can use different target hosts. Without
// a setting, "edge.<host from FRONTEND_URL>" applies.
func customDomainCNAMETarget() string {
	if v := os.Getenv("CUSTOM_DOMAIN_CNAME_TARGET"); v != "" {
		return v
	}
	return "edge." + frontendURLHost()
}

// normalizeDomain trims/lowercases a domain entered by the client and
// lets only real hostnames through (RFC 1123): at least two labels made of
// a-z, 0-9 and hyphen (not at the edges), at most 63 characters each, at most 253
// in total, no purely numeric ending. This rejects
// scheme, path, port, wildcards, IP literals, empty labels, a trailing
// dot (it would bypass the uniqueness check: "example.com." against
// "example.com") and non-ASCII characters (international names only in
// punycode notation). Empty stays empty and valid (deletes the domain).
func normalizeDomain(raw string) (string, bool) {
	d := strings.ToLower(strings.TrimSpace(raw))
	if d == "" {
		return "", true
	}
	if len(d) > 253 {
		return "", false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, l := range labels {
		if !validHostnameLabel(l) {
			return "", false
		}
	}
	// A purely numeric ending would be an IP literal (127.0.0.1) or at least
	// not a valid top-level name.
	if tld := labels[len(labels)-1]; strings.Trim(tld, "0123456789") == "" {
		return "", false
	}
	return d, true
}

// validHostnameLabel checks a single label of a hostname.
func validHostnameLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for i := 0; i < len(l); i++ {
		ch := l[i]
		isLetter := ch >= 'a' && ch <= 'z'
		isDigit := ch >= '0' && ch <= '9'
		if !isLetter && !isDigit && ch != '-' {
			return false
		}
	}
	return true
}

// loadStatusPageForCustomDomain parses :page_id and loads the associated
// status page - common precondition of all three custom-domain admin
// endpoints (Set/Delete/Verify). On error the response has already been
// written; the caller then only needs to return.
func (h *Handler) loadStatusPageForCustomDomain(c *gin.Context) (models.StatusPage, uint64, bool) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return models.StatusPage{}, 0, false
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return models.StatusPage{}, 0, false
	}

	return page, pageID, true
}

// Reasons for a failed DNS check, as stable codes
// for the frontend (texts there in de/en).
const (
	customDomainCheckTXTNotFound = "txt_not_found"
	customDomainCheckLookupError = "dns_lookup_failed"
)

// resetCustomDomainCheck forgets the result of the last check (new or
// removed domain: an old check result does not belong to the new entry).
func resetCustomDomainCheck(page *models.StatusPage) {
	page.CustomDomainCheckedAt = nil
	page.CustomDomainCheckError = ""
}

// PUT /api/admin/pages/:page_id/custom-domain — registers a custom domain
// (or changes an existing one). Always resets the verification status to
// "pending"; an old "verified"/"active" status of a previous
// domain must never be "inherited" by the new domain.
func (h *Handler) SetCustomDomainHandler(c *gin.Context) {
	page, pageID, ok := h.loadStatusPageForCustomDomain(c)
	if !ok {
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	domain, ok := normalizeDomain(req.Domain)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Domain", "code": "domain_invalid"})
		return
	}

	// Deliberately no uniqueness check on registration: an unproven
	// entry (pending) must not reserve the domain, otherwise any
	// page admin could block foreign domains, and the response would reveal which
	// tenant it is registered with. Who wins is decided by the
	// TXT proof in VerifyCustomDomainHandler (whoever verifies first wins).

	page.CustomDomain = domain
	if domain == "" {
		page.CustomDomainStatus = ""
		page.CustomDomainVerificationToken = ""
	} else {
		page.CustomDomainStatus = "pending"
		// New token on every Set (also when saving the same
		// domain again) - consistent with the status reset right above: an old
		// token must never be inherited by a "new" entry.
		page.CustomDomainVerificationToken = randomToken(16)
	}
	page.CustomDomainVerifiedAt = nil
	resetCustomDomainCheck(&page)

	if err := h.DB.Save(&page).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern der Domain", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.custom_domain_set", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, domain)
	resp := gin.H{
		"custom_domain":        page.CustomDomain,
		"custom_domain_status": page.CustomDomainStatus,
		"cname_target":         customDomainCNAMETarget(),
		"apex_domain":          domain != "" && isApexDomain(domain),
	}
	if domain != "" {
		resp["verification_token"] = page.CustomDomainVerificationToken
		resp["txt_record_value"] = customDomainTXTChallenge(page.CustomDomainVerificationToken)
	}
	c.JSON(http.StatusOK, resp)
}

// GET /api/admin/pages/:page_id/custom-domain — the state of the custom domain for
// the admin form: the CNAME target of this installation (also before a domain is
// saved), whether the domain is an apex domain, and while the TXT proof is still
// open the expected TXT value (so it is shown again when the form is reopened).
func (h *Handler) GetCustomDomainHandler(c *gin.Context) {
	page, _, ok := h.loadStatusPageForCustomDomain(c)
	if !ok {
		return
	}
	resp := gin.H{
		"custom_domain":        page.CustomDomain,
		"custom_domain_status": page.CustomDomainStatus,
		"cname_target":         customDomainCNAMETarget(),
		"apex_domain":          page.CustomDomain != "" && isApexDomain(page.CustomDomain),
	}
	if page.CustomDomainStatus == "pending" || page.CustomDomainStatus == "check_failed" {
		resp["txt_record_value"] = customDomainTXTChallenge(page.CustomDomainVerificationToken)
	}
	c.JSON(http.StatusOK, resp)
}

// DELETE /api/admin/pages/:page_id/custom-domain — removes a configured
// custom domain completely (domain string + status).
func (h *Handler) DeleteCustomDomainHandler(c *gin.Context) {
	page, pageID, ok := h.loadStatusPageForCustomDomain(c)
	if !ok {
		return
	}

	page.CustomDomain = ""
	page.CustomDomainStatus = ""
	page.CustomDomainVerifiedAt = nil
	page.CustomDomainVerificationToken = ""
	resetCustomDomainCheck(&page)
	if err := h.DB.Save(&page).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Entfernen der Domain", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.custom_domain_removed", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Custom Domain entfernt"})
}

// POST /api/admin/pages/:page_id/custom-domain/verify — checks via DNS TXT
// lookup whether the generated verification string (CustomDomainVerificationToken)
// has been published for the domain, and on success switches the status from
// "pending" to "verified" (prerequisite for Caddy's on-demand TLS
// to be allowed to issue a certificate for the domain, see
// InternalCustomDomainCheckHandler). The TXT record only proves
// DNS ownership - the actual routing via CNAME to the edge server is
// shown to the customer as the next step only after successful verification
// (cname_target in the response), analogous to Google Search
// Console/Mailgun.
func (h *Handler) VerifyCustomDomainHandler(c *gin.Context) {
	page, pageID, ok := h.loadStatusPageForCustomDomain(c)
	if !ok {
		return
	}

	if page.CustomDomain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Keine Custom Domain eingetragen", "code": "custom_domain_missing"})
		return
	}

	target := customDomainCNAMETarget()
	expected := customDomainTXTChallenge(page.CustomDomainVerificationToken)
	ctx, cancel := context.WithTimeout(c.Request.Context(), customDomainLookupTimeout)
	records, err := lookupTXT(ctx, page.CustomDomain)
	cancel()

	// A missing TXT record is a *net.DNSError{IsNotFound} in Go (NXDOMAIN
	// or no TXT records) and thus the normal case "not found"; only
	// timeouts, SERVFAIL etc. are a lookup error.
	var dnsErr *net.DNSError
	lookupFailed := err != nil && (!errors.As(err, &dnsErr) || !dnsErr.IsNotFound)
	found := false
	for _, r := range records {
		if r == expected {
			found = true
			break
		}
	}

	now := time.Now()
	// Verified/active are live (Caddy check, domain resolution): a
	// check, however it turns out, never changes their status.
	live := page.CustomDomainStatus == "verified" || page.CustomDomainStatus == "active"
	updates := map[string]any{"custom_domain_checked_at": now}
	if !found {
		// own state instead of "pending", so that the UI
		// distinguishes "not yet checked" from "checked but failed".
		reason := customDomainCheckTXTNotFound
		message := "TXT-Eintrag nicht gefunden. Bitte füge bei " + page.CustomDomain + " einen TXT-Record mit dem Wert \"" + expected + "\" hinzu."
		if lookupFailed {
			reason = customDomainCheckLookupError
			message = "Die DNS-Abfrage für " + page.CustomDomain + " ist fehlgeschlagen. Bitte versuche es später erneut."
		}
		updates["custom_domain_check_error"] = reason
		if !live {
			updates["custom_domain_status"] = "check_failed"
		}
		if !h.saveCustomDomainCheck(c, page, updates) {
			return
		}
		status := page.CustomDomainStatus
		if !live {
			status = "check_failed"
		}
		c.JSON(http.StatusOK, gin.H{
			"custom_domain_status":      status,
			"custom_domain_checked_at":  now,
			"custom_domain_check_error": reason,
			"verified":                  false,
			"message":                   message,
			"txt_record_value":          expected,
			"cname_target":              target,
		})
		return
	}

	if !live {
		// The proof has been provided, but another page already carries the domain
		// as verified or active: verified domains stay protected. Only whoever
		// was able to set the TXT record sees this response.
		taken, err := h.customDomainLiveOnOtherPage(page)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern des Verifikationsstatus", "code": "server_error"})
			return
		}
		if taken {
			c.JSON(http.StatusConflict, gin.H{"error": "Domain wird bereits von einer anderen Statusseite verwendet", "code": "domain_taken"})
			return
		}
	}

	updates["custom_domain_check_error"] = ""
	status := page.CustomDomainStatus
	if !live {
		status = "verified"
		updates["custom_domain_status"] = status
		updates["custom_domain_verified_at"] = now
	}
	if !h.saveCustomDomainCheck(c, page, updates) {
		return
	}

	h.RecordAudit(c, "statuspage.custom_domain_verified", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, page.CustomDomain)
	// The TXT record proves ownership; whether the domain already reaches the edge
	// server is only a hint for the next step (DNS changes take time to spread).
	dnsCtx, dnsCancel := context.WithTimeout(c.Request.Context(), customDomainLookupTimeout)
	pointsToTarget := customDomainPointsToTarget(dnsCtx, page.CustomDomain, target)
	dnsCancel()
	c.JSON(http.StatusOK, gin.H{
		"custom_domain_status": status,
		"verified":             true,
		"points_to_target":     pointsToTarget,
		"apex_domain":          isApexDomain(page.CustomDomain),
		"message":              "Domain erfolgreich verifiziert. Bitte richte nun den CNAME auf " + target + " ein.",
		"txt_record_value":     expected,
		"cname_target":         target,
		"verified_at":          now,
	})
}

// customDomainLiveOnOtherPage reports whether another (non-deleted) page
// carries the same domain in status verified or active.
func (h *Handler) customDomainLiveOnOtherPage(page models.StatusPage) (bool, error) {
	var n int64
	err := h.DB.Model(&models.StatusPage{}).
		Where("lower(custom_domain) = ? AND id != ? AND custom_domain_status IN ('verified','active')", strings.ToLower(page.CustomDomain), page.ID).
		Count(&n).Error
	return n > 0, err
}

// saveCustomDomainCheck writes only the check columns, and only if domain and
// token are still the ones checked against: the DNS lookup
// takes time, and in the meantime the domain may have been changed or removed;
// saving the whole row would overwrite that (a removed
// domain would come back as "verified"). On error the response (500 or 409
// on a change in the meantime) has been written and false is returned. If
// two pages prove the same domain at the same time, both pass the pre-check
// (customDomainLiveOnOtherPage); only the partial unique index
// idx_status_pages_custom_domain_live decides. As with the pre-check, the loser
// gets 409 domain_taken instead of 500.
func (h *Handler) saveCustomDomainCheck(c *gin.Context, page models.StatusPage, updates map[string]any) bool {
	res := h.DB.Model(&models.StatusPage{}).
		Where("id = ? AND custom_domain = ? AND custom_domain_verification_token = ?",
			page.ID, page.CustomDomain, page.CustomDomainVerificationToken).
		Updates(updates)
	if isUniqueViolation(res.Error) {
		c.JSON(http.StatusConflict, gin.H{"error": "Domain wird bereits von einer anderen Statusseite verwendet", "code": "domain_taken"})
		return false
	}
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern des Verifikationsstatus", "code": "server_error"})
		return false
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Die Domain wurde während der Prüfung geändert oder entfernt. Bitte erneut prüfen.", "code": "custom_domain_changed"})
		return false
	}
	return true
}

// GET /api/public/resolve-domain?host=... — public lookup that maps a host
// called via a custom domain to the associated status page slug
// (see frontend/src/views/public/PublicRoot.vue). Only domains in
// status "verified"/"active" are resolved. The status is never
// changed here: an anonymous call proves neither working DNS nor an
// issued certificate; the transition verified -> active is triggered solely by the
// internal check (InternalCustomDomainCheckHandler). Private pages
// return their slug only to members; for everyone else the response looks
// like that of an unknown domain.
func (h *Handler) ResolvePublicDomainHandler(c *gin.Context) {
	host, _ := normalizeDomain(c.Query("host"))
	if host == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nicht gefunden", "code": "page_not_found"})
		return
	}

	var page models.StatusPage
	err := h.DB.Where("custom_domain = ? AND custom_domain_status IN ('verified','active')", host).First(&page).Error
	if err != nil || (page.IsPrivate && !h.userHasRoleOnPrivatePage(c, page.ID)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nicht gefunden", "code": "page_not_found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"slug": page.Slug})
}

// customDomainCheckTokenRequired reports whether the internal check requires a
// CADDY_INTERNAL_TOKEN: everywhere except in explicit
// development environments (APP_ENV development, dev, test, local). Fail closed: a
// forgotten or misspelled APP_ENV ("prod") does not open the endpoint
// up.
func customDomainCheckTokenRequired() bool {
	return !database.IsDevelopmentAppEnv()
}

// WarnIfCustomDomainCheckUnprotected logs a warning at startup if the
// internal domain check rejects all requests with
// 503 due to a missing CADDY_INTERNAL_TOKEN (custom domains then do not work).
func WarnIfCustomDomainCheckUnprotected() {
	if customDomainCheckTokenRequired() && os.Getenv("CADDY_INTERNAL_TOKEN") == "" {
		slog.Warn("CADDY_INTERNAL_TOKEN nicht gesetzt: /api/internal/custom-domain-check antwortet mit 503, eigene Domains (On-Demand-TLS) sind deaktiviert")
	}
}

// GET /api/internal/custom-domain-check?domain=... — called by Caddy's on-demand TLS
// ("ask" endpoint) before a certificate is requested for an unknown
// domain. Answers 200 only for domains in status
// "verified"/"active", otherwise 404 - prevents arbitrary foreign domains merely
// pointed at our server via DNS from obtaining free certificates through
// our server (Caddy otherwise issues a certificate for every domain whose
// TLS handshake reaches us).
//
// The endpoint is an oracle for customer domains and therefore requires, outside
// of development, the shared secret CADDY_INTERNAL_TOKEN (header
// X-Internal-Token). If it is not set, it always answers 503 and
// releases nothing; a wrong or missing token yields 404. A
// released domain in status "verified" is set to "active" here:
// Caddy asks immediately before issuing the certificate.
func (h *Handler) InternalCustomDomainCheckHandler(c *gin.Context) {
	secret := os.Getenv("CADDY_INTERNAL_TOKEN")
	if secret == "" && customDomainCheckTokenRequired() {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if secret != "" {
		provided := c.GetHeader("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.Status(http.StatusNotFound)
			return
		}
	}

	domain, _ := normalizeDomain(c.Query("domain"))
	if domain == "" {
		c.Status(http.StatusNotFound)
		return
	}

	var page models.StatusPage
	err := h.DB.Where("custom_domain = ? AND custom_domain_status IN ('verified','active')", domain).First(&page).Error
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if page.CustomDomainStatus == "verified" {
		// Only promote if the row has not changed since it was read.
		h.DB.Model(&models.StatusPage{}).
			Where("id = ? AND custom_domain = ? AND custom_domain_status = 'verified'", page.ID, domain).
			Update("custom_domain_status", "active")
	}

	c.Status(http.StatusOK)
}
