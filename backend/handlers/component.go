package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// allowedComponentStatuses sind die zulässigen Werte für Component.Status, die
// gezielt per Status-Update (Admin-UI oder Monitoring-Webhook) gesetzt
// werden dürfen. "group" ist bewusst ausgeschlossen - das ist ein reiner
// Ordner-Knoten, dessen Status stets aus den Kindern aggregiert wird (siehe
// buildTree) und nie direkt gesetzt werden soll.
const componentStatusGroup = "group"

// errComponentIsGroup: Statusschreiben auf eine Gruppe.
var errComponentIsGroup = errors.New("component is a group; its status is derived from its children")

var allowedComponentStatuses = map[string]bool{
	"operational":          true,
	"degraded_performance": true,
	"partial_outage":       true,
	"major_outage":         true,
	"under_maintenance":    true,
}

// setComponentStatus validiert den übergebenen Status und persistiert ihn auf
// der Komponente. Zentrale Stelle für die Statuslogik, damit sowohl der
// Admin-Update-Handler als auch der Monitoring-Webhook-Handler
// dieselbe Validierung/Speicherung verwenden statt sie zu duplizieren.
//
// Speichern und Historieneintrag laufen in EINER Transaktion; der
// alte Stand wird unter Zeilensperre frisch gelesen (nicht aus dem evtl.
// veralteten component), damit parallele Schreiber keinen falschen Wechsel
// protokollieren. source: models.StatusSource* (manual/webhook).
func setComponentStatus(db *gorm.DB, component *models.Component, status, source string) error {
	// Der Status einer Gruppe wird aus den Kindern berechnet und
	// darf von keinem Schreiber (Webhook, Quick-Status, Admin) ersetzt werden.
	if component.Status == componentStatusGroup {
		return errComponentIsGroup
	}
	if !allowedComponentStatuses[status] {
		return newCodedError("invalid_component_status", fmt.Sprintf("invalid status: %q", status))
	}
	return db.Transaction(func(tx *gorm.DB) error {
		old, err := statushistory.Lock(tx, component.ID)
		if err != nil {
			return err
		}
		prev := component.Status
		component.Status = status
		if err := tx.Omit("archived_at").Save(component).Error; err != nil {
			component.Status = prev
			return err
		}
		return statushistory.Record(tx, component.ID, old, status, source, time.Now())
	})
}

// statusSeverity/worstStatus delegieren seit der Umstellung an das Paket
// componentstatus, das auch der Abfrage-Dienst für externe Statusseiten nutzt.
func statusSeverity(s string) int { return componentstatus.Severity(s) }

func worstStatus(statuses []string) string { return componentstatus.Worst(statuses...) }

// effectiveComponentStatus: Logik seit der Umstellung in componentstatus
// (FromOpenIncident/Own), siehe dort.
func effectiveComponentStatus(manualStatus, incidentSeverity string) string {
	return componentstatus.Own(manualStatus, incidentSeverity)
}

// slugRetryAttempts: Versuche beim Anlegen mit automatischer Kennung, falls
// eine parallele Anlage dieselbe Kennung vergibt.
const slugRetryAttempts = 3

// Feldlängen in Zeichen (Runen, nicht Bytes, wegen Umlauten).
const (
	maxComponentNameLen        = 60
	maxComponentDescriptionLen = 240
)

// validateComponentTexts prüft Name und Beschreibung gegen die Längengrenzen
// und liefert bei Verstoß den maschinenlesbaren Fehlercode
// (name_too_long/description_too_long), den das Frontend am Feld übersetzt.
// nil heißt "Feld nicht gesendet, nicht prüfen". Bestehende längere Werte
// werden nie abgeschnitten; sie fallen erst beim nächsten Speichern des
// betreffenden Feldes auf.
func validateComponentTexts(name, description *string) (code, msg string) {
	if name != nil && utf8.RuneCountInString(*name) > maxComponentNameLen {
		return "name_too_long", fmt.Sprintf("Name must not exceed %d characters", maxComponentNameLen)
	}
	if description != nil && utf8.RuneCountInString(*description) > maxComponentDescriptionLen {
		return "description_too_long", fmt.Sprintf("Description must not exceed %d characters", maxComponentDescriptionLen)
	}
	return "", ""
}

// respondComponentTextTooLong beantwortet die Längenprüfung mit 400 und Code.
func respondComponentTextTooLong(c *gin.Context, code, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": code})
}

