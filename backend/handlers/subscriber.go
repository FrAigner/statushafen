package handlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/netguard"

	"github.com/gin-gonic/gin"
)

// Wiederholte Literale (go:S1192).
const (
	whereSlugEquals  = "slug = ?"
	errPageIDMissing = "page_id parameter missing"
	errInvalidPageID = "Invalid page_id parameter"
)

// webhookLookupIP löst einen Hostnamen zu IPs auf - als Var injizierbar, damit
// Tests DNS-Rebinding simulieren können (unterschiedliche
// Antworten für denselben Host bei aufeinanderfolgenden Aufrufen).
var webhookLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// allowLoopbackForTests wird ausschließlich von
// SetWebhookLoopbackAllowedForTests umgeschaltet, damit Tests aus einem
// externen _test-Package (das webhookLookupIP nicht überschreiben kann, weil
// es unexportiert ist) ihre httptest.NewServer-Fakes auf Loopback-Adressen
// als Webhook-Ziel verwenden können, ohne den SSRF-Schutz selbst zu lockern.
var allowLoopbackForTests = false

// isBlockedWebhookIP identifiziert interne/nicht-routbare Adressen, die als
// Webhook-Ziel nicht erlaubt sind (SSRF-Schutz). Gilt für Webhooks wie für den
// Abruf von SAML-Metadaten. Die Liste liegt seit der Umstellung in netguard, damit
// der Abruf externer Statusseiten dieselbe Prüfung nutzt.
func isBlockedWebhookIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	if allowLoopbackForTests && addr.Unmap().IsLoopback() {
		return false
	}
	return netguard.IsBlockedAddr(addr)
}

// SetWebhookLoopbackAllowedForTests erlaubt Loopback-Adressen als
// Webhook-Ziel für die Dauer eines Tests (z.B. für httptest.NewServer-Fakes
// in DispatchNotifications-Tests) und macht das über registerCleanup (i.d.R.
// t.Cleanup) automatisch wieder rückgängig. Nur für Tests gedacht.
func SetWebhookLoopbackAllowedForTests(registerCleanup func(func())) {
	allowLoopbackForTests = true
	registerCleanup(func() { allowLoopbackForTests = false })
}

// validateWebhookURL lehnt alles außer https-URLs auf öffentlich auflösbare, nicht-interne
// Hosts ab (SSRF-Schutz für TestWebhookHandler, S4). Diese Prüfung allein reicht
// nicht aus, um SSRF endgültig auszuschließen (siehe safeDialContext):
// zwischen dieser Validierung und dem eigentlichen HTTP-Request kann
// sich der DNS-Eintrag ändern (DNS-Rebinding/TOCTOU).
func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return newCodedError("webhook_https_only", "nur https-URLs sind erlaubt")
	}
	ips, err := webhookLookupIP(context.Background(), u.Hostname())
	if err != nil {
		return newCodedError("webhook_host_unresolvable", "host nicht auflösbar")
	}
	for _, ip := range ips {
		if isBlockedWebhookIP(ip) {
			return newCodedError("webhook_internal_address", "interne Adressen sind nicht erlaubt")
		}
	}
	return nil
}

// safeDialContext ersetzt den Default-Dialer von webhookTestClient (SSRF-Restrisiko
// durch DNS-Rebinding/TOCTOU): validateWebhookURL prüft
// den Host einmalig, bevor http.Client.Post() aufgerufen wird - der Standard-
// Dialer würde den Host beim eigentlichen Connect aber ERNEUT auflösen, und
// zwischen beiden Lookups kann ein Angreifer die DNS-Antwort auf eine interne
// Adresse ändern. safeDialContext führt die Auflösung deshalb selbst durch,
// prüft jede zurückgelieferte IP erneut gegen dieselbe Blockliste und
// verbindet sich direkt mit der geprüften Adresse - es gibt also keinen
// zweiten, unabhängigen Lookup mehr, der ein anderes Ergebnis liefern könnte.
// Der ursprüngliche Hostname bleibt für SNI/Zertifikatsprüfung erhalten, da
// http.Transport das TLS-ServerName weiterhin aus der Request-URL ableitet.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = webhookLookupIP(ctx, host)
		if err != nil {
			return nil, err
		}
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var lastErr error
	for _, ip := range ips {
		if isBlockedWebhookIP(ip) {
			lastErr = fmt.Errorf("interne Adresse nicht erlaubt: %s", ip)
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("keine gültige Adresse für %s gefunden", host)
	}
	return nil, lastErr
}

var webhookTestClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: safeDialContext,
	},
}

type SubscribeRequest struct {
	Channel      string `json:"channel" binding:"required"` // "email" | "google_chat"
	Email        string `json:"email"`                      // required when channel=email
	WebhookURL   string `json:"webhook_url"`                // required when channel=google_chat
	ComponentIDs string `json:"component_ids" binding:"required"`
	Slug         string `json:"slug"`
	Language     string `json:"language"` // optional: Mailsprache, nur bei der Seite aktive Codes
}

// validateSubscribeRequest prüft Kanal-abhängige Pflichtfelder inkl. SSRF-Schutz
// für Google-Chat-Webhooks. Ausgelagert aus SubscribeHandler, damit dessen
// kognitive Komplexität die SonarCloud-Grenze (go:S3776) nicht überschreitet.
func validateSubscribeRequest(req SubscribeRequest) error {
	if err := checkSubscriberTexts(req.Email, req.WebhookURL, req.ComponentIDs, req.Language); err != nil {
		return err
	}
	if err := checkInputTexts(textField{req.Slug, maxSlugInputLength}); err != nil {
		return err
	}
	// nur nackte, gültige Adressen - die Bestätigungsmail geht
	// direkt an den gespeicherten Wert.
	if req.Channel == "email" && !isPlainEmailAddress(req.Email) {
		return newCodedError("email_invalid", "Gültige E-Mail-Adresse ist erforderlich.")
	}
	if req.Channel == "google_chat" && req.WebhookURL == "" {
		return newCodedError("webhook_url_required", "Google Chat Webhook-URL ist erforderlich.")
	}
	// SSRF-Schutz: Die öffentliche Anmeldung nimmt nur echte Google-Chat-Webhooks
	// an (siehe validatePublicWebhookURL). Eine bloße Prüfung auf "öffentlich
	// auflösbar" ließe jeden Dritten den Server POSTs an beliebige Ziele senden.
	if req.Channel == "google_chat" {
		if err := validatePublicWebhookURL(req.WebhookURL); err != nil {
			return err
		}
	}
	if req.Slug == "" {
		return newCodedError("slug_required", "Slug ist erforderlich.")
	}
	return nil
}

// publicWebhookHost ist der einzige Host, den die öffentliche Anmeldung
// (POST /api/subscribe) für den Kanal google_chat annimmt.
const publicWebhookHost = "chat.googleapis.com"

// validatePublicWebhookURL lässt für anonyme bzw. selbst angemeldete Abos nur
// https://chat.googleapis.com/v1/spaces/... auf Port 443 ohne Zugangsdaten zu.
// Alles andere (fremder Host, anderer Port, interne oder nicht auflösbare
// Adresse) ergibt denselben Code und es findet keine DNS-Auflösung statt, damit
// weder interne Hostnamen erkennbar sind noch der Server als Absender
// beliebiger POSTs missbraucht wird. Admin-Wege (validateAdminSubscriberWebhook)
// und der Webhook-Test bleiben bei der weiteren Prüfung in validateWebhookURL.
func validatePublicWebhookURL(raw string) error {
	invalid := newCodedError("webhook_url_invalid", "Die Webhook-URL ist ungültig.")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.ToLower(u.Hostname()) != publicWebhookHost {
		return invalid
	}
	if p := u.Port(); p != "" && p != "443" {
		return invalid
	}
	if !strings.HasPrefix(u.Path, "/v1/spaces/") {
		return invalid
	}
	return nil
}

// validateAdminSubscriberWebhook prüft die Webhook-URL eines im Admin
// angelegten/geänderten Google-Chat-Abos. Der
// Admin-Pfad übernahm URLs bisher ungeprüft, während validateSubscribeRequest
// für den öffentlichen Pfad schon validateWebhookURL aufruft -
// ein Seiten-Admin konnte so interne Ziele (Loopback, private Netze, http)
// hinterlegen. Andere Kanäle brauchen keine Webhook-URL.
func validateAdminSubscriberWebhook(channel, webhookURL string) error {
	if channel != "google_chat" {
		return nil
	}
	if webhookURL == "" {
		return newCodedError("webhook_url_required", "Webhook-URL erforderlich")
	}
	return validateWebhookURL(webhookURL)
}

