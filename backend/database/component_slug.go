package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// BackfillComponentSlugs assigns to all active components and
// groups without an identifier one derived from the name. Order and result are
// deterministic: per status page ascending by ID, collisions (also with
// already assigned identifiers) get -2, -3 …. Idempotent: only
// rows with an empty identifier are touched, assigned identifiers stay. Soft-
// deleted rows stay empty, the unique index excludes them anyway.
//
// Prod-safe: errors only lead to a warning, never to a startup abort. A
// row without an identifier is retried on the next start.
func BackfillComponentSlugs(db *gorm.DB) {
	var missing []models.Component
	if err := db.Where("slug = ?", "").Order("status_page_id ASC, id ASC").Find(&missing).Error; err != nil {
		slog.Warn("Komponenten-Kennungen: Bestand konnte nicht gelesen werden", "error", err)
		return
	}
	if len(missing) == 0 {
		return
	}
	used := map[uint]map[string]bool{} // status_page_id -> assigned identifiers
	usedOn := func(pageID uint) map[string]bool {
		if set, ok := used[pageID]; ok {
			return set
		}
		var slugs []string
		db.Model(&models.Component{}).Where("status_page_id = ? AND slug <> ?", pageID, "").Pluck("slug", &slugs)
		set := make(map[string]bool, len(slugs))
		for _, s := range slugs {
			set[s] = true
		}
		used[pageID] = set
		return set
	}
	for _, comp := range missing {
		set := usedOn(comp.StatusPageID)
		base := models.ComponentSlugBase(comp.Name)
		slug := base
		for n := 2; set[slug]; n++ {
			slug = models.ComponentSlugWithSuffix(base, n)
		}
		if err := db.Model(&models.Component{}).Where("id = ? AND slug = ?", comp.ID, "").Update("slug", slug).Error; err != nil {
			slog.Warn("Komponenten-Kennung konnte nicht vergeben werden", "component_id", comp.ID, "error", err)
			continue
		}
		set[slug] = true
	}
	slog.Info("Komponenten-Kennungen vergeben", "anzahl", len(missing))
}
