package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Die Basis-Images im Dockerfile dürfen nicht End-of-Life sein und sind auf
// eine Version (und Digest) festgelegt. Stand der Lebenszyklen: Alpine 3.21
// und älter sind ab 2026-11 ohne Sicherheitsupdates, Node 20 ist seit
// 2026-04 EOL. Beim Anheben der Mindestversionen die Daten bei
// endoflife.date prüfen.
const (
	minAlpineMinor = 22 // Alpine 3.22 (Support bis 2027-05)
	minNodeMajor   = 24 // Node 24 (LTS bis 2028-04); Node 22 endet 2027-04
)

var fromLine = regexp.MustCompile(`(?im)^FROM\s+(\S+)`)

func TestDockerfile_BaseImagesNotEndOfLife(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	matches := fromLine.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("keine FROM-Zeilen gefunden")
	}
	for _, m := range matches {
		ref, _, _ := strings.Cut(m[1], "@")
		name, tag, _ := strings.Cut(ref, ":")
		switch name {
		case "alpine":
			minor := versionPart(tag, 1)
			if !strings.HasPrefix(tag, "3.") || minor < minAlpineMinor {
				t.Errorf("%s: Alpine-Version ist End-of-Life oder unbestimmt (mindestens 3.%d)", m[1], minAlpineMinor)
			}
		case "node":
			if major := versionPart(tag, 0); major < minNodeMajor {
				t.Errorf("%s: Node-Version ist End-of-Life oder unbestimmt (mindestens %d)", m[1], minNodeMajor)
			}
		}
		if tag == "" || tag == "latest" {
			t.Errorf("%s: Basis-Image braucht eine feste Version", m[1])
		}
	}
}

// Das Laufzeit-Image ist zusätzlich per Digest festgelegt, damit ein
// verschobenes Tag nicht unbemerkt andere Inhalte liefert.
func TestDockerfile_RuntimeImagePinnedByDigest(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	re := regexp.MustCompile(`(?m)^FROM\s+alpine:\S+@sha256:[0-9a-f]{64}\s*$`)
	if !re.Match(raw) {
		t.Error("Laufzeit-Image (alpine) ist nicht per @sha256-Digest festgelegt")
	}
}

// versionPart liest den n-ten Teil (0 = Major) einer Version wie "24-alpine"
// oder "3.23"; -1, wenn er fehlt oder keine Zahl ist.
func versionPart(tag string, n int) int {
	core := strings.FieldsFunc(tag, func(r rune) bool { return r == '-' })
	if len(core) == 0 {
		return -1
	}
	parts := strings.Split(core[0], ".")
	if n >= len(parts) {
		return -1
	}
	v, err := strconv.Atoi(parts[n])
	if err != nil {
		return -1
	}
	return v
}

// Image, lokale Entwicklung (frontend/.nvmrc) und CI bauen mit derselben
// Node-Hauptversion, damit ein Frontend-Build nicht nur in einer der drei
// Umgebungen geprüft ist.
func TestNodeMajorMatchesDockerfileNvmrcAndWorkflows(t *testing.T) {
	docker, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	m := regexp.MustCompile(`(?im)^FROM\s+node:(\d+)`).FindStringSubmatch(string(docker))
	if m == nil {
		t.Fatal("kein node-Image im Dockerfile")
	}
	want := m[1]

	nvmrc, err := os.ReadFile("../frontend/.nvmrc")
	if err != nil {
		t.Fatalf("frontend/.nvmrc nicht lesbar: %v", err)
	}
	if got := strings.TrimSpace(string(nvmrc)); got != want {
		t.Errorf("frontend/.nvmrc = %q, Dockerfile nutzt Node %s", got, want)
	}

	files, err := os.ReadDir("../.github/workflows")
	if err != nil {
		t.Fatalf("Workflows nicht lesbar: %v", err)
	}
	re := regexp.MustCompile(`node-version:\s*['"]?(\d+)`)
	found := 0
	for _, f := range files {
		raw, err := os.ReadFile("../.github/workflows/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, mm := range re.FindAllStringSubmatch(string(raw), -1) {
			found++
			if mm[1] != want {
				t.Errorf("%s: node-version %s, Dockerfile nutzt Node %s", f.Name(), mm[1], want)
			}
		}
	}
	if found == 0 {
		t.Error("keine node-version in den Workflows gefunden")
	}
}

