package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// The PostgreSQL tests (MIGRATION_PG_TEST) let embedded-postgres download its binaries from Maven
// Central once per run. That download has no retry: any non-200 answer fails every PostgreSQL
// test with "no version found matching ...". CI therefore prefetches the archive with retries and
// a checksum into the cache embedded-postgres reads (~/.embedded-postgres-go). The prefetched
// version must be the one the tests ask for (embeddedpostgres.V16), or the cache is never used.
func TestCI_PrefetchesEmbeddedPostgresBeforeTests(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("ci.yml not readable: %v", err)
	}
	ci := string(raw)

	m := regexp.MustCompile(`EMBEDDED_POSTGRES_VERSION:\s*"([0-9.]+)"`).FindStringSubmatch(ci)
	if m == nil {
		t.Fatal(`ci.yml sets no EMBEDDED_POSTGRES_VERSION: "<version>" for the prefetch step`)
	}
	if want := string(embeddedpostgres.V16); m[1] != want {
		t.Errorf("ci.yml prefetches PostgreSQL %s, the tests use embeddedpostgres.V16 = %s", m[1], want)
	}

	prefetch := strings.Index(ci, "- name: PostgreSQL-Binärdateien vorab laden")
	tests := strings.Index(ci, "- name: Run Tests")
	if prefetch < 0 || tests < 0 || prefetch > tests {
		t.Error(`ci.yml must run the step "PostgreSQL-Binärdateien vorab laden" before "Run Tests"`)
	}
	for _, want := range []string{"--retry", "sha256sum -c", ".embedded-postgres-go"} {
		if !strings.Contains(ci, want) {
			t.Errorf("prefetch step lacks %q", want)
		}
	}
}
