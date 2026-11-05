// Package permissions beschreibt das Rechtemodell: je Tab der
// Admin-Navigation eine Ressource mit den vier Aktionen create, read, update
// und delete. Ein Recht ist der String "<ressource>:<aktion>"; Rollen speichern
// sie kommagetrennt (models.Role.Permissions).
//
// Regeln:
// - create, update und delete schließen read ein.
//   - Der frühere Sammelwert "<ressource>:write" wird nicht mehr vergeben.
//     Die Prüfung (Has) akzeptiert ihn aber weiter als create+update+delete+read
//     der Ressource und der früher daran gekoppelten Tabs (writeCoupling),
//     damit Alt-Token, ein noch gecachtes Alt-Frontend und nicht migrierte
//     Rollen bis zur Bereinigung nichts verlieren (reine Kompatibilität beim
//     Lesen, geschrieben wird er nie).
//   - "admin_dashboard" ist ein Sonderrecht ohne Aktionen.
//
// Das Paket hat bewusst keine Abhängigkeiten, damit Datenbank, Middleware und
// Handler es gemeinsam nutzen können.
package permissions

import (
	"slices"
	"strings"
)

// Aktionen in der Reihenfolge der Matrix (C R U D).
const (
	Create = "create"
	Read   = "read"
	Update = "update"
	Delete = "delete"
	// legacyWrite ist der Altwert aus der Zeit vor der Umstellung.
	legacyWrite = "write"
)

// AdminDashboard ist das Sonderrecht fürs Dashboard-Menü der Statusseite.
const AdminDashboard = "admin_dashboard"

// Actions sind alle Aktionen in Matrix-Reihenfolge.
var Actions = []string{Create, Read, Update, Delete}

// Scope sagt, worauf sich eine Ressource bezieht.
type Scope string

const (
	// ScopePage: gilt je Statusseite über die Rolle des Nutzers auf dieser Seite.
	ScopePage Scope = "page"
	// ScopeGlobal: hat keinen page_id-Bezug (mandantenübergreifend) und bleibt
	// Global Admins vorbehalten. Eine Seitenrolle kann sie nicht wirksam
	// vergeben, sonst läse sie z. B. alle Benutzer aller Mandanten.
	ScopeGlobal Scope = "global"
)

// Bereiche der Navigation.
const (
	GroupOperations = "operations"
	GroupContent    = "content"
	GroupManagement = "management"
	GroupSystem     = "system"
)

// Resource ist ein Tab der Admin-Navigation.
type Resource struct {
	Key   string
	Group string
	Scope Scope
}

// Resources sind alle Tabs in Navigationsreihenfolge. Die Schlüssel stehen
// auch im Frontend (frontend/src/utils/permissionResources.js); ein Test
// hält beide Seiten gleich.
var Resources = []Resource{
	{"overview", GroupOperations, ScopeGlobal},
	{"components", GroupOperations, ScopePage},
	{"external_sources", GroupOperations, ScopePage},
	{"incidents", GroupOperations, ScopePage},
	{"maintenance", GroupOperations, ScopePage},
	{"notices", GroupOperations, ScopePage},
	{"incident_templates", GroupOperations, ScopePage},
	{"subscribers", GroupOperations, ScopePage},
	{"notification_templates", GroupContent, ScopePage},
	{"languages", GroupContent, ScopePage},
	{"ui_translations", GroupContent, ScopePage},
	{"pages", GroupManagement, ScopePage},
	{"users", GroupManagement, ScopeGlobal},
	{"roles", GroupManagement, ScopeGlobal},
	{"tags", GroupManagement, ScopeGlobal},
	{"audit", GroupManagement, ScopeGlobal},
	{"monitoring", GroupSystem, ScopeGlobal},
	{"sso", GroupSystem, ScopePage},
	{"system_settings", GroupSystem, ScopeGlobal},
}

// Perm baut den Rechte-String "<ressource>:<aktion>".
func Perm(resource, action string) string { return resource + ":" + action }

