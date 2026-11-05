package coveragegate

// CLI des Gates (Aufruf siehe cmd/coveragegate).

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = `Aufruf:
  coveragegate check -diff <datei> [-go-profile <datei>] [-lcov <datei>] [-baseline <datei>]
  coveragegate baseline-check -go-profile <datei> -lcov <datei> -baseline <datei> [-frontend-tolerance <pp>]
  coveragegate baseline -go-profile <datei> -lcov <datei> -out <datei>
  coveragegate mutate-ranges -diff <datei> [-dir frontend]
  coveragegate mutation-report -gremlins <datei> [-go-dir backend]
`

// Run führt einen Unterbefehl aus und liefert den Exit-Code. Die Logik steckt
// bewusst hier und nicht in cmd/coveragegate (package main): gremlins v0.5.1
// ordnet Mutanten in package main dem falschen Paket zu und führt dann die
// falschen Tests aus (siehe CONTRIBUTING.md).
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	code := 0
	switch args[0] {
	case "check":
		code, err = runCheck(args[1:], stdout, stderr)
	case "baseline-check":
		code, err = runBaselineCheck(args[1:], stdout, stderr)
	case "baseline":
		err = runBaseline(args[1:], stderr)
	case "mutate-ranges":
		err = runMutateRanges(args[1:], stdout, stderr)
	case "mutation-report":
		code, err = runMutationReport(args[1:], stdout, stderr)
	default:
		err = fmt.Errorf("unbekannter Unterbefehl %q\n%s", args[0], usage)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Fehler:", err)
		return 2
	}
	return code
}

// reportFlags sind die Report-Angaben, die check und baseline teilen.
type reportFlags struct {
	goProfile, goModule, goDir string
	lcov, lcovDir              string
}

func (r *reportFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&r.goProfile, "go-profile", "", "Go-Coverprofile (go test -coverpkg=./... -coverprofile)")
	fs.StringVar(&r.goModule, "go-module", "github.com/fraigner/statushafen", "Modulname aus go.mod")
	fs.StringVar(&r.goDir, "go-dir", "backend", "Verzeichnis des Go-Moduls relativ zur Repo-Wurzel")
	fs.StringVar(&r.lcov, "lcov", "", "lcov.info aus vitest --coverage")
	fs.StringVar(&r.lcovDir, "lcov-dir", "frontend", "Verzeichnis, relativ zu dem die lcov-Pfade stehen")
}

// reports enthält die eingelesenen Reports; nil heißt "nicht angegeben".
type reports struct {
	goProfile *GoProfile
	lcov      *LCOV
}

func (r *reportFlags) load() (reports, error) {
	var rep reports
	if r.goProfile != "" {
		f, err := os.Open(r.goProfile)
		if err != nil {
			return rep, err
		}
		defer func() { _ = f.Close() }()
		rep.goProfile, err = ParseGoProfile(f, r.goModule, r.goDir)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", r.goProfile, err)
		}
	}
	if r.lcov != "" {
		f, err := os.Open(r.lcov)
		if err != nil {
			return rep, err
		}
		defer func() { _ = f.Close() }()
		rep.lcov, err = ParseLCOV(f, r.lcovDir)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", r.lcov, err)
		}
	}
	return rep, nil
}

// current liefert die gemessenen Quoten.
func (rep reports) current() Baseline {
	var b Baseline
	if rep.goProfile != nil {
		b.GoStatements = rep.goProfile.Percent()
	}
	if rep.lcov != nil {
		b.FrontendLines = rep.lcov.LinePercent()
		b.FrontendBranches = rep.lcov.BranchPercent()
	}
	return b
}