// findExistingSubscriber sucht einen bestehenden Eintrag, gescoped auf die
// Ziel-Statusseite. E-Mail-Abos werden über das Postfach gefunden (mailboxKey),
// nicht über die exakte Schreibweise.
func (h *Handler) findExistingSubscriber(req SubscribeRequest, pageID uint) (models.Subscriber, error) {
	var sub models.Subscriber
	var err error
	if req.Channel == "email" {
		clause, args := mailboxMatch(req.Email)
		err = h.DB.Where("channel = 'email' AND status_page_id = ? AND "+clause, append([]any{pageID}, args...)...).First(&sub).Error
	} else {
		err = h.DB.Where("webhook_url = ? AND channel = 'google_chat' AND status_page_id = ?", req.WebhookURL, pageID).First(&sub).Error
	}
	return sub, err
}

// createSubscriberFromRequest legt einen neuen Subscriber für die Ziel-
// Statusseite an, inkl. Email-Fallback für eingeloggte Google-Chat-Nutzer
// und Verknüpfung mit dem angemeldeten User (falls vorhanden). Rückgabe
// token: Klartext-Bestätigungstoken, "" wenn keine Bestätigung
// nötig ist.
func (h *Handler) createSubscriberFromRequest(c *gin.Context, req SubscribeRequest, pageID uint) (models.Subscriber, string, error) {
	lang, langErr := h.resolveSubscriberLanguage(pageID, req.Language)
	if langErr != nil {
		return models.Subscriber{}, "", langErr
	}
	sub := models.Subscriber{
		StatusPageID:           pageID,
		Channel:                req.Channel,
		Email:                  req.Email,
		WebhookURL:             req.WebhookURL,
		SubscribedComponentIDs: req.ComponentIDs,
		Language:               lang,
	}
	// Fallback: If logged in user creates a webhook subscription, we want to tie it to their account
	if req.Channel == "google_chat" {
		if loggedInEmail := getEmail(c); loggedInEmail != "" && req.Email == "" {
			sub.Email = loggedInEmail
		}
	}
	// Abo mit dem angemeldeten User verknüpfen (falls vorhanden), damit er es
	// später über /me/subscription wiederfindet – unabhängig vom Kanal, denn
	// der Email-Fallback oben greift nur bei google_chat.
	if uid := getUserID(c); uid != 0 {
		sub.CreatedByUserID = &uid
	}
	token := prepareSubscriptionConfirmation(c, &sub)
	err := h.DB.Create(&sub).Error
	return sub, token, err
}

// subscriberConflictMessage liefert die 409-Fehlermeldung für ein bereits
// bestehendes Abo, passend zum Kanal.
func subscriberConflictMessage(channel string) string {
	if channel == "email" {
		return "Diese E-Mail ist bereits abonniert. Bitte logge dich ein, um dein Abo zu bearbeiten."
	}
	return "Dieser Webhook ist bereits abonniert."
}

// subscriberConflictCode ist der Fehlercode zu subscriberConflictMessage
// das Frontend übersetzt ihn unter api.errors.<code>.
func subscriberConflictCode(channel string) string {
	if channel == "email" {
		return "subscription_exists_email"
	}
	return "subscription_exists_webhook"
}

func (h *Handler) SubscribeHandler(c *gin.Context) {
	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Parameter.", "code": "invalid_request"})
		return
	}

	if err := validateSubscribeRequest(req); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	// Statusseiten-Lookup vor die Dedup-Prüfung gezogen, damit diese
	// zusätzlich auf status_page_id einschränken kann - vorher wurde eine
	// bestehende E-Mail/Webhook-Kombination seitenübergreifend gefunden, sodass
	// ein Abo auf Seite "acme" ein weiteres Abo derselben Adresse auf Seite
	// "techstart" fälschlich mit 409 ablehnte (und nebenbei verriet, dass die
	// Adresse anderswo bereits abonniert ist).
	var targetPage models.StatusPage
	if err := h.DB.Where(whereSlugEquals, req.Slug).First(&targetPage).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden.", "code": "page_not_found"})
		return
	}
	// Das Subscriber-Token im Mail-Link gewährt Lesezugriff auf
	// das Dashboard privater Seiten. Ein Abo darf deshalb nur anlegen, wer die
	// Seite ohnehin sehen darf (Rolle auf der Seite oder Global Admin). Bewusst
	// userHasRoleOnPrivatePage statt checkPrivatePageAccess: Letzteres vergibt
	// per Auto-Join nebenbei eine Rolle. Sonst 404 wie beim Dashboard,
	// damit die Existenz der Seite nicht verraten wird.
	if targetPage.IsPrivate && !h.userHasRoleOnPrivatePage(c, targetPage.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden.", "code": "page_not_found"})
		return
	}

	if existing, err := h.findExistingSubscriber(req, targetPage.ID); err == nil {
		// Bestehendes Abo -> kein offenes Update (Sicherheit!), nur das
		// Double-Opt-in eines wartenden Abos geht weiter.
		h.handleExistingSubscription(c, req, existing)
		return
	}

	// Mailbox-Limit: Über dem Limit bleibt die Antwort gleich, es wird aber
	// weder ein Abo angelegt noch eine Mail verschickt.
	if req.Channel == "email" && !isOwnAccountEmail(c, req.Email) && h.confirmationMailBudgetExhausted(req.Email) {
		respondSubscribed(c, models.Subscriber{Channel: "email", Status: models.SubscriberStatusPending})
		return
	}

	sub, token, err := h.createSubscriberFromRequest(c, req, targetPage.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Abonnement fehlgeschlagen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.created", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "status_page_id="+strconv.FormatUint(uint64(targetPage.ID), 10))
	h.queueSubscriptionConfirmation(c, sub, token)
	respondSubscribed(c, sub)
}