// IsResource: ist key ein bekannter Tab?
func IsResource(key string) bool {
	for _, r := range Resources {
		if r.Key == key {
			return true
		}
	}
	return false
}

// split zerlegt einen Rechte-String in Ressource und Aktion.
func split(perm string) (resource, action string, ok bool) {
	return strings.Cut(perm, ":")
}

// entries zerlegt eine kommagetrennte Liste (ohne leere Einträge).
func entries(list string) []string {
	out := []string{}
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Has prüft, ob die Rechte-Liste das verlangte Recht ("res:act") gewährt:
// das Recht selbst, bei read zusätzlich create/update/delete derselben
// Ressource, und der Altwert "res:write" für jede Aktion. Der Altwert gilt
// außerdem für die Tabs, die vor der Umstellung an "res:write" hingen
// (writeCoupling), genau wie ihn MigrateLegacy übersetzt. Die
// Leseerbschaft (readCoupling) gilt hier bewusst nicht: "components:read" ist
// auch ein gültiges Neu-Recht, dessen Kopplung ein Admin entzogen haben kann.
func Has(list, required string) bool {
	res, act, ok := split(required)
	if !ok {
		return false
	}
	for _, p := range entries(list) {
		if p == required || p == Perm(res, legacyWrite) || coupledWrite(p, res) {
			return true
		}
		if act == Read && (p == Perm(res, Create) || p == Perm(res, Update) || p == Perm(res, Delete)) {
			return true
		}
	}
	return false
}

// Covers: gewährt die Liste have jedes Recht der Liste want?
// Der Altwert "res:write" in want zählt als create+read+update+delete. Eine
// Rolle darf nur vergeben werden, wenn der Vergebende sie selbst abdeckt.
func Covers(have, want string) bool {
	haveEntries := entries(have)
	for _, p := range entries(Normalize(want)) {
		if !strings.Contains(p, ":") {
			// Sonderrecht ohne Aktion (admin_dashboard): nur exakter Treffer.
			if !slices.Contains(haveEntries, p) {
				return false
			}
			continue
		}
		if !Has(have, p) {
			return false
		}
	}
	return true
}

// coupledWrite: ist p ein Alt-"key:write", an dem der Tab res früher hing?
func coupledWrite(p, res string) bool {
	key, act, ok := split(p)
	return ok && act == legacyWrite && slices.Contains(writeCoupling[key], res)
}

// expandWrite löst "res:write" in create+read+update+delete der Ressource und
// aller früher daran gekoppelten Tabs auf.
func expandWrite(res string) []string {
	out := CRUD(res)
	for _, c := range writeCoupling[res] {
		out = append(out, CRUD(c)...)
	}
	return out
}

// HasAny: mindestens eines der Rechte.
func HasAny(list string, required []string) bool {
	for _, r := range required {
		if Has(list, r) {
			return true
		}
	}
	return false
}

// CanChange: gewährt die Liste irgendeine verändernde Aktion (create, update,
// delete oder den Altwert write) auf irgendeiner Ressource? Auto-Join-Standard-
// rollen dürfen das nicht.
func CanChange(list string) bool {
	for _, p := range entries(list) {
		_, act, ok := split(p)
		if ok && (act == Create || act == Update || act == Delete || act == legacyWrite) {
			return true
		}
	}
	return false
}

// dedupe entfernt doppelte Einträge und behält die erste Position.
func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// CRUD gibt die vier Rechte-Strings einer Ressource zurück.
func CRUD(resource string) []string {
	return []string{Perm(resource, Create), Perm(resource, Read), Perm(resource, Update), Perm(resource, Delete)}
}

// withRead ergänzt zu jeder Ressource mit create/update/delete das Recht read
// (ans Ende), damit die Rolle im Editor und in der Matrix vollständig aussieht.
func withRead(list []string) []string {
	have := map[string]bool{}
	for _, p := range list {
		have[p] = true
	}
	out := append([]string{}, list...)
	for _, p := range list {
		res, act, ok := split(p)
		if ok && (act == Create || act == Update || act == Delete) && !have[Perm(res, Read)] {
			have[Perm(res, Read)] = true
			out = append(out, Perm(res, Read))
		}
	}
	return out
}

// Normalize ersetzt "res:write" durch create+read+update+delete (samt der
// früher gekoppelten Tabs) und stellt read sicher. Reihenfolge und sonstige Einträge bleiben erhalten. Beim
// Speichern einer Rolle angewandt, damit ein Alt-Frontend den Altwert nicht
// wieder einschleppt.
func Normalize(list string) string {
	out := []string{}
	for _, p := range entries(list) {
		if res, act, ok := split(p); ok && act == legacyWrite {
			out = append(out, expandWrite(res)...)
			continue
		}
		out = append(out, p)
	}
	return strings.Join(withRead(dedupe(out)), ",")
}

// Vor der Umstellung hingen einige Tabs an den Rechten einer anderen Ressource.
// Die Migration gibt ihnen dieselben Aktionen, die die Schlüsselressource hatte:
//
//	external_sources    <- components   (Lesen und Schreiben)
//	maintenance         <- incidents    (Lesen und Schreiben, teilt die Vorfall-API)
//	incident_templates  <- incidents    (nur Schreiben, verlangte auch zum Lesen write)
//	notification_templates, languages, ui_translations, sso <- pages (nur Schreiben)
var (
	// writeCoupling: Tabs, die mit "key:write" alle Aktionen bekamen.
	writeCoupling = map[string][]string{
		"components": {"external_sources"},
		"incidents":  {"maintenance", "incident_templates"},
		"pages":      {"notification_templates", "languages", "ui_translations", "sso"},
	}
	// readCoupling: Tabs, die schon das Leserecht der Schlüsselressource erbten.
	// Vorfallvorlagen und alles an pages fehlen bewusst: Sie verlangten schon
	// fürs Lesen das Schreibrecht.
	readCoupling = map[string][]string{
		"components": {"external_sources"},
		"incidents":  {"maintenance"},
	}
)

// MigrateLegacy übersetzt eine Rechte-Liste aus der Zeit vor der Umstellung so,
// dass jeder Nutzer dieselben Zugriffe behält und keine neuen gewinnt:
//   - "res:write" wird zu create+read+update+delete (res) und für alle
//     gekoppelten Tabs ebenfalls;
//   - "res:read" gilt zusätzlich für die gekoppelten Tabs, die das Leserecht
//     erbten (external_sources, maintenance);
//   - alle übrigen Einträge (auch unbekannte) bleiben unverändert.
//
// Das Ergebnis enthält kein "write" mehr; ein zweiter Aufruf ändert nichts.
func MigrateLegacy(list string) string {
	out := []string{}
	for _, p := range entries(list) {
		res, act, ok := split(p)
		switch {
		case ok && act == legacyWrite:
			out = append(out, expandWrite(res)...)
		case ok && act == Read:
			out = append(out, p)
			for _, c := range readCoupling[res] {
				out = append(out, Perm(c, Read))
			}
		default:
			out = append(out, p)
		}
	}
	return strings.Join(withRead(dedupe(out)), ",")
}

// HasLegacyWrite: enthält die Liste noch einen "res:write"-Eintrag?
func HasLegacyWrite(list string) bool {
	for _, p := range entries(list) {
		if _, act, ok := split(p); ok && act == legacyWrite {
			return true
		}
	}
	return false
}

// FullAccess liefert alle Rechte aller Ressourcen (Rolle "Global Admin").
func FullAccess() string {
	out := []string{}
	for _, r := range Resources {
		out = append(out, CRUD(r.Key)...)
	}
	return strings.Join(out, ",")
}

// Union vereinigt zwei Listen (erst a, dann die neuen Einträge aus b).
func Union(a, b string) string {
	return strings.Join(dedupe(append(entries(a), entries(b)...)), ",")
}

// List fasst Rechte-Strings zu einer Rollenliste zusammen und ergänzt read.
func List(perms ...string) string {
	return strings.Join(withRead(dedupe(perms)), ",")
}
