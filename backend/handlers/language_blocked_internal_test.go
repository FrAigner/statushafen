package handlers

import "testing"

// The rejection of the delete transaction names its error code as text (for the log, for callers without errors.As).
func TestLanguageBlocked_ErrorNenntCode(t *testing.T) {
	var err error = &languageBlocked{code: "language_in_use"}
	if err.Error() != "language_in_use" {
		t.Fatalf("unerwartet: %q", err.Error())
	}
}