// hiddenComponentIDs: die öffentliche Regel steht in models.HiddenComponentIDs
// (gemeinsam mit der Selbstüberwachung).
func hiddenComponentIDs(all []models.Component) map[uint]bool {
	return models.HiddenComponentIDs(all)
}

// withoutHiddenComponents entfernt unsichtbare Komponenten samt Nachfahren
// aus der flachen Liste, für alle öffentlichen Sichten.
func withoutHiddenComponents(all []models.Component) []models.Component {
	hidden := hiddenComponentIDs(all)
	if len(hidden) == 0 {
		return all
	}
	out := make([]models.Component, 0, len(all))
	for _, comp := range all {
		if !hidden[comp.ID] {
			out = append(out, comp)
		}
	}
	return out
}

// callerMaySeeHiddenComponents: Global Admin oder eine Rolle auf der Seite, die
// Komponenten bzw. Vorfälle liest oder schreibt (die Vorfall- und Wartungs-
// formulare brauchen auch unsichtbare Komponenten zur Auswahl).
func (h *Handler) callerMaySeeHiddenComponents(c *gin.Context, pageID uint) bool {
	for _, perm := range []string{"components:read", "incidents:read", "maintenance:read", "external_sources:read"} {
		if h.userHasPermissionOnPrivatePage(c, pageID, perm) {
			return true
		}
	}
	return false
}

// stripHiddenIncidentComponents entfernt unsichtbare Komponenten aus den
// Zuordnungen der Vorfälle, damit ihr Name öffentlich nicht über
// einen Vorfall durchsickert. Der Vorfall selbst bleibt sichtbar.
func stripHiddenIncidentComponents(incidents []models.Incident, hidden map[uint]bool) {
	if len(hidden) == 0 {
		return
	}
	for i := range incidents {
		kept := make([]models.IncidentComponent, 0, len(incidents[i].IncidentComponents))
		for _, ic := range incidents[i].IncidentComponents {
			if !hidden[ic.ComponentID] {
				kept = append(kept, ic)
			}
		}
		incidents[i].IncidentComponents = kept
	}
}

// buildTree baut aus einer flachen Liste einen Baum und aggregiert Statuses nach oben
func buildTree(all []models.Component, parentID *uint) []models.Component {
	// nicht-nil, damit leere Listen als [] statt null serialisiert werden.
	nodes := []models.Component{}
	for i := range all {
		comp := all[i]
		if (parentID == nil && comp.ParentID == nil) ||
			(parentID != nil && comp.ParentID != nil && *comp.ParentID == *parentID) {
			// Kinder rekursiv laden
			comp.Children = buildTree(all, &comp.ID)
			// Gruppe kennzeichnen, bevor Status gleich durch den
			// Kindstatus ersetzt wird. Sonst kann der Admin eine Gruppe nicht
			// von einer normalen Komponente unterscheiden.
			comp.IsGroup = comp.Status == componentStatusGroup
			// Status aggregieren: wenn Gruppe, schlechtesten Kindstatus hochreichen
			if comp.IsGroup && len(comp.Children) > 0 {
				// archivierte Kinder zählen nicht in den Status der
				// aktiven Ansicht; eine selbst archivierte Gruppe aggregiert alle.
				childStatuses := make([]string, 0, len(comp.Children))
				childEffective := make([]string, 0, len(comp.Children))
				for _, ch := range comp.Children {
					if ch.ArchivedAt != nil && comp.ArchivedAt == nil {
						continue
					}
					childStatuses = append(childStatuses, ch.Status)
					childEffective = append(childEffective, ch.EffectiveStatus)
				}
				if len(childStatuses) > 0 {
					comp.Status = worstStatus(childStatuses)
					// der effektive Status (falls annotiert) ebenso.
					if comp.EffectiveStatus != "" {
						comp.EffectiveStatus = worstStatus(childEffective)
					}
				}
			}
			nodes = append(nodes, comp)
		}
	}
	// Sortierung nach SortOrder
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].SortOrder < nodes[j].SortOrder
	})
	return nodes
}

