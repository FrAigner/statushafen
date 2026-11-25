// Package coveragegate checks in CI whether every line changed in a PR
// is covered by tests (coverage and mutation gate).
//
// Design decision: the gate only evaluates the lines the PR
// adds or changes (git diff -U0 against the target branch). For
// existing code a baseline of overall rates applies instead, which may only
// increase. This way new code is immediately covered 100 %, without
// legacy debt blocking every PR.
//
// Go only knows statement coverage (blocks), no real branch coverage.
// An unexecuted if branch still stands out, because its block has
// counter 0. The "branch not taken" case without its own block (if without
// else) is covered by the mutation tests (gremlins, CONDITIONALS_NEGATION).
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

// maxDiffLine allows very long diff lines (e.g. minified files);
// bufio.Scanner otherwise aborts at 64 KiB.
const maxDiffLine = 16 << 20

// ChangedLines contains, per file (path relative to the repo root), the line
// numbers that the diff adds on the new side.
type ChangedLines map[string]map[int]bool

// Finding is a changed line without sufficient test coverage.
type Finding struct {
	File   string
	Line   int
	Reason string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Reason)
}

// ParseDiff reads the output of `git diff -U0` and collects the new
// lines. Deleted files (+++ /dev/null) do not appear.
func ParseDiff(r io.Reader) (ChangedLines, error) {
	changed := ChangedLines{}
	current := ""
	inFile := false
	// Remaining open lines of the current hunk body (old/new side). Only
	// outside the body is "+++ " a file header; an added
	// content line "++ x" also appears in the diff as "+++ x".
	remOld, remNew := 0, 0
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, maxDiffLine)
	for sc.Scan() {
		line := sc.Text()
		if remOld > 0 || remNew > 0 {
			if consumeHunkLine(line, &remOld, &remNew) {
				continue
			}
			// Unexpected line: hunk was shorter than announced.
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

// consumeHunkLine counts a body line off the open lines and
// reports whether it belonged to the body.
func consumeHunkLine(line string, remOld, remNew *int) bool {
	// Deliberately if + switch on the value instead of switch with conditions: gremlins
	// does not assign conditions in case expressions to any coverage block and
	// would otherwise report them as NOT COVERED.
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

// parseHunkHeader reads from "@@ -a,b +c,d @@ ..." the old line count b
// as well as start c and count d of the new side.
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

// parseRange reads "start[,count]"; without count it is one line.
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

// GoProfile is a parsed `go test -coverprofile` profile.
type GoProfile struct {
	blocks map[string]map[string]*goBlock // file -> block key -> block
}

// ParseGoProfile reads a cover profile. modulePath is the module name from
// go.mod, repoDir the directory of the module relative to the repo root. With
// -coverpkg=./... every test package writes the same blocks again; a
// block counts as covered as soon as any entry has executed it.
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

// parseGoProfileLine splits "file.go:10.20,12.3 2 1".
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

// lineOf reads the line from "line.column".
func lineOf(pos string) (int, error) {
	l, _, ok := strings.Cut(pos, ".")
	if !ok {
		return 0, fmt.Errorf("ungültige Position %q", pos)
	}
	return strconv.Atoi(l)
}

// Percent returns the statement coverage over all blocks.
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

// Uncovered reports changed lines touched by an unexecuted
// block. Lines outside any block (declarations, comments) and
// files without statements do not appear in the profile and do not count.
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
	branches map[int][]int // -1 stands for "-" (branch never reached)
}

// LCOV is a parsed lcov report (vitest/v8).
type LCOV struct {
	files map[string]*lcovFile
}

// ParseLCOV reads lcov.info. repoDir is the directory relative to which
// the SF paths are given (for vitest: frontend).
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

// LinePercent returns the line coverage over all files.
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

// BranchPercent returns the branch coverage over all files.
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

// Uncovered reports changed lines that were never executed or at which
// a branch was never taken. Files without an entry in the report (tests,
// configuration, JSON) do not count: vitest includes all source files
// under src/ via coverage.include, even untested ones.
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

// Baseline holds the overall rates below which the existing code must not
// fall (coverage-baseline.json in the repo root).
type Baseline struct {
	GoStatements     float64 `json:"go_statements"`
	FrontendLines    float64 `json:"frontend_lines"`
	FrontendBranches float64 `json:"frontend_branches"`
}

// CompareBaseline reports every rate that is below the baseline (rounded to
// two decimal places). frontendTolerance (percentage points) applies
// only to the frontend rates: v8 measures identical code with varying
// results depending on timing.
// Go is deterministic and is always compared exactly.
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

// MutateRanges builds the --mutate arguments for StrykerJS: contiguous
// changed lines per source file under <dir>/src, relative to dir.
// Tests, test helpers and non-code files are not mutated.
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

// gremlinsReport is the excerpt of `gremlins unleash -o <file>` that
// the gate needs.
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

// ParseGremlinsReport reads the JSON report from gremlins and returns the
// surviving (LIVED) and uncovered (NOT COVERED) mutants as well as
// the total count. We evaluate the report ourselves instead of using gremlins'
// thresholds: v0.5.1 checks "efficacy <= threshold" (so 100 % can
// never pass) and counts "no mutants" as 0 %.
// TIMED OUT counts as killed, as with StrykerJS; SKIPPED and NOT VIABLE
// do not count at all.
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
			default: // SKIPPED (not in the diff), NOT VIABLE (does not compile)
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

// Round2 rounds a (non-negative) rate to two decimal places.
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
