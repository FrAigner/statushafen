package coveragegate

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleDiff = `diff --git a/backend/handlers/a.go b/backend/handlers/a.go
index 111..222 100644
--- a/backend/handlers/a.go
+++ b/backend/handlers/a.go
@@ -10,0 +11,3 @@ func A() {
+	x := 1
+	y := 2
+	_ = x + y
@@ -20 +23 @@ func B() {
-	old()
+	neu()
@@ -30,2 +33,0 @@ func C() {
-	weg()
-	weg()
diff --git a/frontend/src/b.js b/frontend/src/b.js
new file mode 100644
--- /dev/null
+++ b/frontend/src/b.js
@@ -0,0 +1,2 @@
+export const b = 1
+export const c = 2
diff --git a/backend/geloescht.go b/backend/geloescht.go
deleted file mode 100644
--- a/backend/geloescht.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package backend
-var z = 1
`

func TestParseDiff_SammeltNeueZeilenProDatei(t *testing.T) {
	changed, err := ParseDiff(strings.NewReader(sampleDiff))
	require.NoError(t, err)

	require.Equal(t, ChangedLines{
		"backend/handlers/a.go": {11: true, 12: true, 13: true, 23: true},
		"frontend/src/b.js":     {1: true, 2: true},
	}, changed)
}

func TestParseDiff_KaputterHunkHeaderIstFehler(t *testing.T) {
	_, err := ParseDiff(strings.NewReader("+++ b/x.go\n@@ -1 +abc @@\n"))
	require.Error(t, err)
}

func TestParseDiff_KaputteZeilenanzahlIstFehler(t *testing.T) {
	_, err := ParseDiff(strings.NewReader("+++ b/x.go\n@@ -1 +3,zz @@\n"))
	require.Error(t, err)
}

func TestParseDiff_HunkOhneDateiIstFehler(t *testing.T) {
	_, err := ParseDiff(strings.NewReader("@@ -1 +1 @@\n"))
	require.Error(t, err)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("lesefehler") }

func TestParseDiff_LesefehlerWirdDurchgereicht(t *testing.T) {
	_, err := ParseDiff(failingReader{})
	require.Error(t, err)
}

const sampleProfile = `mode: set
github.com/fraigner/statushafen/handlers/a.go:10.20,12.3 2 1
github.com/fraigner/statushafen/handlers/a.go:12.3,14.4 1 0
github.com/fraigner/statushafen/handlers/a.go:12.3,14.4 1 1
github.com/fraigner/statushafen/handlers/a.go:20.1,25.2 3 0
github.com/fraigner/statushafen/main.go:1.1,2.2 4 1
`

func TestParseGoProfile_MapptModulPfadeUndFuehrtDuplikateZusammen(t *testing.T) {
	p, err := ParseGoProfile(strings.NewReader(sampleProfile), "github.com/fraigner/statushafen", "backend")
	require.NoError(t, err)

	// Duplikat 12.3,14.4 ist einmal mit 1 abgedeckt -> zählt als abgedeckt.
	// Abgedeckt: 2 + 1 + 4 = 7 von 10 Statements.
	require.InDelta(t, 70.0, p.Percent(), 0.001)
}

func TestGoProfile_UncoveredMeldetNurGeaenderteZeilenInNichtAusgefuehrtenBloecken(t *testing.T) {
	p, err := ParseGoProfile(strings.NewReader(sampleProfile), "github.com/fraigner/statushafen", "backend")
	require.NoError(t, err)

	findings := p.Uncovered(ChangedLines{
		"backend/handlers/a.go": {11: true, 13: true, 22: true, 30: true},
		"backend/unbekannt.go":  {1: true},
	})

	require.Equal(t, []Finding{
		{File: "backend/handlers/a.go", Line: 22, Reason: "Statement nicht ausgeführt"},
	}, findings)
}

func TestParseGoProfile_LeeresProfilHatHundertProzent(t *testing.T) {
	p, err := ParseGoProfile(strings.NewReader("mode: set\n"), "github.com/fraigner/statushafen", "backend")
	require.NoError(t, err)
	require.InDelta(t, 100.0, p.Percent(), 0.001)
}

