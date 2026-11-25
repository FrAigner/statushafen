package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// codePasswordLoginDisabled answers every request that would sign in with a
// password or set/change a login password while PASSWORD_LOGIN_ENABLED is off.
const codePasswordLoginDisabled = "password_login_disabled"

// passwordLoginEnabled reports whether the password login is switched on:
// PASSWORD_LOGIN_ENABLED set to true, 1 or yes (any case). Everything else,
// including unset, means off; sign-in then works through the global OIDC
// providers only. The CLI (create-admin) is not affected.
func passwordLoginEnabled() bool {
	return envFlag("PASSWORD_LOGIN_ENABLED")
}

// envFlag reports whether an environment variable is set to true, 1 or yes (any case).
func envFlag(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// rejectDisabledPasswordLogin answers 403 password_login_disabled and reports
// true when the password login is off.
func rejectDisabledPasswordLogin(c *gin.Context) bool {
	if passwordLoginEnabled() {
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "Die Anmeldung mit Passwort ist abgeschaltet", "code": codePasswordLoginDisabled})
	return true
}