func (h *Handler) GetComponentsHandler(c *gin.Context) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_id parameter missing", "code": "invalid_request"})
		return
	}

	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, tenantID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, "Status page not found", "page_not_found") {
		return
	}

	all := []models.Component{}
	// id als zweites Kriterium, damit gleiche SortOrder (z. B. 0 bei
	// älteren Komponenten) stabil nach Anlage sortiert wird; Postgres garantiert
	// bei Gleichstand sonst keine Reihenfolge.
	if err := h.DB.Where("status_page_id = ?", tenantID).Order("sort_order asc, id asc").Find(&all).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch components", "code": "server_error"})
		return
	}
	// Diese Route ist auch anonym lesbar (main.go). Unsichtbare
	// Komponenten sieht nur, wer die Seite verwaltet; alle anderen bekommen den
	// gefilterten Baum wie auf der öffentlichen Statusseite.
	if !h.callerMaySeeHiddenComponents(c, page.ID) {
		all = withoutHiddenComponents(all)
	}
	tagsByComponent := h.tagsByComponentOfPage(page.ID)
	for i := range all {
		all[i].Tags = tagsByComponent.of(all[i].ID)
	}
	// Status bleibt der gespeicherte Wert (Admin-Formular),
	// effective_status/external_sources kommen dazu.
	h.annotateEffectiveStatus(page.ID, all, h.componentIDsWithOpenIncidents(page.ID))

	tree := buildTree(all, nil)
	c.JSON(http.StatusOK, tree)
}

