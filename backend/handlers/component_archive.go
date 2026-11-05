package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Komponenten archivieren statt löschen. Archivierte Komponenten
// behalten Verlauf, Verfügbarkeit, Vorfall-Zuordnungen und Übersetzungen und
// lassen sich wiederherstellen; öffentlich sind sie wie unsichtbare Komponenten
// ausgeblendet (hiddenComponentIDs). Die Statusänderung per Webhook bleibt
// möglich (wie bei unsichtbaren), damit ein Monitoring nicht in Fehler läuft.

// descendantComponentIDs liefert die IDs aller Nachfahren von rootID in all.
// Besuchte IDs schützen vor Zyklen in Altdaten.
func descendantComponentIDs(all []models.Component, rootID uint) []uint {
	children := map[uint][]uint{}
	for _, comp := range all {
		if comp.ParentID != nil {
			children[*comp.ParentID] = append(children[*comp.ParentID], comp.ID)
		}
	}
	seen := map[uint]bool{rootID: true}
	var out []uint
	queue := []uint{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, ch := range children[id] {
			if !seen[ch] {
				seen[ch] = true
				out = append(out, ch)
				queue = append(queue, ch)
			}
		}
	}
	return out
}

// loadComponentForArchive lädt die Komponente der Seite und prüft Tag-Zugriff;
// bei Fehlern ist die Antwort schon geschrieben (ok=false).
func (h *Handler) loadComponentForArchive(c *gin.Context) (comp models.Component, tenantID uint, ok bool) {
	compID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return comp, 0, false
	}
	tenantID, ok = parseTenantID(c)
	if !ok {
		return comp, 0, false
	}
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&comp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return comp, 0, false
	}
	if !h.userHasTagAccess(c, h.tagsForComponent(comp.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return comp, 0, false
	}
	return comp, tenantID, true
}

// cascadeAccess lädt die Tag-Rechte des Aufrufers (einmal, VOR der Transaktion)
// und liefert eine Prüfung für die IDs der Kaskade. Die Komponenten-Tags kommen
// mit zwei Abfragen für alle IDs, damit die Zeilensperre nicht während hunderter
// Roundtrips gehalten wird. Verhalten wie userHasTagAccess.
func (h *Handler) cascadeAccess(c *gin.Context) func(db *gorm.DB, ids []uint) (bool, error) {
	userTags, isAdmin := h.userTagsAndAdmin(c)
	return func(db *gorm.DB, ids []uint) (bool, error) {
		if isAdmin || len(userTags) == 0 || len(ids) == 0 {
			return true, nil
		}
		var links []models.ComponentTag
		if err := db.Where("component_id IN ?", ids).Find(&links).Error; err != nil {
			return false, err
		}
		if len(links) == 0 {
			return true, nil
		}
		tagIDs := make([]uint, 0, len(links))
		for _, l := range links {
			tagIDs = append(tagIDs, l.TagID)
		}
		var tags []models.Tag
		if err := db.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
			return false, err
		}
		byID := make(map[uint]models.Tag, len(tags))
		for _, tg := range tags {
			byID[tg.ID] = tg
		}
		perComp := map[uint][]models.Tag{}
		for _, l := range links {
			if tg, ok := byID[l.TagID]; ok {
				perComp[l.ComponentID] = append(perComp[l.ComponentID], tg)
			}
		}
		for _, id := range ids {
			if rt := perComp[id]; len(rt) > 0 && !middleware.UserHasAccessToAnyTag(userTags, rt) {
				return false, nil
			}
		}
		return true, nil
	}
}

// errCascadeForbidden bricht die Transaktion ab, wenn die Tag-Rechte fehlen.
var (
	errCascadeForbidden = errors.New("cascade forbidden")
	errParentArchived   = errors.New("parent archived")
)

// isActiveIn: ob die Komponente id in all nicht archiviert ist.
func isActiveIn(all []models.Component, id uint) bool {
	for _, x := range all {
		if x.ID == id {
			return x.ArchivedAt == nil
		}
	}
	return false
}

// archivedAtIn: ob die Komponente id in all genau zum Zeitpunkt at archiviert wurde.
func archivedAtIn(all []models.Component, id uint, at time.Time) bool {
	for _, x := range all {
		if x.ID == id {
			return x.ArchivedAt != nil && x.ArchivedAt.Equal(at)
		}
	}
	return false
}

// lockedPageComponents liest alle Komponenten der Seite. Auf Postgres mit
// Zeilensperre (FOR NO KEY UPDATE in fester Reihenfolge: reicht für archived_at und kollidiert nicht mit den FK-Prüfungen parallel angelegter Vorfälle, Deadlock-frei), damit gleichzeitiges Umhängen/Archivieren bis zum
// Ende der Transaktion wartet; SQLite kennt die Klausel nicht und
// serialisiert Schreiber selbst.
func lockedPageComponents(tx *gorm.DB, tenantID uint) ([]models.Component, error) {
	var all []models.Component
	err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("status_page_id = ?", tenantID).Order("id").Find(&all).Error
	return all, err
}

