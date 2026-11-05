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

// Wartungen und Vorfälle teilen sich Tabelle und Routen. Die
// Rechte-Ressource folgt deshalb der Art des Datensatzes: "maintenance" für
// Wartungsfenster (IsMaintenance), sonst "incidents". Die Resolver laufen in
// middleware.RequireResolvedPermission; ein unbekannter Datensatz gilt als
// "incidents" (der Handler antwortet danach selbst mit 404); Lese- und
// Datenbankfehler brechen dagegen ab.
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

// maxKindBody begrenzt den Body der Anlegen-Route. Ein Vorfall oder
// eine Wartung trägt höchstens 5.000 Zeichen Text, ein paar IDs und Zeiten;
// 64 KiB reichen großzügig. Größere Bodys werden mit 413 abgewiesen.
const maxKindBody = 64 << 10

// IncidentResourceFromBody: Art aus dem Feld IsMaintenance des Anlegen-Bodys.
// Der Body wird für den Handler wiederhergestellt. Ein unlesbarer, zu großer
// oder kaputter Body bricht die Anfrage mit 413/400 ab, statt
// still auf "incidents" zurückzufallen: sonst bekäme der Handler einen Body,
// den die Arterkennung nie gesehen hat.
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

// readLimitedBody liest den Body vollständig, höchstens limit Bytes; mehr
// ergibt *http.MaxBytesError. Ein fehlender Body gilt als leer.
func readLimitedBody(c *gin.Context, limit int64) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	return io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
}

// isBodyTooLarge: der Fehler stammt vom MaxBytesReader.
func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// respondRequestTooLarge antwortet mit 413 und dem Code request_too_large
// (Text de/en unter api.errors.request_too_large).
func respondRequestTooLarge(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Anfrage zu groß", "code": "request_too_large"})
}

// respondBodyError bricht die Anfrage ab: 413 bei zu großem Body, sonst 400.
func respondBodyError(c *gin.Context, err error) {
	if isBodyTooLarge(err) {
		respondRequestTooLarge(c)
		return
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
}

// abortKindLookup bricht bei einem Datenbankfehler der Arterkennung mit 500 ab.
// "Nicht gefunden" ist kein Fehler dieser Art: Der Handler antwortet dann selbst
// mit 404 (und kennt die Art des fehlenden Datensatzes nie).
func abortKindLookup(c *gin.Context, err error) string {
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("Arterkennung (Vorfall/Wartung) fehlgeschlagen", "path", c.FullPath(), "error", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
	}
	return resourceIncidents
}

// IncidentResourceByParam: Art des Datensatzes, dessen ID im Pfad-Parameter
// param steht. Auf Routen mit :page_id wird die Seite mitgeprüft, damit ein
// Datensatz einer fremden Seite die Art nicht verrät. Eine ungültige ID oder ein
// unbekannter Datensatz gilt als "incidents" (der Handler antwortet mit 400/404,
// legt aber nichts an); ein Datenbankfehler bricht mit 500 ab.
func (h *Handler) IncidentResourceByParam(param string) func(c *gin.Context) string {
	return func(c *gin.Context) string {
		id, err := strconv.ParseUint(c.Param(param), 10, 32)
		if err != nil {
			return resourceIncidents
		}
		return h.incidentKindByID(c, uint(id), c.Param("page_id"))
	}
}

// IncidentResourceByTranslation: Art des Vorfalls, zu dem die Übersetzung
// :translation_id gehört.
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