func (h *Handler) GetSubscribersHandler(c *gin.Context) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPageIDMissing, "code": "parameter_missing"})
		return
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}

	subs := []models.Subscriber{}
	if err := h.DB.Where("status_page_id = ?", tenantID).Find(&subs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch subscribers", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, h.subscriberDTOs(subs, uint(tenantID), getUserID(c)))
}

// findMySubscriber authentifiziert den eingeloggten User, löst die
// StatusPage über den Query-Parameter "slug" auf und sucht das zugehörige
// eigene Abonnement - gemeinsame Logik für GetMySubscriptionHandler und
// UpdateMySubscriptionHandler (vorher fast identisch dupliziert). Bei einem
// Fehler ist die Response bereits geschrieben, der Aufrufer muss nur noch
// zurückkehren.
func (h *Handler) findMySubscriber(c *gin.Context) (models.Subscriber, bool) {
	email := getEmail(c)
	if email == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Nicht angemeldet", "code": "unauthorized"})
		return models.Subscriber{}, false
	}

	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug erforderlich", "code": "slug_required"})
		return models.Subscriber{}, false
	}

	var page models.StatusPage
	if err := h.DB.Where(whereSlugEquals, slug).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return models.Subscriber{}, false
	}

	// Über die eigene E-Mail-Adresse, und zusätzlich über CreatedByUserID (deckt
	// auch Webhook-Abos ohne Email ab, die z.B. ein Admin angelegt hat). Das
	// Ersteller-Recht gilt nur, solange der User noch zur Seite gehört: Wem die
	// Rolle entzogen wurde, soll die von ihm für Dritte angelegten Abos (fremde
	// Adressen, Webhook-URLs mit Geheimnis) nicht weiter lesen oder ändern.
	query := h.DB.Where("status_page_id = ?", page.ID)
	if uid := getUserID(c); uid != 0 && h.userStillBelongsToPage(uid, page.ID) {
		query = query.Where("created_by_user_id = ? OR email = ?", uid, email)
	} else {
		query = query.Where("email = ?", email)
	}
	var sub models.Subscriber
	if err := query.First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Kein Abonnement gefunden", "code": "subscriber_not_found"})
		return models.Subscriber{}, false
	}

	return sub, true
}

func (h *Handler) GetMySubscriptionHandler(c *gin.Context) {
	sub, ok := h.findMySubscriber(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"channel":       sub.Channel,
		"email":         sub.Email,
		"webhook_url":   sub.WebhookURL,
		"component_ids": sub.SubscribedComponentIDs,
		"status":        sub.Status,
		"language":      sub.Language,
	})
}

func (h *Handler) UpdateMySubscriptionHandler(c *gin.Context) {
	sub, ok := h.findMySubscriber(c)
	if !ok {
		return
	}

	type UpdateReq struct {
		ComponentIDs string `json:"component_ids" binding:"required"`
		// Language: nur wenn angegeben ändern ("" = Seitenstandardsprache).
		Language *string `json:"language"`
	}
	var req UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Parameter", "code": "invalid_request"})
		return
	}

	newLanguage := ""
	if req.Language != nil {
		newLanguage = *req.Language
	}
	if err := checkSubscriberTexts("", "", req.ComponentIDs, newLanguage); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	sub.SubscribedComponentIDs = req.ComponentIDs
	if req.Language != nil {
		lang, err := h.resolveSubscriberLanguage(sub.StatusPageID, *req.Language)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern", "code": "server_error"})
			return
		}
		sub.Language = lang
	}
	if err := h.DB.Save(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.updated", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")

	c.JSON(http.StatusOK, gin.H{"message": "Abo erfolgreich aktualisiert"})
}

