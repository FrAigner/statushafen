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

// generateWebhookToken erzeugt einen kryptografisch zufälligen, hex-kodierten
// Token (32 Byte Entropie -> 64 Zeichen), der als eingehender Monitoring-
// Webhook-Token pro Statusseite verwendet wird.
func generateWebhookToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// POST /api/admin/pages/:page_id/webhook-token — erzeugt (oder erneuert) den
// Webhook-Token einer Statusseite. Ein vorhandener Token wird dabei ungültig
// (überschrieben); der Klartext-Token wird nur in dieser Antwort ausgeliefert.
// In der Datenbank steht nur sein SHA-256-Hash (models.HashToken), er lässt sich
// danach nicht mehr anzeigen.
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
	// Der Klartext-Token wird bewusst nur hier zurückgegeben (analog zu z.B.
	// GitHub Personal Access Tokens) - das Admin-UI muss ihn direkt anzeigen/
	// kopieren lassen, danach ist er nicht mehr abrufbar.
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// DELETE /api/admin/pages/:page_id/webhook-token — widerruft den aktuellen
// Webhook-Token einer Statusseite (falls vorhanden).
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

// POST /api/webhooks/components/:component_id/status — eingehender
// Monitoring-Webhook: setzt den Status einer Komponente, OHNE die
// normale Session/JWT-Middleware zu durchlaufen. Authentifiziert wird
// stattdessen per X-Webhook-Token-Header (oder alternativ ?token=... Query-
// Param, für Monitoring-Tools, die keine Custom-Header setzen können) gegen
// den für die zugehörige Statusseite hinterlegten Token. Nutzt dieselbe
// Statuslogik (setComponentStatus) wie der authentifizierte Admin-Update-
// Handler in component.go, um Validierung/Aggregation nicht zu duplizieren.
func (h *Handler) ComponentStatusWebhookHandler(c *gin.Context) {
	compID, err := strconv.ParseUint(c.Param("component_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component_id parameter", "code": "invalid_id"})
		return
	}

	// Reihenfolge: erst Token, dann Existenz. Eine unbekannte ID (oder eine
	// Komponente ohne Seite) antwortet exakt wie eine bekannte ID mit falschem
	// Token, sonst ließen sich Komponenten-IDs ohne Token abtasten.
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

// webhookTokenValid prüft den mitgeschickten Token (Header, sonst Query)
// konstant-zeitig gegen den gespeicherten Hash des Seiten-Tokens. Kein
// hinterlegter Token (nie generiert oder widerrufen) sperrt den Endpunkt für
// die Seite komplett; auch ein nicht umgestellter Klartextwert passt nie.
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

// maxWebhookBody begrenzt den Body (nur {"status": ...} nötig).
const maxWebhookBody = 4 << 10

// respondWebhookLookupFailed bricht bei einem Datenbankfehler ab (fail closed),
// ohne etwas über Komponente oder Token preiszugeben.
func respondWebhookLookupFailed(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
}

func respondWebhookTokenInvalid(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing webhook token", "code": "webhook_token_invalid"})
}

// POST /api/webhooks/component-slugs/:slug/status — wie
// ComponentStatusWebhookHandler, adressiert die Komponente aber über ihre
// Kennung. Die Statusseite kommt AUSSCHLIESSLICH aus dem Token
// (Request liefert sie nie); die Kennung wird nur innerhalb dieser Seite
// aufgelöst (je Seite eindeutig). Reihenfolge: erst Token, dann
// Kennung — ein ungültiger Token gibt immer 401, egal ob die Kennung existiert
// (keine Enumeration über fremde Seiten). Archivierte Komponenten bleiben
// ansprechbar (wie beim ID-Weg), gelöschte nicht (404, kein Fallback).
// Speichern, Historie und Audit laufen über applyWebhookStatus, also denselben
// Pfad wie der ID-Weg.
func (h *Handler) ComponentStatusWebhookBySlugHandler(c *gin.Context) {
	token := c.GetHeader("X-Webhook-Token")
	if token == "" {
		token = c.Query("token")
	}
	// Find statt First: ein Fehlversuch ist kein Fehler und soll keine
	// "record not found"-Zeile im GORM-Log erzeugen.
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

	// Nur gültige Kennungen werden gesucht: eine leere Kennung träfe sonst
	// eine Komponente ohne Slug (Altdaten); Format wie beim Anlegen.
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

// applyWebhookStatus liest den Status aus dem Body, setzt ihn über
// setComponentStatus (Validierung, Transaktion, Statushistorie), schreibt den
// Audit-Eintrag und antwortet. Gemeinsamer Teil beider Webhook-Wege.
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
		// Aufruf auf eine Gruppe ist ein Konfigurationsfehler des
		// Monitorings; ablehnen (409), damit der Aufrufer es merkt.
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
