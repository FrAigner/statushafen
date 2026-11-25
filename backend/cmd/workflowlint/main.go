// Command workflowlint is the workflow gate: no self-hosted runner on
// pull_request, all actions pinned by SHA. Usage: workflowlint ../.github/workflows
package main

import (
	"os"

	"github.com/fraigner/statushafen/workflowlint"
)

// exit is replaceable, so that main() can run through in the test.
var exit = os.Exit

func main() {
	exit(workflowlint.Run(os.Args[1:], os.Stdout, os.Stderr))
}
