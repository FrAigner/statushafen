package middleware

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// RejectInvalidCharacters weist Anfragen mit einem NUL-Byte oder ungültigem
// UTF-8 ab, bevor ein Handler läuft. PostgreSQL lehnt beides beim Schreiben ab
// (SQLSTATE 22021); ohne diese Prüfung wurde daraus an rund 25 Schreibrouten ein
// 500 statt eines 400. Die Prüfung sitzt zentral, weil sie sonst für jedes Feld
// jedes Handlers einzeln nachzurüsten wäre.
//
// Geprüft werden
//   - der Body jeder Anfrage, außer formularkodierten (SAML-Rückkanal): rohes
//     NUL-Byte, ungültiges UTF-8 und die JSON-Schreibweise \u0000, auch in
//     Schlüsseln und verschachtelten Werten. Gin bindet JSON unabhängig vom
//     Content-Type, deshalb gilt die Prüfung auch ohne JSON-Kopfzeile;
//   - die Query-Parameter (Schlüssel und Werte) und der entschlüsselte Pfad.
//
// Der Body wird einmal gelesen (die Grenze setzt BodyLimit davor) und dem
// Handler danach unverändert wieder angeboten.
func RejectInvalidCharacters() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !validRequestTarget(c.Request.URL) {
			abortInvalidCharacters(c)
			return
		}
		if c.Request.Body == nil || c.Request.Body == http.NoBody || isFormContent(c.GetHeader("Content-Type")) {
			c.Next()
			return
		}
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Anfrage zu groß", "code": "request_too_large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Ungültige Eingabe", "code": "invalid_request"})
			return
		}
		if !validBodyText(raw) {
			abortInvalidCharacters(c)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		c.Next()
	}
}

func abortInvalidCharacters(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Eine Eingabe enthält unzulässige Zeichen.", "code": "input_invalid_characters"})
}

// isFormContent: formularkodierte Bodys (nur der SAML-Rückkanal) tragen Base64,
// kein JSON, und werden vom Handler selbst geparst.
func isFormContent(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/x-www-form-urlencoded" || strings.HasPrefix(mt, "multipart/"))
}

// validText: kein NUL-Byte und gültiges UTF-8.
func validText(s string) bool {
	return utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}

// validRequestTarget prüft den entschlüsselten Pfad sowie Schlüssel und Werte
// der Query. Ohne Query und ohne Prozentkodierung im Pfad kostet das fast nichts.
func validRequestTarget(u *url.URL) bool {
	if !validText(u.Path) {
		return false
	}
	if u.RawQuery == "" {
		return true
	}
	// Fehlerhafte Kodierungen überspringt ParseQuery wie auch Gin; geprüft wird,
	// was ein Handler daraus tatsächlich lesen kann.
	q, _ := url.ParseQuery(u.RawQuery)
	for k, vs := range q {
		if !validText(k) {
			return false
		}
		for _, v := range vs {
			if !validText(v) {
				return false
			}
		}
	}
	return true
}

// validBodyText: gültiges UTF-8, kein rohes NUL-Byte und kein JSON-Escape
// \u0000. Der Scan folgt den Escape-Folgen, damit ein maskierter Backslash
// (\\u0000, also der Text „\u0000“) nicht fälschlich zählt.
func validBodyText(b []byte) bool {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	for i := 0; i < len(b); {
		j := bytes.IndexByte(b[i:], '\\')
		if j < 0 {
			return true
		}
		i += j
		if i+1 < len(b) && b[i+1] == 'u' && isEscapedNUL(b[i+2:]) {
			return false
		}
		i += 2 // der Backslash und das maskierte Zeichen
	}
	return true
}

// isEscapedNUL: Auf „\u“ folgen die vier Hexziffern 0000.
func isEscapedNUL(rest []byte) bool {
	return len(rest) >= 4 && string(rest[:4]) == "0000"
}
