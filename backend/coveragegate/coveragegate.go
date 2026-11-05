// Package coveragegate prüft in der CI, ob jede in einem PR geänderte Zeile
// von Tests abgedeckt ist (Coverage- und Mutations-Gate).
//
// Design-Entscheidung: Der Gate wertet nur die Zeilen aus, die der PR
// hinzufügt oder ändert (git diff -U0 gegen den Ziel-Branch). Für den
// Bestandscode gilt stattdessen eine Baseline der Gesamtquoten, die nur
// steigen darf. So wird neuer Code sofort zu 100 % abgedeckt, ohne dass
// Altlasten jeden PR blockieren.
//
// Go kennt nur Statement-Coverage (Blöcke), keine echte Branch-Coverage.
// Ein nicht ausgeführter if-Zweig fällt trotzdem auf, weil sein Block den
// Zähler 0 hat. Den "Zweig nicht genommen"-Fall ohne eigenen Block (if ohne
// else) decken die Mutationstests (gremlins, CONDITIONALS_NEGATION) ab.
package coveragegate

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// maxDiffLine erlaubt sehr lange Diff-Zeilen (z. B. minifizierte Dateien);
// bufio.Scanner bricht sonst bei 64 KiB ab.
const maxDiffLine = 16 << 20

// ChangedLines enthält je Datei (Pfad relativ zur Repo-Wurzel) die Zeilen-
// nummern, die der Diff auf der neuen Seite hinzufügt.
type ChangedLines map[string]map[int]bool

// Finding ist eine geänderte Zeile ohne ausreichende Testabdeckung.
type Finding struct {
	File   string
	Line   int
	Reason string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Reason)
}

// ParseDiff liest die Ausgabe von `git diff -U0` und sammelt die neuen
// Zeilen. Gelöschte Dateien (+++ /dev/null) tauchen nicht auf.
func ParseDiff(r io.Reader) (ChangedLines, error) {
	changed := ChangedLines{}
	current := ""
	inFile := false
	// Noch offene Zeilen des aktuellen Hunk-Rumpfs (alte/neue Seite). Nur
	// außerhalb des Rumpfs ist "+++ " ein Dateikopf; eine hinzugefügte
	// Inhaltszeile "++ x" steht im Diff ebenfalls als "+++ x".
	remOld, remNew := 0, 0
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, maxDiffLine)
	for sc.Scan() {
		line := sc.Text()
		if remOld > 0 || remNew > 0 {
			if consumeHunkLine(line, &remOld, &remNew) {
				continue
			}
			// Unerwartete Zeile: Hunk war kürzer als angekündigt.
			remOld, remNew = 0, 0
		}
		switch {
		case strings.HasPrefix(line, "+++ "):
			target := strings.TrimPrefix(line, "+++ ")
			inFile = true
			current = ""
			if target != "/dev/null" {
				current = strings.TrimPrefix(target, "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			if !inFile {
				return nil, fmt.Errorf("hunk ohne Dateikopf: %q", line)
			}
			oldCount, start, count, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			remOld, remNew = oldCount, count
			if current == "" || count == 0 {
				continue
			}
			if changed[current] == nil {
				changed[current] = map[int]bool{}
			}
			for l := start; l < start+count; l++ {
				changed[current][l] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return changed, nil
}

// consumeHunkLine zählt eine Rumpfzeile von den offenen Zeilen ab und
// meldet, ob sie zum Rumpf gehörte.
func consumeHunkLine(line string, remOld, remNew *int) bool {
	// Bewusst if + switch auf den Wert statt switch mit Bedingungen: gremlins
	// ordnet Bedingungen in case-Ausdrücken keinem Coverage-Block zu und
	// meldet sie sonst als NOT COVERED.
	if line == "" || line[0] == ' ' {
		*remOld--
		*remNew--
		return true
	}
	switch line[0] {
	case '-':
		*remOld--
	case '+':
		*remNew--
	case '\\': // "\ No newline at end of file"
	default:
		return false
	}
	return true
}

// parseHunkHeader liest aus "@@ -a,b +c,d @@ ..." die alte Zeilenanzahl b
// sowie Start c und Anzahl d der neuen Seite.
func parseHunkHeader(header string) (int, int, int, error) {
	fields := strings.Fields(header)
	if len(fields) < 3 || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, 0, fmt.Errorf("ungültiger Hunk-Kopf: %q", header)
	}
	_, oldCount, err := parseRange(fields[1][1:])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ungültiger Hunk-Kopf %q: %w", header, err)
	}
	start, count, err := parseRange(fields[2][1:])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ungültiger Hunk-Kopf %q: %w", header, err)
	}
	return oldCount, start, count, nil
}

