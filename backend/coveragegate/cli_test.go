package coveragegate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const diff = `+++ b/backend/a.go
@@ -1,0 +2,2 @@
+	x()
+	y()
+++ b/frontend/src/a.js
@@ -1,0 +1,2 @@
+a()
+b()
`

const goProfile = `mode: set
github.com/fraigner/statushafen/a.go:1.1,2.10 1 1
github.com/fraigner/statushafen/a.go:3.1,3.10 1 0
`

const lcov = `SF:src/a.js
DA:1,1
DA:2,1
BRDA:2,0,0,1
BRDA:2,0,1,1
end_of_record
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func runCmd(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRun_OhneUnterbefehl(t *testing.T) {
	code, _, errOut := runCmd()
	require.Equal(t, 2, code)
	require.Contains(t, errOut, "Aufruf:")
}

func TestRun_UnbekannterUnterbefehl(t *testing.T) {
	code, _, errOut := runCmd("quatsch")
	require.Equal(t, 2, code)
	require.Contains(t, errOut, "unbekannter Unterbefehl")
}

func TestCheck_UngetesteteZeileMachtGateRot(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", diff),
		"-go-profile", writeFile(t, dir, "go.out", goProfile),
		"-lcov", writeFile(t, dir, "lcov.info", lcov))

	require.Equal(t, 1, code)
	require.Contains(t, out, "backend/a.go:3: Statement nicht ausgeführt")
	require.NotContains(t, out, "backend/a.go:2:")
	require.Contains(t, out, "❌")
}

func TestCheck_AllesAbgedecktIstGruen(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", "+++ b/frontend/src/a.js\n@@ -0,0 +1,2 @@\n"),
		"-lcov", writeFile(t, dir, "lcov.info", lcov))

	require.Equal(t, 0, code, out)
	require.Contains(t, out, "✅")
	require.Contains(t, out, "Frontend-Zeilen: 100.00 %")
	require.NotContains(t, out, "Go-Statements")
}

func TestCheck_BaselineUnterschrittenIstRot(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", ""),
		"-go-profile", writeFile(t, dir, "go.out", goProfile),
		"-baseline", writeFile(t, dir, "b.json", `{"go_statements": 60}`))

	require.Equal(t, 1, code)
	require.Contains(t, out, "Go-Statements: 50.00 % < Baseline 60.00 %")
	require.NotContains(t, out, "Geänderter Code ohne Test")
}

func TestCheck_BaselineNurFuerGemesseneQuoten(t *testing.T) {
	dir := t.TempDir()
	// Ohne lcov-Report darf die Frontend-Baseline nicht als 0 % zählen.
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", ""),
		"-go-profile", writeFile(t, dir, "go.out", goProfile),
		"-baseline", writeFile(t, dir, "b.json", `{"go_statements": 50, "frontend_lines": 90, "frontend_branches": 80}`))

	require.Equal(t, 0, code, out)
}

func TestCheck_HinweisWennBaselineAngehobenWerdenKann(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", ""),
		"-lcov", writeFile(t, dir, "lcov.info", lcov),
		"-baseline", writeFile(t, dir, "b.json", `{"frontend_lines": 90, "frontend_branches": 100}`))

	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Baseline kann angehoben werden")
}

func TestCheck_Fehlerfaelle(t *testing.T) {
	dir := t.TempDir()
	okDiff := writeFile(t, dir, "d.diff", diff)
	missing := filepath.Join(dir, "fehlt")
	for name, args := range map[string][]string{
		"unbekanntes Flag":   {"check", "-gibtsnicht"},
		"ohne -diff":         {"check"},
		"diff fehlt":         {"check", "-diff", missing},
		"diff kaputt":        {"check", "-diff", writeFile(t, dir, "bad.diff", "@@ -1 +1 @@\n")},
		"go-profile fehlt":   {"check", "-diff", okDiff, "-go-profile", missing},
		"go-profile kaputt":  {"check", "-diff", okDiff, "-go-profile", writeFile(t, dir, "bad.out", "kaputt\n")},
		"lcov fehlt":         {"check", "-diff", okDiff, "-lcov", missing},
		"lcov kaputt":        {"check", "-diff", okDiff, "-lcov", writeFile(t, dir, "bad.info", "DA:1,1\n")},
		"baseline fehlt":     {"check", "-diff", okDiff, "-baseline", missing},
		"baseline kein JSON": {"check", "-diff", okDiff, "-baseline", writeFile(t, dir, "bad.json", "{")},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runCmd(args...)
			require.Equal(t, 2, code)
			require.NotEmpty(t, errOut)
		})
	}
}

func TestBaseline_SchreibtGemesseneQuoten(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "baseline.json")
	code, _, errOut := runCmd("baseline",
		"-go-profile", writeFile(t, dir, "go.out", goProfile),
		"-lcov", writeFile(t, dir, "lcov.info", lcov),
		"-out", out)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var b Baseline
	require.NoError(t, json.Unmarshal(data, &b))
	require.Equal(t, Baseline{GoStatements: 50, FrontendLines: 100, FrontendBranches: 100}, b)
}

func TestBaseline_Fehlerfaelle(t *testing.T) {
	dir := t.TempDir()
	prof := writeFile(t, dir, "go.out", goProfile)
	info := writeFile(t, dir, "lcov.info", lcov)
	for name, args := range map[string][]string{
		"unbekanntes Flag":     {"baseline", "-gibtsnicht"},
		"Reports fehlen":       {"baseline", "-out", filepath.Join(dir, "x.json")},
		"ohne -out":            {"baseline", "-go-profile", prof, "-lcov", info},
		"go-profile kaputt":    {"baseline", "-go-profile", writeFile(t, dir, "bad.out", "kaputt\n"), "-lcov", info, "-out", filepath.Join(dir, "x.json")},
		"lcov kaputt":          {"baseline", "-go-profile", prof, "-lcov", writeFile(t, dir, "bad.info", "DA:1,1\n"), "-out", filepath.Join(dir, "x.json")},
		"out nicht schreibbar": {"baseline", "-go-profile", prof, "-lcov", info, "-out", filepath.Join(dir, "gibtsnicht", "x.json")},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runCmd(args...)
			require.Equal(t, 2, code)
			require.NotEmpty(t, errOut)
		})
	}
}

func TestMutateRanges_GibtKommaListeAus(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("mutate-ranges", "-diff", writeFile(t, dir, "d.diff", diff))
	require.Equal(t, 0, code)
	require.Equal(t, "src/a.js:1-2\n", out)
}

func TestMutateRanges_OhneFrontendAenderungLeer(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("mutate-ranges", "-diff", writeFile(t, dir, "d.diff", "+++ b/backend/a.go\n@@ -0,0 +1 @@\n"))
	require.Equal(t, 0, code)
	require.Empty(t, out)
}

func TestMutateRanges_Fehlerfaelle(t *testing.T) {
	dir := t.TempDir()
	for name, args := range map[string][]string{
		"unbekanntes Flag": {"mutate-ranges", "-gibtsnicht"},
		"ohne -diff":       {"mutate-ranges"},
		"diff fehlt":       {"mutate-ranges", "-diff", filepath.Join(dir, "fehlt")},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runCmd(args...)
			require.Equal(t, 2, code)
			require.NotEmpty(t, errOut)
		})
	}
}

func TestMutationReport_UeberlebenderMutantMachtGateRot(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("mutation-report", "-gremlins", writeFile(t, dir, "g.json",
		`{"files":[{"file_name":"a.go","mutations":[{"type":"CONDITIONALS_NEGATION","status":"LIVED","line":3,"column":1},{"type":"ARITHMETIC_BASE","status":"KILLED","line":4,"column":1}]}]}`))

	require.Equal(t, 1, code)
	require.Contains(t, out, "❌")
	require.Contains(t, out, "backend/a.go:3: Mutant überlebt (CONDITIONALS_NEGATION, Spalte 1)")
	require.Contains(t, out, "1 von 2")
}

func TestMutationReport_AlleGetoetetIstGruen(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("mutation-report", "-gremlins", writeFile(t, dir, "g.json",
		`{"files":[{"file_name":"a.go","mutations":[{"type":"ARITHMETIC_BASE","status":"KILLED","line":4,"column":1}]}]}`))

	require.Equal(t, 0, code)
	require.Contains(t, out, "✅ Mutationstest bestanden: alle 1 Mutanten getötet.")
}

func TestMutationReport_OhneMutantenIstGruen(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("mutation-report", "-gremlins", writeFile(t, dir, "g.json", `{"files":[]}`))

	require.Equal(t, 0, code)
	require.Contains(t, out, "keine Mutanten")
}

func TestMutationReport_Fehlerfaelle(t *testing.T) {
	dir := t.TempDir()
	for name, args := range map[string][]string{
		"unbekanntes Flag": {"mutation-report", "-gibtsnicht"},
		"ohne -gremlins":   {"mutation-report"},
		"Datei fehlt":      {"mutation-report", "-gremlins", filepath.Join(dir, "fehlt")},
		"kein JSON":        {"mutation-report", "-gremlins", writeFile(t, dir, "bad.json", "{")},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, errOut := runCmd(args...)
			require.Equal(t, 2, code)
			require.NotEmpty(t, errOut)
		})
	}
}

func TestCheck_KeinHinweisWennBaselineGenauErreicht(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCmd("check",
		"-diff", writeFile(t, dir, "d.diff", ""),
		"-lcov", writeFile(t, dir, "lcov.info", lcov),
		"-baseline", writeFile(t, dir, "b.json", `{"frontend_lines": 100, "frontend_branches": 100}`))

	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "Baseline kann angehoben werden")
}

func TestCheck_FrontendToleranzAusFlag(t *testing.T) {
	dir := t.TempDir()
	args := []string{"check",
		"-diff", writeFile(t, dir, "d.diff", ""),
		"-lcov", writeFile(t, dir, "lcov.info", lcov),
		"-baseline", writeFile(t, dir, "b.json", `{"frontend_lines": 100.5, "frontend_branches": 100}`)}

	code, out, _ := runCmd(args...)
	require.Equal(t, 1, code, out)

	code, out, _ = runCmd(append(args, "-frontend-tolerance", "0.5")...)
	require.Equal(t, 0, code, out)
}

func TestCheck_NegativeToleranzIstFehler(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runCmd("check", "-diff", writeFile(t, dir, "d.diff", ""), "-frontend-tolerance", "-1")
	require.Equal(t, 2, code)
	require.Contains(t, errOut, "frontend-tolerance")
}