// DeleteMySubscriptionHandler beendet das eigene Abonnement
// ("Abonnement beenden"). Besitzprüfung über findMySubscriber, analog zu
// GetMySubscriptionHandler/UpdateMySubscriptionHandler.
func (h *Handler) DeleteMySubscriptionHandler(c *gin.Context) {
	sub, ok := h.findMySubscriber(c)
	if !ok {
		return
	}

	if err := h.DB.Delete(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Beenden des Abonnements", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.deleted", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")

	c.JSON(http.StatusOK, gin.H{"message": "Abonnement beendet"})
}

func (h *Handler) UpdateSubscriberHandler(c *gin.Context) {
	idStr := c.Param("id")
	subID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subscriber ID", "code": "invalid_id"})
		return
	}

	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPageIDMissing, "code": "parameter_missing"})
		return
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}

	var sub models.Subscriber
	if err := h.DB.Where("id = ? AND status_page_id = ?", subID, tenantID).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscriber not found", "code": "subscriber_not_found"})
		return
	}

	type UpdateSubReq struct {
		Channel                string `json:"Channel"`
		Email                  string `json:"Email"`
		WebhookURL             string `json:"WebhookURL"`
		SubscribedComponentIDs string `json:"SubscribedComponentIDs"`
		// Language: nur wenn angegeben ändern ("" = Seitenstandardsprache).
		Language *string `json:"Language"`
	}
	var req UpdateSubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fields", "code": "invalid_request"})
		return
	}
	updateLanguage := ""
	if req.Language != nil {
		updateLanguage = *req.Language
	}
	if err := checkSubscriberTexts(req.Email, req.WebhookURL, req.SubscribedComponentIDs, updateLanguage); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if req.Channel == "email" && !isPlainEmailAddress(req.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email address", "code": "email_invalid"})
		return
	}
	// nur neue Ziele prüfen (geänderte URL oder
	// Kanalwechsel). Eine unveränderte Bestands-URL sperrt das Abo nicht für
	// andere Änderungen; beim Versand schützt ohnehin safeDialContext.
	if req.WebhookURL != sub.WebhookURL || req.Channel != sub.Channel {
		if err := validateAdminSubscriberWebhook(req.Channel, req.WebhookURL); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
	}

	// Eine neue Adresse (oder ein Kanalwechsel) muss erneut
	// bestätigt werden, sonst ließe sich das Double-Opt-in per Bearbeiten
	// umgehen. Reine Komponenten-Änderungen lassen den Status unverändert.
	addressChanged := req.Channel != sub.Channel || req.Email != sub.Email
	sub.Channel = req.Channel
	sub.Email = req.Email
	sub.WebhookURL = req.WebhookURL
	sub.SubscribedComponentIDs = req.SubscribedComponentIDs
	if req.Language != nil {
		lang, err := h.resolveSubscriberLanguage(sub.StatusPageID, *req.Language)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update subscriber", "code": "server_error"})
			return
		}
		sub.Language = lang
	}
	token := ""
	if addressChanged {
		token = prepareSubscriptionConfirmation(c, &sub)
	}

	if err := h.DB.Save(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update subscriber", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.updated", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")
	h.queueSubscriptionConfirmation(c, sub, token)

	c.JSON(http.StatusOK, h.subscriberDTO(sub, getUserID(c)))
}

func (h *Handler) DeleteSubscriberHandler(c *gin.Context) {
	idStr := c.Param("id")
	subID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subscriber ID", "code": "invalid_id"})
		return
	}

	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPageIDMissing, "code": "parameter_missing"})
		return
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}

	var sub models.Subscriber
	if err := h.DB.Where("id = ? AND status_page_id = ?", subID, tenantID).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscriber not found", "code": "subscriber_not_found"})
		return
	}

	if err := h.DB.Delete(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete subscriber", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.deleted", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")
	c.JSON(http.StatusOK, gin.H{"message": "Subscriber deleted successfully"})
}

