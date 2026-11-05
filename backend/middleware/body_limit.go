package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit begrenzt die Größe jedes Request-Bodys. Ohne Grenze liest ein
// Handler (ShouldBindJSON, ParseForm) beliebig große Bodys in den Speicher.
//
// Meldet der Client eine Content-Length über der Grenze, antwortet die
// Middleware sofort mit 413 und dem Code request_too_large, ohne dass ein
// Handler läuft. Ohne bekannte Länge (chunked) wird der Body mit
// http.MaxBytesReader gedeckelt: Der Handler liest höchstens max Byte und
// bekommt dann einen *http.MaxBytesError.
//
// overrides ordnet Routen-Templates (c.FullPath()) eine eigene Grenze zu, für
// Pfade mit legitim größeren Bodys. Handler mit engerer eigener Grenze
// (Webhooks, Vorfall-Art) bleiben davon unberührt, weil sie ihre Grenze
// zusätzlich um den bereits gedeckelten Body legen.
func BodyLimit(max int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := max
		if v, ok := overrides[c.FullPath()]; ok {
			limit = v
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Anfrage zu groß", "code": "request_too_large"})
			return
		}
		if c.Request.Body != nil && c.Request.Body != http.NoBody {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
