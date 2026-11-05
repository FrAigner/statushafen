package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// lookupTXT ist als Package-Variable statt direktem Resolver-Aufruf
// deklariert, damit Tests eine DNS-Antwort (auch echte *net.DNSError-Werte)
// simulieren können, ohne echte Netzwerk-Lookups zu benötigen. Der Context
// trägt die Frist (vorher ohne Timeout, ein hängender Resolver
// blockierte den Request).
var lookupTXT = net.DefaultResolver.LookupTXT

// customDomainLookupTimeout begrenzt einen einzelnen TXT-Lookup (Variable,
// damit der Timeout-Test nicht 5 s wartet).
var customDomainLookupTimeout = 5 * time.Second

// customDomainTXTChallenge liefert den erwarteten TXT-Record-Wert für den
// Owner-Nachweis einer Domain (Standard-Verfahren wie bei Google
// Search Console/Mailgun: TXT beweist DNS-Kontrolle, unabhängig vom später
// gesetzten CNAME).
func customDomainTXTChallenge(token string) string {
	return "statushafen-verify=" + token
}

// customDomainCNAMETarget liefert den Hostnamen, auf den Kunden ihren
// CNAME zeigen müssen (der gemeinsame Reverse-Proxy/Edge-Server der
// Installation). Konfigurierbar über CUSTOM_DOMAIN_CNAME_TARGET, damit
// mehrere Umgebungen unterschiedliche Ziel-Hosts verwenden können. Ohne
// Angabe gilt "edge.<Host aus FRONTEND_URL>".
func customDomainCNAMETarget() string {
	if v := os.Getenv("CUSTOM_DOMAIN_CNAME_TARGET"); v != "" {
		return v
	}
	return "edge." + frontendURLHost()
}

// normalizeDomain trimmt/lowercased eine vom Client eingegebene Domain und
// lässt nur echte Hostnamen durch (RFC 1123): mindestens zwei Labels aus
// a-z, 0-9 und Bindestrich (nicht am Rand), je höchstens 63 Zeichen, gesamt
// höchstens 253, keine rein numerische Endung. Abgewiesen werden damit
// Schema, Pfad, Port, Wildcards, IP-Literale, leere Labels, ein abschließender
// Punkt (er würde die Eindeutigkeitsprüfung umgehen: "example.com." gegen
// "example.com") und Nicht-ASCII-Zeichen (internationale Namen nur in der
// Punycode-Schreibweise). Leer bleibt leer und gültig (löscht die Domain).
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
	// Eine rein numerische Endung wäre ein IP-Literal (127.0.0.1) oder zumindest
	// kein gültiger Top-Level-Name.
	if tld := labels[len(labels)-1]; strings.Trim(tld, "0123456789") == "" {
		return "", false
	}
	return d, true
}

// validHostnameLabel prüft ein einzelnes Label eines Hostnamens.
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

// loadStatusPageForCustomDomain parst :page_id und lädt die zugehörige
// Statusseite - gemeinsame Vorbedingung aller drei Custom-Domain-Admin-
// Endpunkte (Set/Delete/Verify). Bei Fehler ist die Response bereits
// geschrieben; der Aufrufer muss dann nur returnen.
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

// Gründe einer fehlgeschlagenen DNS-Prüfung, als stabile Codes
// für das Frontend (Texte dort in de/en).
const (
	customDomainCheckTXTNotFound = "txt_not_found"
	customDomainCheckLookupError = "dns_lookup_failed"
)

// resetCustomDomainCheck vergisst das Ergebnis der letzten Prüfung (neue oder
// entfernte Domain: ein altes Prüfergebnis gehört nicht zur neuen Eintragung).
func resetCustomDomainCheck(page *models.StatusPage) {
	page.CustomDomainCheckedAt = nil
	page.CustomDomainCheckError = ""
}