// parseRange liest "start[,count]"; ohne count ist es eine Zeile.
func parseRange(spec string) (int, int, error) {
	startStr, countStr, hasCount := strings.Cut(spec, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, err
	}
	count := 1
	if hasCount {
		count, err = strconv.Atoi(countStr)
		if err != nil {
			return 0, 0, err
		}
	}
	return start, count, nil
}

type goBlock struct {
	startLine, endLine int
	stmts              int
	covered            bool
}

// GoProfile ist ein eingelesenes `go test -coverprofile`-Profil.
type GoProfile struct {
	blocks map[string]map[string]*goBlock // Datei -> Blockschlüssel -> Block
}

// ParseGoProfile liest ein Coverprofile. modulePath ist der Modulname aus
// go.mod, repoDir das Verzeichnis des Moduls relativ zur Repo-Wurzel. Mit
// -coverpkg=./... schreibt jedes Testpaket dieselben Blöcke erneut; ein
// Block gilt als abgedeckt, sobald irgendein Eintrag ihn ausgeführt hat.
func ParseGoProfile(r io.Reader, modulePath, repoDir string) (*GoProfile, error) {
	p := &GoProfile{blocks: map[string]map[string]*goBlock{}}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		file, b, key, err := parseGoProfileLine(line)
		if err != nil {
			return nil, err
		}
		file = path.Join(repoDir, strings.TrimPrefix(file, modulePath+"/"))
		if p.blocks[file] == nil {
			p.blocks[file] = map[string]*goBlock{}
		}
		if existing, ok := p.blocks[file][key]; ok {
			existing.covered = existing.covered || b.covered
			continue
		}
		p.blocks[file][key] = b
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

// parseGoProfileLine zerlegt "datei.go:10.20,12.3 2 1".
func parseGoProfileLine(line string) (string, *goBlock, string, error) {
	idx := strings.LastIndex(line, ":")
	if idx <= 0 {
		return "", nil, "", fmt.Errorf("ungültige Profilzeile: %q", line)
	}
	file, rest := line[:idx], line[idx+1:]
	fields := strings.Fields(rest)
	if len(fields) != 3 {
		return "", nil, "", fmt.Errorf("ungültige Profilzeile: %q", line)
	}
	from, to, ok := strings.Cut(fields[0], ",")
	if !ok {
		return "", nil, "", fmt.Errorf("ungültiger Bereich in %q", line)
	}
	startLine, err := lineOf(from)
	if err != nil {
		return "", nil, "", fmt.Errorf("%q: %w", line, err)
	}
	endLine, err := lineOf(to)
	if err != nil {
		return "", nil, "", fmt.Errorf("%q: %w", line, err)
	}
	stmts, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", nil, "", fmt.Errorf("%q: %w", line, err)
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", nil, "", fmt.Errorf("%q: %w", line, err)
	}
	b := &goBlock{startLine: startLine, endLine: endLine, stmts: stmts, covered: count > 0}
	return file, b, fields[0], nil
}

// lineOf liest die Zeile aus "zeile.spalte".
func lineOf(pos string) (int, error) {
	l, _, ok := strings.Cut(pos, ".")
	if !ok {
		return 0, fmt.Errorf("ungültige Position %q", pos)
	}
	return strconv.Atoi(l)
}

// Percent liefert die Statement-Coverage über alle Blöcke.
func (p *GoProfile) Percent() float64 {
	total, covered := 0, 0
	for _, blocks := range p.blocks {
		for _, b := range blocks {
			total += b.stmts
			if b.covered {
				covered += b.stmts
			}
		}
	}
	return percent(covered, total)
}

// Uncovered meldet geänderte Zeilen, die ein nicht ausgeführter Block
// berührt. Zeilen außerhalb jedes Blocks (Deklarationen, Kommentare) und
// Dateien ohne Statements tauchen im Profil nicht auf und zählen nicht.
func (p *GoProfile) Uncovered(changed ChangedLines) []Finding {
	var findings []Finding
	for file, lines := range changed {
		blocks := p.blocks[file]
		for line := range lines {
			for _, b := range blocks {
				if !b.covered && b.stmts > 0 && line >= b.startLine && line <= b.endLine {
					findings = append(findings, Finding{File: file, Line: line, Reason: "Statement nicht ausgeführt"})
					break
				}
			}
		}
	}
	sortFindings(findings)
	return findings
}