func (h *Handler) CreateSubscriberAdminHandler(c *gin.Context) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errPageIDMissing, "code": "parameter_missing"})
		return
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}

	type CreateReq struct {
		Channel                string `json:"Channel" binding:"required"`
		Email                  string `json:"Email"`
		WebhookURL             string `json:"WebhookURL"`
		SubscribedComponentIDs string `json:"SubscribedComponentIDs"`
		// Language: Mailsprache, nur bei der Seite aktive Codes, sonst Seitenstandard.
		Language string `json:"Language"`
	}
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Parameter", "code": "invalid_request"})
		return
	}

	if err := checkSubscriberTexts(req.Email, req.WebhookURL, req.SubscribedComponentIDs, req.Language); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if req.Channel == "email" && !isPlainEmailAddress(req.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "E-Mail erforderlich", "code": "email_invalid"})
		return
	}
	if err := validateAdminSubscriberWebhook(req.Channel, req.WebhookURL); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	// Kein zweites Abo derselben Adresse bzw. Webhook-URL: ein erneutes
	// Anlegen würde sonst jedes Mal eine weitere Bestätigungsmail auslösen.
	if _, err := h.findExistingSubscriber(SubscribeRequest{Channel: req.Channel, Email: req.Email, WebhookURL: req.WebhookURL}, uint(tenantID)); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": subscriberConflictMessage(req.Channel), "code": subscriberConflictCode(req.Channel)})
		return
	}
	if req.Channel == "email" {
		if err := h.checkAdminSubscriberLimits(uint(tenantID), req.Email, time.Now()); err != nil {
			// Coded: 429; Datenbankfehler: 500 (fail closed, nichts wird angelegt).
			respondCoded(c, http.StatusTooManyRequests, err)
			return
		}
	}

	lang, langErr := h.resolveSubscriberLanguage(uint(tenantID), req.Language)
	if langErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
		return
	}
	sub := models.Subscriber{
		StatusPageID:           uint(tenantID),
		Channel:                req.Channel,
		Email:                  req.Email,
		WebhookURL:             req.WebhookURL,
		SubscribedComponentIDs: req.SubscribedComponentIDs,
		Language:               lang,
	}
	// Der anlegende (angemeldete) User wird als Ersteller vermerkt, damit er dieses
	// Abo später selbst über /me/subscription wiederfindet und bearbeiten kann –
	// insbesondere bei Google-Chat-Abos ohne Email gab es dafür bisher keine
	// Verknüpfung zu einem Account.
	if uid := getUserID(c); uid != 0 {
		sub.CreatedByUserID = &uid
	}
	// Auch vom Admin eingetragene fremde Adressen müssen ihr Abo
	// selbst bestätigen; die eigene Konto-Adresse des Admins nicht.
	token := prepareSubscriptionConfirmation(c, &sub)

	if err := h.DB.Create(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
		return
	}
	h.queueSubscriptionConfirmation(c, sub, token)

	h.RecordAudit(c, "subscriber.created", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "status_page_id="+strconv.FormatUint(tenantID, 10))
	c.JSON(http.StatusCreated, h.subscriberDTO(sub, getUserID(c)))
}

func (h *Handler) TestWebhookHandler(c *gin.Context) {
	type WebhookReq struct {
		WebhookURL string `json:"webhook_url" binding:"required"`
	}
	var req WebhookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook-URL erforderlich", "code": "webhook_url_required"})
		return
	}

	if err := checkInputTexts(textField{req.WebhookURL, maxWebhookURLLength}); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if err := validateWebhookURL(req.WebhookURL); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	payload := map[string]interface{}{
		"text": "👋 **Test-Benachrichtigung**\nDie Verbindung zu deinem Status-Dashboard funktioniert einwandfrei!",
	}
	// ein Versuch, nie warten; die Fehlerklasse bestimmt die Antwort.
	res := postWebhook(c.Request.Context(), req.WebhookURL, payload, webhookModeProbe)
	switch res.Kind {
	case webhookOK:
	case webhookRateLimited:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Webhook-Ziel drosselt gerade", "code": "webhook_rate_limited", "retry_after": res.RetryAfterSeconds()})
		return
	case webhookPermanent:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Webhook-Ziel hat die Anfrage abgelehnt", "code": "webhook_rejected"})
		return
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Webhook-Ziel nicht erreichbar", "code": "webhook_unreachable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test erfolgreich gesendet!"})
}
