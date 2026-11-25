package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Capability is a feature that Statushafen offers, and at the same time a
// component of the system status page (dogfooding). Doc is the name of the
// help page under frontend/src/docs/de (without .md) and the stable key:
// the component slug "feature-<Doc>" is derived from it.
type Capability struct {
	Doc  string
	Name string
	// LegacyName: an already existing component of the same name (top level)
	// is adopted into the group once instead of creating a second one.
	LegacyName string
}

// CapabilityGroup bundles features into a group of the system page.
type CapabilityGroup struct {
	Key   string
	Name  string
	Items []Capability
}

// SystemCapabilities is the directory of all offered features. NEW
// FEATURE = NEW ENTRY HERE (together with its help page); a test
// (system_capabilities_test.go) fails if a help page is missing.
// On startup new entries are added to the system page (SyncSystemCapabilities).
var SystemCapabilities = []CapabilityGroup{
	{Key: "plattform", Name: "Plattform", Items: []Capability{
		{Doc: "plattform-api", Name: "API", LegacyName: "API"},
		{Doc: "plattform-datenbank", Name: "Datenbank", LegacyName: "Datenbank"},
		{Doc: "plattform-frontend", Name: "Frontend", LegacyName: "Frontend"},
		{Doc: "plattform-email", Name: "E-Mail-Versand", LegacyName: "E-Mail-Versand"},
		{Doc: "plattform-backup", Name: "Datenbank-Backup"}, // age of the nightly backup
	}},
	{Key: "statusseiten", Name: "Statusseiten", Items: []Capability{
		{Doc: "statusseiten", Name: "Statusseiten und Mandanten"},
		{Doc: "oeffentliche-statusseite", Name: "Öffentliche Statusseite"},
		{Doc: "komponenten-und-tags", Name: "Komponenten und Tags"},
		{Doc: "externe-quellen", Name: "Externe Quellen"},
		{Doc: "sprachen", Name: "Mehrsprachigkeit"},
	}},
	{Key: "meldungen", Name: "Meldungen", Items: []Capability{
		{Doc: "vorfaelle", Name: "Vorfälle"},
		{Doc: "wartungsfenster", Name: "Wartungsfenster"},
		{Doc: "ankuendigungen", Name: "Ankündigungen"},
		{Doc: "formatierte-texte", Name: "Formatierte Texte"},
		{Doc: "vorlagen", Name: "Vorlagen"},
	}},
	{Key: "benachrichtigungen", Name: "Benachrichtigungen", Items: []Capability{
		{Doc: "abonnements", Name: "Abonnements"},
		{Doc: "mail-templates", Name: "Mail-Vorlagen"},
	}},
	{Key: "zugang", Name: "Zugang und Verwaltung", Items: []Capability{
		{Doc: "benutzer-und-rollen", Name: "Benutzer, Rollen und Rechte"},
		{Doc: "sso", Name: "Single Sign-on"},
		{Doc: "audit-log", Name: "Audit-Log"},
		{Doc: "systemeinstellungen", Name: "Systemeinstellungen"},
		{Doc: "selbstueberwachung", Name: "Selbstüberwachung"},
	}},
	{Key: "integration", Name: "Integration", Items: []Capability{
		{Doc: "api-und-webhooks", Name: "API und Webhooks"},
	}},
}

// DocsWithoutCapability are help pages that do not describe a feature of their own.
// Platform entries have no help page of their own (Doc starts with "plattform-").
var DocsWithoutCapability = map[string]bool{"faq": true, "was-ist-statushafen": true, "erste-schritte": true}

// CapabilitySlug is the component slug of a feature on the system page
// ("feature-<Doc>"); self-monitoring uses it to address its components.
func CapabilitySlug(doc string) string { return capabilityItemSlugPrefix + doc }

const (
	capabilityGroupSlugPrefix = "gruppe-"
	capabilityItemSlugPrefix  = "feature-"
)

// SyncSystemCapabilities adds the directory to the system page: one
// group component per group, one component below it per feature. The slug
// is the stable key. Idempotent and admin-friendly: the lookup runs
// Unscoped, so a renamed or deleted component does not come back;
// only genuinely new entries (new features) are added. Existing
// platform components (LegacyName) move into their group once.
func SyncSystemCapabilities(db *gorm.DB, pageID uint) {
	for gi, group := range SystemCapabilities {
		groupComp, ok := ensureComponent(db, pageID, capabilityGroupSlugPrefix+group.Key, models.Component{
			Name: group.Name, Status: "group", SortOrder: 100 + gi,
		})
		if !ok {
			continue // group deleted by the admin: do not re-create its features next to it
		}
		for ii, item := range group.Items {
			slug := CapabilitySlug(item.Doc)
			if adoptLegacyComponent(db, pageID, slug, item, groupComp.ID, ii) {
				continue
			}
			ensureComponent(db, pageID, slug, models.Component{
				Name: item.Name, Status: "operational", ParentID: &groupComp.ID, SortOrder: ii,
			})
		}
	}
}

// ensureComponent returns the component with the slug (also a deleted one, then
// ok=false) or creates it (ok=true).
func ensureComponent(db *gorm.DB, pageID uint, slug string, c models.Component) (models.Component, bool) {
	var found []models.Component
	if err := db.Unscoped().Where("status_page_id = ? AND slug = ?", pageID, slug).Limit(1).Find(&found).Error; err == nil && len(found) > 0 {
		return found[0], !found[0].DeletedAt.Valid
	}
	c.StatusPageID = pageID
	c.Slug = slug
	if err := db.Create(&c).Error; err != nil {
		slog.Error("Systemseite: Funktion konnte nicht angelegt werden", "slug", slug, "error", err)
		return c, false
	}
	return c, true
}

// adoptLegacyComponent adopts an existing top-level component
// (only the first time: afterwards it carries the slug and is not touched again).
// Returns true if the entry is thereby done (adopted or already there).
func adoptLegacyComponent(db *gorm.DB, pageID uint, slug string, item Capability, groupID uint, order int) bool {
	var byslug int64
	db.Unscoped().Model(&models.Component{}).Where("status_page_id = ? AND slug = ?", pageID, slug).Count(&byslug)
	if byslug > 0 {
		return true
	}
	if item.LegacyName == "" {
		return false
	}
	res := db.Model(&models.Component{}).
		Where("status_page_id = ? AND name = ? AND parent_id IS NULL AND status <> ?", pageID, item.LegacyName, "group").
		Updates(map[string]any{"parent_id": groupID, "slug": slug, "sort_order": order})
	return res.Error == nil && res.RowsAffected > 0
}
