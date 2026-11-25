// Command coveragegate is the CI gate. It checks whether all lines changed in
// a PR are covered by tests, whether the overall rates
// do not fall below the baseline, and supplies the line ranges for the
// frontend mutation tests (StrykerJS --mutate).
//
// Usage (see CONTRIBUTING.md, section Coverage and mutation gate):
//
//	coveragegate check -diff pr.diff -go-profile coverage.out -lcov lcov.info -baseline coverage-baseline.json
//	coveragegate baseline-check -go-profile coverage.out -lcov lcov.info -baseline coverage-baseline.json -frontend-tolerance 1.5
//	coveragegate baseline -go-profile coverage.out -lcov lcov.info -out coverage-baseline.json
//	coveragegate mutate-ranges -diff pr.diff
//	coveragegate mutation-report -gremlins gremlins.json
//
// Exit codes: 0 = ok, 1 = gate red, 2 = usage or read error.
package main

import (
	"os"

	"github.com/fraigner/statushafen/coveragegate"
)

// exit is replaceable, so that main() can run through in the test.
var exit = os.Exit

func main() {
	exit(coveragegate.Run(os.Args[1:], os.Stdout, os.Stderr))
}
