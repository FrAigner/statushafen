package main

// Drift-Test für die handgeschriebene API-Dokumentation
// (docs/api/openapi.yaml). Die Spec ist bewusst nicht generiert (keine
// Annotationen an den Handlern, keine Laufzeit-Abhängigkeit). Damit sie
// trotzdem nicht veraltet, gleicht dieser Test sie in beide Richtungen mit
// den tatsächlich in newRouter() registrierten Routen ab: eine neue Route
// ohne Doku macht ihn ebenso rot wie ein dokumentierter Pfad, den es nicht
// (mehr) gibt. kin-openapi wird nur hier im Test importiert und landet
// deshalb nicht im Server-Binary.

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/testutils"

	"github.com/getkin/kin-openapi/openapi3"
)

const openAPISpecPath = "../docs/api/openapi.yaml"

// ginParamPattern findet gin-Pfadparameter (":id") und Catch-all-Parameter
// ("*filepath"), die OpenAPI als "{id}" bzw. "{filepath}" schreibt.
var ginParamPattern = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)

// openAPIPath übersetzt einen gin-Routenpfad in die OpenAPI-Schreibweise.
func openAPIPath(ginPath string) string {
	return ginParamPattern.ReplaceAllString(ginPath, "{$1}")
}

func loadOpenAPISpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(openAPISpecPath)
	if err != nil {
		t.Fatalf("%s lässt sich nicht laden: %v", openAPISpecPath, err)
	}
	return doc
}

// registeredRoutes baut den echten Router und liefert alle Routen als
// "METHOD /pfad/{param}". Die Metrics-Zugangsdaten werden gesetzt, damit
// auch das nur bedingt registrierte /metrics geprüft wird. Die statischen
// Frontend-Routen (registerStaticFrontend) entstehen nur mit gebautem
// Frontend unter ./static und sind keine API; im Testlauf fehlen sie.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "metrics")
	t.Setenv(handlers.MetricsBasicAuthPasswordEnv, "metrics")
	h := handlers.NewHandler(testutils.SetupTestDB())

	routes := map[string]bool{}
	for _, route := range newRouter(h).Routes() {
		routes[route.Method+" "+openAPIPath(route.Path)] = true
	}
	return routes
}

// documentedRoutes liefert alle Operationen der Spec als "METHOD /pfad".
func documentedRoutes(doc *openapi3.T) map[string]bool {
	routes := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			routes[strings.ToUpper(method)+" "+path] = true
		}
	}
	return routes
}