// PUT /api/admin/pages/:page_id/custom-domain — trägt eine Custom Domain ein
// (oder ändert eine bestehende). Setzt den Verifikationsstatus immer auf
// "pending" zurück, ein alter "verified"/"active"-Status einer vorherigen
// Domain darf nie an die neue Domain "vererbt" werden.
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

	// Bewusst keine Eindeutigkeitsprüfung beim Eintragen: Eine unbewiesene
	// Eintragung (pending) darf die Domain nicht reservieren, sonst könnte jeder
	// Seiten-Admin fremde Domains sperren, und die Antwort würde verraten, bei
	// welchem Mandanten sie hinterlegt ist. Wer gewinnt, entscheidet der
	// TXT-Nachweis in VerifyCustomDomainHandler (wer zuerst verifiziert, gewinnt).

	page.CustomDomain = domain
	if domain == "" {
		page.CustomDomainStatus = ""
		page.CustomDomainVerificationToken = ""
	} else {
		page.CustomDomainStatus = "pending"
		// Neuer Token bei jedem Set (auch beim erneuten Speichern derselben
		// Domain) - konsistent mit dem Status-Reset direkt darüber: ein alter
		// Token darf nie an eine "neue" Eintragung vererbt werden.
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
	}
	if domain != "" {
		resp["verification_token"] = page.CustomDomainVerificationToken
		resp["txt_record_value"] = customDomainTXTChallenge(page.CustomDomainVerificationToken)
	}
	c.JSON(http.StatusOK, resp)
}

// DELETE /api/admin/pages/:page_id/custom-domain — entfernt eine gesetzte
// Custom Domain vollständig (Domain-String + Status).
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

// POST /api/admin/pages/:page_id/custom-domain/verify — prüft per DNS-TXT-
// Lookup, ob der generierte Verifizierungsstring (CustomDomainVerificationToken)
// bei der Domain hinterlegt wurde, und schaltet den Status bei Erfolg von
// "pending" auf "verified" (Voraussetzung dafür, dass Caddys On-Demand-TLS
// ein Zertifikat für die Domain ausstellen darf, siehe
// InternalCustomDomainCheckHandler). Der TXT-Eintrag beweist nur den
// DNS-Besitz - das eigentliche Routing per CNAME auf den Edge-Server wird
// dem Kunden erst nach erfolgreicher Verifizierung als nächster Schritt
// angezeigt (cname_target in der Response), analog zu Google Search
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

	// Ein fehlender TXT-Eintrag ist für Go ein *net.DNSError{IsNotFound} (NXDOMAIN
	// bzw. keine TXT-Records) und damit der Normalfall "nicht gefunden"; nur
	// Timeouts, SERVFAIL u. Ä. sind ein Lookup-Fehler.
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
	// Verified/active sind live (Caddy-Check, Auflösung der Domain): eine
	// Prüfung, egal wie sie ausgeht, ändert deren Status nie.
	live := page.CustomDomainStatus == "verified" || page.CustomDomainStatus == "active"
	updates := map[string]any{"custom_domain_checked_at": now}
	if !found {
		// eigener Zustand statt "pending", damit die Oberfläche
		// "noch nicht geprüft" von "geprüft, aber fehlgeschlagen" trennt.
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
		// Der Nachweis ist erbracht, aber eine andere Seite trägt die Domain bereits
		// verifiziert oder aktiv: Verifizierte Domains bleiben geschützt. Diese
		// Antwort sieht nur, wer den TXT-Eintrag setzen konnte.
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
	c.JSON(http.StatusOK, gin.H{
		"custom_domain_status": status,
		"verified":             true,
		"message":              "Domain erfolgreich verifiziert. Bitte richte nun den CNAME auf " + target + " ein.",
		"txt_record_value":     expected,
		"cname_target":         target,
		"verified_at":          now,
	})
}

// customDomainLiveOnOtherPage meldet, ob eine andere (nicht gelöschte) Seite
// dieselbe Domain im Status verified oder active trägt.
func (h *Handler) customDomainLiveOnOtherPage(page models.StatusPage) (bool, error) {
	var n int64
	err := h.DB.Model(&models.StatusPage{}).
		Where("lower(custom_domain) = ? AND id != ? AND custom_domain_status IN ('verified','active')", strings.ToLower(page.CustomDomain), page.ID).
		Count(&n).Error
	return n > 0, err
}

