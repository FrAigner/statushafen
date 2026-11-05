package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"
)

// Die SSO-Doku unter docs/sso/ ist Teil des Produkts. Diese Tests
// halten sie prüfbar: alle Dateien sind da, relative Links zeigen auf
// existierende Dateien, die Architektur-Datei enthält Mermaid-Diagramme und
// die Endpunkte in api.md entsprechen den Routen des echten Routers.

const ssoDocsDir = "../docs/sso"

var ssoDocFiles = []string{
	"README.md", "oidc.md", "google.md", "keycloak.md", "saml.md",
	"okta.md", "azure-ad.md", "api.md", "architecture.md",
}

func readSSODoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ssoDocsDir, name))
	if err != nil {
		t.Fatalf("SSO-Doku %s fehlt: %v", name, err)
	}
	return string(b)
}

func TestSSODocs_AllFilesExist(t *testing.T) {
	for _, name := range ssoDocFiles {
		if strings.TrimSpace(readSSODoc(t, name)) == "" {
			t.Errorf("%s ist leer", name)
		}
	}
}

var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

var mdHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)

// headingSlug bildet die GitHub-Anker-Regel nach: klein, Leerzeichen zu "-",
// Satzzeichen außer "-" und "_" entfallen, Buchstaben (auch Umlaute) bleiben.
func headingSlug(h string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		switch {
		case r == ' ':
			sb.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func headingAnchors(md string) map[string]bool {
	anchors := map[string]bool{}
	for _, m := range mdHeading.FindAllStringSubmatch(md, -1) {
		anchors[headingSlug(m[1])] = true
	}
	return anchors
}

// relativeLinkProblems liefert alle kaputten relativen Links einer Datei:
// fehlende Zieldatei oder ein Anker, der unter den Überschriften der
// Zieldatei (bzw. der Datei selbst bei reinen #-Links) nicht vorkommt.
func relativeLinkProblems(dir, name string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, m := range mdLink.FindAllStringSubmatch(string(b), -1) {
		link := m[1]
		if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
			continue
		}
		file, anchor, hasAnchor := strings.Cut(link, "#")
		if file == "" {
			file = name
		}
		target, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: Link %q zeigt auf keine existierende Datei", name, link))
			continue
		}
		if hasAnchor && !headingAnchors(string(target))[anchor] {
			problems = append(problems, fmt.Sprintf("%s: Link %q: Anker %q fehlt in %s", name, link, anchor, file))
		}
	}
	return problems, nil
}

