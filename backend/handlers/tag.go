package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Wiederholte Literale (go:S1192).
const whereTagIDEquals = "tag_id = ?"

// tagNameTakenMessage ist die 409-Meldung, wenn ein aktiver Tag im selben Scope
// den Namen schon trägt (zusätzlich mit Fehlercode tag_name_taken).
const tagNameTakenMessage = "Ein Tag mit diesem Namen existiert in diesem Scope bereits"

// GetTagsHandler liefert den Tag-Katalog (Schritt 3/4).
// Ohne status_page_id werden alle Tags zurückgegeben (global + alle
// Statusseiten) - für die globale Verwaltungsseite, die beide Scopes in
// einer Übersicht zeigt. Mit status_page_id werden nur die für diese Seite
// relevanten Tags geliefert (global + genau diese Seite), analog zum
// page_id-Scoping in GetComponentsHandler.
func (h *Handler) GetTagsHandler(c *gin.Context) {
	tags := []models.Tag{}
	query := h.DB.Order("status_page_id asc, name asc")

	if pageIDStr := c.Query("status_page_id"); pageIDStr != "" {
		pageID, err := strconv.ParseUint(pageIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status_page_id parameter", "code": "invalid_id"})
			return
		}
		query = query.Where("status_page_id IS NULL OR status_page_id = ?", pageID)
	}

	if err := query.Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

// GetPageTagsHandler liefert die für eine Statusseite wählbaren Tags
// globale Tags plus die Tags genau dieser Seite, nie Tags
// anderer Seiten (Mandantentrennung). Anders als GetTagsHandler braucht er
// nur ein Seitenrecht (Route: components:read), damit Seiten-Rollen das
// Tag-Feld in Komponenten-/Vorfallformularen nutzen können. Jeder Tag trägt
// ohnehin status_page_id; das Frontend nutzt es, um globale Tags für
// Seiten-Rollen zu sperren (die Vergabe prüft das Backend zusätzlich).
func (h *Handler) GetPageTagsHandler(c *gin.Context) {
	pageID, ok := parseTenantID(c)
	if !ok {
		return
	}
	if err := h.DB.Select("id").First(&models.StatusPage{}, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}
	tags := []models.Tag{}
	if err := h.DB.Where("status_page_id IS NULL OR status_page_id = ?", pageID).
		Order("status_page_id asc, name asc").Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

// CreateTagHandler legt einen neuen Tag im Katalog an - global (StatusPageID
// == nil) oder an eine Statusseite gebunden, siehe models.Tag.
func (h *Handler) CreateTagHandler(c *gin.Context) {
	type CreateTagReq struct {
		Name         string `json:"name" binding:"required"`
		StatusPageID *uint  `json:"status_page_id"`
	}
	var req CreateTagReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name darf nicht leer sein", "code": "name_required"})
		return
	}

	if req.StatusPageID != nil {
		if err := h.DB.First(&models.StatusPage{}, *req.StatusPageID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
			return
		}
	}

	// Fallunabhängiger Duplikat-Check im selben Scope, konsistent mit den
	// partiellen Unique-Indizes idx_tags_global_name_active / idx_tags_scoped_name_active aus
	// CreateSoftDeleteUniqueIndexes() in backend/database/db.go. Der
	// Pre-Check liefert eine saubere 409-Antwort statt eines rohen DB-Fehlers;
	// die Indizes bleiben zusätzlich als DB-seitiges Sicherheitsnetz gegen Races.
	dupQuery := h.DB.Model(&models.Tag{}).Where("LOWER(name) = LOWER(?)", name)
	if req.StatusPageID != nil {
		dupQuery = dupQuery.Where("status_page_id = ?", *req.StatusPageID)
	} else {
		dupQuery = dupQuery.Where("status_page_id IS NULL")
	}
	var count int64
	if err := dupQuery.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate tag", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
		return
	}

	tag := models.Tag{Name: name, StatusPageID: req.StatusPageID}
	if err := h.DB.Create(&tag).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.created", "config", "Tag", strconv.FormatUint(uint64(tag.ID), 10), tag.Name, "")
	c.JSON(http.StatusCreated, tag)
}

// UpdateTagHandler benennt einen bestehenden Tag um (nennt explizit
// "Anlegen, Umbenennen und Löschen"). Der Scope (global vs. Statusseite) ist
// bewusst nicht änderbar - ein nachträgliches Verschieben zwischen den Scopes
// ist nicht Teil der Anforderung und würde bereits zugewiesene
// ComponentTag/IncidentTag/UserTag-Zeilen in einen inkonsistenten
// Zustand bringen.
func (h *Handler) UpdateTagHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID", "code": "invalid_id"})
		return
	}
	var tag models.Tag
	if err := h.DB.First(&tag, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "code": "tag_not_found"})
		return
	}

	type UpdateTagReq struct {
		Name string `json:"name" binding:"required"`
	}
	var req UpdateTagReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name darf nicht leer sein", "code": "name_required"})
		return
	}

	dupQuery := h.DB.Model(&models.Tag{}).Where("LOWER(name) = LOWER(?) AND id <> ?", name, tag.ID)
	if tag.StatusPageID != nil {
		dupQuery = dupQuery.Where("status_page_id = ?", *tag.StatusPageID)
	} else {
		dupQuery = dupQuery.Where("status_page_id IS NULL")
	}
	var count int64
	if err := dupQuery.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate tag", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
		return
	}

	tag.Name = name
	if err := h.DB.Save(&tag).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.updated", "config", "Tag", strconv.FormatUint(id, 10), tag.Name, "")
	c.JSON(http.StatusOK, tag)
}

// DeleteTagHandler entfernt einen Tag aus dem Katalog. Die Zuweisung von Tags
// zu Components/Incidents/Usern (ComponentTag/IncidentTag/UserTag) ist nicht
// Teil dieser Änderung - beim Löschen müssen die entsprechenden
// Join-Zeilen dennoch aufgeräumt werden.
func (h *Handler) DeleteTagHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID", "code": "invalid_id"})
		return
	}
	var tag models.Tag
	if err := h.DB.First(&tag, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "code": "tag_not_found"})
		return
	}

	// Analog zu DeleteIncidentHandler: Tag nutzt wie alle Modelle hier
	// Soft-Delete (gorm.Model -> UPDATE statt DELETE), das DB-seitige
	// OnDelete:CASCADE auf ComponentTag/IncidentTag/UserTag feuert
	// dabei nie - Join-Zeilen daher explizit aufräumen statt sich auf die
	// FK-Constraint zu verlassen.
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.ComponentTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up component tag links", "code": "server_error"})
		return
	}
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.IncidentTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up incident tag links", "code": "server_error"})
		return
	}
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.UserTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up user tag links", "code": "server_error"})
		return
	}

	if err := h.DB.Delete(&tag).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.deleted", "config", "Tag", strconv.FormatUint(id, 10), tag.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Tag deleted successfully"})
}
