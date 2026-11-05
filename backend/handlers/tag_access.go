package handlers

import (
	"net/http"
	"strings"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// tagNameList joint die Namen einer Tag-Liste komma-separiert für
// Anzeigetexte (Notification-Mails, Webhooks) - Ersatz für das frühere
// einzelne Country-Feld, das jetzt mehreren Tags weichen kann. Leere Liste
// (Ressource ganz ohne Tag) ergibt einen leeren String, kein Platzhalter wie
// "global".
func tagNameList(tags []models.Tag) string {
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// tagIDList extrahiert die IDs einer Tag-Liste - für API-Responses, die dem
// Frontend nur die zugewiesenen Tag-IDs zurückgeben müssen (z.B. Login-/Me-
// Response), nicht die vollen Tag-Objekte.
func tagIDList(tags []models.Tag) []uint {
	ids := make([]uint, len(tags))
	for i, t := range tags {
		ids[i] = t.ID
	}
	return ids
}

// userTagsAndAdmin lädt für den eingeloggten User (aus dem Gin-Context) einen
// frischen Datenbank-Blick auf IsGlobalAdmin sowie die zugewiesenen Tags
// (Schritt 4/4). Bewusst eine frische DB-Abfrage statt
// eines JWT-Claims-Werts (wie bei der alten AllowedCountries) - Tags können
// sich häufiger ändern als Rollen, und eine Abfrage pro Request vermeidet,
// dass ein Nutzer nach Tag-Entzug bis zum nächsten Login weiterhin Zugriff
// über einen veralteten Claim behält.
func (h *Handler) userTagsAndAdmin(c *gin.Context) (tags []models.Tag, isGlobalAdmin bool) {
	userID := getUserID(c)
	if userID == 0 {
		return nil, false
	}
	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return nil, false
	}
	if middleware.EffectiveGlobalAdmin(c, user) {
		return nil, true
	}
	h.DB.Model(&models.Tag{}).
		Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userID).
		Find(&tags)
	return tags, false
}

// tagsForComponent/tagsForIncident laden die aktuell zugewiesenen Tags einer
// Ressource - für den Zugriffs-Check auf BESTEHENDE Komponenten/Incidents
// (z.B. beim Lesen/Löschen), getrennt von den TagIDs aus einem Request-Body
// (bei Create/Update), die erst noch validiert werden müssen.
func (h *Handler) tagsForComponent(componentID uint) []models.Tag {
	return tagsForComponentDB(h.DB, componentID)
}

// tagsForComponentDB liest die Tags über db (z. B. eine Transaktion).
func tagsForComponentDB(db *gorm.DB, componentID uint) []models.Tag {
	var tags []models.Tag
	db.Model(&models.Tag{}).
		Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.component_id = ? AND component_tags.deleted_at IS NULL", componentID).
		Find(&tags)
	return tags
}

// tagsByComponentOfPage liest die Tags aller Komponenten einer Seite in einer
// einzigen Abfrage (Unterabfrage auf die Komponenten der Seite, damit weder eine
// Liste von IDs noch eine Abfrage je Komponente nötig ist). Gelöschte Tags,
// Zuordnungen und Komponenten fehlen wie bei tagsForComponentDB.
func (h *Handler) tagsByComponentOfPage(pageID uint) componentTagMap {
	var rows []struct {
		models.Tag
		OwnerComponentID uint
	}
	h.DB.Model(&models.Tag{}).
		Select("tags.*, component_tags.component_id AS owner_component_id").
		Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.deleted_at IS NULL AND component_tags.component_id IN (?)",
			h.DB.Model(&models.Component{}).Select("id").Where("status_page_id = ?", pageID)).
		Order("tags.id").
		Scan(&rows)
	out := make(componentTagMap, len(rows))
	for _, r := range rows {
		out[r.OwnerComponentID] = append(out[r.OwnerComponentID], r.Tag)
	}
	return out
}

// componentTagMap ordnet Komponenten-IDs ihre Tags zu.
type componentTagMap map[uint][]models.Tag

// of liefert die Tags der Komponente, ohne Tags eine leere Liste (JSON [] statt null).
func (m componentTagMap) of(componentID uint) []models.Tag {
	if tags := m[componentID]; tags != nil {
		return tags
	}
	return []models.Tag{}
}

