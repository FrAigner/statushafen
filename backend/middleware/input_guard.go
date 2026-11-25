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

// RejectInvalidCharacters rejects requests with a NUL byte or invalid
// UTF-8 before a handler runs. PostgreSQL rejects both on write
// (SQLSTATE 22021); without this check about 25 write routes returned a
// 500 instead of a 400. The check sits centrally because it would otherwise have to be retrofitted
// for every field of every handler individually.
//
// Checked are
//   - the body of every request, except form-encoded ones (SAML back channel): raw
//     NUL byte, invalid UTF-8 and the JSON notation \u0000, also in
//     keys and nested values. Gin binds JSON regardless of the
//     Content-Type, so the check also applies without a JSON header;
//   - the query parameters (keys and values) and the decoded path.
//
// The body is read once (BodyLimit sets the limit before this) and then offered
// to the handler again unchanged.
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

// isFormContent: form-encoded bodies (only the SAML back channel) carry Base64,
// not JSON, and are parsed by the handler itself.
func isFormContent(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/x-www-form-urlencoded" || strings.HasPrefix(mt, "multipart/"))
}

// validText: no NUL byte and valid UTF-8.
func validText(s string) bool {
	return utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}

// validRequestTarget checks the decoded path as well as keys and values
// of the query. Without a query and without percent-encoding in the path this costs almost nothing.
func validRequestTarget(u *url.URL) bool {
	if !validText(u.Path) {
		return false
	}
	if u.RawQuery == "" {
		return true
	}
	// ParseQuery skips malformed encodings, as does Gin; what is checked is
	// what a handler can actually read from it.
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

// validBodyText: valid UTF-8, no raw NUL byte and no JSON escape
// \u0000. The scan follows the escape sequences so that an escaped backslash
// (\\u0000, i.e. the text „\u0000“) is not wrongly counted.
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
		i += 2 // the backslash and the escaped character
	}
	return true
}

// isEscapedNUL: „\u“ is followed by the four hex digits 0000.
func isEscapedNUL(rest []byte) bool {
	return len(rest) >= 4 && string(rest[:4]) == "0000"
}