type lcovFile struct {
	lines    map[int]int
	branches map[int][]int // -1 steht für "-" (Zweig nie erreicht)
}

// LCOV ist ein eingelesener lcov-Report (vitest/v8).
type LCOV struct {
	files map[string]*lcovFile
}

// ParseLCOV liest lcov.info. repoDir ist das Verzeichnis, relativ zu dem
// die SF-Pfade stehen (bei vitest: frontend).
func ParseLCOV(r io.Reader, repoDir string) (*LCOV, error) {
	l := &LCOV{files: map[string]*lcovFile{}}
	var cur *lcovFile
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		tag, value, _ := strings.Cut(line, ":")
		switch tag {
		case "SF":
			cur = &lcovFile{lines: map[int]int{}, branches: map[int][]int{}}
			l.files[path.Join(repoDir, value)] = cur
		case "DA":
			if cur == nil {
				return nil, fmt.Errorf("DA ohne SF: %q", line)
			}
			parts := strings.Split(value, ",")
			if len(parts) < 2 {
				return nil, fmt.Errorf("ungültige DA-Zeile: %q", line)
			}
			n, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("%q: %w", line, err)
			}
			hits, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("%q: %w", line, err)
			}
			cur.lines[n] += hits
		case "BRDA":
			if cur == nil {
				return nil, fmt.Errorf("BRDA ohne SF: %q", line)
			}
			parts := strings.Split(value, ",")
			if len(parts) != 4 {
				return nil, fmt.Errorf("ungültige BRDA-Zeile: %q", line)
			}
			n, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("%q: %w", line, err)
			}
			taken := -1
			if parts[3] != "-" {
				taken, err = strconv.Atoi(parts[3])
				if err != nil {
					return nil, fmt.Errorf("%q: %w", line, err)
				}
			}
			cur.branches[n] = append(cur.branches[n], taken)
		case "end_of_record":
			cur = nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return l, nil
}

// LinePercent liefert die Zeilenabdeckung über alle Dateien.
func (l *LCOV) LinePercent() float64 {
	total, covered := 0, 0
	for _, f := range l.files {
		for _, hits := range f.lines {
			total++
			if hits > 0 {
				covered++
			}
		}
	}
	return percent(covered, total)
}

// BranchPercent liefert die Zweigabdeckung über alle Dateien.
func (l *LCOV) BranchPercent() float64 {
	total, covered := 0, 0
	for _, f := range l.files {
		for _, takens := range f.branches {
			for _, taken := range takens {
				total++
				if taken > 0 {
					covered++
				}
			}
		}
	}
	return percent(covered, total)
}

// Uncovered meldet geänderte Zeilen, die nie ausgeführt wurden oder an denen
// ein Zweig nie durchlaufen wurde. Dateien ohne Eintrag im Report (Tests,
// Konfiguration, JSON) zählen nicht: vitest nimmt per coverage.include alle
// Quelldateien unter src/ auf, auch ungetestete.
func (l *LCOV) Uncovered(changed ChangedLines) []Finding {
	var findings []Finding
	for file, lines := range changed {
		f := l.files[file]
		if f == nil {
			continue
		}
		for line := range lines {
			if hits, ok := f.lines[line]; ok && hits == 0 {
				findings = append(findings, Finding{File: file, Line: line, Reason: "Zeile nicht ausgeführt"})
				continue
			}
			for _, taken := range f.branches[line] {
				if taken <= 0 {
					findings = append(findings, Finding{File: file, Line: line, Reason: "Zweig nicht durchlaufen"})
					break
				}
			}
		}
	}
	sortFindings(findings)
	return findings
}

// Baseline hält die Gesamtquoten, unter die der Bestandscode nicht fallen
// darf (coverage-baseline.json in der Repo-Wurzel).
type Baseline struct {
	GoStatements     float64 `json:"go_statements"`
	FrontendLines    float64 `json:"frontend_lines"`
	FrontendBranches float64 `json:"frontend_branches"`
}

