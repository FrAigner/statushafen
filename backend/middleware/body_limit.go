package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit limits the size of every request body. Without a limit a
// handler (ShouldBindJSON, ParseForm) reads arbitrarily large bodies into memory.
//
// If the client reports a Content-Length above the limit, the
// middleware answers immediately with 413 and the code request_too_large, without any
// handler running. Without a known length (chunked) the body is capped with
// http.MaxBytesReader: the handler reads at most max bytes and
// then gets an *http.MaxBytesError.
//
// overrides maps route templates (c.FullPath()) to their own limit, for
// paths with legitimately larger bodies. Handlers with a narrower limit of their own
// (webhooks, incident type) are unaffected, because they additionally
// wrap their limit around the already capped body.
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
