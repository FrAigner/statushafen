package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Checks and responses for the identifier (slug) of a component.
// Error codes: slug_invalid (400, format/length), slug_taken (409, already assigned on the
// status page), slug_immutable (400, change after creation).

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

// checkNewComponentSlug checks an identifier given on creation for format
// and uniqueness on the status page (only active components, like the
// partial unique index). Empty = automatic, nothing to check. On a
// violation the response is written and false is returned.
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