func (h *Handler) tagsForIncident(incidentID uint) []models.Tag {
	var tags []models.Tag
	h.DB.Model(&models.Tag{}).
		Joins("JOIN incident_tags ON incident_tags.tag_id = tags.id").
		Where("incident_tags.incident_id = ? AND incident_tags.deleted_at IS NULL", incidentID).
		Find(&tags)
	return tags
}

// tagsForUser lädt die Tags eines bestimmten Users direkt per ID, ohne den
// Gin-Context - für Stellen außerhalb eines Requests wie
// DispatchNotifications, das für jeden zu benachrichtigenden internen User
// (nicht nur den eingeloggten Aufrufer) prüfen muss, ob dessen Tags zum
// Incident passen.
func (h *Handler) tagsForUser(userID uint) []models.Tag {
	var tags []models.Tag
	h.DB.Model(&models.Tag{}).
		Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userID).
		Find(&tags)
	return tags
}

// userHasTagAccess kombiniert userTagsAndAdmin + middleware.UserHasAccessToAnyTag.
// Tags sind eine ZUSÄTZLICHE, optionale Eingrenzung oben auf der eigentlichen
// Zugriffskontrolle (RequirePermission/UserStatusPageRole, main.go), nicht deren
// Ersatz - wer per Rolle bereits Schreibrecht auf einer Statusseite hat, soll
// dadurch nicht zusätzlich ausgesperrt werden, nur weil weder er noch die
// Ressource (noch) Tags zugewiesen bekommen haben. Die Prüfung greift daher
// nur dann tatsächlich einschränkend, wenn BEIDE Seiten (User UND Ressource)
// mindestens einen Tag tragen:
//   - Global-Admins umgehen die Prüfung komplett (wie zuvor AllowedCountries="global").
//   - Eine Ressource ganz ohne Tag ist für jeden mit Seitenrecht sichtbar/verwaltbar.
//   - Ein User ganz ohne Tag ist durch Tags nicht eingeschränkt (Tags sind
//     opt-in als Filter, kein verpflichtendes Freischalt-Attribut).
//   - Erst wenn beide Seiten Tags haben, entscheidet middleware.UserHasAccessToAnyTag
//     über eine echte Überschneidung (inkl. globalem Wildcard-Tag).
func (h *Handler) userHasTagAccess(c *gin.Context, resourceTags []models.Tag) bool {
	userTags, isGlobalAdmin := h.userTagsAndAdmin(c)
	if isGlobalAdmin || len(resourceTags) == 0 || len(userTags) == 0 {
		return true
	}
	return middleware.UserHasAccessToAnyTag(userTags, resourceTags)
}

// resolveTagsByIDs lädt und validiert eine Menge von Tag-IDs aus einem
// Request-Body (Create/Update von Component/Incident): jeder Tag
// muss entweder global sein (StatusPageID == nil) oder an GENAU die
// Statusseite gebunden, für die die Ressource angelegt/geändert wird - ein
// Tag einer FREMDEN Statusseite darf nicht zugewiesen werden, sonst könnte
// ein Admin mit Rechten auf Seite A eine Ressource auf Seite B über einen
// dort fremden Tag sichtbar machen. Leere tagIDs sind erlaubt (Ressource ganz
// ohne Tag) - das ist kein Wildcard, sondern fail-closed, also
// bewusst restriktiver als der alte "leer = global"-Default.
func (h *Handler) resolveTagsByIDs(tagIDs []uint, statusPageID uint) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}
	var tags []models.Tag
	if err := h.DB.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
	}
	for _, t := range tags {
		if t.StatusPageID != nil && *t.StatusPageID != statusPageID {
			// Gleiche Antwort wie bei einer unbekannten ID und ohne Tag-Namen:
			// sonst verriete die Meldung Existenz und Namen von Tags anderer
			// Mandanten (Tag-IDs sind aufsteigende Ganzzahlen).
			return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
		}
	}
	return tags, nil
}

// errGlobalTagForbidden ist der Fehlercode, wenn eine Seiten-Rolle einen
// GLOBALEN Tag vergeben will.
const errGlobalTagForbidden = "global_tag_forbidden"

