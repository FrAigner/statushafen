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

// Repeated literals (go:S1192).
const (
	whereSlugEquals  = "slug = ?"
	errPageIDMissing = "page_id parameter missing"
	errInvalidPageID = "Invalid page_id parameter"
)

// webhookLookupIP resolves a hostname to IPs - injectable as a var so that
// tests can simulate DNS rebinding (different
// answers for the same host on consecutive calls).
var webhookLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// allowLoopbackForTests is toggled exclusively by
// SetWebhookLoopbackAllowedForTests so that tests from an
// external _test package (which cannot override webhookLookupIP because
// it is unexported) can use loopback addresses for their httptest.NewServer fakes
// as webhook targets without loosening the SSRF protection itself.
var allowLoopbackForTests = false

// isBlockedWebhookIP identifies internal/non-routable addresses that are not allowed as a
// webhook target (SSRF protection). Since the refactoring the list lives in netguard, so that
// fetching external status pages uses the same check.
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

// SetWebhookLoopbackAllowedForTests allows loopback addresses as a
// webhook target for the duration of a test (e.g. for httptest.NewServer fakes
// in DispatchNotifications tests) and automatically undoes this via registerCleanup (usually
// t.Cleanup). Intended for tests only.
func SetWebhookLoopbackAllowedForTests(registerCleanup func(func())) {
	allowLoopbackForTests = true
	registerCleanup(func() { allowLoopbackForTests = false })
}

// validateWebhookURL rejects everything except https URLs to publicly resolvable, non-internal
// hosts (SSRF protection for TestWebhookHandler, S4). This check alone is not
// sufficient to rule out SSRF conclusively (see safeDialContext):
// between this validation and the actual HTTP request the
// DNS entry can change (DNS rebinding/TOCTOU).
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

// safeDialContext replaces the default dialer of webhookTestClient (residual SSRF risk
// from DNS rebinding/TOCTOU): validateWebhookURL checks
// the host once before http.Client.Post() is called - but the default
// dialer would resolve the host AGAIN at the actual connect, and
// between the two lookups an attacker can change the DNS answer to an internal
// address. safeDialContext therefore performs the resolution itself,
// re-checks every returned IP against the same blocklist and
// connects directly to the checked address - so there is no
// second, independent lookup anymore that could return a different result.
// The original hostname is retained for SNI/certificate verification, since
// http.Transport still derives the TLS ServerName from the request URL.
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
	Language     string `json:"language"` // optional: mail language, only codes active on the page
}

