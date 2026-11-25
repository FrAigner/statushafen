package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// minGoVersion is the lowest Go release without known vulnerabilities that
// the backend calls. govulncheck in CI blocks on them, and CI installs the
// version from go.mod (go-version-file). Raise it when a new standard
// library advisory is fixed in a newer patch release.
// 1.26.11: GO-2026-6859 (net/http MaxBytesReader header race).
const minGoVersion = "1.26.11"

var goDirective = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+\.\d+)\s*$`)

func goModVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("go.mod not readable: %v", err)
	}
	m := goDirective.FindSubmatch(raw)
	if m == nil {
		t.Fatal("go.mod has no go directive with a full patch version (e.g. go 1.26.11)")
	}
	return string(m[1])
}

func TestGoMod_NotBelowMinimumGoVersion(t *testing.T) {
	if v := goModVersion(t); compareVersions(v, minGoVersion) < 0 {
		t.Errorf("go.mod requires Go %s, at least %s is needed", v, minGoVersion)
	}
}

// The Dockerfile build stage and the mock-saml service in docker-compose.yml
// must use the same Go release as go.mod, otherwise the shipped binary is
// built with a different standard library than the one CI scans and tests.
func TestGoVersion_SameInGoModDockerfileAndCompose(t *testing.T) {
	want := goModVersion(t)
	golangImage := regexp.MustCompile(`golang:(\d+\.\d+\.\d+)-alpine`)
	for _, file := range []string{"../Dockerfile", "../docker-compose.yml"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s not readable: %v", file, err)
		}
		matches := golangImage.FindAllSubmatch(raw, -1)
		if len(matches) == 0 {
			t.Errorf("%s: no golang:<version>-alpine image found", file)
		}
		for _, m := range matches {
			if got := string(m[1]); got != want {
				t.Errorf("%s uses golang:%s-alpine, go.mod requires %s", file, got, want)
			}
		}
	}
}

// compareVersions compares dotted numeric versions such as "1.26.11";
// it returns -1, 0 or 1.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.26.11", "1.26.11", 0},
		{"1.26.10", "1.26.11", -1},
		{"1.26.11", "1.26.9", 1},
		{"1.27.0", "1.26.11", 1},
		{"1.26", "1.26.0", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