// CompareBaseline meldet jede Quote, die (auf zwei Nachkommastellen
// gerundet) unter der Baseline liegt. frontendTolerance (Prozentpunkte) gilt
// nur für die Frontend-Quoten: v8 misst auf identischem Code je nach Timing
// unterschiedlich viel.
// Go ist deterministisch und wird immer exakt verglichen.
func CompareBaseline(base, current Baseline, frontendTolerance float64) []string {
	var regressions []string
	check := func(name string, b, c, tol float64) {
		if Round2(c) >= Round2(b-tol) {
			return
		}
		msg := fmt.Sprintf("%s: %.2f %% < Baseline %.2f %%", name, c, b)
		if tol > 0 {
			msg += fmt.Sprintf(" (Toleranz %.2f Punkte)", tol)
		}
		regressions = append(regressions, msg)
	}
	check("Go-Statements", base.GoStatements, current.GoStatements, 0)
	check("Frontend-Zeilen", base.FrontendLines, current.FrontendLines, frontendTolerance)
	check("Frontend-Zweige", base.FrontendBranches, current.FrontendBranches, frontendTolerance)
	return regressions
}

// MutateRanges baut die --mutate-Angaben für StrykerJS: zusammenhängende
// geänderte Zeilen je Quelldatei unter <dir>/src, relativ zu dir.
// Tests, Test-Hilfen und Nicht-Code-Dateien werden nicht mutiert.
func MutateRanges(changed ChangedLines, dir string) []string {
	var ranges []string
	for file, lines := range changed {
		rel, ok := strings.CutPrefix(file, dir+"/")
		if !ok || !isMutatableSource(rel) {
			continue
		}
		nums := make([]int, 0, len(lines))
		for n := range lines {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		start := nums[0]
		for i := 1; i <= len(nums); i++ {
			if i < len(nums) && nums[i] == nums[i-1]+1 {
				continue
			}
			ranges = append(ranges, fmt.Sprintf("%s:%d-%d", rel, start, nums[i-1]))
			if i < len(nums) {
				start = nums[i]
			}
		}
	}
	sort.Strings(ranges)
	return ranges
}

func isMutatableSource(rel string) bool {
	if !strings.HasPrefix(rel, "src/") || strings.HasPrefix(rel, "src/test-support/") {
		return false
	}
	switch path.Ext(rel) {
	case ".js", ".ts", ".vue":
	default:
		return false
	}
	base := path.Base(rel)
	for _, marker := range []string{".test.", ".spec."} {
		if strings.Contains(base, marker) {
			return false
		}
	}
	return true
}

func percent(covered, total int) float64 {
	if total == 0 {
		return 100
	}
	return float64(covered) * 100 / float64(total)
}

// gremlinsReport ist der Ausschnitt aus `gremlins unleash -o <datei>`, den
// der Gate braucht.
type gremlinsReport struct {
	Files []struct {
		FileName  string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
		} `json:"mutations"`
	} `json:"files"`
}

// ParseGremlinsReport liest den JSON-Report von gremlins und liefert die
// überlebenden (LIVED) und nicht abgedeckten (NOT COVERED) Mutanten sowie
// die Gesamtzahl. Den Report werten wir selbst aus, statt gremlins'
// Schwellwerte zu nutzen: v0.5.1 prüft "efficacy <= threshold" (100 % kann
// also nie bestehen) und wertet "keine Mutanten" als 0 %.
// TIMED OUT zählt wie bei StrykerJS als getötet, SKIPPED und NOT VIABLE
// zählen gar nicht.
func ParseGremlinsReport(r io.Reader, repoDir string) ([]Finding, int, error) {
	var rep gremlinsReport
	if err := json.NewDecoder(r).Decode(&rep); err != nil {
		return nil, 0, fmt.Errorf("gremlins-Report: %w", err)
	}
	var survivors []Finding
	total := 0
	for _, f := range rep.Files {
		for _, m := range f.Mutations {
			reason := ""
			switch m.Status {
			case "KILLED", "TIMED OUT":
				total++
				continue
			case "LIVED":
				reason = "Mutant überlebt"
			case "NOT COVERED":
				reason = "Mutant nicht abgedeckt"
			default: // SKIPPED (nicht im Diff), NOT VIABLE (kompiliert nicht)
				continue
			}
			total++
			survivors = append(survivors, Finding{
				File:   path.Join(repoDir, f.FileName),
				Line:   m.Line,
				Reason: fmt.Sprintf("%s (%s, Spalte %d)", reason, m.Type, m.Column),
			})
		}
	}
	sortFindings(survivors)
	return survivors, total, nil
}

// Round2 rundet eine (nicht negative) Quote auf zwei Nachkommastellen.
func Round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func sortFindings(findings []Finding) {
	slices.SortFunc(findings, func(a, b Finding) int {
		if c := cmp.Compare(a.File, b.File); c != 0 {
			return c
		}
		return cmp.Compare(a.Line, b.Line)
	})
}
