package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Mandanten-Übersicht (Kern-Screen der Enterprise-App)
//
// Aggregiert für JEDE Statusseite: schlechtesten Komponenten-Status,
// Anzahl offener Incidents, Anzahl Abonnenten. Ein einziger Roundtrip statt
// N Client-Calls (löst zugleich das N+1-Risiko aus Review-Befund A3 für dieses
// Dashboard).
//
// Route (in main.go, innerhalb der authGroup):
//   authGroup.GET("/admin/overview", middleware.RequireGlobalAdmin(), h.GetOverviewHandler)
// ─────────────────────────────────────────────────────────────────────────────

type overviewPage struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Status        string `json:"status"` // operational | degraded_performance | partial_outage | major_outage
	OpenIncidents int64  `json:"open_incidents"`
	Subscribers   int64  `json:"subscribers"`
	Components    int64  `json:"components"`
}

type overviewResponse struct {
	Pages       []overviewPage `json:"pages"`
	TotalPages  int            `json:"total_pages"`
	TotalOpen   int64          `json:"total_open_incidents"`
	TotalSubs   int64          `json:"total_subscribers"`
	WorstStatus string         `json:"worst_status"`
}

func (h *Handler) GetOverviewHandler(c *gin.Context) {
	var pages []models.StatusPage
	if err := h.DB.Order("name asc").Find(&pages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load status pages", "code": "server_error"})
		return
	}

	resp := overviewResponse{Pages: make([]overviewPage, 0, len(pages)), WorstStatus: "operational"}
	openStatuses := []string{"investigating", "identified", "monitoring"}

	for _, p := range pages {
		row := overviewPage{ID: p.ID, Name: p.Name, Slug: p.Slug, Status: "operational"}

		// Schlechtester Komponenten-Status (nur echte Komponenten, keine Gruppen).
		var comps []models.Component
		h.DB.Where("status_page_id = ? AND status <> 'group' AND archived_at IS NULL", p.ID).Find(&comps)
		row.Components = int64(len(comps))
		// offene Vorfälle überlagern den gespeicherten Status wie im
		// Dashboard; seit der Umstellung zählen auch gekoppelte externe Komponenten.
		// Bei einem DB-Fehler fehlen sie nur (leere Map = operational).
		effective := h.effectiveSnapshot(p.ID)
		statuses := make([]string, 0, len(effective))
		for _, s := range effective {
			statuses = append(statuses, s)
		}
		row.Status = worstStatus(statuses) // aus component.go wiederverwendet

		h.DB.Model(&models.Incident{}).
			Where("status_page_id = ? AND status IN ?", p.ID, openStatuses).
			Count(&row.OpenIncidents)

		h.DB.Model(&models.Subscriber{}).
			Where("status_page_id = ?", p.ID).
			Count(&row.Subscribers)

		resp.TotalOpen += row.OpenIncidents
		resp.TotalSubs += row.Subscribers
		if statusSeverity(row.Status) > statusSeverity(resp.WorstStatus) {
			resp.WorstStatus = row.Status
		}
		resp.Pages = append(resp.Pages, row)
	}
	resp.TotalPages = len(pages)

	c.JSON(http.StatusOK, resp)
}
