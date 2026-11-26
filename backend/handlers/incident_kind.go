package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Maintenances and incidents share a table and routes. The
// permission resource therefore follows the kind of the record: "maintenance" for
// maintenance windows (IsMaintenance), otherwise "incidents". The resolvers run in
// middleware.RequireResolvedPermission; an unknown record counts as
// "incidents" (the handler then answers with 404 itself); read and
// database errors, on the other hand, abort.
const (
	resourceIncidents   = "incidents"
	resourceMaintenance = "maintenance"
)

func incidentResource(isMaintenance bool) string {
	if isMaintenance {
		return resourceMaintenance
	}
	return resourceIncidents
}

// maxKindBody limits the body of the create route. An incident or
// a maintenance carries at most 5,000 characters of text, a few IDs and times;
// 64 KiB is more than enough. Larger bodies are rejected with 413.
const maxKindBody = 64 << 10

// IncidentResourceFromBody: kind from the IsMaintenance field of the create body.
// The body is restored for the handler. An unreadable, too large
// or broken body aborts the request with 413/400 instead of
// silently falling back to "incidents": otherwise the handler would get a body
// that the kind detection never saw.
func (h *Handler) IncidentResourceFromBody() func(c *gin.Context) string {
	return func(c *gin.Context) string {
		raw, err := readLimitedBody(c, maxKindBody)
		if err != nil {
			respondBodyError(c, err)
			return resourceIncidents
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var probe struct {
			IsMaintenance bool `json:"IsMaintenance"`
		}
		if json.Unmarshal(raw, &probe) != nil {
			respondBodyError(c, errBodyInvalid)
			return resourceIncidents
		}
		return incidentResource(probe.IsMaintenance)
	}
}

var errBodyInvalid = errors.New("body invalid")

// readLimitedBody reads the body completely, at most limit bytes; more
// yields *http.MaxBytesError. A missing body counts as empty.
func readLimitedBody(c *gin.Context, limit int64) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	return io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
}

// isBodyTooLarge: the error comes from the MaxBytesReader.
func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// respondRequestTooLarge answers with 413 and the code request_too_large
// (text de/en under api.errors.request_too_large).
func respondRequestTooLarge(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Anfrage zu groß", "code": "request_too_large"})
}

// respondBodyError aborts the request: 413 for a too large body, otherwise 400.
func respondBodyError(c *gin.Context, err error) {
	if isBodyTooLarge(err) {
		respondRequestTooLarge(c)
		return
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
}

// abortKindLookup aborts with 500 on a database error in the kind detection.
// "Not found" is not an error of this kind: the handler then answers
// with 404 itself (and never knows the kind of the missing record).
func abortKindLookup(c *gin.Context, err error) string {
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("Arterkennung (Vorfall/Wartung) fehlgeschlagen", "path", c.FullPath(), "error", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
	}
	return resourceIncidents
}

// IncidentResourceByParam: kind of the record whose ID is in the path parameter
// param. On routes with :page_id the page is checked as well, so that a
// record of a foreign page does not reveal the kind. An invalid ID or an
// unknown record counts as "incidents" (the handler answers with 400/404,
// but creates nothing); a database error aborts with 500.
func (h *Handler) IncidentResourceByParam(param string) func(c *gin.Context) string {
	return func(c *gin.Context) string {
		id, err := strconv.ParseUint(c.Param(param), 10, 32)
		if err != nil {
			return resourceIncidents
		}
		return h.incidentKindByID(c, uint(id), c.Param("page_id"))
	}
}

// IncidentResourceByTranslation: kind of the incident to which the translation
// :translation_id belongs.
func (h *Handler) IncidentResourceByTranslation() func(c *gin.Context) string {
	return func(c *gin.Context) string {
		id, err := strconv.ParseUint(c.Param("translation_id"), 10, 32)
		if err != nil {
			return resourceIncidents
		}
		var tr models.IncidentTranslation
		if err := h.DB.Select("id", "incident_id").First(&tr, id).Error; err != nil {
			return abortKindLookup(c, err)
		}
		return h.incidentKindByID(c, tr.IncidentID, c.Param("page_id"))
	}
}

func (h *Handler) incidentKindByID(c *gin.Context, id uint, pageID string) string {
	q := h.DB.Select("id", "is_maintenance", "status_page_id")
	if pageID != "" {
		q = q.Where("status_page_id = ?", pageID)
	}
	var inc models.Incident
	if err := q.First(&inc, id).Error; err != nil {
		return abortKindLookup(c, err)
	}
	return incidentResource(inc.IsMaintenance)
}