// Das Build-Image nutzt dieselbe Go-Version wie das Modul (go.mod). Ein älteres
// Image wäre End-of-Life und könnte das Modul nicht einmal bauen; ein
// vergessenes Anheben fiele sonst erst beim Image-Build auf.
func TestDockerfile_GoImageMatchesGoMod(t *testing.T) {
	docker, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	img := regexp.MustCompile(`(?im)^FROM\s+golang:(\d+\.\d+(?:\.\d+)?)`).FindStringSubmatch(string(docker))
	if img == nil {
		t.Fatal("kein golang-Image im Dockerfile")
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("go.mod nicht lesbar: %v", err)
	}
	want := regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`).FindStringSubmatch(string(mod))
	if want == nil {
		t.Fatal("keine go-Direktive in go.mod")
	}
	if img[1] != want[1] {
		t.Errorf("Dockerfile baut mit golang:%s, go.mod verlangt %s", img[1], want[1])
	}
}

// Alle Basis-Images (node, golang, alpine) sind per Digest festgelegt: Ein
// verschobenes Tag, auch beim Build-Image, würde sonst unbemerkt ein anderes
// Binary liefern. Den Digest des Index liefert die Registry-API:
//
//	TOKEN=$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" | jq -r .token)
//	curl -sI -H "Authorization: Bearer $TOKEN" \
//	  -H "Accept: application/vnd.oci.image.index.v1+json" \
//	  https://registry-1.docker.io/v2/library/golang/manifests/<tag> | grep -i docker-content-digest
func TestDockerfile_AllBaseImagesPinnedByDigest(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	digest := regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	matches := fromLine.FindAllStringSubmatch(string(raw), -1)
	if len(matches) < 3 {
		t.Fatalf("erwartet mindestens drei FROM-Zeilen (node, golang, alpine), gefunden %d", len(matches))
	}
	for _, m := range matches {
		if !digest.MatchString(m[1]) {
			t.Errorf("%s: Basis-Image ist nicht per @sha256-Digest festgelegt", m[1])
		}
	}
}

// Der Container meldet seinen Zustand selbst (/healthz prüft auch die
// Datenbank). Das Laufzeit-Image hat kein curl; busybox-wget genügt und liegt
// in Alpine bei.
func TestDockerfile_HasHealthcheckOnHealthz(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	// Fortsetzungszeilen ("\" am Zeilenende) zu einer Zeile zusammenfassen.
	raw = []byte(strings.ReplaceAll(string(raw), "\\\n", " "))
	re := regexp.MustCompile(`(?m)^HEALTHCHECK\s+.*CMD\s+wget\b.*/healthz`)
	if !re.Match(raw) {
		t.Error("Dockerfile braucht einen HEALTHCHECK mit wget auf /healthz")
	}
	if regexp.MustCompile(`(?m)^HEALTHCHECK\s+.*\bcurl\b`).Match(raw) {
		t.Error("das Laufzeit-Image hat kein curl")
	}
	if !strings.Contains(string(raw), "${PORT") {
		t.Error("der Healthcheck muss den Port aus PORT lesen (Render setzt ihn zur Laufzeit)")
	}
}

// Der Kommentar im Dockerfile behauptet, alle Basis-Images seien festgelegt;
// das gilt nur, solange der Test oben alle FROM-Zeilen prüft.
func TestDockerfile_CommentMatchesPinning(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	if !strings.Contains(string(raw), "Alle Basis-Images sind auf Version und Digest festgelegt") {
		t.Error("der Kommentar im Dockerfile muss sagen, dass alle Basis-Images festgelegt sind")
	}
}

// Der Docker-Build-Kontext enthält keine Zugangsdaten und keine Prüfberichte:
// Was nicht im Kontext liegt, landet weder in Build-Cache noch in Schichten der
// Builder-Stufen.
func TestDockerignore_ExcludesSecretsAndReports(t *testing.T) {
	raw, err := os.ReadFile("../.dockerignore")
	if err != nil {
		t.Fatalf(".dockerignore nicht lesbar: %v", err)
	}
	lines := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	for _, want := range []string{"**/.env", "**/.env.*", "security-*", "**/*.pem", "**/*.key"} {
		if !lines[want] {
			t.Errorf(".dockerignore braucht den Eintrag %q", want)
		}
	}
	// Die Beispiel-Konfiguration bleibt im Kontext (kein Geheimnis, wird gebraucht).
	if !lines["!**/.env.example"] {
		t.Error(".dockerignore muss .env.example ausnehmen")
	}
}
