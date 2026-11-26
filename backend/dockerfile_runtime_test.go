package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// lastStage returns the text from the last FROM line onward (the runtime image).
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

// The runtime image does not run as root: the last USER instruction of the
// last stage names a user other than root or 0.
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

// For the healthcheck, Docker only knows the exit codes 0 (healthy) and 1
// (unhealthy); any other exit code of wget (e.g. 4 on a
// network error or 8 on a server error) would be invalid. The line
// therefore maps every failure to 1.
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
