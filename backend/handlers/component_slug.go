package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Prüfungen und Antworten zur Kennung (Slug) einer Komponente.
// Fehlercodes: slug_invalid (400, Format/Länge), slug_taken (409, auf der
// Statusseite schon vergeben), slug_immutable (400, Änderung nach dem Anlegen).

func respondSlugInvalid(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "Slug must consist of lowercase letters, digits and single hyphens (max. 60 characters)",
		"code":  "slug_invalid",
	})
}

func respondSlugTaken(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": "Slug is already used by another component on this status page", "code": "slug_taken"})
}

func respondSlugImmutable(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Slug cannot be changed after creation", "code": "slug_immutable"})
}

// checkNewComponentSlug prüft eine beim Anlegen vorgegebene Kennung auf Format
// und Eindeutigkeit auf der Statusseite (nur aktive Komponenten, wie der
// partielle Unique-Index). Leer = automatisch, nichts zu prüfen. Bei einem
// Verstoß ist die Antwort geschrieben und die Rückgabe false.
func (h *Handler) checkNewComponentSlug(c *gin.Context, tenantID uint, slug string) bool {
	if slug == "" {
		return true
	}
	if !models.ValidComponentSlug(slug) {
		respondSlugInvalid(c)
		return false
	}
	var count int64
	if err := h.DB.Model(&models.Component{}).Where("status_page_id = ? AND slug = ?", tenantID, slug).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create component", "code": "server_error"})
		return false
	}
	if count > 0 {
		respondSlugTaken(c)
		return false
	}
	return true
}
