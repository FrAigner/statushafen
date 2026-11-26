package coveragegate

// Overall-rate check without diff. Since the switch the diff gate (check) only runs locally;
// on push to develop/main CI additionally checks with baseline-check that the overall rates do not
// fall below coverage-baseline.json (the Go rate was unnoticed below the baseline).

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// runBaselineCheck compares the measured overall rates with the baseline. Unlike check, the
// mode requires ALL three inputs: in CI a missing report must never silently pass as "not measured",
// otherwise the step would be ineffective.
func runBaselineCheck(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("baseline-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var rf reportFlags
	rf.register(fs)
	baselinePath := fs.String("baseline", "", "coverage-baseline.json")
	frontendTolerance := fs.Float64("frontend-tolerance", 0, "erlaubte Unterschreitung der Frontend-Baseline in Prozentpunkten (Messschwankung)")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if rf.goProfile == "" || rf.lcov == "" || *baselinePath == "" {
		return 0, errors.New("baseline-check braucht -go-profile, -lcov und -baseline")
	}
	if *frontendTolerance < 0 {
		return 0, errors.New("-frontend-tolerance darf nicht negativ sein")
	}
	rep, err := rf.load()
	if err != nil {
		return 0, err
	}
	base, err := readBaseline(*baselinePath)
	if err != nil {
		return 0, err
	}
	current := rep.current()
	_, _ = fmt.Fprintf(stdout, "Go-Statements: %.2f %%\n", current.GoStatements)
	_, _ = fmt.Fprintf(stdout, "Frontend-Zeilen: %.2f %%, Frontend-Zweige: %.2f %%\n", current.FrontendLines, current.FrontendBranches)
	regressions := CompareBaseline(base, current, *frontendTolerance)
	if len(regressions) > 0 {
		_, _ = fmt.Fprintln(stdout, "❌ Gesamtquoten-Check rot.")
		for _, r := range regressions {
			_, _ = fmt.Fprintln(stdout, "Gesamtquote gesunken: "+r)
		}
		return 1, nil
	}
	_, _ = fmt.Fprintln(stdout, "✅ Gesamtquoten-Check bestanden: keine Quote unter der Baseline.")
	if len(CompareBaseline(current, base, 0)) > 0 {
		_, _ = fmt.Fprintln(stdout, "Hinweis: Baseline kann angehoben werden (coveragegate baseline …).")
	}
	return 0, nil
}