// missingFrom liefert die Schlüssel aus want, die in have fehlen, sortiert.
func missingFrom(want, have map[string]bool) []string {
	var missing []string
	for k := range want {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

func TestOpenAPIPath(t *testing.T) {
	cases := map[string]string{
		"/healthz":                          "/healthz",
		"/api/pages/:page_id/incidents/:id": "/api/pages/{page_id}/incidents/{id}",
		"/assets/*filepath":                 "/assets/{filepath}",
	}
	for in, want := range cases {
		if got := openAPIPath(in); got != want {
			t.Errorf("openAPIPath(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestMissingFrom(t *testing.T) {
	got := missingFrom(map[string]bool{"b": true, "a": true, "c": true}, map[string]bool{"c": true})
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("missingFrom = %v, erwartet [a b]", got)
	}
}

// TestOpenAPISpec_IsValidOpenAPI31 stellt sicher, dass die Spec ein
// gültiges OpenAPI-3.1-Dokument ist (Schema-Referenzen, Pfadparameter,
// Antworten, Security-Schemes, Beispiele), damit Redoc/Swagger UI und
// Client-Generatoren sie ohne Fehler verarbeiten.
func TestOpenAPISpec_IsValidOpenAPI31(t *testing.T) {
	doc := loadOpenAPISpec(t)
	if doc.OpenAPIMajorMinor() != "3.1" {
		t.Fatalf("openapi = %q, erwartet 3.1.x", doc.OpenAPI)
	}
	err := doc.Validate(context.Background(),
		openapi3.EnableMultiError(),
		openapi3.EnableExamplesValidation(),
	)
	if err != nil {
		t.Fatalf("%s ist kein gültiges OpenAPI 3.1:\n%v", openAPISpecPath, err)
	}
}

// TestOpenAPISpec_MatchesRegisteredRoutes ist der eigentliche Drift-Test:
// jede registrierte Route muss dokumentiert sein und umgekehrt.
func TestOpenAPISpec_MatchesRegisteredRoutes(t *testing.T) {
	registered := registeredRoutes(t)
	documented := documentedRoutes(loadOpenAPISpec(t))

	for _, r := range missingFrom(registered, documented) {
		t.Errorf("Route %s ist registriert, fehlt aber in %s", r, openAPISpecPath)
	}
	for _, r := range missingFrom(documented, registered) {
		t.Errorf("%s dokumentiert %s, die Route ist aber nicht registriert", openAPISpecPath, r)
	}
	// Plausibilitätsprüfung: ein nur halb aufgebauter Router würde sonst
	// gegen eine ebenso unvollständige Spec grün melden.
	if !registered["GET /healthz"] || !registered["GET /metrics"] {
		t.Errorf("Router unvollständig aufgebaut (healthz/metrics fehlen), %d Routen", len(registered))
	}
}

// TestOpenAPISpec_OperationsAreDescribed verlangt pro Operation mindestens
// eine Zusammenfassung und eine eindeutige operationId, damit die Doku
// auch für Admin-Endpunkte mehr als eine nackte Pfadliste ist und
// generierte Clients stabile Methodennamen bekommen.
func TestOpenAPISpec_OperationsAreDescribed(t *testing.T) {
	doc := loadOpenAPISpec(t)
	seen := map[string]string{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			where := strings.ToUpper(method) + " " + path
			if strings.TrimSpace(op.Summary) == "" {
				t.Errorf("%s: summary fehlt", where)
			}
			if op.OperationID == "" {
				t.Errorf("%s: operationId fehlt", where)
				continue
			}
			if other, dup := seen[op.OperationID]; dup {
				t.Errorf("%s: operationId %q schon bei %s vergeben", where, op.OperationID, other)
			}
			seen[op.OperationID] = where
		}
	}
}

// TestOpenAPISpec_DocumentsAuthTokenHeaderAndErrorCode: Das Backend
// liest das Login-Token zuerst aus X-Auth-Token; die Spec nannte
// nur "Authorization: Bearer". Außerdem tragen Fehlerantworten längst einen
// maschinenlesbaren "code", das Error-Schema kannte nur "error".
func TestOpenAPISpec_DocumentsAuthTokenHeaderAndErrorCode(t *testing.T) {
	doc := loadOpenAPISpec(t)

	scheme := doc.Components.SecuritySchemes["authToken"]
	if scheme == nil || scheme.Value == nil {
		t.Fatal("securityScheme authToken fehlt")
	}
	if scheme.Value.Type != "apiKey" || scheme.Value.In != "header" || scheme.Value.Name != middleware.AuthTokenHeader {
		t.Errorf("authToken = %s/%s/%s, erwartet apiKey/header/%s",
			scheme.Value.Type, scheme.Value.In, scheme.Value.Name, middleware.AuthTokenHeader)
	}

	hasAuthToken := false
	for _, req := range doc.Security {
		if _, ok := req["authToken"]; ok {
			hasAuthToken = true
		}
	}
	if !hasAuthToken {
		t.Error("die globale security-Liste nennt authToken nicht")
	}

	errSchema := doc.Components.Schemas["Error"]
	if errSchema == nil || errSchema.Value == nil {
		t.Fatal("Schema Error fehlt")
	}
	if _, ok := errSchema.Value.Properties["code"]; !ok {
		t.Error("Schema Error hat keine Eigenschaft code")
	}
}

// Review N1: Die Antwort der öffentlichen Verfügbarkeit trägt `history_truncated`,
// wenn die Statushistorie gekürzt wurde. Das Schema Availability muss es dokumentieren (sonst
// sehen API-Nutzer nicht, dass ihre Verfügbarkeit zu gut sein kann). Dass die Antwort das Feld
// wirklich liefert, prüft TestAvailability_DauerflatternWirdGekuerztUndGemeldet im Paket handlers.
func TestOpenAPISpec_AvailabilityDokumentiertHistoryTruncated(t *testing.T) {
	doc := loadOpenAPISpec(t)
	schema := doc.Components.Schemas["Availability"]
	if schema == nil || schema.Value == nil {
		t.Fatal("Schema Availability fehlt in openapi.yaml")
	}
	if _, ok := schema.Value.Properties["history_truncated"]; !ok {
		t.Error("Schema Availability dokumentiert history_truncated nicht")
	}
}
