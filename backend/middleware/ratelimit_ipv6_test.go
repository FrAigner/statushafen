package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func limitedRouter(burst int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.NewRateLimiter(burst, time.Hour))
	r.GET("/t", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func hitFrom(r *gin.Engine, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// Ein IPv6-Anschluss besitzt ein ganzes /64-Netz. Wechselt der Absender die
// Hostadresse darin, darf er nicht jedes Mal einen frischen Bucket bekommen.
func TestRateLimiter_IPv6HostsOfSame64ShareBucket(t *testing.T) {
	r := limitedRouter(3)
	var limited int
	for i := 1; i <= 50; i++ {
		if hitFrom(r, fmt.Sprintf("[2001:db8:1234:5678::%x]:4000", i)) == http.StatusTooManyRequests {
			limited++
		}
	}
	assert.Equal(t, 47, limited, "nur die ersten 3 Anfragen aus dem /64 dürfen durch")
}

func TestRateLimiter_Different64NetworksAreIndependent(t *testing.T) {
	r := limitedRouter(1)
	assert.Equal(t, http.StatusOK, hitFrom(r, "[2001:db8:1:1::1]:1"))
	assert.Equal(t, http.StatusTooManyRequests, hitFrom(r, "[2001:db8:1:1::2]:1"))
	// Gegenprobe: ein anderes /64 und eine IPv4-Adresse haben eigene Buckets.
	assert.Equal(t, http.StatusOK, hitFrom(r, "[2001:db8:1:2::1]:1"))
	assert.Equal(t, http.StatusOK, hitFrom(r, "192.0.2.1:1"))
	assert.Equal(t, http.StatusTooManyRequests, hitFrom(r, "192.0.2.1:2"))
}

func TestRateLimiter_IPv4MappedIPv6SharesBucketWithIPv4(t *testing.T) {
	r := limitedRouter(1)
	assert.Equal(t, http.StatusOK, hitFrom(r, "192.0.2.7:1"))
	assert.Equal(t, http.StatusTooManyRequests, hitFrom(r, "[::ffff:192.0.2.7]:1"))
}
