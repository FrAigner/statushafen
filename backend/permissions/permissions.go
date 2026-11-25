// Package permissions describes the permission model: for each tab of the
// admin navigation a resource with the four actions create, read, update
// and delete. A permission is the string "<resource>:<action>"; roles store
// them comma-separated (models.Role.Permissions).
//
// Rules:
// - create, update and delete imply read.
//   - The former catch-all value "<resource>:write" is no longer granted.
//     The check (Has) still accepts it as create+update+delete+read
//     of the resource and of the tabs formerly coupled to it (writeCoupling),
//     so that legacy tokens, a still-cached legacy frontend and unmigrated
//     roles lose nothing until cleanup (pure compatibility when
//     reading; it is never written).
//   - "admin_dashboard" is a special permission without actions.
//
// The package deliberately has no dependencies so that the database, middleware and
// handlers can share it.
package permissions

import (
	"slices"
	"strings"
)

// Actions in the order of the matrix (C R U D).
const (
	Create = "create"
	Read   = "read"
	Update = "update"
	Delete = "delete"
	// legacyWrite is the legacy value from before the switch.
	legacyWrite = "write"
)

// AdminDashboard is the special permission for the status page's dashboard menu.
const AdminDashboard = "admin_dashboard"

// Actions are all actions in matrix order.
var Actions = []string{Create, Read, Update, Delete}

// Scope says what a resource relates to.
type Scope string

const (
	// ScopePage: applies per status page via the user's role on that page.
	ScopePage Scope = "page"
	// ScopeGlobal: has no page_id reference (cross-tenant) and remains
	// reserved for Global Admins. A page role cannot effectively
	// grant it, otherwise it would read e.g. all users of all tenants.
	ScopeGlobal Scope = "global"
)

// Areas of the navigation.
const (
	GroupOperations = "operations"
	GroupContent    = "content"
	GroupManagement = "management"
	GroupSystem     = "system"
)

// Resource is a tab of the admin navigation.
type Resource struct {
	Key   string
	Group string
	Scope Scope
}

// Resources are all tabs in navigation order. The keys are also
// in the frontend (frontend/src/utils/permissionResources.js); a test
// keeps both sides equal.
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

// Perm builds the permission string "<resource>:<action>".
func Perm(resource, action string) string { return resource + ":" + action }

// IsResource: is key a known tab?
func IsResource(key string) bool {
	for _, r := range Resources {
		if r.Key == key {
			return true
		}
	}
	return false
}

// split splits a permission string into resource and action.
func split(perm string) (resource, action string, ok bool) {
	return strings.Cut(perm, ":")
}