// checkRelativeLinks prüft alle relativen Markdown-Links einer Datei.
func checkRelativeLinks(t *testing.T, dir, name string) {
	t.Helper()
	problems, err := relativeLinkProblems(dir, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// Anker müssen geprüft werden, nicht nur Dateien.
func TestSSODocs_AnchorCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ziel.md", "# Titel\n\n## Konten und Verknüpfung\n\n## Größe: Was (nicht)?\n\n### Mit_Unterstrich-und-Strich ##\n")
	write("ok.md", "[a](ziel.md#konten-und-verknüpfung) [b](ziel.md#größe-was-nicht) [c](ziel.md#mit_unterstrich-und-strich) [d](ziel.md) [e](#lokal) [f](https://x.example/#y) [g](mailto:a@example.com)\n\n## Lokal\n")
	write("kaputt.md", "[a](ziel.md#gibt-es-nicht) [b](#nirgends) [c](fehlt.md) [d](fehlt.md#x)\n")

	if p, err := relativeLinkProblems(dir, "ok.md"); err != nil || len(p) != 0 {
		t.Errorf("ok.md: erwartet keine Fehler, bekam %v (err=%v)", p, err)
	}
	p, err := relativeLinkProblems(dir, "kaputt.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 4 {
		t.Errorf("kaputt.md: erwartet 4 Fehler, bekam %d: %v", len(p), p)
	}
	if _, err := relativeLinkProblems(dir, "gibtsnicht.md"); err == nil {
		t.Error("fehlende Quelldatei muss einen Fehler liefern")
	}
}

func TestSSODocs_RelativeLinksResolve(t *testing.T) {
	for _, name := range ssoDocFiles {
		checkRelativeLinks(t, ssoDocsDir, name)
	}
}

func TestSSODocs_IndexLinksAllGuides(t *testing.T) {
	idx := readSSODoc(t, "README.md")
	for _, name := range ssoDocFiles[1:] {
		if !strings.Contains(idx, "("+name+")") {
			t.Errorf("README.md verlinkt %s nicht", name)
		}
	}
	if !strings.Contains(idx, "SAML 2.0 (je Statusseite) | verfügbar") {
		t.Error("README.md nennt den SAML-Status (verfügbar) nicht")
	}
}

// SAML ist verfügbar. Die SAML-Guides dürfen deshalb
// nicht mehr als Vorab-Doku auftreten und müssen die SP-Endpunkte nennen,
// die der Admin beim IdP einträgt.
func TestSSODocs_SAMLGuidesDescribeAvailableSAML(t *testing.T) {
	for _, name := range []string{"saml.md", "okta.md", "azure-ad.md"} {
		doc := readSSODoc(t, name)
		lower := strings.ToLower(doc)
		if strings.Contains(lower, "noch nicht verfügbar") || strings.Contains(doc, "TBD") {
			t.Errorf("%s: SAML ist verfügbar, Vorab-Hinweise müssen raus", name)
		}
		for _, want := range []string{"/api/auth/saml/<SEITEN-ID>/acs", "/api/auth/saml/<SEITEN-ID>/metadata", "persistent"} {
			if !strings.Contains(lower, strings.ToLower(want)) {
				t.Errorf("%s: %q fehlt", name, want)
			}
		}
	}
}

// Das Login-Token geht per X-Auth-Token; die API-Doku muss das
// nennen, sonst schicken Integrationen weiter nur den Authorization-Header.
func TestSSODocs_APINamesAuthTokenHeader(t *testing.T) {
	if !strings.Contains(readSSODoc(t, "api.md"), "`X-Auth-Token: <token>`") {
		t.Error("api.md nennt den Header X-Auth-Token nicht")
	}
}

func TestSSODocs_ArchitectureHasMermaid(t *testing.T) {
	if n := strings.Count(readSSODoc(t, "architecture.md"), "```mermaid"); n < 2 {
		t.Errorf("architecture.md: %d Mermaid-Blöcke, erwartet mindestens 2 (Login, Verknüpfung)", n)
	}
}

func TestSSODocs_AdminGuideLinksToSSO(t *testing.T) {
	b, err := os.ReadFile("../docs/user-guide/admin.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "(../sso/README.md)") {
		t.Error("docs/user-guide/admin.md verlinkt docs/sso/README.md nicht")
	}
	checkRelativeLinks(t, "../docs/user-guide", "admin.md")
}

var apiHeading = regexp.MustCompile("(?m)^### `(GET|POST|PUT|DELETE) (/api/[^`]+)`")

// docRoutePath wandelt die Schreibweise der Doku (`:page_id`) unverändert
// durch; gin nutzt dieselbe. Der Vergleich ist deshalb 1:1.
func TestSSODocs_APIEndpointsMatchRouter(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	routes := map[string]bool{}
	for _, rt := range r.Routes() {
		routes[rt.Method+" "+rt.Path] = true
	}

	var documented []string
	for _, m := range apiHeading.FindAllStringSubmatch(readSSODoc(t, "api.md"), -1) {
		documented = append(documented, m[1]+" "+m[2])
	}
	sort.Strings(documented)

	want := []string{
		"DELETE /api/admin/pages/:page_id/sso",
		"DELETE /api/me/sso/:id",
		"GET /api/admin/pages/:page_id/sso",
		"GET /api/auth/oidc/callback",
		"GET /api/auth/oidc/login",
		"GET /api/auth/saml/:page_id/metadata",
		"GET /api/auth/saml/login",
		"GET /api/auth/sso/login",
		"GET /api/me/sso",
		"GET /api/public/pages/:slug/sso",
		"POST /api/admin/pages/:page_id/sso/saml-test",
		"POST /api/admin/pages/:page_id/sso/test",
		"POST /api/auth/oidc/link",
		"POST /api/auth/saml/:page_id/acs",
		"POST /api/auth/saml/link",
		"POST /api/auth/sso-exchange",
		"PUT /api/admin/pages/:page_id/sso",
	}
	if strings.Join(documented, "\n") != strings.Join(want, "\n") {
		t.Errorf("api.md dokumiert %v, erwartet %v", documented, want)
	}
	for _, d := range documented {
		if !routes[d] {
			t.Errorf("api.md dokumiert %q, der Router kennt die Route nicht", d)
		}
	}
	// Umgekehrt: Jede SSO-Route des Routers steht in api.md, damit neue
	// Endpunkte (wie die SAML-Routen) nicht undokumentiert bleiben.
	documentedSet := map[string]bool{}
	for _, d := range documented {
		documentedSet[d] = true
	}
	for rt := range routes {
		if isSSORoute(rt) && !documentedSet[rt] {
			t.Errorf("Route %q fehlt in api.md", rt)
		}
	}
}

// isSSORoute erkennt die Routen des Seiten-SSO. Der globale Google-/GitHub-
// Login (/api/auth/google, /api/auth/github) gehört nicht dazu.
func isSSORoute(route string) bool {
	_, path, _ := strings.Cut(route, " ")
	return strings.HasPrefix(path, "/api/auth/oidc/") || strings.HasPrefix(path, "/api/auth/saml/") ||
		path == "/api/auth/sso-exchange" || strings.HasSuffix(path, "/sso") ||
		strings.Contains(path, "/sso/")
}

func TestSSODocs_IsSSORoute(t *testing.T) {
	cases := map[string]bool{
		"GET /api/auth/oidc/login":                true,
		"POST /api/auth/saml/:page_id/acs":        true,
		"POST /api/auth/sso-exchange":             true,
		"GET /api/me/sso":                         true,
		"DELETE /api/me/sso/:id":                  true,
		"POST /api/admin/pages/:page_id/sso/test": true,
		"GET /api/auth/google":                    false,
		"GET /api/auth/github/callback":           false,
		"GET /api/admin/pages/:page_id":           false,
	}
	for route, want := range cases {
		if got := isSSORoute(route); got != want {
			t.Errorf("isSSORoute(%q) = %v, erwartet %v", route, got, want)
		}
	}
}

func TestSSODocs_NoSecretsOrRealDomains(t *testing.T) {
	realish := regexp.MustCompile(`(?i)(client_secret\s*[:=]\s*"?[A-Za-z0-9_-]{16,}|GOCSPX-|\.apps\.googleusercontent\.com)`)
	for _, name := range ssoDocFiles {
		if m := realish.FindString(readSSODoc(t, name)); m != "" {
			t.Errorf("%s enthält etwas, das wie ein echtes Secret/ID aussieht: %q", name, m)
		}
	}
}