func TestParseGoProfile_FehlerhafteZeilen(t *testing.T) {
	for name, line := range map[string]string{
		"ohne Doppelpunkt":   "kaputt 1 1",
		"zu wenige Felder":   "github.com/fraigner/statushafen/a.go:1.1,2.2 1",
		"Bereich ohne Komma": "github.com/fraigner/statushafen/a.go:1.1 1 1",
		"Start ohne Punkt":   "github.com/fraigner/statushafen/a.go:1,2.2 1 1",
		"Ende ohne Punkt":    "github.com/fraigner/statushafen/a.go:1.1,2 1 1",
		"Startzeile kaputt":  "github.com/fraigner/statushafen/a.go:x.1,2.2 1 1",
		"Endzeile kaputt":    "github.com/fraigner/statushafen/a.go:1.1,y.2 1 1",
		"Statements kaputt":  "github.com/fraigner/statushafen/a.go:1.1,2.2 x 1",
		"Zähler kaputt":      "github.com/fraigner/statushafen/a.go:1.1,2.2 1 x",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseGoProfile(strings.NewReader("mode: set\n"+line+"\n"), "github.com/fraigner/statushafen", "backend")
			require.Error(t, err)
		})
	}
}

func TestParseGoProfile_LesefehlerWirdDurchgereicht(t *testing.T) {
	_, err := ParseGoProfile(failingReader{}, "github.com/fraigner/statushafen", "backend")
	require.Error(t, err)
}

const sampleLCOV = `TN:
SF:src/a.js
FN:1,f
DA:1,1
DA:2,0
DA:3,1
BRDA:3,0,0,1
BRDA:3,0,1,0
BRDA:4,0,0,-
BRDA:5,0,0,2
LF:3
LH:2
end_of_record
SF:src/b.vue
DA:1,1
end_of_record
`

func TestParseLCOV_BerechnetZeilenUndZweigquote(t *testing.T) {
	l, err := ParseLCOV(strings.NewReader(sampleLCOV), "frontend")
	require.NoError(t, err)

	require.InDelta(t, 75.0, l.LinePercent(), 0.001)   // 3 von 4 Zeilen
	require.InDelta(t, 50.0, l.BranchPercent(), 0.001) // 2 von 4 Zweigen
}

func TestLCOV_UncoveredMeldetZeilenUndZweige(t *testing.T) {
	l, err := ParseLCOV(strings.NewReader(sampleLCOV), "frontend")
	require.NoError(t, err)

	findings := l.Uncovered(ChangedLines{
		"frontend/src/a.js":  {1: true, 2: true, 3: true, 4: true, 5: true},
		"frontend/src/b.vue": {1: true},
		"frontend/src/x.js":  {1: true},
	})

	require.Equal(t, []Finding{
		{File: "frontend/src/a.js", Line: 2, Reason: "Zeile nicht ausgeführt"},
		{File: "frontend/src/a.js", Line: 3, Reason: "Zweig nicht durchlaufen"},
		{File: "frontend/src/a.js", Line: 4, Reason: "Zweig nicht durchlaufen"},
	}, findings)
}

func TestParseLCOV_LeererReportHatHundertProzent(t *testing.T) {
	l, err := ParseLCOV(strings.NewReader(""), "frontend")
	require.NoError(t, err)
	require.InDelta(t, 100.0, l.LinePercent(), 0.001)
	require.InDelta(t, 100.0, l.BranchPercent(), 0.001)
}

func TestParseLCOV_FehlerhafteZeilen(t *testing.T) {
	for name, body := range map[string]string{
		"DA ohne Datei":      "DA:1,1\n",
		"DA zu kurz":         "SF:a.js\nDA:1\n",
		"DA Zeile kaputt":    "SF:a.js\nDA:x,1\n",
		"DA Zähler kaputt":   "SF:a.js\nDA:1,x\n",
		"BRDA ohne Datei":    "BRDA:1,0,0,1\n",
		"BRDA zu kurz":       "SF:a.js\nBRDA:1,0,0\n",
		"BRDA Zeile kaputt":  "SF:a.js\nBRDA:x,0,0,1\n",
		"BRDA Zähler kaputt": "SF:a.js\nBRDA:1,0,0,x\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseLCOV(strings.NewReader(body), "frontend")
			require.Error(t, err)
		})
	}
}

