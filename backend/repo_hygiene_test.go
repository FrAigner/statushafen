package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Das Repository soll ohne Betriebs- und Personenbezug veröffentlichbar
// bleiben: keine Deploy-Workflows, Zugangs- oder Schlüsseldateien und keine
// privaten Postfach-Adressen in versionierten Dateien. Geprüft wird der Index
// (git ls-files), nicht das Dateisystem. Drittsoftware-Lizenztexte enthalten
// Autorenadressen und sind ausgenommen.
var (
	// forbiddenTrackedPath trifft Deploy-Workflows und -Konfigurationen sowie
	// Dateien, die Geheimnisse tragen (.env ohne .example, Schlüssel, Zertifikate).
	forbiddenTrackedPath = regexp.MustCompile(`(?i)(^|/)(\.github/workflows/[^/]*deploy[^/]*|docker-compose\.[^/]*(prod|production|staging)[^/]*|\.env|\.env\.(?:prod|production|local)[^/]*|id_(?:rsa|ed25519|ecdsa)|[^/]+\.(?:pem|key|p12|pfx|kdbx))$`)
	// freeMailAddress trifft Adressen bei privaten Postfachanbietern.
	freeMailAddress = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@(?:gmail|googlemail|hotmail|outlook|live|gmx|web|yahoo|icloud|proton|protonmail)\.(?:com|de|net|me)\b`)
	// ticketReference trifft Verweise auf Tickets, Issues und Pull Requests
	// ("#123", "PR 2") in Kommentaren und Dokumentation. Ohne das Ticketsystem
	// sind sie tote Verweise; der Kommentar soll das Warum selbst erklären.
	ticketReference = regexp.MustCompile(`(?:^|[\s(])#[0-9]{1,5}\b|\bPR [0-9]+\b`)
	// inlineReference trifft Verweise auch außerhalb von Kommentaren, etwa in
	// Testnamen und Meldungen: "(#660)", "(#383/#387)" und "seit #543".
	inlineReference = regexp.MustCompile(`(?:^|\s)\(#[0-9]{1,5}[/,)]|\b(?:vor|seit|aus|vgl\.?|siehe|Regeln?|Fehlerbild|Sichtprüfung|Zusammenführung|for)\s+#[0-9]{1,5}\b`)
	// commentMarker trennt in Quelltext den Kommentar vom Code (Farbwerte wie
	// "#666" in Code und CSS-Werten sind keine Verweise).
	commentMarker = regexp.MustCompile(`//|/\*|<!--|^\s*\*|^\s*#\s`)
	// referenceFixtures sind Dateien, deren Tests Verweisnummern bewusst als
	// Testdaten enthalten (z. B. 3-stellige Hexfarben in Kommentaren).
	referenceFixtures = map[string]bool{
		"frontend/src/hexColors.test.js":              true,
		"frontend/src/patternsCss.test.js":            true,
		"frontend/src/publicDesign4a.test.js":         true,
		"frontend/src/test-support/noInlineStyles.js": true,
		"backend/sql_concat_guard_test.go":            true,
		"backend/repo_hygiene_test.go":                true,
	}
	// thirdPartyFiles dürfen fremde Autorenadressen enthalten.
	thirdPartyFiles = map[string]bool{"THIRD_PARTY_NOTICES.md": true, "LICENSE": true, "frontend/package-lock.json": true}
)

func trackedRepoFiles(t *testing.T) (root string, files []string) {
	t.Helper()
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("git nicht verfügbar: %v", err)
	}
	root = strings.TrimSpace(string(rootOut))
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	require.NoError(t, err)
	for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	require.NotEmpty(t, files)
	return root, files
}

func TestRepo_NoDeployOrSecretFilesTracked(t *testing.T) {
	_, files := trackedRepoFiles(t)
	for _, f := range files {
		assert.False(t, forbiddenTrackedPath.MatchString(f), "%s gehört nicht ins veröffentlichte Repository (Deploy-/Geheimnisdatei)", f)
	}
}