func runCheck(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var rf reportFlags
	rf.register(fs)
	diffPath := fs.String("diff", "", "Ausgabe von git diff -U0 gegen den Ziel-Branch")
	baselinePath := fs.String("baseline", "", "coverage-baseline.json")
	frontendTolerance := fs.Float64("frontend-tolerance", 0, "erlaubte Unterschreitung der Frontend-Baseline in Prozentpunkten (Messschwankung)")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if *frontendTolerance < 0 {
		return 0, errors.New("-frontend-tolerance darf nicht negativ sein")
	}
	changed, err := readDiff(*diffPath)
	if err != nil {
		return 0, err
	}
	rep, err := rf.load()
	if err != nil {
		return 0, err
	}

	var findings []Finding
	if rep.goProfile != nil {
		findings = append(findings, rep.goProfile.Uncovered(changed)...)
	}
	if rep.lcov != nil {
		findings = append(findings, rep.lcov.Uncovered(changed)...)
	}

	current := rep.current()
	var regressions []string
	improved := false
	if *baselinePath != "" {
		base, err := readBaseline(*baselinePath)
		if err != nil {
			return 0, err
		}
		// Nicht gemessene Quoten nicht vergleichen (sonst zählte ein
		// fehlender Report als 0 %).
		if rep.goProfile == nil {
			base.GoStatements = 0
		}
		if rep.lcov == nil {
			base.FrontendLines, base.FrontendBranches = 0, 0
		}
		regressions = CompareBaseline(base, current, *frontendTolerance)
		improved = len(CompareBaseline(current, base, 0)) > 0
	}

	if rep.goProfile != nil {
		_, _ = fmt.Fprintf(stdout, "Go-Statements: %.2f %%\n", current.GoStatements)
	}
	if rep.lcov != nil {
		_, _ = fmt.Fprintf(stdout, "Frontend-Zeilen: %.2f %%, Frontend-Zweige: %.2f %%\n", current.FrontendLines, current.FrontendBranches)
	}
	if len(findings) == 0 && len(regressions) == 0 {
		_, _ = fmt.Fprintln(stdout, "✅ Coverage-Gate bestanden: alle geänderten Zeilen und Zweige sind getestet.")
		if improved {
			_, _ = fmt.Fprintln(stdout, "Hinweis: Baseline kann angehoben werden (coveragegate baseline …).")
		}
		return 0, nil
	}
	_, _ = fmt.Fprintln(stdout, "❌ Coverage-Gate rot.")
	if len(findings) > 0 {
		_, _ = fmt.Fprintln(stdout, "Geänderter Code ohne Test:")
		for _, f := range findings {
			_, _ = fmt.Fprintln(stdout, "  "+f.String())
		}
	}
	for _, r := range regressions {
		_, _ = fmt.Fprintln(stdout, "Gesamtquote gesunken: "+r)
	}
	return 1, nil
}

func runBaseline(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("baseline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var rf reportFlags
	rf.register(fs)
	out := fs.String("out", "", "Zieldatei, z. B. ../coverage-baseline.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if rf.goProfile == "" || rf.lcov == "" {
		return errors.New("baseline braucht -go-profile und -lcov")
	}
	if *out == "" {
		return errors.New("baseline braucht -out")
	}
	rep, err := rf.load()
	if err != nil {
		return err
	}
	cur := rep.current()
	data, _ := json.MarshalIndent(Baseline{
		GoStatements:     Round2(cur.GoStatements),
		FrontendLines:    Round2(cur.FrontendLines),
		FrontendBranches: Round2(cur.FrontendBranches),
	}, "", "  ")
	return os.WriteFile(*out, append(data, '\n'), 0o644) //nolint:gosec // Baseline ist eine eingecheckte, lesbare Datei
}

func runMutateRanges(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mutate-ranges", flag.ContinueOnError)
	fs.SetOutput(stderr)
	diffPath := fs.String("diff", "", "Ausgabe von git diff -U0 gegen den Ziel-Branch")
	dir := fs.String("dir", "frontend", "Frontend-Verzeichnis relativ zur Repo-Wurzel")
	if err := fs.Parse(args); err != nil {
		return err
	}
	changed, err := readDiff(*diffPath)
	if err != nil {
		return err
	}
	ranges := MutateRanges(changed, *dir)
	if len(ranges) > 0 {
		_, _ = fmt.Fprintln(stdout, strings.Join(ranges, ","))
	}
	return nil
}

func runMutationReport(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("mutation-report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reportPath := fs.String("gremlins", "", "JSON-Report von gremlins unleash -o")
	goDir := fs.String("go-dir", "backend", "Verzeichnis des Go-Moduls relativ zur Repo-Wurzel")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if *reportPath == "" {
		return 0, errors.New("-gremlins fehlt")
	}
	f, err := os.Open(*reportPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	survivors, total, err := ParseGremlinsReport(f, *goDir)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		_, _ = fmt.Fprintln(stdout, "✅ Mutationstest bestanden: keine Mutanten im geänderten Go-Code.")
		return 0, nil
	}
	if len(survivors) == 0 {
		_, _ = fmt.Fprintf(stdout, "✅ Mutationstest bestanden: alle %d Mutanten getötet.\n", total)
		return 0, nil
	}
	_, _ = fmt.Fprintf(stdout, "❌ Mutationstest rot: %d von %d Mutanten nicht getötet.\n", len(survivors), total)
	for _, s := range survivors {
		_, _ = fmt.Fprintln(stdout, "  "+s.String())
	}
	return 1, nil
}

func readDiff(p string) (ChangedLines, error) {
	if p == "" {
		return nil, errors.New("-diff fehlt")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return ParseDiff(f)
}

func readBaseline(p string) (Baseline, error) {
	var b Baseline
	data, err := os.ReadFile(p)
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("%s: %w", p, err)
	}
	return b, nil
}