// userMayAssignTags prüft, dass der Aufrufer die Tags vergeben
// darf: Global Admins dürfen alles, Seiten-Rollen nur seiteneigene Tags.
// Globale Tags (z. B. Regions-Tags, die auch Nutzerzugriffe steuern) bleiben
// Global Admins vorbehalten. Globale Tags, die die Ressource bereits trägt
// (current), dürfen beim Bearbeiten unverändert mitgeschickt werden, sonst
// könnte eine Seiten-Rolle einen Eintrag mit globalem Tag nicht mehr
// speichern. Bei Verstoß wird 403 mit Code geschrieben und false geliefert.
func (h *Handler) userMayAssignTags(c *gin.Context, newTags, current []models.Tag) bool {
	_, isGlobalAdmin := h.userTagsAndAdmin(c)
	if isGlobalAdmin {
		return true
	}
	have := make(map[uint]bool, len(current))
	for _, t := range current {
		have[t.ID] = true
	}
	for _, t := range newTags {
		if t.StatusPageID == nil && !have[t.ID] {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Forbidden: Globale Tags dürfen nur Global Admins vergeben",
				"code":  errGlobalTagForbidden,
			})
			return false
		}
	}
	return true
}

// resolveGlobalTagsByIDs ist die User-spezifische Variante von
// resolveTagsByIDs: User-Tags sind - wie schon AllowedCountries
// zuvor - bewusst seitenübergreifend, ein User bekommt also ausschließlich
// globale Tags (StatusPageID == nil) zugewiesen, keine seitengebundenen.
func (h *Handler) resolveGlobalTagsByIDs(tagIDs []uint) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}
	var tags []models.Tag
	if err := h.DB.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
	}
	for _, t := range tags {
		if t.StatusPageID != nil {
			return nil, newCodedError("tag_page_bound", "ein seitengebundener Tag kann keinem User zugewiesen werden")
		}
	}
	return tags, nil
}

// replaceComponentTags/replaceIncidentTags ersetzen die Tag-Zuordnung einer
// Ressource vollständig (Delete-alle + Insert-neue) - dasselbe einfache,
// konsistente Muster wie UserStatusPageRole-Neusetzung in user.go, statt
// eines Diffs zwischen altem und neuem Tag-Set.
func (h *Handler) replaceComponentTags(tx *gorm.DB, componentID uint, tags []models.Tag) error {
	if err := tx.Where("component_id = ?", componentID).Delete(&models.ComponentTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.ComponentTag{ComponentID: componentID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) replaceIncidentTags(tx *gorm.DB, incidentID uint, tags []models.Tag) error {
	if err := tx.Where("incident_id = ?", incidentID).Delete(&models.IncidentTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.IncidentTag{IncidentID: incidentID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) replaceUserTags(tx *gorm.DB, userID uint, tags []models.Tag) error {
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.UserTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.UserTag{UserID: userID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

// incidentTagFilterScope ist das Tag-Pendant zu middleware.GeoFilterScope
// schränkt eine Incident-Query auf die Incidents ein, die der
// eingeloggte User laut Tag-Zugriff sehen darf. Wie userHasTagAccess ist das
// eine ZUSÄTZLICHE, optionale Eingrenzung oben auf der eigentlichen
// Seiten-Zugriffskontrolle, kein Ersatz dafür (siehe dortiger Kommentar) -
// gefiltert wird nur, wenn der User TATSÄCHLICH (nicht-globale) Tags trägt.
// Ohne Tags beim User, bei Global-Admins oder beim reservierten globalen
// User-Tag gibt es keinen Filter (volle Sicht auf alle Incidents dieser
// Seite). Ein Incident ganz ohne eigenen Tag bleibt zusätzlich immer
// sichtbar, unabhängig von den Tags des Users.
func (h *Handler) incidentTagFilterScope(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	userTags, isGlobalAdmin := h.userTagsAndAdmin(c)

	hasGlobalUserTag := false
	var userTagIDs []uint
	for _, t := range userTags {
		if middleware.IsGlobalTag(t) {
			hasGlobalUserTag = true
		}
		userTagIDs = append(userTagIDs, t.ID)
	}

	return func(db *gorm.DB) *gorm.DB {
		if isGlobalAdmin || hasGlobalUserTag || len(userTags) == 0 {
			return db
		}
		return db.Where(
			`NOT EXISTS (
				SELECT 1 FROM incident_tags it0
				WHERE it0.incident_id = incidents.id AND it0.deleted_at IS NULL
			)
			OR EXISTS (
				SELECT 1 FROM incident_tags it
				JOIN tags t ON t.id = it.tag_id
				WHERE it.incident_id = incidents.id
				AND it.deleted_at IS NULL
				AND (it.tag_id IN ? OR (t.status_page_id IS NULL AND LOWER(t.name) = 'global'))
			)`,
			userTagIDs,
		)
	}
}