// hasArchivedAncestor: ob irgendein Vorfahr von comp archiviert ist (ganze
// Kette, nicht nur der direkte Elternknoten). Besuchte IDs schützen vor Zyklen.
func hasArchivedAncestor(all []models.Component, comp models.Component) bool {
	byID := make(map[uint]models.Component, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	seen := map[uint]bool{comp.ID: true}
	for cur := comp.ParentID; cur != nil && !seen[*cur]; {
		seen[*cur] = true
		parent, ok := byID[*cur]
		if !ok {
			return false
		}
		if parent.ArchivedAt != nil {
			return true
		}
		cur = parent.ParentID
	}
	return false
}

func (h *Handler) archiveNow() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ArchiveComponentHandler archiviert eine Komponente samt Nachfahren (eine
// Gruppe nimmt ihre Mitglieder mit). Bereits archivierte Einträge behalten
// ihren Zeitpunkt; ein zweiter Aufruf ändert nichts (idempotent).
func (h *Handler) ArchiveComponentHandler(c *gin.Context) {
	comp, tenantID, ok := h.loadComponentForArchive(c)
	if !ok {
		return
	}
	if comp.ArchivedAt != nil {
		c.JSON(http.StatusOK, comp)
		return
	}
	now := h.archiveNow()
	allowed := h.cascadeAccess(c)
	changed := true
	// Lesen, Prüfen und Schreiben in EINER Transaktion, sonst bliebe
	// ein gleichzeitig eingehängtes aktives Element unter der archivierten Gruppe aktiv.
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		all, err := lockedPageComponents(tx, tenantID)
		if err != nil {
			return err
		}
		// Die Wurzel kommt aus dem gesperrten Stand: wurde sie inzwischen
		// archiviert, bleibt der Aufruf ohne Schreiben und Audit (idempotent).
		root, found := componentIn(all, comp.ID)
		if !found {
			return gorm.ErrRecordNotFound
		}
		comp = root
		if root.ArchivedAt != nil {
			changed = false
			return nil
		}
		// Nur die Komponenten, die sich wirklich ändern (noch aktiv), brauchen ein Tag-Recht.
		var ids []uint
		for _, id := range append(descendantComponentIDs(all, comp.ID), comp.ID) {
			if isActiveIn(all, id) {
				ids = append(ids, id)
			}
		}
		ok, err := allowed(tx, ids)
		if err != nil {
			return err
		}
		if !ok {
			return errCascadeForbidden
		}
		return tx.Model(&models.Component{}).
			Where("id IN ? AND status_page_id = ? AND archived_at IS NULL", ids, tenantID).
			Update("archived_at", now).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}
	if errors.Is(err, errCascadeForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage all components of this group", "code": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to archive component", "code": "server_error"})
		return
	}
	if !changed {
		c.JSON(http.StatusOK, comp)
		return
	}
	comp.ArchivedAt = &now
	h.RecordAudit(c, "component.archived", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, comp)
}

// componentIn sucht die Komponente id in all.
func componentIn(all []models.Component, id uint) (models.Component, bool) {
	for _, x := range all {
		if x.ID == id {
			return x, true
		}
	}
	return models.Component{}, false
}

// RestoreComponentHandler stellt eine Komponente wieder her. Liegt sie in einer
// archivierten Gruppe, ist das nur über die Gruppe möglich (409
// parent_archived). Bei einer Gruppe kommen die Mitglieder zurück, die mit ihr
// zusammen archiviert wurden (gleicher Zeitpunkt); einzeln früher archivierte
// bleiben archiviert.
func (h *Handler) RestoreComponentHandler(c *gin.Context) {
	comp, tenantID, ok := h.loadComponentForArchive(c)
	if !ok {
		return
	}
	if comp.ArchivedAt == nil {
		c.JSON(http.StatusOK, comp)
		return
	}
	allowed := h.cascadeAccess(c)
	changed := true
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		all, err := lockedPageComponents(tx, tenantID)
		if err != nil {
			return err
		}
		// Wurzel aus dem gesperrten Stand (Zeitpunkt und Eltern können sich seit
		// dem Laden geändert haben).
		root, found := componentIn(all, comp.ID)
		if !found {
			return gorm.ErrRecordNotFound
		}
		comp = root
		if root.ArchivedAt == nil {
			changed = false
			return nil
		}
		if hasArchivedAncestor(all, root) {
			return errParentArchived
		}
		// Nur die Komponenten, die tatsächlich zurückkommen (die Wurzel und
		// die mit ihr zusammen archivierten), brauchen ein Tag-Recht.
		at := *root.ArchivedAt
		ids := []uint{comp.ID}
		for _, id := range descendantComponentIDs(all, comp.ID) {
			if archivedAtIn(all, id, at) {
				ids = append(ids, id)
			}
		}
		ok, err := allowed(tx, ids)
		if err != nil {
			return err
		}
		if !ok {
			return errCascadeForbidden
		}
		return tx.Model(&models.Component{}).
			Where("id IN ? AND status_page_id = ? AND (id = ? OR archived_at = ?)", ids, tenantID, comp.ID, at).
			Update("archived_at", nil).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}
	if errors.Is(err, errParentArchived) {
		c.JSON(http.StatusConflict, gin.H{"error": "Parent group is archived: restore the group first", "code": "parent_archived"})
		return
	}
	if errors.Is(err, errCascadeForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage all components of this group", "code": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore component", "code": "server_error"})
		return
	}
	if !changed {
		c.JSON(http.StatusOK, comp)
		return
	}
	comp.ArchivedAt = nil
	h.RecordAudit(c, "component.restored", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, comp)
}
