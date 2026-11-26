package main

import (
	"strings"
	"testing"

	"github.com/fraigner/statushafen/permissions"
)

// expectedRequiredPermission is the value of x-required-permission in the
// OpenAPI spec for a route: "global-admin", "page-role", "<res>:<action>",
// "a|b" (at least one, also several actions of one resource), "a&b" (all) or, for maintenance/incident,
// "incidents:<action>|maintenance:<action>" (the kind of the record decides).
func expectedRequiredPermission(info routeInfo) string {
	perms := make([]string, len(info.Resources))
	for i, r := range info.Resources {
		perms[i] = permissions.Perm(r, info.Action)
	}
	switch info.Mode {
	case modeGlobal:
		return "global-admin"
	case modePageRole:
		return "page-role"
	case modeAll:
		return strings.Join(perms, "&")
	case modeAny, modeKind:
		return strings.Join(perms, "|")
	case modeAnyAction:
		var alt []string
		for _, a := range info.Actions {
			alt = append(alt, permissions.Perm(info.Resources[0], a))
		}
		return strings.Join(alt, "|")
	}
	return perms[0]
}

// For each route the spec names the same permission as the router.
func TestOpenAPISpec_RequiredPermissionMatchesRouteGuards(t *testing.T) {
	doc := loadOpenAPISpec(t)
	newPermTestRouter(t) // fills the registry
	checked := 0
	for key, info := range guardedRoutes() {
		method, path, _ := strings.Cut(key, " ")
		item := doc.Paths.Find(openAPIPath(path))
		if item == nil {
			t.Errorf("%s fehlt in der Spec", key)
			continue
		}
		op := item.GetOperation(method)
		if op == nil {
			t.Errorf("%s fehlt in der Spec", key)
			continue
		}
		got, _ := op.Extensions["x-required-permission"].(string)
		if want := expectedRequiredPermission(info); got != want {
			t.Errorf("%s: x-required-permission = %q, Router verlangt %q", key, got, want)
		}
		checked++
	}
	if checked < 90 {
		t.Errorf("nur %d Routen geprüft, die Registry wirkt unvollständig", checked)
	}
}
