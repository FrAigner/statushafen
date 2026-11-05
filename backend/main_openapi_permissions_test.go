package main

import (
	"strings"
	"testing"

	"github.com/fraigner/statushafen/permissions"
)

// expectedRequiredPermission ist der Wert von x-required-permission in der
// OpenAPI-Spec für eine Route: "global-admin", "page-role", "<res>:<aktion>",
// "a|b" (mindestens eines), "a&b" (alle) oder bei Wartung/Vorfall
// "incidents:<aktion>|maintenance:<aktion>" (die Art des Datensatzes entscheidet).
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
	}
	return perms[0]
}

// Die Spec nennt je Route dieselbe Berechtigung wie der Router.
func TestOpenAPISpec_RequiredPermissionMatchesRouteGuards(t *testing.T) {
	doc := loadOpenAPISpec(t)
	newPermTestRouter(t) // füllt die Registry
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
