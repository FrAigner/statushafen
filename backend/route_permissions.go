package main

import (
	"strings"
	"sync"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
)

// Jede geschützte Admin-Route hängt über genau einen Eintrag an
// einer Ressource (Tab) und einer Aktion. Die Routen werden nur über
// guarded() registriert; dabei landet die Zuordnung in routeRegistry, und
// main_route_permissions_test.go erzwingt, dass keine Route daran vorbei
// registriert wird und die Aktion zur HTTP-Methode passt.

type guardMode string

const (
	// modePage: Recht auf der Seite aus :page_id (RequirePermission).
	modePage guardMode = "page"
	// modeGlobal: Global Admin (RequireGlobalAdmin) - Ressource ohne
	// page_id-Bezug oder Plattform-Operation (z. B. Seite anlegen).
	modeGlobal guardMode = "global"
	// modePageRole: irgendeine Rolle auf der Seite genügt (RequirePageRole).
	modePageRole guardMode = "page_role"
	// modeAny: mindestens eines der Rechte (RequireAnyPermission).
	modeAny guardMode = "any"
	// modeAll: alle Rechte (RequireAllPermissions).
	modeAll guardMode = "all"
	// modeKind: Ressource folgt der Art des Datensatzes (Vorfall/Wartung).
	modeKind guardMode = "kind"
)

// routeGuard beschreibt die Rechteprüfung einer Route.
type routeGuard struct {
	Mode      guardMode
	Resources []string
	Action    string
	mw        gin.HandlerFunc
	pre       []gin.HandlerFunc
}

// routeInfo ist der Registry-Eintrag einer Route.
type routeInfo struct {
	Mode      guardMode
	Resources []string
	Action    string
}

var (
	routeRegistryMu sync.Mutex
	routeRegistry   = map[string]routeInfo{}
)

// guardedRoutes liefert eine Kopie der Registry (Schlüssel "METHOD /pfad").
func guardedRoutes() map[string]routeInfo {
	routeRegistryMu.Lock()
	defer routeRegistryMu.Unlock()
	out := make(map[string]routeInfo, len(routeRegistry))
	for k, v := range routeRegistry {
		out[k] = v
	}
	return out
}

// need verlangt res:act. Globale Ressourcen (permissions.ScopeGlobal) prüfen
// auf Global Admin, Seiten-Ressourcen auf die Rolle der Seite aus :page_id.
func need(resource, action string) routeGuard {
	for _, r := range permissions.Resources {
		if r.Key == resource && r.Scope == permissions.ScopeGlobal {
			return platform(resource, action)
		}
	}
	return routeGuard{Mode: modePage, Resources: []string{resource}, Action: action,
		mw: middleware.RequirePermission(permissions.Perm(resource, action))}
}

// platform: Plattform-Operation ohne page_id (Global Admin), auch für eine
// Seiten-Ressource, z. B. das Anlegen einer Statusseite oder der Sprachkatalog.
func platform(resource, action string) routeGuard {
	return routeGuard{Mode: modeGlobal, Resources: []string{resource}, Action: action,
		mw: middleware.RequireGlobalAdmin()}
}

// pageRole: jede Rolle auf der Seite genügt (Lesen von Konfiguration, die jeder
// Admin-Bereich der Seite braucht). resource nennt nur die zugehörige Ressource
// für die Dokumentation.
func pageRole(resource string) routeGuard {
	return routeGuard{Mode: modePageRole, Resources: []string{resource}, Action: permissions.Read,
		mw: middleware.RequirePageRole()}
}

// anyOf verlangt action auf mindestens einer der Ressourcen.
func anyOf(action string, resources ...string) routeGuard {
	perms := make([]string, len(resources))
	for i, r := range resources {
		perms[i] = permissions.Perm(r, action)
	}
	return routeGuard{Mode: modeAny, Resources: resources, Action: action,
		mw: middleware.RequireAnyPermission(perms...)}
}

// allOf verlangt action auf allen Ressourcen.
func allOf(action string, resources ...string) routeGuard {
	perms := make([]string, len(resources))
	for i, r := range resources {
		perms[i] = permissions.Perm(r, action)
	}
	return routeGuard{Mode: modeAll, Resources: resources, Action: action,
		mw: middleware.RequireAllPermissions(perms...)}
}

// byKind: incidents oder maintenance, je nach Art des Datensatzes.
func byKind(action string, resolve func(c *gin.Context) string) routeGuard {
	return routeGuard{Mode: modeKind, Resources: []string{"incidents", "maintenance"}, Action: action,
		mw: middleware.RequireResolvedPermission(action, resolve)}
}

// after hängt Middlewares an, die VOR der Rechteprüfung laufen (z. B. die
// *PageScope-Middlewares, die :page_id setzen).
func (g routeGuard) after(pre ...gin.HandlerFunc) routeGuard {
	g.pre = pre
	return g
}

// guarded registriert eine Route mit Rechteprüfung und trägt sie in die
// Registry ein. handlers laufen nach der Prüfung (Rate-Limit, Handler).
func guarded(group *gin.RouterGroup, method, path string, guard routeGuard, handlers ...gin.HandlerFunc) {
	chain := make([]gin.HandlerFunc, 0, len(guard.pre)+1+len(handlers))
	chain = append(chain, guard.pre...)
	chain = append(chain, guard.mw)
	chain = append(chain, handlers...)
	group.Handle(method, path, chain...)

	full := group.BasePath()
	if path != "" {
		full = strings.TrimRight(full, "/") + path
	}
	routeRegistryMu.Lock()
	routeRegistry[method+" "+full] = routeInfo{Mode: guard.Mode, Resources: guard.Resources, Action: guard.Action}
	routeRegistryMu.Unlock()
}