func (h *Handler) CreateComponentHandler(c *gin.Context) {
	type CreateReq struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		// TagIDs ersetzt das frühere Country-Freitextfeld. Leer
		// bedeutet "kein Tag" - anders als beim alten Country-Default fällt
		// das NICHT automatisch auf "global" zurück (fail-closed).
		TagIDs []uint `json:"tag_ids"`
		// Status beim Anlegen (leer = operational) und Eltern-Komponente.
		Status   string `json:"status"`
		ParentID *uint  `json:"parent_id"`
		// true legt eine Gruppe an. Eine Gruppe hat keinen eigenen
		// Status (er wird aus den Kindern berechnet, siehe buildTree); ein
		// mitgeschickter status ist deshalb ein Fehler statt still ignoriert.
		IsGroup bool `json:"is_group"`
		// nil (Feld fehlt) = sichtbar.
		Visible *bool `json:"visible"`
		// leer = automatisch aus dem Namen.
		Slug string `json:"slug"`
	}
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if code, msg := validateComponentTexts(&req.Name, &req.Description); code != "" {
		respondComponentTextTooLong(c, code, msg)
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	if !h.checkNewComponentSlug(c, tenantID, req.Slug) {
		return
	}

	status, err := initialComponentStatus(req.IsGroup, req.Status)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	var parentID *uint
	if req.ParentID != nil && *req.ParentID != 0 {
		if err := h.validateParent(tenantID, 0, *req.ParentID); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
		parentID = req.ParentID
	}

	tags, err := h.resolveTagsByIDs(req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if !h.userHasTagAccess(c, tags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to create components with these tags", "code": "forbidden"})
		return
	}
	if !h.userMayAssignTags(c, tags, nil) {
		return
	}

	comp := models.Component{
		StatusPageID: tenantID,
		Name:         req.Name,
		Slug:         req.Slug,
		Description:  req.Description,
		Status:       status,
		ParentID:     parentID,
		SortOrder:    h.nextSiblingSortOrder(tenantID, parentID),
		Visible:      req.Visible == nil || *req.Visible,
	}
	err = h.createComponent(&comp, tags, req.Visible)
	if isUniqueViolation(err) { // Race nach der Vorabprüfung
		respondSlugTaken(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create component", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "component.created", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	comp.Tags = tags
	comp.IsGroup = req.IsGroup
	c.JSON(http.StatusCreated, comp)
}

// createComponent legt die Komponente samt Tags in einer Transaktion an.
// GORM ersetzt den Nullwert false beim Create durch den Tag-Default true; ein
// unsichtbar angelegter Eintrag wird deshalb nachgezogen. Hat der Aufrufer
// keine Kennung vorgegeben, vergibt BeforeCreate eine; kollidiert sie im
// Wettlauf mit einer parallelen Anlage (Unique-Index), wird mit einer neu
// berechneten Kennung wiederholt.
func (h *Handler) createComponent(comp *models.Component, tags []models.Tag, visible *bool) error {
	autoSlug := comp.Slug == ""
	var err error
	for attempt := 0; attempt < slugRetryAttempts; attempt++ {
		err = h.DB.Transaction(func(tx *gorm.DB) error {
			if autoSlug {
				comp.Slug = ""
			}
			if err := tx.Create(comp).Error; err != nil {
				return err
			}
			// Anfangsstatus als erster Eintrag der Historie.
			if err := statushistory.Record(tx, comp.ID, "", comp.Status, models.StatusSourceCreated, time.Now()); err != nil {
				return err
			}
			if visible != nil && !*visible {
				if err := tx.Model(comp).Update("visible", false).Error; err != nil {
					return err
				}
				comp.Visible = false
			}
			return h.replaceComponentTags(tx, comp.ID, tags)
		})
		if !autoSlug || !isUniqueViolation(err) {
			return err
		}
		comp.ID = 0
	}
	return err
}

// initialComponentStatus liefert den Anfangsstatus einer neuen Komponente bzw.
// Gruppe. Gruppen bekommen den Marker-Status "group" und dürfen
// keinen eigenen Status mitbringen; Komponenten: leer = operational, sonst
// muss der Wert zulässig sein.
func initialComponentStatus(isGroup bool, requested string) (string, error) {
	if isGroup {
		if requested != "" {
			return "", newCodedError("group_has_no_status", "a group has no status of its own")
		}
		return componentStatusGroup, nil
	}
	if requested == "" {
		return "operational", nil
	}
	if !allowedComponentStatuses[requested] {
		return "", newCodedError("invalid_component_status", fmt.Sprintf("invalid status: %q", requested))
	}
	return requested, nil
}

// nextSiblingSortOrder liefert die SortOrder für eine neue Komponente unter
// parentID (nil = oberste Ebene): eins mehr als die höchste ihrer Geschwister.
// Vorher bekam jede neue Komponente den Default 0 und landete so
// vor allen Geschwistern mit gepflegter Reihenfolge (Seed: 1, 2, 3 …), statt
// wie erwartet am Ende.
func (h *Handler) nextSiblingSortOrder(tenantID uint, parentID *uint) int {
	q := h.DB.Model(&models.Component{}).Where("status_page_id = ?", tenantID)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	var maxOrder int
	// Bei einem Lesefehler bleibt maxOrder 0; die Reihenfolge ist dann nur
	// kosmetisch falsch, das Anlegen soll daran nicht scheitern.
	q.Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder)
	return maxOrder + 1
}

type componentUpdateReq struct {
	Name string `json:"name"`
	// nicht mehr binding:"required", weil Gruppen keinen Status
	// mitschicken müssen; für normale Komponenten prüft der Handler selbst.
	Status string `json:"status"`
	// nil (Feld fehlt) = unverändert, "" = Beschreibung leeren.
	Description *string `json:"description"`
	TagIDs      *[]uint `json:"tag_ids"` // nil = unverändert lassen, [] = Tags entfernen
	// nil = unverändert, 0 = Wurzel, sonst ID der Eltern-Komponente.
	ParentID *uint `json:"parent_id"`
	// nil = unverändert.
	Visible *bool `json:"visible"`
	// die Kennung ist unveränderlich; erlaubt ist nur der
	// unveränderte Wert (das Formular schickt ihn mit), jeder andere ergibt 400.
	Slug *string `json:"slug"`
}

// applyParentChange setzt die neue Eltern-Komponente (0 = oberste Ebene) und
// schreibt bei Fehlern die Antwort (false). Eine archivierte
// Komponente bleibt in ihrer Gruppe (409), sonst umginge das Umhängen die Regel
// „Wiederherstellen nur über die Gruppe“; eine unveränderte Zuordnung (das
// Formular schickt sie mit) ist erlaubt und wird nicht neu geprüft.
func (h *Handler) applyParentChange(c *gin.Context, component *models.Component, tenantID, parentID uint) bool {
	return h.applyParentChangeDB(c, h.DB, component, tenantID, parentID)
}

// applyParentChangeDB wie applyParentChange, liest aber über db (
// innerhalb der Transaktion mit Zeilensperre).
func (h *Handler) applyParentChangeDB(c *gin.Context, db *gorm.DB, component *models.Component, tenantID, parentID uint) bool {
	// frisch lesen; wurde die Komponente seit dem Laden archiviert,
	// gilt der aktuelle Stand (sonst landete ein archiviertes Element oben).
	var fresh models.Component
	if err := db.Select("id", "archived_at").First(&fresh, component.ID).Error; err == nil {
		component.ArchivedAt = fresh.ArchivedAt
	}
	if component.ArchivedAt != nil {
		if sameParent(component.ParentID, parentID) {
			return true
		}
		c.JSON(http.StatusConflict, gin.H{"error": "Archived component cannot be moved: restore it first", "code": "component_archived"})
		return false
	}
	if parentID == 0 {
		component.ParentID = nil
		return true
	}
	if err := validateParentDB(db, tenantID, component.ID, parentID); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return false
	}
	component.ParentID = &parentID
	return true
}

// sameParent: ob die angeforderte parent_id (0 = oberste Ebene) der aktuellen entspricht.
func sameParent(current *uint, requested uint) bool {
	if current == nil {
		return requested == 0
	}
	return *current == requested
}

// validateParent prüft, ob parentID als Eltern-Komponente von
// compID (0 = neue Komponente) taugt: sie muss auf derselben Statusseite
// existieren (Mandantentrennung), darf nicht die Komponente selbst sein und
// keinen Zyklus erzeugen (Eltern ist Nachfahre von compID).
func (h *Handler) validateParent(tenantID, compID, parentID uint) error {
	return validateParentDB(h.DB, tenantID, compID, parentID)
}

func validateParentDB(db *gorm.DB, tenantID, compID, parentID uint) error {
	if parentID == compID {
		return newCodedError("parent_is_self", "component cannot be its own parent")
	}
	var parent models.Component
	if err := db.Where("id = ? AND status_page_id = ?", parentID, tenantID).First(&parent).Error; err != nil {
		return newCodedError("parent_not_found", "parent component not found")
	}
	// in eine archivierte Gruppe lässt sich nichts verschieben.
	if parent.ArchivedAt != nil {
		return newCodedError("parent_archived", "parent group is archived")
	}
	// Von der Eltern-Komponente aufwärts laufen; begegnet dabei compID, wäre es ein Zyklus.
	// Besuchte IDs verhindern Endlosschleifen bei einem bereits bestehenden Zyklus
	// (Altlast, direkter DB-Eingriff), der compID nicht enthält.
	visited := map[uint]bool{parent.ID: true}
	for cur := parent.ParentID; cur != nil; {
		if *cur == compID {
			return newCodedError("parent_cycle", "parent would create a cycle")
		}
		if visited[*cur] {
			return newCodedError("parent_cycle", "parent chain is cyclic")
		}
		visited[*cur] = true
		var anc models.Component
		// Fehlt ein Vorfahr (gelöscht), bleibt anc.ParentID nil und die Schleife endet.
		db.Select("id", "parent_id").First(&anc, *cur)
		cur = anc.ParentID
	}
	return nil
}

// resolveComponentUpdateTags löst req.TagIDs (falls gesetzt) zu Tags auf und
// prüft die Zugriffsberechtigung darauf. ok=true mit newTags=nil bedeutet
// "TagIDs war nil, Tags unverändert lassen". Ausgelagert aus
// UpdateComponentHandler, damit dessen kognitive Komplexität die
// SonarCloud-Grenze (go:S3776) nicht überschreitet.
func (h *Handler) resolveComponentUpdateTags(c *gin.Context, req componentUpdateReq, tenantID uint, current []models.Tag) (newTags []models.Tag, ok bool) {
	if req.TagIDs == nil {
		return nil, true
	}
	newTags, err := h.resolveTagsByIDs(*req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return nil, false
	}
	if !h.userHasTagAccess(c, newTags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to move components to these tags", "code": "forbidden"})
		return nil, false
	}
	if !h.userMayAssignTags(c, newTags, current) {
		return nil, false
	}
	return newTags, true
}

// saveComponentUpdate speichert die geänderte Komponente. Normale Komponenten
// laufen über setComponentStatus (Validierung + Speichern), Gruppen behalten
// ihren Status "group". Ausgelagert, damit die kognitive
// Komplexität von UpdateComponentHandler nicht weiter steigt (go:S3776).
func (h *Handler) saveComponentUpdate(db *gorm.DB, component *models.Component, isGroup bool, status string) error {
	if isGroup {
		return db.Omit("archived_at").Save(component).Error
	}
	return setComponentStatus(db, component, status, models.StatusSourceManual)
}

// errParentChangeRejected bricht die Transaktion ab, wenn die Antwort schon geschrieben ist.
var errParentChangeRejected = errors.New("parent change rejected")

// saveWithParentChange: Sperre der Seite, Prüfung der Eltern und Speichern in
// einer Transaktion. false = Antwort ist geschrieben.
func (h *Handler) saveWithParentChange(c *gin.Context, component *models.Component, tenantID, parentID uint, isGroup bool, status string) bool {
	var saveErr error
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockedPageComponents(tx, tenantID); err != nil {
			return err
		}
		if !h.applyParentChangeDB(c, tx, component, tenantID, parentID) {
			return errParentChangeRejected
		}
		saveErr = h.saveComponentUpdate(tx, component, isGroup, status)
		return saveErr
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, errParentChangeRejected):
	case saveErr != nil:
		respondCoded(c, http.StatusBadRequest, saveErr)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update component", "code": "server_error"})
	}
	return false
}

