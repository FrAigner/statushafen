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

// Regression 2026-10-29: /logo.png und /fonts/*.woff2 lieferten auf Prod
// index.html (text/html) statt der Dateien, weil der Router nur /assets,
// /favicon.svg und /icons.svg kannte. Der Vite-Devserver liefert public/ selbst
// aus, deshalb fiel es lokal nicht auf. Diese Tests laufen gegen den echten
// Handler mit einem Build-Ordner im Temp-Verzeichnis.

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
	// Datei neben dem Build-Ordner, per ../ nicht erreichbar.
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
		{"/../a.txt", true}, // Clean löst das auf /a.txt auf, bleibt also im Ordner
	}
	for _, tt := range tests {
		if _, ok := staticFileFor(root, tt.url); ok != tt.ok {
			t.Errorf("staticFileFor(%q) = %v, erwartet %v", tt.url, ok, tt.ok)
		}
	}
}

// Wächter: Jede Datei, die in frontend/public/ liegt, muss ausgeliefert werden.
// Legt jemand eine neue Datei dort ab (Logo, Schrift, Bild), steht sie sofort
// im Build; dieser Test stellt sicher, dass der Server sie nicht verschluckt.
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
