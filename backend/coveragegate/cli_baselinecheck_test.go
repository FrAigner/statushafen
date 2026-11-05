package coveragegate

// baseline-check prüft nur die Gesamtquoten (Push auf develop/main), ohne Diff.

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func baselineCheck(t *testing.T, base string, extra ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	args := append([]string{"baseline-check",
		"-go-profile", writeFile(t, dir, "go.out", goProfile),
		"-lcov", writeFile(t, dir, "lcov.info", lcov),
		"-baseline", writeFile(t, dir, "b.json", base)}, extra...)
	return runCmd(args...)
}

func TestBaselineCheck_GleichOderDarueberIstGruen(t *testing.T) {
	code, out, _ := baselineCheck(t, `{"go_statements": 50, "frontend_lines": 90, "frontend_branches": 100}`)

	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Go-Statements: 50.00 %")
	require.Contains(t, out, "Frontend-Zeilen: 100.00 %")
	require.Contains(t, out, "✅")
}

func TestBaselineCheck_GoUnterBaselineIstRot(t *testing.T) {
	code, out, _ := baselineCheck(t, `{"go_statements": 50.01, "frontend_lines": 90, "frontend_branches": 90}`)

	require.Equal(t, 1, code)
	require.Contains(t, out, "❌")
	require.Contains(t, out, "Gesamtquote gesunken: Go-Statements: 50.00 % < Baseline 50.01 %")
}

func TestBaselineCheck_FrontendToleranz(t *testing.T) {
	base := `{"go_statements": 50, "frontend_lines": 101, "frontend_branches": 100}`

	code, out, _ := baselineCheck(t, base, "-frontend-tolerance", "1.5")
	require.Equal(t, 0, code, out)

	code, out, _ = baselineCheck(t, base, "-frontend-tolerance", "0.5")
	require.Equal(t, 1, code)
	require.Contains(t, out, "Frontend-Zeilen: 100.00 % < Baseline 101.00 % (Toleranz 0.50 Punkte)")

	// Go bleibt auch mit Toleranz exakt.
	code, _, _ = baselineCheck(t, `{"go_statements": 51}`, "-frontend-tolerance", "1.5")
	require.Equal(t, 1, code)
}

func TestBaselineCheck_HinweisWennBaselineAngehobenWerdenKann(t *testing.T) {
	_, out, _ := baselineCheck(t, `{"go_statements": 40, "frontend_lines": 90, "frontend_branches": 90}`)
	require.Contains(t, out, "Baseline kann angehoben werden")

	_, out, _ = baselineCheck(t, `{"go_statements": 50, "frontend_lines": 100, "frontend_branches": 100}`)
	require.NotContains(t, out, "Baseline kann angehoben werden")
}

func TestBaselineCheck_Fehlerfaelle(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "fehlt")
	goOut := writeFile(t, dir, "go.out", goProfile)
	info := writeFile(t, dir, "lcov.info", lcov)
	base := writeFile(t, dir, "b.json", `{"go_statements": 50}`)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"unbekanntes Flag":   {[]string{"baseline-check", "-gibtsnicht"}, "flag provided but not defined"},
		"ohne -go-profile":   {[]string{"baseline-check", "-lcov", info, "-baseline", base}, "-go-profile"},
		"ohne -lcov":         {[]string{"baseline-check", "-go-profile", goOut, "-baseline", base}, "-lcov"},
		"ohne -baseline":     {[]string{"baseline-check", "-go-profile", goOut, "-lcov", info}, "-baseline"},
		"go-profile fehlt":   {[]string{"baseline-check", "-go-profile", missing, "-lcov", info, "-baseline", base}, "fehlt"},
		"lcov fehlt":         {[]string{"baseline-check", "-go-profile", goOut, "-lcov", missing, "-baseline", base}, "fehlt"},
		"baseline fehlt":     {[]string{"baseline-check", "-go-profile", goOut, "-lcov", info, "-baseline", missing}, "fehlt"},
		"baseline kein JSON": {[]string{"baseline-check", "-go-profile", goOut, "-lcov", info, "-baseline", writeFile(t, dir, "bad.json", "{")}, "bad.json"},
		"negative Toleranz":  {[]string{"baseline-check", "-go-profile", goOut, "-lcov", info, "-baseline", base, "-frontend-tolerance", "-1"}, "negativ"},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runCmd(tc.args...)
			require.Equal(t, 2, code)
			require.Contains(t, errOut, tc.want)
		})
	}
}
