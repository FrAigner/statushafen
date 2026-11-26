package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// generateWebhookToken generates a cryptographically random, hex-encoded
// token (32 bytes of entropy -> 64 characters), used as the incoming monitoring
// webhook token per status page.
func generateWebhookToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// POST /api/admin/pages/:page_id/webhook-token — creates (or renews) the
// webhook token of a status page. An existing token becomes invalid in the process
// (overwritten); the plaintext token is delivered only in this response.
// The database holds only its SHA-256 hash (models.HashToken); it cannot be
// displayed again afterwards.
func (h *Handler) GenerateWebhookTokenHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	token, err := generateWebhookToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erzeugen des Tokens"})
		return
	}

	page.WebhookTokenHash = models.HashToken(token)
	if err := h.DB.Save(&page).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern des Tokens", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.webhook_token_generated", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "")
	// The plaintext token is deliberately returned only here (like e.g.
	// GitHub personal access tokens) - the admin UI must show it and
	// let it be copied directly; afterwards it can no longer be retrieved.
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// DELETE /api/admin/pages/:page_id/webhook-token — revokes the current
// webhook token of a status page (if present).
func (h *Handler) RevokeWebhookTokenHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	page.WebhookTokenHash = ""
	if err := h.DB.Save(&page).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Widerrufen des Tokens", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.webhook_token_revoked", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Token widerrufen"})
}

// POST /api/webhooks/components/:component_id/status — incoming
// Monitoring webhook: sets the status of a component WITHOUT passing through the
// normal session/JWT middleware. Authentication happens
// instead via the X-Webhook-Token header (or alternatively the ?token=... query
// param, for monitoring tools that cannot set custom headers) against
// the token stored for the associated status page. Uses the same
// status logic (setComponentStatus) as the authenticated admin update
// handler in component.go, to avoid duplicating validation/aggregation.
func (h *Handler) ComponentStatusWebhookHandler(c *gin.Context) {
	compID, err := strconv.ParseUint(c.Param("component_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component_id parameter", "code": "invalid_id"})
		return
	}

	// Order: token first, then existence. An unknown ID (or a
	// component without a page) answers exactly like a known ID with a wrong
	// token, otherwise component IDs could be probed without a token.
	var component models.Component
	compRes := h.DB.Limit(1).Find(&component, compID)
	if compRes.Error != nil {
		respondWebhookLookupFailed(c)
		return
	}
	var page models.StatusPage
	if compRes.RowsAffected > 0 {
		pageRes := h.DB.Limit(1).Find(&page, component.StatusPageID)
		if pageRes.Error != nil {
			respondWebhookLookupFailed(c)
			return
		}
	}

	if !webhookTokenValid(c, page.WebhookTokenHash) {
		respondWebhookTokenInvalid(c)
		return
	}

	h.applyWebhookStatus(c, &component)
}

// webhookTokenValid checks the supplied token (header, otherwise query)
// in constant time against the stored hash of the page token. No
// stored token (never generated or revoked) blocks the endpoint for
// the page entirely; an unconverted plaintext value never matches either.
func webhookTokenValid(c *gin.Context, pageTokenHash string) bool {
	providedToken := c.GetHeader("X-Webhook-Token")
	if providedToken == "" {
		providedToken = c.Query("token")
	}
	return models.TokenMatches(pageTokenHash, providedToken)
}

func respondComponentNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
}

// maxWebhookBody limits the body (only {"status": ...} needed).
const maxWebhookBody = 4 << 10

// respondWebhookLookupFailed aborts on a database error (fail closed),
// without revealing anything about the component or token.
func respondWebhookLookupFailed(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
}

func respondWebhookTokenInvalid(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing webhook token", "code": "webhook_token_invalid"})
}

// POST /api/webhooks/component-slugs/:slug/status — like
// ComponentStatusWebhookHandler, but addresses the component by its
// identifier. The status page comes EXCLUSIVELY from the token
// (the request never supplies it); the identifier is resolved only within this page
// (unique per page). Order: token first, then
// identifier — an invalid token always gives 401, whether or not the identifier exists
// (no enumeration across other pages). Archived components remain
// addressable (as with the ID route), deleted ones do not (404, no fallback).
// Saving, history and audit go through applyWebhookStatus, i.e. the same
// path as the ID route.
func (h *Handler) ComponentStatusWebhookBySlugHandler(c *gin.Context) {
	token := c.GetHeader("X-Webhook-Token")
	if token == "" {
		token = c.Query("token")
	}
	// Find instead of First: a failed attempt is not an error and should not produce a
	// "record not found" line in the GORM log.
	var page models.StatusPage
	if token == "" {
		respondWebhookTokenInvalid(c)
		return
	}
	res := h.DB.Where("webhook_token = ?", models.HashToken(token)).Limit(1).Find(&page)
	if res.Error != nil || res.RowsAffected == 0 || !webhookTokenValid(c, page.WebhookTokenHash) {
		respondWebhookTokenInvalid(c)
		return
	}

	// Only valid identifiers are looked up: an empty identifier would otherwise match
	// a component without a slug (legacy data); format as when creating.
	slug := c.Param("slug")
	var component models.Component
	if !models.ValidComponentSlug(slug) {
		respondComponentNotFound(c)
		return
	}
	res = h.DB.Where("status_page_id = ? AND slug = ?", page.ID, slug).Limit(1).Find(&component)
	if res.Error != nil || res.RowsAffected == 0 {
		respondComponentNotFound(c)
		return
	}

	h.applyWebhookStatus(c, &component)
}

// applyWebhookStatus reads the status from the body, sets it via
// setComponentStatus (validation, transaction, status history), writes the
// audit entry and responds. Shared part of both webhook routes.
func (h *Handler) applyWebhookStatus(c *gin.Context, component *models.Component) {
	type StatusReq struct {
		Status string `json:"status" binding:"required"`
	}
	var req StatusReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBody)
	if err := c.ShouldBindJSON(&req); err != nil {
		if isBodyTooLarge(err) {
			respondRequestTooLarge(c)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	if err := setComponentStatus(h.DB, component, req.Status, models.StatusSourceWebhook); err != nil {
		// A call on a group is a configuration error of the
		// monitoring; reject (409) so that the caller notices.
		if errors.Is(err, errComponentIsGroup) {
			c.JSON(http.StatusConflict, gin.H{"error": "Component is a group; its status is derived from its children. Use a child component.", "code": "component_is_group"})
			return
		}
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	h.RecordAudit(c, "component.status_webhook_updated", "config", "Component", strconv.FormatUint(uint64(component.ID), 10), component.Name, "")
	c.JSON(http.StatusOK, component)
}
