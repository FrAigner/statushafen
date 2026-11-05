// Command mocksamlidp startet den Mock-SAML-IdP für lokale Tests und den
// nächtlichen E2E-Lauf (Paket github.com/fraigner/statushafen/cmd/mocksamlidp). Nie
// produktiv einsetzen.
//
//	go run ./cmd/mocksamlidp   (MOCK_SAML_ADDR, MOCK_SAML_URL, MOCK_SAML_ACS_PREFIX)
package main

import (
	"os"

	"github.com/fraigner/statushafen/mocksamlidp"
)

// exit und run sind austauschbar, damit main() im Test durchlaufen kann.
var (
	exit = os.Exit
	run  = mocksamlidp.Run
)

func main() {
	exit(run(os.Getenv))
}
