package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Die Allowlist von gitleaks darf nur genau die bekannten Fehlalarme
// unterdrücken. Mit regexTarget = "line" und unverankerten Mustern verschwand
// jede Zeile, in der ein solcher Name nur vorkam, samt echtem Geheimnis. Der
// Test liest .gitleaks.toml und prüft das Verhalten der Muster ohne gitleaks.
func gitleaksAllowlist(t *testing.T) (target string, patterns []*regexp.Regexp) {
	t.Helper()
	raw, err := os.ReadFile("../.gitleaks.toml")
	if err != nil {
		t.Fatalf(".gitleaks.toml nicht lesbar: %v", err)
	}
	text := string(raw)
	if m := regexp.MustCompile(`(?m)^\s*regexTarget\s*=\s*"([a-z]+)"`).FindStringSubmatch(text); m != nil {
		target = m[1]
	}
	for _, m := range regexp.MustCompile(`(?m)^\s*'''(.+)'''\s*,?\s*$`).FindAllStringSubmatch(text, -1) {
		patterns = append(patterns, regexp.MustCompile(m[1]))
	}
	if len(patterns) == 0 {
		t.Fatal("keine Allowlist-Muster gefunden")
	}
	return target, patterns
}

func TestGitleaksAllowlist_TargetsOnlyTheFindingItself(t *testing.T) {
	target, _ := gitleaksAllowlist(t)
	if target != "secret" {
		t.Errorf("regexTarget = %q, erwartet \"secret\": Mit \"line\" oder \"match\" würde die ganze Zeile bzw. Fundstelle ausgenommen", target)
	}
}

func TestGitleaksAllowlist_PatternsAreAnchored(t *testing.T) {
	_, patterns := gitleaksAllowlist(t)
	for _, p := range patterns {
		s := p.String()
		if !strings.HasPrefix(s, "^") || !strings.HasSuffix(s, "$") {
			t.Errorf("Muster %q ist nicht an beiden Enden verankert", s)
		}
	}
}

// dash setzt Beispielwerte aus Teilen zusammen, damit der Scanner die Testdaten nicht selbst als Fund meldet.
func dash(parts ...string) string { return strings.Join(parts, "-") }

func TestGitleaksAllowlist_SuppressesOnlyKnownFalsePositives(t *testing.T) {
	_, patterns := gitleaksAllowlist(t)
	allowed := func(secret string) bool {
		for _, p := range patterns {
			if p.MatchString(secret) {
				return true
			}
		}
		return false
	}
	for _, ok := range []string{"avg_latency_ms", "p50_latency_ms", "p95_latency_ms", "max_latency_ms", dash("whk", "secret", "123"), "konto-fremd797"} {
		if !allowed(ok) {
			t.Errorf("bekannter Fehlalarm %q wird nicht mehr unterdrückt", ok)
		}
	}
	for _, bad := range []string{
		"sk-live-FAKEFAKEFAKEFAKE1234",
		"p95_latency_ms=sk-live-FAKEFAKEFAKEFAKE1234",
		`api_key = "p95_latency_ms"`,
		"x p95_latency_ms",
		"p95_latency_ms x",
		"abcdefghijklmnop0123456789_latency_ms",
		dash("whk", "secret", "1234"),
		dash("my", "whk", "secret", "123"),
		dash("konto", "fremd797", "und", "mehr"),
		"",
	} {
		if allowed(bad) {
			t.Errorf("%q darf nicht unterdrückt werden", bad)
		}
	}
}