// validateSubscribeRequest checks channel-dependent required fields including SSRF protection
// for Google Chat webhooks. Extracted from SubscribeHandler so that its
// cognitive complexity does not exceed the SonarCloud limit (go:S3776).
func validateSubscribeRequest(req SubscribeRequest) error {
	if err := checkSubscriberTexts(req.Email, req.WebhookURL, req.ComponentIDs, req.Language); err != nil {
		return err
	}
	if err := checkInputTexts(textField{req.Slug, maxSlugInputLength}); err != nil {
		return err
	}
	// only bare, valid addresses - the confirmation mail goes
	// directly to the stored value.
	if req.Channel == "email" && !isPlainEmailAddress(req.Email) {
		return newCodedError("email_invalid", "Gültige E-Mail-Adresse ist erforderlich.")
	}
	if req.Channel == "google_chat" && req.WebhookURL == "" {
		return newCodedError("webhook_url_required", "Google Chat Webhook-URL ist erforderlich.")
	}
	// SSRF protection: the public signup accepts only real Google Chat webhooks
	// (see validatePublicWebhookURL). A mere check for "publicly
	// resolvable" would let any third party make the server send POSTs to arbitrary targets.
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

// publicWebhookHost is the only host that the public signup
// (POST /api/subscribe) accepts for the google_chat channel.
const publicWebhookHost = "chat.googleapis.com"

// validatePublicWebhookURL permits for anonymous or self-registered subscriptions only
// https://chat.googleapis.com/v1/spaces/... on port 443 without credentials.
// Everything else (foreign host, other port, internal or unresolvable
// address) yields the same code and no DNS resolution takes place, so that
// neither internal hostnames can be detected nor the server abused as a sender
// of arbitrary POSTs. Admin paths (validateAdminSubscriberWebhook)
// and the webhook test keep the further check in validateWebhookURL.
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

// validateAdminSubscriberWebhook checks the webhook URL of a Google Chat
// subscription created/changed in the admin area. The
// admin path previously took URLs unchecked, while validateSubscribeRequest
// already calls validateWebhookURL for the public path -
// a page admin could thus store internal targets (loopback, private networks, http).
// Other channels need no webhook URL.
func validateAdminSubscriberWebhook(channel, webhookURL string) error {
	if channel != "google_chat" {
		return nil
	}
	if webhookURL == "" {
		return newCodedError("webhook_url_required", "Webhook-URL erforderlich")
	}
	return validateWebhookURL(webhookURL)
}

// findExistingSubscriber looks up an existing entry, scoped to the
// target status page. Email subscriptions are found via the mailbox (mailboxKey),
// not via the exact spelling.
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

// createSubscriberFromRequest creates a new subscriber for the target
// status page, including email fallback for signed-in Google Chat users
// and linking to the signed-in user (if any). Return value
// token: plaintext confirmation token, "" if no confirmation
// is needed.
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
	// Link the subscription to the signed-in user (if any) so they can find it
	// again later via /me/subscription - regardless of channel, because
	// the email fallback above applies only to google_chat.
	if uid := getUserID(c); uid != 0 {
		sub.CreatedByUserID = &uid
	}
	token := prepareSubscriptionConfirmation(c, &sub)
	err := h.DB.Create(&sub).Error
	return sub, token, err
}

// subscriberConflictMessage returns the 409 error message for an already
// existing subscription, matching the channel.
func subscriberConflictMessage(channel string) string {
	if channel == "email" {
		return "Diese E-Mail ist bereits abonniert. Bitte logge dich ein, um dein Abo zu bearbeiten."
	}
	return "Dieser Webhook ist bereits abonniert."
}

// subscriberConflictCode is the error code for subscriberConflictMessage;
// the frontend translates it under api.errors.<code>.
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

	// Status page lookup moved before the dedup check so that it
	// can additionally restrict to status_page_id - previously an
	// existing email/webhook combination was found across pages, so that
	// a subscription on page "acme" wrongly rejected another subscription of the same address on page
	// "techstart" with 409 (and incidentally revealed that the
	// address is already subscribed elsewhere).
	var targetPage models.StatusPage
	if err := h.DB.Where(whereSlugEquals, req.Slug).First(&targetPage).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden.", "code": "page_not_found"})
		return
	}
	// The subscriber token in the mail link grants read access to
	// the dashboard of private pages. A subscription may therefore only be created by someone who may
	// see the page anyway (role on the page or Global Admin). Deliberately
	// userHasRoleOnPrivatePage instead of checkPrivatePageAccess: the latter grants
	// a role as a side effect via auto-join. Otherwise 404 as for the dashboard,
	// so the existence of the page is not revealed.
	if targetPage.IsPrivate && !h.userHasRoleOnPrivatePage(c, targetPage.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden.", "code": "page_not_found"})
		return
	}

	if existing, err := h.findExistingSubscriber(req, targetPage.ID); err == nil {
		// Existing subscription -> no open update (security!), only the
		// double opt-in of a pending subscription continues.
		h.handleExistingSubscription(c, req, existing)
		return
	}

	// Mailbox limit: above the limit the response stays the same, but neither
	// is a subscription created nor a mail sent.
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

