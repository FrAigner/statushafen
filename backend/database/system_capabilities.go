package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Capability ist eine Funktion, die Statushafen anbietet, und zugleich eine
// Komponente der Systemstatusseite (Dogfooding). Doc ist der Name der
// Hilfeseite unter frontend/src/docs/de (ohne .md) und der stabile Schlüssel:
// daraus wird der Komponenten-Slug "feature-<Doc>".
type Capability struct {
	Doc  string
	Name string
	// LegacyName: bereits vorhandene Komponente gleichen Namens (oberste Ebene)
	// wird einmalig in die Gruppe übernommen statt eine zweite anzulegen.
	LegacyName string
}

// CapabilityGroup bündelt Funktionen zu einer Gruppe der Systemseite.
type CapabilityGroup struct {
	Key   string
	Name  string
	Items []Capability
}

// SystemCapabilities ist das Verzeichnis aller angebotenen Funktionen. NEUE
// FUNKTION = NEUER EINTRAG HIER (zusammen mit ihrer Hilfeseite); ein Test
// (system_capabilities_test.go) schlägt fehl, wenn eine Hilfeseite fehlt.
// Der Start trägt neue Einträge auf der Systemseite nach (SyncSystemCapabilities).
var SystemCapabilities = []CapabilityGroup{
	{Key: "plattform", Name: "Plattform", Items: []Capability{
		{Doc: "plattform-api", Name: "API", LegacyName: "API"},
		{Doc: "plattform-datenbank", Name: "Datenbank", LegacyName: "Datenbank"},
		{Doc: "plattform-frontend", Name: "Frontend", LegacyName: "Frontend"},
		{Doc: "plattform-email", Name: "E-Mail-Versand", LegacyName: "E-Mail-Versand"},
		{Doc: "plattform-backup", Name: "Datenbank-Backup"}, // Alter des nächtlichen Backups
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

// DocsWithoutCapability sind Hilfeseiten, die keine eigene Funktion beschreiben.
// Plattform-Einträge haben keine eigene Hilfeseite (Doc beginnt mit "plattform-").
var DocsWithoutCapability = map[string]bool{"faq": true, "was-ist-statushafen": true, "erste-schritte": true}

// CapabilitySlug ist der Komponenten-Slug einer Funktion auf der Systemseite
// ("feature-<Doc>"); die Selbstüberwachung adressiert darüber ihre Komponenten.
func CapabilitySlug(doc string) string { return capabilityItemSlugPrefix + doc }

const (
	capabilityGroupSlugPrefix = "gruppe-"
	capabilityItemSlugPrefix  = "feature-"
)

// SyncSystemCapabilities trägt das Verzeichnis auf der Systemseite nach: je
// Gruppe eine Gruppen-Komponente, je Funktion eine Komponente darunter. Der Slug
// ist der stabile Schlüssel. Idempotent und Admin-freundlich: Die Suche läuft
// Unscoped, eine umbenannte oder gelöschte Komponente kommt also nicht zurück;
// nur wirklich neue Einträge (neue Funktionen) werden ergänzt. Vorhandene
// Plattform-Komponenten (LegacyName) wandern einmalig in ihre Gruppe.
func SyncSystemCapabilities(db *gorm.DB, pageID uint) {
	for gi, group := range SystemCapabilities {
		groupComp, ok := ensureComponent(db, pageID, capabilityGroupSlugPrefix+group.Key, models.Component{
			Name: group.Name, Status: "group", SortOrder: 100 + gi,
		})
		if !ok {
			continue // Gruppe vom Admin gelöscht: ihre Funktionen nicht daneben neu anlegen
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

// ensureComponent liefert die Komponente mit dem Slug (auch eine gelöschte, dann
// ok=false) oder legt sie an (ok=true).
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

// adoptLegacyComponent übernimmt eine vorhandene Komponente der obersten Ebene
// (nur beim ersten Mal: danach trägt sie den Slug und wird nicht mehr angefasst).
// Liefert true, wenn der Eintrag damit erledigt ist (übernommen oder schon da).
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