// saveCustomDomainCheck schreibt nur die Prüf-Spalten, und nur wenn Domain und
// Token noch die sind, gegen die geprüft wurde: Der DNS-Lookup
// dauert, in der Zwischenzeit kann die Domain geändert oder entfernt worden
// sein; ein Save der ganzen Zeile würde das überschreiben (eine entfernte
// Domain käme als "verified" zurück). Bei Fehler ist die Antwort (500 bzw. 409
// bei zwischenzeitlicher Änderung) geschrieben und false zurückgegeben. Weisen
// zwei Seiten dieselbe Domain gleichzeitig nach, passiert beide die Vorabprüfung
// (customDomainLiveOnOtherPage); erst der partielle Unique-Index
// idx_status_pages_custom_domain_live entscheidet. Der Verlierer bekommt wie bei
// der Vorabprüfung 409 domain_taken statt 500.
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

// GET /api/public/resolve-domain?host=... — öffentlicher Lookup, der einen
// per Custom Domain aufgerufenen Host auf den zugehörigen Statusseiten-Slug
// abbildet (siehe frontend/src/views/public/PublicRoot.vue). Nur Domains im
// Status "verified"/"active" werden aufgelöst. Der Status wird hier nie
// verändert: Ein anonymer Aufruf beweist weder funktionierendes DNS noch ein
// ausgestelltes Zertifikat; den Wechsel verified -> active löst allein der
// interne Check aus (InternalCustomDomainCheckHandler). Private Seiten
// liefern ihren Slug nur an Mitglieder, für alle anderen sieht die Antwort aus
// wie bei einer unbekannten Domain.
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

// customDomainCheckTokenRequired meldet, ob der interne Check ein
// CADDY_INTERNAL_TOKEN verlangt: überall außer in ausdrücklichen
// Entwicklungsumgebungen (APP_ENV development, dev, test, local). Fail closed: Eine
// vergessene oder abweichend geschriebene APP_ENV ("prod") öffnet den Endpunkt
// nicht.
func customDomainCheckTokenRequired() bool {
	return !database.IsDevelopmentAppEnv()
}

// WarnIfCustomDomainCheckUnprotected schreibt beim Start eine Warnung, wenn der
// interne Domain-Check wegen fehlendem CADDY_INTERNAL_TOKEN alle Anfragen mit
// 503 abweist (eigene Domains funktionieren dann nicht).
func WarnIfCustomDomainCheckUnprotected() {
	if customDomainCheckTokenRequired() && os.Getenv("CADDY_INTERNAL_TOKEN") == "" {
		slog.Warn("CADDY_INTERNAL_TOKEN nicht gesetzt: /api/internal/custom-domain-check antwortet mit 503, eigene Domains (On-Demand-TLS) sind deaktiviert")
	}
}

// GET /api/internal/custom-domain-check?domain=... — von Caddys On-Demand-TLS
// ("ask"-Endpunkt) aufgerufen, bevor ein Zertifikat für eine unbekannte
// Domain angefordert wird. Antwortet 200 nur für Domains im Status
// "verified"/"active", sonst 404 - verhindert, dass beliebige, nur per DNS
// auf unseren Server gezeigte Fremd-Domains kostenlose Zertifikate über
// unseren Server erschleichen können (Caddy stellt sonst für jede Domain ein
// Zertifikat aus, deren TLS-Handshake bei uns ankommt).
//
// Der Endpunkt ist ein Orakel für Kundendomains und verlangt deshalb außerhalb
// der Entwicklung das gemeinsame Secret CADDY_INTERNAL_TOKEN (Header
// X-Internal-Token). Ist es nicht gesetzt, antwortet er immer mit 503 und
// gibt nichts frei; ein falscher oder fehlender Token ergibt 404. Eine
// freigegebene Domain im Status "verified" wird hier auf "active" gesetzt:
// Caddy fragt unmittelbar vor dem Ausstellen des Zertifikats.
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
		// Nur hochstufen, wenn sich die Zeile seit dem Lesen nicht geändert hat.
		h.DB.Model(&models.StatusPage{}).
			Where("id = ? AND custom_domain = ? AND custom_domain_status = 'verified'", page.ID, domain).
			Update("custom_domain_status", "active")
	}

	c.Status(http.StatusOK)
}