func TestParseLCOV_LesefehlerWirdDurchgereicht(t *testing.T) {
	_, err := ParseLCOV(failingReader{}, "frontend")
	require.Error(t, err)
}

func TestCompareBaseline(t *testing.T) {
	base := Baseline{GoStatements: 70.5, FrontendLines: 93.52, FrontendBranches: 83.75}

	t.Run("gleich oder besser ist ok", func(t *testing.T) {
		require.Empty(t, CompareBaseline(base, Baseline{GoStatements: 70.504, FrontendLines: 94, FrontendBranches: 83.75}, 0))
	})

	t.Run("jede gesunkene Quote wird gemeldet", func(t *testing.T) {
		require.Equal(t, []string{
			"Go-Statements: 70.40 % < Baseline 70.50 %",
			"Frontend-Zeilen: 93.00 % < Baseline 93.52 %",
			"Frontend-Zweige: 80.00 % < Baseline 83.75 %",
		}, CompareBaseline(base, Baseline{GoStatements: 70.4, FrontendLines: 93, FrontendBranches: 80}, 0))
	})
}

func TestMutateRanges_LiefertStrykerBereicheFuerQuellcodeImVerzeichnis(t *testing.T) {
	ranges := MutateRanges(ChangedLines{
		"frontend/src/a.js":                     {3: true, 4: true, 5: true, 9: true},
		"frontend/src/views/B.vue":              {1: true},
		"frontend/src/a.test.js":                {1: true},
		"frontend/src/test-support/stubs.js":    {1: true},
		"frontend/src/i18n/locales/de.json":     {1: true},
		"frontend/vite.config.js":               {1: true},
		"backend/handlers/a.go":                 {1: true},
		"frontend/src/composables/useX.spec.ts": {1: true},
		"frontend/src/c.ts":                     {7: true},
	}, "frontend")

	require.Equal(t, []string{
		"src/a.js:3-5",
		"src/a.js:9-9",
		"src/c.ts:7-7",
		"src/views/B.vue:1-1",
	}, ranges)
}

func TestParseDiff_HunkKopfOhneNeueSeiteIstFehler(t *testing.T) {
	for _, header := range []string{"@@ -1 @@", "@@ -1 -2 @@"} {
		_, err := ParseDiff(strings.NewReader("+++ b/x.go\n" + header + "\n"))
		require.Error(t, err, header)
	}
}

func TestFinding_String(t *testing.T) {
	require.Equal(t, "frontend/src/a.js:7: Zeile nicht ausgeführt",
		Finding{File: "frontend/src/a.js", Line: 7, Reason: "Zeile nicht ausgeführt"}.String())
}

func TestUncovered_SortiertNachDateiUndZeile(t *testing.T) {
	p, err := ParseGoProfile(strings.NewReader("mode: set\nm/b.go:1.1,9.1 1 0\nm/a.go:1.1,9.1 1 0\n"), "m", "backend")
	require.NoError(t, err)

	findings := p.Uncovered(ChangedLines{"backend/b.go": {2: true}, "backend/a.go": {5: true, 3: true}})

	require.Equal(t, []Finding{
		{File: "backend/a.go", Line: 3, Reason: "Statement nicht ausgeführt"},
		{File: "backend/a.go", Line: 5, Reason: "Statement nicht ausgeführt"},
		{File: "backend/b.go", Line: 2, Reason: "Statement nicht ausgeführt"},
	}, findings)
}

func TestParseDiff_SehrLangeZeilenSindErlaubt(t *testing.T) {
	long := "+" + strings.Repeat("x", 200*1024)
	changed, err := ParseDiff(strings.NewReader("+++ b/frontend/src/min.js\n@@ -0,0 +1 @@\n" + long + "\n"))
	require.NoError(t, err)
	require.Equal(t, ChangedLines{"frontend/src/min.js": {1: true}}, changed)
}

