package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// codedError is a validation error with a stable error code. The code
// belongs to the response (`code`), the frontend translates it under
// api.errors.<code>; msg remains as fallback text for API callers. Raw
// Go error texts (validator, GORM, fmt.Errorf) never belong in a response.
type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }

func newCodedError(code, msg string) error { return &codedError{code: code, msg: msg} }

// respondCoded answers a validation error. A codedError is returned with status,
// text and code; any other error (e.g. a database error)
// yields 500 server_error without revealing the error text.
func respondCoded(c *gin.Context, status int, err error) {
	var ce *codedError
	if errors.As(err, &ce) {
		c.JSON(status, gin.H{"error": ce.msg, "code": ce.code})
		return
	}
	slog.Error("Unerwarteter Fehler in einem Handler", "path", c.FullPath(), "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
}

// respondInvalidInput answers a failed ShouldBindJSON with a
// fixed, understandable message including an error code. The raw
// Validator-Text ("Key: 'X.Name' Error:Field validation for 'Name' failed on
// the 'required' tag") is unreadable for users and reveals Go field names; the
// frontend translates the code `invalid_request` (api.errors.invalid_request).
func respondInvalidInput(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Eingabe", "code": "invalid_request"})
}

// codeForStatus returns, for responses whose text originates deeper in the code
// (check and access functions with status and message), a coarse
// error code by HTTP status. The message remains as fallback text.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	}
	return "server_error"
}