func (h *Handler) UpdateComponentHandler(c *gin.Context) {
	compIDStr := c.Param("id")
	compID, err := strconv.ParseUint(compIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var component models.Component
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&component).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForComponent(component.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return
	}

	var req componentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	// Der Status einer Gruppe ist kein gespeicherter Wert, sondern
	// wird aus den Kindern berechnet (buildTree). Ein mitgeschickter Status
	// wird bei Gruppen deshalb ignoriert statt mit 400 abgelehnt: Ältere,
	// noch im Browser geladene Admin-Stände schicken beim Umbenennen den
	// aggregierten Kindstatus mit; ein 400 würde dort das Bearbeiten von
	// Gruppen blockieren, das Ignorieren hält sie korrekt. Eine Umwandlung
	// Gruppe <-> Komponente ist per Update nicht vorgesehen (das Issue
	// verlangt sie nicht).
	isGroup := component.Status == componentStatusGroup
	if !isGroup && req.Status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	if req.Slug != nil && *req.Slug != component.Slug {
		respondSlugImmutable(c)
		return
	}
	if code, msg := validateComponentTexts(&req.Name, req.Description); code != "" {
		respondComponentTextTooLong(c, code, msg)
		return
	}
	if req.Name != "" {
		component.Name = req.Name
	}
	if req.Visible != nil {
		component.Visible = *req.Visible
	}
	newTags, ok := h.resolveComponentUpdateTags(c, req, tenantID, h.tagsForComponent(component.ID))
	if !ok {
		return
	}
	if req.Description != nil {
		component.Description = *req.Description
	}
	if req.ParentID != nil && !sameParent(component.ParentID, *req.ParentID) {
		// Ein Wechsel der Eltern prüft und speichert unter der Sperre
		// der Seite in einer Transaktion (Archivieren läuft sonst dazwischen).
		if !h.saveWithParentChange(c, &component, tenantID, *req.ParentID, isGroup, req.Status) {
			return
		}
	} else {
		if req.ParentID != nil && !h.applyParentChange(c, &component, tenantID, *req.ParentID) {
			return
		}
		if err := h.saveComponentUpdate(h.DB, &component, isGroup, req.Status); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
	}
	component.IsGroup = isGroup
	if req.TagIDs != nil {
		if err := h.replaceComponentTags(h.DB, component.ID, newTags); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tags", "code": "server_error"})
			return
		}
	}

	h.RecordAudit(c, "component.updated", "config", "Component", strconv.FormatUint(uint64(component.ID), 10), component.Name, "")
	component.Tags = h.tagsForComponent(component.ID)
	c.JSON(http.StatusOK, component)
}

