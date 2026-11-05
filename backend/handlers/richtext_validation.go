package handlers

import (
	"net/http"
	"unicode/utf8"

	"github.com/fraigner/statushafen/richtext"

	"github.com/gin-gonic/gin"
)

// requireMessageLength prüft die Länge einer formatierten Nachricht bzw.
// Beschreibung (höchstens richtext.MaxLen Zeichen) und beantwortet
// eine zu lange Eingabe mit 400 und dem Code message_too_long (Text de/en unter
// api.errors.message_too_long). Rückgabe false = Antwort ist bereits gesendet.
// Gezählt werden Zeichen des Markdown-Quelltexts, so wie sie gespeichert werden.
func requireMessageLength(c *gin.Context, message string) bool {
	if !richtext.TooLong(message) {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("message_too_long", "Die Nachricht ist zu lang (höchstens 5000 Zeichen)"))
	return false
}

// maxTitleLen ist die Obergrenze (in Zeichen) für Titel und Seitennamen. Eine
// Zeile in den Admin-Listen und im Betreff von Mails bleibt so lesbar; ohne
// Grenze wurden Titel bis zum Body-Limit gespeichert. Komponentennamen sind
// mit maxComponentNameLen enger gefasst.
const maxTitleLen = 200

// requireTitleLength prüft die Länge eines Titels in Zeichen (nicht Bytes) und
// beantwortet eine zu lange Eingabe mit 400 und dem Code title_too_long
// (Text de/en unter api.errors.title_too_long). Rückgabe false = Antwort ist
// bereits gesendet. Ein leerer Titel besteht die Prüfung; Pflichtfelder prüft
// der Aufrufer.
func requireTitleLength(c *gin.Context, title string) bool {
	if utf8.RuneCountInString(title) <= maxTitleLen {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("title_too_long", "Der Titel ist zu lang (höchstens 200 Zeichen)"))
	return false
}

// requirePageNameLength ist requireTitleLength für Seitennamen (Code
// page_name_too_long).
func requirePageNameLength(c *gin.Context, name string) bool {
	if utf8.RuneCountInString(name) <= maxTitleLen {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("page_name_too_long", "Der Seitenname ist zu lang (höchstens 200 Zeichen)"))
	return false
}