func TestParseDiff_HunkKopfMitGenauDreiFeldern(t *testing.T) {
	changed, err := ParseDiff(strings.NewReader("+++ b/x.go\n@@ -1 +2\n"))
	require.NoError(t, err)
	require.Equal(t, ChangedLines{"x.go": {2: true}}, changed)
}

func TestParseGoProfile_LeererDateinameIstFehler(t *testing.T) {
	_, err := ParseGoProfile(strings.NewReader("mode: set\n:1.1,2.2 1 1\n"), "github.com/fraigner/statushafen", "backend")
	require.Error(t, err)
}

func TestGoProfile_UncoveredBlockgrenzen(t *testing.T) {
	p, err := ParseGoProfile(strings.NewReader("mode: set\nm/a.go:10.1,12.2 1 0\nm/a.go:20.1,20.5 0 0\n"), "m", "backend")
	require.NoError(t, err)

	findings := p.Uncovered(ChangedLines{"backend/a.go": {9: true, 10: true, 12: true, 13: true, 20: true}})

	// Erste und letzte Blockzeile zählen, Nachbarzeilen und Blöcke ohne
	// Statements nicht.
	require.Equal(t, []Finding{
		{File: "backend/a.go", Line: 10, Reason: "Statement nicht ausgeführt"},
		{File: "backend/a.go", Line: 12, Reason: "Statement nicht ausgeführt"},
	}, findings)
}

func TestParseLCOV_ZweigquoteZaehltNurDurchlaufeneZweige(t *testing.T) {
	l, err := ParseLCOV(strings.NewReader("SF:src/a.js\nBRDA:1,0,0,1\nBRDA:1,0,1,3\nBRDA:1,0,2,1\nBRDA:1,0,3,0\nend_of_record\n"), "frontend")
	require.NoError(t, err)
	require.InDelta(t, 75.0, l.BranchPercent(), 0.001)
}

func TestRound2(t *testing.T) {
	require.InDelta(t, 70.50, Round2(70.496), 1e-9)
	require.InDelta(t, 70.49, Round2(70.494), 1e-9)
	require.InDelta(t, 83.75, Round2(83.75), 1e-9)
}

const sampleGremlins = `{"go_module":"github.com/fraigner/statushafen","mutants_total":4,"files":[
 {"file_name":"handlers/a.go","mutations":[
  {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":3,"column":1},
  {"type":"ARITHMETIC_BASE","status":"LIVED","line":9,"column":5},
  {"type":"INCREMENT_DECREMENT","status":"TIMED OUT","line":4,"column":2}]},
 {"file_name":"main.go","mutations":[
  {"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":7,"column":3},
  {"type":"INVERT_NEGATIVES","status":"NOT VIABLE","line":8,"column":3},
  {"type":"INVERT_NEGATIVES","status":"SKIPPED","line":20,"column":3}]}]}`

func TestParseGremlinsReport_MeldetUeberlebendeUndUnabgedeckteMutanten(t *testing.T) {
	survivors, total, err := ParseGremlinsReport(strings.NewReader(sampleGremlins), "backend")
	require.NoError(t, err)

	// SKIPPED (außerhalb des Diffs) und NOT VIABLE (kompiliert nicht)
	// zählen nicht mit.
	require.Equal(t, 4, total)
	require.Equal(t, []Finding{
		{File: "backend/handlers/a.go", Line: 9, Reason: "Mutant überlebt (ARITHMETIC_BASE, Spalte 5)"},
		{File: "backend/main.go", Line: 7, Reason: "Mutant nicht abgedeckt (CONDITIONALS_BOUNDARY, Spalte 3)"},
	}, survivors)
}

func TestParseGremlinsReport_KeineMutantenIstOk(t *testing.T) {
	survivors, total, err := ParseGremlinsReport(strings.NewReader(`{"files":[]}`), "backend")
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, survivors)
}

func TestParseGremlinsReport_KaputtesJSONIstFehler(t *testing.T) {
	_, _, err := ParseGremlinsReport(strings.NewReader(`{`), "backend")
	require.Error(t, err)
}

