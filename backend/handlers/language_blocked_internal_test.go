package handlers

import "testing"

// Die Ablehnung der Lösch-Transaktion nennt ihren Fehlercode als Text (fürs Log bei errors.As-losen Aufrufern).
func TestLanguageBlocked_ErrorNenntCode(t *testing.T) {
	var err error = &languageBlocked{code: "language_in_use"}
	if err.Error() != "language_in_use" {
		t.Fatalf("unerwartet: %q", err.Error())
	}
}
