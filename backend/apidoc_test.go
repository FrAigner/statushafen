package main

// Drift test for the hand-written API documentation
// (docs/api/openapi.yaml). The spec is deliberately not generated (no
// annotations on the handlers, no runtime dependency). To keep it from
// going stale anyway, this test compares it in both directions with
// the routes actually registered in newRouter(): a new route
// without docs turns it red just like a documented path that does not
// (or no longer) exist. kin-openapi is imported only here in the test and
// therefore does not end up in the server binary.

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

// ginParamPattern finds gin path parameters (":id") and catch-all parameters
// ("*filepath"), which OpenAPI writes as "{id}" and "{filepath}" respectively.
var ginParamPattern = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)

// openAPIPath translates a gin route path into OpenAPI notation.
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

// registeredRoutes builds the real router and returns all routes as
// "METHOD /path/{param}". The metrics credentials are set so that
// the conditionally registered /metrics is checked as well. The static
// frontend routes (registerStaticFrontend) only exist with a built
// frontend under ./static and are not API; they are absent in the test run.
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

// documentedRoutes returns all operations of the spec as "METHOD /path".
func documentedRoutes(doc *openapi3.T) map[string]bool {
	routes := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			routes[strings.ToUpper(method)+" "+path] = true
		}
	}
	return routes
}

// missingFrom returns the keys from want that are missing in have, sorted.
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

// TestOpenAPISpec_IsValidOpenAPI31 ensures that the spec is a
// valid OpenAPI 3.1 document (schema references, path parameters,
// responses, security schemes, examples) so that Redoc/Swagger UI and
// client generators can process it without errors.
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

// TestOpenAPISpec_MatchesRegisteredRoutes is the actual drift test:
// every registered route must be documented and vice versa.
func TestOpenAPISpec_MatchesRegisteredRoutes(t *testing.T) {
	registered := registeredRoutes(t)
	documented := documentedRoutes(loadOpenAPISpec(t))

	for _, r := range missingFrom(registered, documented) {
		t.Errorf("Route %s ist registriert, fehlt aber in %s", r, openAPISpecPath)
	}
	for _, r := range missingFrom(documented, registered) {
		t.Errorf("%s dokumentiert %s, die Route ist aber nicht registriert", openAPISpecPath, r)
	}
	// Sanity check: a half-built router would otherwise report green
	// against an equally incomplete spec.
	if !registered["GET /healthz"] || !registered["GET /metrics"] {
		t.Errorf("Router unvollständig aufgebaut (healthz/metrics fehlen), %d Routen", len(registered))
	}
}

// TestOpenAPISpec_OperationsAreDescribed requires at least
// a summary and a unique operationId, so that the docs
// are more than a bare path list even for admin endpoints and
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

// TestOpenAPISpec_DocumentsAuthTokenHeaderAndErrorCode: the backend
// reads the login token from X-Auth-Token first; the spec only
// mentioned "Authorization: Bearer". Also, error responses have long carried a
// machine-readable "code", but the error schema only knew "error".
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

// Review N1: the public availability response carries `history_truncated`
// when the status history was shortened. The Availability schema must document it (otherwise
// API users cannot see that their availability may be too good). That the response
// really delivers the field is checked by TestAvailability_DauerflatternWirdGekuerztUndGemeldet in the handlers package.
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