func TestParseDiff_InhaltszeileMitPlusPlusIstKeinDateikopf(t *testing.T) {
	d := "diff --git a/docs/x.md b/docs/x.md\n--- a/docs/x.md\n+++ b/docs/x.md\n@@ -0,0 +1,2 @@\n+++ b/fake.go\n+zwei\n@@ -3,0 +9 @@\n+neun\n" +
		"diff --git a/backend/a.go b/backend/a.go\n--- a/backend/a.go\n+++ b/backend/a.go\n@@ -1,0 +5 @@\n+x\n"
	changed, err := ParseDiff(strings.NewReader(d))
	require.NoError(t, err)
	require.Equal(t, ChangedLines{
		"docs/x.md":    {1: true, 2: true, 9: true},
		"backend/a.go": {5: true},
	}, changed)
}

func TestParseDiff_MitKontextzeilenUndSonderfaellen(t *testing.T) {
	// git diff mit Kontext (-U1), "\ No newline"-Marker und einer leeren
	// Kontextzeile (manche Tools kürzen " " zu "").
	d := "+++ b/a.js\n@@ -1,4 +1,5 @@\n kontext\n-alt\n+neu\n+++ plusplus\n\n\\ No newline at end of file\n eins\n" +
		"+++ b/b.js\n@@ -1 +1 @@\n+x\n"
	changed, err := ParseDiff(strings.NewReader(d))
	require.NoError(t, err)
	require.Equal(t, ChangedLines{
		"a.js": {1: true, 2: true, 3: true, 4: true, 5: true},
		"b.js": {1: true},
	}, changed)
}

func TestParseDiff_UnerwarteteZeileBeendetHunk(t *testing.T) {
	// Abgeschnittener Hunk: Der Kopf verspricht 3 Zeilen, danach folgt
	// schon die nächste Datei.
	d := "+++ b/a.js\n@@ -0,0 +1,3 @@\n+eins\ndiff --git a/b.js b/b.js\n+++ b/b.js\n@@ -0,0 +7 @@\n+x\n"
	changed, err := ParseDiff(strings.NewReader(d))
	require.NoError(t, err)
	require.Equal(t, ChangedLines{
		"a.js": {1: true, 2: true, 3: true},
		"b.js": {7: true},
	}, changed)
}

func TestParseDiff_KaputteAlteSeiteIstFehler(t *testing.T) {
	for _, header := range []string{"@@ -x +1 @@", "@@ -1,y +1 @@", "@@ +1 +1 @@"} {
		_, err := ParseDiff(strings.NewReader("+++ b/x.go\n" + header + "\n"))
		require.Error(t, err, header)
	}
}

// Die Frontend-Quoten (v8) schwanken zwischen
// identischen Läufen; die Toleranz gilt nur fürs Frontend, Go bleibt exakt.
func TestCompareBaseline_FrontendToleranz(t *testing.T) {
	base := Baseline{GoStatements: 70, FrontendLines: 93.5, FrontendBranches: 83.75}

	t.Run("knapp innerhalb ist ok", func(t *testing.T) {
		require.Empty(t, CompareBaseline(base, Baseline{GoStatements: 70, FrontendLines: 92.51, FrontendBranches: 82.76}, 1))
	})

	t.Run("genau auf der Grenze ist ok", func(t *testing.T) {
		require.Empty(t, CompareBaseline(base, Baseline{GoStatements: 70, FrontendLines: 92.5, FrontendBranches: 82.75}, 1))
	})

	t.Run("knapp darüber ist rot", func(t *testing.T) {
		require.Equal(t, []string{
			"Frontend-Zeilen: 92.49 % < Baseline 93.50 % (Toleranz 1.00 Punkte)",
			"Frontend-Zweige: 82.74 % < Baseline 83.75 % (Toleranz 1.00 Punkte)",
		}, CompareBaseline(base, Baseline{GoStatements: 70, FrontendLines: 92.49, FrontendBranches: 82.74}, 1))
	})

	t.Run("Go bekommt keine Toleranz", func(t *testing.T) {
		require.Equal(t, []string{"Go-Statements: 69.99 % < Baseline 70.00 %"},
			CompareBaseline(base, Baseline{GoStatements: 69.99, FrontendLines: 93.5, FrontendBranches: 83.75}, 1))
	})
}
