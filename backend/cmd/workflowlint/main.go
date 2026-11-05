// Command workflowlint ist der Workflow-Gate: kein self-hosted Runner bei
// pull_request, alle Actions per SHA gepinnt. Aufruf: workflowlint ../.github/workflows
package main

import (
	"os"

	"github.com/fraigner/statushafen/workflowlint"
)

// exit ist austauschbar, damit main() im Test durchlaufen kann.
var exit = os.Exit

func main() {
	exit(workflowlint.Run(os.Args[1:], os.Stdout, os.Stderr))
}
