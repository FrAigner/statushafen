package handlers

import (
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Vorfall-Listen sind anonym lesbar (öffentliche Seiten) und wuchsen mit der
// Zahl der Vorfälle unbegrenzt: Jeder Aufruf lud alle Zeilen samt Komponenten
// und Tags in den Speicher. Jetzt ist jede Liste begrenzt; wer mehr braucht,
// blättert mit limit/offset.
const (
	// defaultIncidentListLimit: Seitengröße ohne Angabe.
	defaultIncidentListLimit = 50
	// maxIncidentListLimit: Obergrenze der Seitengröße und der gleichzeitig
	// geladenen offenen Vorfälle im Dashboard.
	maxIncidentListLimit = 200
	// maxIncidentListOffset begrenzt das Blättern, damit kein Aufruf die
	// Datenbank über Millionen Zeilen springen lässt.
	maxIncidentListOffset = 100000

	// incidentNewestFirst ist die Reihenfolge aller Listen; die ID macht sie
	// bei gleichem Zeitpunkt stabil, sonst gingen beim Blättern Zeilen verloren.
	incidentNewestFirst = "COALESCE(started_at, created_at) desc, id desc"
)

func badIncidentWindow(c *gin.Context) {
	respondInvalidInput(c)
}

// parseCount liest einen Ganzzahl-Parameter ab min; fehlt er, gilt def.
func parseCount(c *gin.Context, name string, def, minValue int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minValue {
		return 0, false
	}
	return n, true
}

// parseIncidentWindow liest limit (Standard 50, mindestens 1, über 200 wird auf
// 200 gekappt) und offset (ab 0, höchstens 100000). Bei einem ungültigen Wert
// ist die 400-Antwort schon geschrieben.
func parseIncidentWindow(c *gin.Context) (limit, offset int, ok bool) {
	limit, ok = parseCount(c, "limit", defaultIncidentListLimit, 1)
	if !ok {
		badIncidentWindow(c)
		return 0, 0, false
	}
	offset, ok = parseCount(c, "offset", 0, 0)
	if !ok || offset > maxIncidentListOffset {
		badIncidentWindow(c)
		return 0, 0, false
	}
	return min(limit, maxIncidentListLimit), offset, true
}

// incidentStateScope filtert nach offen/behoben ("" = alle).
func incidentStateScope(state string) (func(*gorm.DB) *gorm.DB, bool) {
	switch state {
	case "":
		return func(db *gorm.DB) *gorm.DB { return db }, true
	case "open":
		return func(db *gorm.DB) *gorm.DB {
			return db.Where("incidents.status NOT IN ?", models.ResolvedIncidentStatuses)
		}, true
	case "resolved":
		return func(db *gorm.DB) *gorm.DB { return db.Where("incidents.status IN ?", models.ResolvedIncidentStatuses) }, true
	}
	return nil, false
}
