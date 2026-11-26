package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Regression 2026-10-29: /logo.png and /fonts/*.woff2 served index.html
// (text/html) on prod instead of the files, because the router only knew /assets,
// /favicon.svg and /icons.svg. The Vite dev server serves public/ itself,
// so it went unnoticed locally. These tests run against the real
// handler with a build folder in the temp directory.

const spaMarker = "<html>SPA-INDEX</html>"

func staticTestRouter(t *testing.T, files map[string][]byte) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	files["index.html"] = []byte(spaMarker)
	for name, data := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	registerStaticFrontendFrom(r, root)
	return r, root
}

func getStatic(r *gin.Engine, urlPath string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, urlPath, nil))
	return w
}

func TestStatic_PublicDateienWerdenAusgeliefert(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nlogo")
	font := []byte("wOF2font-bytes")
	r, _ := staticTestRouter(t, map[string][]byte{
		"logo.png":                    png,
		"fonts/inter-latin-400.woff2": font,
		"favicon.svg":                 []byte("<svg/>"),
	})

	w := getStatic(r, "/logo.png")
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("/logo.png: %d, %q (erwartet das PNG, nicht index.html)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("Content-Type /logo.png = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Error("Cache-Control fehlt für statische Dateien")
	}

	w = getStatic(r, "/fonts/inter-latin-400.woff2")
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), font) {
		t.Fatalf("Schrift: %d, %q", w.Code, w.Body.String())
	}
	if w = getStatic(r, "/favicon.svg"); w.Code != http.StatusOK || w.Body.String() != "<svg/>" {
		t.Errorf("/favicon.svg: %d %q", w.Code, w.Body.String())
	}
}

func TestStatic_UnbekanntesFaelltAufIndexZurueck(t *testing.T) {
	r, _ := staticTestRouter(t, map[string][]byte{"logo.png": []byte("png")})
	for _, p := range []string{"/status/acme", "/admin/overview", "/nicht/da.png", "/"} {
		if w := getStatic(r, p); w.Code != http.StatusOK || w.Body.String() != spaMarker {
			t.Errorf("%s: %d %q (SPA-Index erwartet)", p, w.Code, w.Body.String())
		}
	}
	w := getStatic(r, "/api/gibt-es-nicht")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not found") {
		t.Errorf("/api/...: %d %q (JSON-404 erwartet)", w.Code, w.Body.String())
	}
}

func TestStatic_GibtKeineGeheimnisseOderVerzeichnisseHeraus(t *testing.T) {
	r, root := staticTestRouter(t, map[string][]byte{
		"fonts/a.woff2": []byte("font"),
		".versteckt":    []byte("GEHEIM"),
		"fonts/.env":    []byte("GEHEIM"),
	})
	// File next to the build folder, not reachable via ../.
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "geheim.txt"), []byte("GEHEIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/.versteckt", "/fonts/.env", "/../geheim.txt", "/fonts/../../geheim.txt",
		"/%2e%2e/geheim.txt", "/fonts/%2e%2e/%2e%2e/geheim.txt", "/fonts", "/fonts/",
	} {
		w := getStatic(r, p)
		if strings.Contains(w.Body.String(), "GEHEIM") {
			t.Errorf("%s liefert geschützten Inhalt aus", p)
		}
		if w.Code == http.StatusOK && w.Body.String() != spaMarker && p != "/fonts/%2e%2e/%2e%2e/geheim.txt" {
			t.Errorf("%s: erwartet SPA-Index, bekam %d %q", p, w.Code, w.Body.String())
		}
	}
}

func TestStaticFileFor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		url string
		ok  bool
	}{
		{"/a.txt", true}, {"a.txt", true}, {"/dir", false}, {"/nein.txt", false},
		{"/", false}, {"/index.html", false}, {"/.a", false}, {"/dir/../a.txt", true},
		{"/../a.txt", true}, // Clean resolves this to /a.txt, so it stays within the folder
	}
	for _, tt := range tests {
		if _, ok := staticFileFor(root, tt.url); ok != tt.ok {
			t.Errorf("staticFileFor(%q) = %v, erwartet %v", tt.url, ok, tt.ok)
		}
	}
}

// Guard: every file in frontend/public/ must be served.
// If someone puts a new file there (logo, font, image), it is immediately
// in the build; this test ensures the server does not swallow it.
func TestStatic_JedeDateiAusFrontendPublicWirdAusgeliefert(t *testing.T) {
	publicDir := filepath.Join("..", "frontend", "public")
	files := map[string][]byte{}
	var names []string
	err := filepath.WalkDir(publicDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(publicDir, p)
		rel = filepath.ToSlash(rel)
		files[rel] = []byte("INHALT:" + rel)
		names = append(names, rel)
		return nil
	})
	if err != nil {
		t.Skipf("frontend/public nicht lesbar: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("frontend/public ist leer, der Wächter prüft nichts")
	}
	r, _ := staticTestRouter(t, files)
	for _, rel := range names {
		w := getStatic(r, "/"+rel)
		if w.Code != http.StatusOK || w.Body.String() != "INHALT:"+rel {
			t.Errorf("/%s wird nicht als Datei ausgeliefert (%d, %q)", rel, w.Code, w.Body.String())
		}
	}
}

// A link in the build folder that points out of the folder must not
// serve anything: os.Stat follows links, and path.Clean does not see them.
func TestStaticFileFor_LinkAusDemOrdnerHeraus(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "dist")
	outside := filepath.Join(base, "aussen")
	for _, d := range []string{root, outside, filepath.Join(root, "fonts")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(outside, "geheim.txt")
	if err := os.WriteFile(secret, []byte("GEHEIM"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "echt.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"datei.txt":    secret,                          // Link to a file outside
		"fonts/aussen": outside,                         // Link to a folder outside
		"alias.txt":    filepath.Join(root, "echt.txt"), // Link inside: allowed
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Skipf("Symlinks nicht möglich: %v", err)
		}
	}

	for _, url := range []string{"/datei.txt", "/fonts/aussen/geheim.txt"} {
		if got, ok := staticFileFor(root, url); ok {
			t.Errorf("staticFileFor(%q) liefert %q aus dem Ordner heraus aus", url, got)
		}
	}
	// Control case: a normal file and a link within the folder work.
	for _, url := range []string{"/echt.txt", "/alias.txt"} {
		if _, ok := staticFileFor(root, url); !ok {
			t.Errorf("staticFileFor(%q) lehnt eine Datei im Ordner ab", url)
		}
	}
	// The link is resolved before anything is opened, so the file actually served is
	// the checked target, not the link.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := staticFileFor(root, "/alias.txt"); got != filepath.Join(realRoot, "echt.txt") {
		t.Errorf("staticFileFor(/alias.txt) = %q, want the resolved target", got)
	}
}

// Error paths: an unresolvable folder or path counts as outside.
func TestWithinRoot_NichtAufloesbareGelten_AlsAusserhalb(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !withinRoot(root, file) {
		t.Error("Datei im Ordner muss innerhalb liegen")
	}
	if withinRoot(filepath.Join(root, "fehlt"), file) {
		t.Error("nicht auflösbarer Ordner darf nichts freigeben")
	}
	if withinRoot(root, filepath.Join(root, "fehlt.txt")) {
		t.Error("nicht auflösbarer Pfad darf nicht innerhalb liegen")
	}
	if withinRoot(filepath.Join(root, "a.txt"), root) {
		t.Error("übergeordneter Ordner liegt nicht innerhalb einer Datei darunter")
	}
}
