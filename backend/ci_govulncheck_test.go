package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// govulncheck builds SSA for the whole program including the standard library. v1.1.4 (x/tools
// v0.29) panics on the Go 1.27 standard library ("unexpected expr: *ast.KeyValueExpr"), so the
// pinned version has to keep up with the Go version in go.mod; v1.10.0 handles Go 1.27.
func TestCodeQuality_GovulncheckSupportsGoVersion(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/code-quality.yml")
	if err != nil {
		t.Fatalf("code-quality.yml not readable: %v", err)
	}
	m := regexp.MustCompile(`govulncheck@v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("code-quality.yml pins no govulncheck version")
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 1 || (major == 1 && minor < 10) {
		t.Errorf("govulncheck v%s.%s.%s cannot analyse the Go 1.27 standard library, need >= v1.10.0", m[1], m[2], m[3])
	}
}