func TestRepo_NoPrivateMailboxAddressesTracked(t *testing.T) {
	root, files := trackedRepoFiles(t)
	for _, f := range files {
		if thirdPartyFiles[f] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil || len(raw) > 4<<20 || strings.ContainsRune(string(raw[:min(len(raw), 8000)]), 0) {
			continue // nicht lesbar, sehr groß oder binär
		}
		if m := freeMailAddress.FindString(string(raw)); m != "" {
			t.Errorf("%s enthält eine private Postfachadresse (%s): Beispieladressen unter example.com/example.org verwenden", f, m)
		}
	}
}

func TestRepo_NoTicketReferencesInCommentsOrDocs(t *testing.T) {
	root, files := trackedRepoFiles(t)
	for _, f := range files {
		if thirdPartyFiles[f] || referenceFixtures[f] || strings.HasSuffix(f, ".json") || strings.HasSuffix(f, ".lock") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil || len(raw) > 4<<20 || strings.ContainsRune(string(raw[:min(len(raw), 8000)]), 0) {
			continue
		}
		isDoc := strings.HasSuffix(f, ".md")
		for i, line := range strings.Split(string(raw), "\n") {
			if !hasTicketReference(line, isDoc) {
				continue
			}
			t.Errorf("%s:%d enthält einen Ticket-/PR-Verweis: den Grund im Kommentar selbst nennen", f, i+1)
		}
	}
}

// hasTicketReference prüft eine Zeile; in Quelltext zählt nur der Teil ab dem
// Kommentarzeichen.
func hasTicketReference(line string, isDoc bool) bool {
	if inlineReference.MatchString(line) {
		return true
	}
	if isDoc {
		return ticketReference.MatchString(line)
	}
	loc := commentMarker.FindStringIndex(line)
	return loc != nil && ticketReference.MatchString(line[loc[0]:])
}

func TestTicketReferencePattern(t *testing.T) {
	for _, l := range []string{"// vgl. #321", "x := 1 // (#668)", "/* PR 2: Abo */", " * seit #543", "<!-- PR 2 -->", "# Regel #643", `it('Rechte (#541/#660) gelten', f)`, `"war vor #660 an"`, "Zusammenführung #534"} {
		assert.True(t, hasTicketReference(l, false), l)
	}
	for _, l := range []string{`color:#666">`, "border: 1px solid #666;", "// Farbe #abc und #ffffff", "x := \"#1\"", "// Schritt 2 von 3", "// Support 24/7", "background: url(#12)", "var(--x, #666)"} {
		assert.False(t, hasTicketReference(l, false), l)
	}
	assert.True(t, hasTicketReference("siehe #434 dazu", true))
	assert.False(t, hasTicketReference("Farbe #abc", true))
}

// Gegenprobe: Die Muster erkennen, wogegen sie schützen, und lassen Beispiele durch.
func TestRepoHygienePatterns(t *testing.T) {
	for _, p := range []string{".github/workflows/deploy-prod.yml", "docker-compose.production.yml", ".env", "backend/.env.production", "keys/server.pem", "id_rsa"} {
		assert.True(t, forbiddenTrackedPath.MatchString(p), p)
	}
	for _, p := range []string{".github/workflows/ci.yml", "docker-compose.yml", "backend/.env.example", "frontend/.env.example", "README.md"} {
		assert.False(t, forbiddenTrackedPath.MatchString(p), p)
	}
	// Die Beispiele werden zusammengesetzt, damit diese Datei die Prüfung nicht
	// selbst auslöst.
	for _, a := range []string{"jemand@" + "gmail.com", "Name.Nachname@" + "hotmail.de", "x@" + "GMX.net", "a+b@" + "outlook.com"} {
		assert.True(t, freeMailAddress.MatchString(a), a)
	}
	for _, a := range []string{"admin@example.com", "user@statushafen.local", "a@" + "gmail.example", "owner@x.example"} {
		assert.False(t, freeMailAddress.MatchString(a), a)
	}
}