// findMySubscriber authenticates the signed-in user, resolves the
// StatusPage via the query parameter "slug" and looks up the associated
// own subscription - shared logic for GetMySubscriptionHandler and
// UpdateMySubscriptionHandler (previously duplicated almost identically). On an
// error the response has already been written, the caller only has to
// return.
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

	// Via the own email address, and additionally via CreatedByUserID (also covers
	// webhook subscriptions without email, e.g. created by an admin). The
	// creator right applies only as long as the user still belongs to the page: whoever
	// had their role revoked should no longer read or change the subscriptions they created for third parties (foreign
	// addresses, webhook URLs with secrets).
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
		// Language: change only if given ("" = page default language).
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

// DeleteMySubscriptionHandler ends the own subscription
// ("Abonnement beenden"). Ownership check via findMySubscriber, analogous to
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
		// Language: change only if given ("" = page default language).
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
	// check only new targets (changed URL or
	// channel change). An unchanged existing URL does not block the subscription for
	// other changes; safeDialContext protects at send time anyway.
	if req.WebhookURL != sub.WebhookURL || req.Channel != sub.Channel {
		if err := validateAdminSubscriberWebhook(req.Channel, req.WebhookURL); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
	}

	// A new address (or a channel change) must be confirmed
	// again, otherwise double opt-in could be bypassed via editing.
	// Pure component changes leave the status unchanged.
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
		// Language: mail language, only codes active on the page, otherwise page default.
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

	// No second subscription for the same address or webhook URL: creating it
	// again would otherwise trigger another confirmation mail every time.
	if _, err := h.findExistingSubscriber(SubscribeRequest{Channel: req.Channel, Email: req.Email, WebhookURL: req.WebhookURL}, uint(tenantID)); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": subscriberConflictMessage(req.Channel), "code": subscriberConflictCode(req.Channel)})
		return
	}
	if req.Channel == "email" {
		if err := h.checkAdminSubscriberLimits(uint(tenantID), req.Email, time.Now()); err != nil {
			// Coded: 429; database error: 500 (fail closed, nothing is created).
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
	// The creating (signed-in) user is recorded as creator so that they can find this
	// subscription later via /me/subscription and edit it themselves -
	// especially for Google Chat subscriptions without email there was previously no
	// link to an account.
	if uid := getUserID(c); uid != 0 {
		sub.CreatedByUserID = &uid
	}
	// Foreign addresses entered by the admin must also confirm their subscription
	// themselves; the admin's own account address need not.
	token := prepareSubscriptionConfirmation(c, &sub)

	if err := h.DB.Create(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
		return
	}
	h.queueSubscriptionConfirmation(c, sub, token)

	h.RecordAudit(c, "subscriber.created", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "status_page_id="+strconv.FormatUint(tenantID, 10))
	c.JSON(http.StatusCreated, h.subscriberDTO(sub, getUserID(c)))
}

// TestStoredSubscriberWebhookHandler sends a test message to the webhook of
// an existing subscriber of the page. The target comes only from the
// database, never from the request: before, a free URL let every signed-in
// account have the server send requests anywhere (security audit, finding 2).
func (h *Handler) TestStoredSubscriberWebhookHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}
	subID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subscriber ID", "code": "invalid_id"})
		return
	}
	var sub models.Subscriber
	if err := h.DB.Where("id = ? AND status_page_id = ?", subID, pageID).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscriber not found", "code": "subscriber_not_found"})
		return
	}
	if sub.Channel != "google_chat" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Abonnent hat keinen Webhook", "code": "channel_invalid"})
		return
	}
	// Re-check: old entries (from before the https rule) or a host whose
	// DNS answer has since become internal.
	if err := validateWebhookURL(sub.WebhookURL); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	payload := map[string]interface{}{
		"text": "👋 **Test-Benachrichtigung**\nDie Verbindung zu deinem Status-Dashboard funktioniert einwandfrei!",
	}
	// one attempt, never wait; the error class determines the response.
	respondWebhookProbe(c, postWebhook(c.Request.Context(), sub.WebhookURL, payload, webhookModeProbe))
}