func (h *Handler) DeleteComponentHandler(c *gin.Context) {
	idStr := c.Param("id")
	compID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var comp models.Component
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&comp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForComponent(comp.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to delete this component", "code": "forbidden"})
		return
	}

	// Eine Gruppe mit Kindern nicht löschen. Die Kinder würden
	// sonst verwaist (parent_id zeigt ins Leere) und aus dem Baum fallen.
	// Maschinenlesbarer code, damit das Frontend die Meldung übersetzt.
	if comp.Status == componentStatusGroup {
		var children int64
		if err := h.DB.Model(&models.Component{}).Where("parent_id = ? AND status_page_id = ?", comp.ID, tenantID).Count(&children).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete component", "code": "server_error"})
			return
		}
		if children > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "Group is not empty: move or delete its components first", "code": "group_not_empty"})
			return
		}
	}

	// ein Incident kann mehrere Komponenten betreffen - beim Löschen
	// einer Komponente nur die Zuordnung entfernen, nicht mehr den ganzen
	// Incident (der könnte noch für andere Komponenten gültig sein).
	h.DB.Where("component_id = ?", comp.ID).Delete(&models.IncidentComponent{})
	// Kopplungen an externe Komponenten ebenso entfernen.
	h.DB.Where("component_id = ?", comp.ID).Delete(&models.ExternalComponentLink{})

	if err := h.DB.Delete(&comp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete component", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "component.deleted", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Component deleted successfully"})
}
