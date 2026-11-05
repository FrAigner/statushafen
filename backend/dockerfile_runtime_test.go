package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// lastStage liefert den Text ab der letzten FROM-Zeile (das Laufzeit-Image).
func lastStage(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	idx := fromLine.FindAllStringIndex(string(raw), -1)
	if len(idx) == 0 {
		t.Fatal("Dockerfile ohne FROM")
	}
	return string(raw)[idx[len(idx)-1][0]:]
}

// Das Laufzeit-Image läuft nicht als root: Die letzte USER-Anweisung der
// letzten Stufe nennt einen anderen Benutzer als root bzw. 0.
func TestDockerfile_RuntimeStageRunsAsNonRoot(t *testing.T) {
	users := regexp.MustCompile(`(?im)^USER\s+(\S+)`).FindAllStringSubmatch(lastStage(t), -1)
	if len(users) == 0 {
		t.Fatal("die letzte Stufe setzt keinen USER, der Prozess liefe als root")
	}
	user := strings.ToLower(users[len(users)-1][1])
	if user == "root" || user == "0" || strings.HasPrefix(user, "root:") || strings.HasPrefix(user, "0:") {
		t.Errorf("der Prozess läuft als %q", user)
	}
}

// Docker kennt für den Healthcheck nur die Rückgabewerte 0 (gesund) und 1
// (nicht gesund); jeder andere Rückgabewert von wget (z. B. 4 bei einem
// Netzfehler oder 8 bei einem Serverfehler) wäre ungültig. Die Zeile bildet
// deshalb jeden Fehlschlag auf 1 ab.
func TestDockerfile_HealthcheckMapsFailureToExit1(t *testing.T) {
	raw, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile nicht lesbar: %v", err)
	}
	flat := strings.ReplaceAll(string(raw), "\\\n", " ")
	line := regexp.MustCompile(`(?m)^HEALTHCHECK\s+.*$`).FindString(flat)
	if line == "" {
		t.Fatal("kein HEALTHCHECK")
	}
	if !regexp.MustCompile(`\|\|\s*exit 1\s*$`).MatchString(line) {
		t.Errorf("der Healthcheck muss mit \"|| exit 1\" enden: %s", line)
	}
}
