package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestGetEmail(t *testing.T) {
	t.Run("returns_email_when_set", func(t *testing.T) {
		c, _ := newTestContext()
		c.Set("email", "a@b.de")

		assert.Equal(t, "a@b.de", getEmail(c))
	})

	t.Run("returns_empty_string_when_not_set", func(t *testing.T) {
		c, _ := newTestContext()

		assert.Equal(t, "", getEmail(c))
	})

	t.Run("returns_empty_string_when_wrong_type", func(t *testing.T) {
		c, _ := newTestContext()
		c.Set("email", 123)

		assert.Equal(t, "", getEmail(c))
	})
}

func TestGetUserID(t *testing.T) {
	t.Run("returns_id_when_set", func(t *testing.T) {
		c, _ := newTestContext()
		c.Set("userID", uint(42))

		assert.Equal(t, uint(42), getUserID(c))
	})

	t.Run("returns_zero_when_not_set", func(t *testing.T) {
		c, _ := newTestContext()

		assert.Equal(t, uint(0), getUserID(c))
	})

	t.Run("returns_zero_when_wrong_type", func(t *testing.T) {
		c, _ := newTestContext()
		c.Set("userID", "not-a-uint")

		assert.Equal(t, uint(0), getUserID(c))
	})
}

func TestParseTenantID(t *testing.T) {
	t.Run("parses_valid_page_id", func(t *testing.T) {
		c, w := newTestContext()
		c.Params = gin.Params{{Key: "page_id", Value: "7"}}

		id, ok := parseTenantID(c)

		assert.True(t, ok)
		assert.Equal(t, uint(7), id)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns_400_when_page_id_missing", func(t *testing.T) {
		c, w := newTestContext()

		_, ok := parseTenantID(c)

		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns_400_when_page_id_not_numeric", func(t *testing.T) {
		c, w := newTestContext()
		c.Params = gin.Params{{Key: "page_id", Value: "not-a-number"}}

		_, ok := parseTenantID(c)

		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
