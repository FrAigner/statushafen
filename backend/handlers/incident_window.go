package handlers

import (
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Incident lists are readable anonymously (public pages) and grew without limit with the
// number of incidents: every call loaded all rows including components
// and tags into memory. Now every list is bounded; anyone who needs more
// uses limit/offset paging.
const (
	// defaultIncidentListLimit: page size when none is given.
	defaultIncidentListLimit = 50
	// maxIncidentListLimit: upper bound of the page size and of the simultaneously
	// loaded open incidents in the dashboard.
	maxIncidentListLimit = 200
	// maxIncidentListOffset bounds paging so that no call makes the
	// database skip over millions of rows.
	maxIncidentListOffset = 100000

	// incidentNewestFirst is the order of all lists; the ID makes it
	// stable for equal timestamps, otherwise rows would be lost while paging.
	incidentNewestFirst = "COALESCE(started_at, created_at) desc, id desc"
)

func badIncidentWindow(c *gin.Context) {
	respondInvalidInput(c)
}

// parseCount reads an integer parameter with minimum min; if it is missing, def applies.
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

// parseIncidentWindow reads limit (default 50, at least 1, values above 200 are capped to
// 200) and offset (from 0, at most 100000). For an invalid value
// the 400 response has already been written.
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

// incidentStateScope filters by open/resolved ("" = all).
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