// entries splits a comma-separated list (without empty entries).
func entries(list string) []string {
	out := []string{}
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Has checks whether the permission list grants the requested permission ("res:act"):
// the permission itself, for read additionally create/update/delete of the same
// resource, and the legacy value "res:write" for every action. The legacy value also applies
// to the tabs that were tied to "res:write" before the switch
// (writeCoupling), exactly as MigrateLegacy translates it. The
// read inheritance (readCoupling) deliberately does not apply here: "components:read" is
// also a valid new permission whose coupling an admin may have revoked.
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

// Covers: does the list have grant every permission of the list want?
// The legacy value "res:write" in want counts as create+read+update+delete. A
// role may only be granted if the grantor covers it themselves.
func Covers(have, want string) bool {
	haveEntries := entries(have)
	for _, p := range entries(Normalize(want)) {
		if !strings.Contains(p, ":") {
			// Special permission without action (admin_dashboard): exact match only.
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

// coupledWrite: is p a legacy "key:write" to which the tab res used to be tied?
func coupledWrite(p, res string) bool {
	key, act, ok := split(p)
	return ok && act == legacyWrite && slices.Contains(writeCoupling[key], res)
}

// expandWrite resolves "res:write" into create+read+update+delete of the resource and
// of all tabs formerly coupled to it.
func expandWrite(res string) []string {
	out := CRUD(res)
	for _, c := range writeCoupling[res] {
		out = append(out, CRUD(c)...)
	}
	return out
}

// HasAny: at least one of the permissions.
func HasAny(list string, required []string) bool {
	for _, r := range required {
		if Has(list, r) {
			return true
		}
	}
	return false
}

// CanChange: does the list grant any modifying action (create, update,
// delete or the legacy value write) on any resource? Auto-join default
// roles must not.
func CanChange(list string) bool {
	for _, p := range entries(list) {
		_, act, ok := split(p)
		if ok && (act == Create || act == Update || act == Delete || act == legacyWrite) {
			return true
		}
	}
	return false
}

// GrantsAdminArea: does the list grant any right on a known resource (any
// action, including the legacy write)? Without one a role is "status page
// only": its members may view and subscribe to the (private) page, but get no
// admin area and no admin API. admin_dashboard alone does not count, it only
// shows the Dashboard link.
func GrantsAdminArea(list string) bool {
	for _, p := range entries(list) {
		res, act, ok := split(p)
		if ok && IsResource(res) && (slices.Contains(Actions, act) || act == legacyWrite) {
			return true
		}
	}
	return false
}

// dedupe removes duplicate entries and keeps the first position.
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

// CRUD returns the four permission strings of a resource.
func CRUD(resource string) []string {
	return []string{Perm(resource, Create), Perm(resource, Read), Perm(resource, Update), Perm(resource, Delete)}
}

// withRead adds the read permission to every resource with create/update/delete
// (at the end), so the role looks complete in the editor and in the matrix.
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

// Normalize replaces "res:write" with create+read+update+delete (including the
// formerly coupled tabs) and ensures read. Order and other entries are preserved. Applied when
// saving a role, so a legacy frontend does not
// reintroduce the legacy value.
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

// Before the switch, some tabs were tied to the permissions of another resource.
// The migration gives them the same actions the key resource had:
//
// external_sources    <- components   (read and write)
// maintenance         <- incidents    (read and write, shares the incident API)
// incident_templates  <- incidents    (write only, also required write to read)
// notification_templates, languages, ui_translations, sso <- pages (write only)
var (
	// writeCoupling: tabs that received all actions with "key:write".
	writeCoupling = map[string][]string{
		"components": {"external_sources"},
		"incidents":  {"maintenance", "incident_templates"},
		"pages":      {"notification_templates", "languages", "ui_translations", "sso"},
	}
	// readCoupling: tabs that already inherited the read permission of the key resource.
	// Incident templates and everything on pages are deliberately missing: they already required
	// the write permission for reading.
	readCoupling = map[string][]string{
		"components": {"external_sources"},
		"incidents":  {"maintenance"},
	}
)

// MigrateLegacy translates a permission list from before the switch so
// that every user keeps the same access and gains no new access:
//   - "res:write" becomes create+read+update+delete (res) and likewise for all
//     coupled tabs;
//   - "res:read" additionally applies to the coupled tabs that inherited
//     the read permission (external_sources, maintenance);
//   - all other entries (including unknown ones) remain unchanged.
//
// The result no longer contains "write"; a second call changes nothing.
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

// HasLegacyWrite: does the list still contain a "res:write" entry?
func HasLegacyWrite(list string) bool {
	for _, p := range entries(list) {
		if _, act, ok := split(p); ok && act == legacyWrite {
			return true
		}
	}
	return false
}

// FullAccess returns all permissions of all resources (role "Global Admin").
func FullAccess() string {
	out := []string{}
	for _, r := range Resources {
		out = append(out, CRUD(r.Key)...)
	}
	return strings.Join(out, ",")
}

// Union merges two lists (first a, then the new entries from b).
func Union(a, b string) string {
	return strings.Join(dedupe(append(entries(a), entries(b)...)), ",")
}

// List combines permission strings into a role list and adds read.
func List(perms ...string) string {
	return strings.Join(withRead(dedupe(perms)), ",")
}
