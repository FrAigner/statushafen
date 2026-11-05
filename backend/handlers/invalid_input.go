package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// codedError ist ein Prüffehler mit stabilem Fehlercode. Der Code
// gehört zur Antwort (`code`), das Frontend übersetzt ihn unter
// api.errors.<code>; msg bleibt als Rückfalltext für API-Aufrufer. Rohe
// Go-Fehlertexte (Validator, GORM, fmt.Errorf) gehören nie in eine Antwort.
type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }

func newCodedError(code, msg string) error { return &codedError{code: code, msg: msg} }

// respondCoded beantwortet einen Prüffehler. Ein codedError wird mit status,
// Text und Code ausgeliefert; jeder andere Fehler (z. B. ein Datenbankfehler)
// ergibt 500 server_error, ohne den Fehlertext preiszugeben.
func respondCoded(c *gin.Context, status int, err error) {
	var ce *codedError
	if errors.As(err, &ce) {
		c.JSON(status, gin.H{"error": ce.msg, "code": ce.code})
		return
	}
	slog.Error("Unerwarteter Fehler in einem Handler", "path", c.FullPath(), "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
}

// respondInvalidInput beantwortet ein fehlgeschlagenes ShouldBindJSON mit einer
// festen, verständlichen Meldung samt Fehlercode. Der rohe
// Validator-Text ("Key: 'X.Name' Error:Field validation for 'Name' failed on
// the 'required' tag") ist für Nutzer unlesbar und verrät Go-Feldnamen; das
// Frontend übersetzt den Code `invalid_request` (api.errors.invalid_request).
func respondInvalidInput(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Eingabe", "code": "invalid_request"})
}

// codeForStatus liefert für Antworten, deren Text tiefer im Code entsteht
// (Prüf- und Zugriffsfunktionen mit Status und Meldung), einen groben
// Fehlercode nach HTTP-Status. Die Meldung bleibt als Rückfalltext.
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
