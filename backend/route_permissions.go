package main

import (
	"strings"
	"sync"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
)

// Every protected admin route is tied via exactly one entry to
// a resource (tab) and an action. Routes are registered only via
// guarded(); the mapping ends up in routeRegistry, and
// main_route_permissions_test.go enforces that no route is registered
// bypassing it and that the action matches the HTTP method.

type guardMode string

const (
	// modePage: permission on the page from :page_id (RequirePermission).
	modePage guardMode = "page"
	// modeGlobal: Global Admin (RequireGlobalAdmin) - resource without
	// page_id reference or platform operation (e.g. creating a page).
	modeGlobal guardMode = "global"
	// modePageRole: any role on the page suffices (RequirePageRole).
	modePageRole guardMode = "page_role"
	// modeAny: at least one of the permissions (RequireAnyPermission).
	modeAny guardMode = "any"
	// modeAnyAction: one of several actions on one resource (RequireAnyPermission).
	modeAnyAction guardMode = "any_action"
	// modeAll: all permissions (RequireAllPermissions).
	modeAll guardMode = "all"
	// modeKind: the resource follows the kind of the record (incident/maintenance).
	modeKind guardMode = "kind"
)

// routeGuard describes the permission check of a route.
type routeGuard struct {
	Mode      guardMode
	Resources []string
	Action    string
	// Actions: all accepted actions (modeAnyAction only).
	Actions []string
	mw      gin.HandlerFunc
	pre     []gin.HandlerFunc
}

// routeInfo is the registry entry of a route.
type routeInfo struct {
	Mode      guardMode
	Resources []string
	Action    string
	Actions   []string
}

var (
	routeRegistryMu sync.Mutex
	routeRegistry   = map[string]routeInfo{}
)

// guardedRoutes returns a copy of the registry (key "METHOD /path").
func guardedRoutes() map[string]routeInfo {
	routeRegistryMu.Lock()
	defer routeRegistryMu.Unlock()
	out := make(map[string]routeInfo, len(routeRegistry))
	for k, v := range routeRegistry {
		out[k] = v
	}
	return out
}

// need requires res:act. Global resources (permissions.ScopeGlobal) check
// for Global Admin, page resources for the role on the page from :page_id.
func need(resource, action string) routeGuard {
	for _, r := range permissions.Resources {
		if r.Key == resource && r.Scope == permissions.ScopeGlobal {
			return platform(resource, action)
		}
	}
	return routeGuard{Mode: modePage, Resources: []string{resource}, Action: action,
		mw: middleware.RequirePermission(permissions.Perm(resource, action))}
}

// platform: platform operation without page_id (Global Admin), also for a
// page resource, e.g. creating a status page or the language catalog.
func platform(resource, action string) routeGuard {
	return routeGuard{Mode: modeGlobal, Resources: []string{resource}, Action: action,
		mw: middleware.RequireGlobalAdmin()}
}

// pageRole: any role on the page suffices (reading configuration that every
// admin area of the page needs). resource only names the associated resource
// for documentation.
func pageRole(resource string) routeGuard {
	return routeGuard{Mode: modePageRole, Resources: []string{resource}, Action: permissions.Read,
		mw: middleware.RequirePageRole()}
}

// anyOf requires action on at least one of the resources.
func anyOf(action string, resources ...string) routeGuard {
	perms := make([]string, len(resources))
	for i, r := range resources {
		perms[i] = permissions.Perm(r, action)
	}
	return routeGuard{Mode: modeAny, Resources: resources, Action: action,
		mw: middleware.RequireAnyPermission(perms...)}
}

// anyAction requires one of actions on resource. The first action is the one
// the HTTP method check compares against.
func anyAction(resource string, actions ...string) routeGuard {
	perms := make([]string, len(actions))
	for i, a := range actions {
		perms[i] = permissions.Perm(resource, a)
	}
	return routeGuard{Mode: modeAnyAction, Resources: []string{resource}, Action: actions[0], Actions: actions,
		mw: middleware.RequireAnyPermission(perms...)}
}

// allOf requires action on all resources.
func allOf(action string, resources ...string) routeGuard {
	perms := make([]string, len(resources))
	for i, r := range resources {
		perms[i] = permissions.Perm(r, action)
	}
	return routeGuard{Mode: modeAll, Resources: resources, Action: action,
		mw: middleware.RequireAllPermissions(perms...)}
}

// byKind: incidents or maintenance, depending on the kind of the record.
func byKind(action string, resolve func(c *gin.Context) string) routeGuard {
	return routeGuard{Mode: modeKind, Resources: []string{"incidents", "maintenance"}, Action: action,
		mw: middleware.RequireResolvedPermission(action, resolve)}
}

// after attaches middlewares that run BEFORE the permission check (e.g. the
// *PageScope middlewares that set :page_id).
func (g routeGuard) after(pre ...gin.HandlerFunc) routeGuard {
	g.pre = pre
	return g
}

// guarded registers a route with a permission check and enters it in the
// registry. handlers run after the check (rate limit, handler).
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
	routeRegistry[method+" "+full] = routeInfo{Mode: guard.Mode, Resources: guard.Resources, Action: guard.Action, Actions: guard.Actions}
	routeRegistryMu.Unlock()
}
