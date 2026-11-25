package handlers

import (
	"os"
	"testing"
)

// TestMain switches the password login on for this package: most tests use
// the password login (or set passwords) as a fixture. The default (off) and
// its 403 answers are tested explicitly with t.Setenv, see
// password_login_test.go.
func TestMain(m *testing.M) {
	_ = os.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	os.Exit(m.Run())
}
