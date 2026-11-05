// Command coveragegate ist der CI-Gate. Er prüft, ob alle in
// einem PR geänderten Zeilen von Tests abgedeckt sind, ob die Gesamtquoten
// nicht unter die Baseline fallen, und liefert die Zeilenbereiche für die
// Frontend-Mutationstests (StrykerJS --mutate).
//
// Aufruf (siehe CONTRIBUTING.md, Abschnitt Coverage- und Mutations-Gate):
//
//	coveragegate check -diff pr.diff -go-profile coverage.out -lcov lcov.info -baseline coverage-baseline.json
//	coveragegate baseline-check -go-profile coverage.out -lcov lcov.info -baseline coverage-baseline.json -frontend-tolerance 1.5
//	coveragegate baseline -go-profile coverage.out -lcov lcov.info -out coverage-baseline.json
//	coveragegate mutate-ranges -diff pr.diff
//	coveragegate mutation-report -gremlins gremlins.json
//
// Exit-Codes: 0 = ok, 1 = Gate rot, 2 = Aufruf- oder Lesefehler.
package main

import (
	"os"

	"github.com/fraigner/statushafen/coveragegate"
)

// exit ist austauschbar, damit main() im Test durchlaufen kann.
var exit = os.Exit

func main() {
	exit(coveragegate.Run(os.Args[1:], os.Stdout, os.Stderr))
}
