package testutils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMustHash(t *testing.T) {
	hash := MustHash("kurz-genug")
	assert.True(t, database.CheckPasswordHash("kurz-genug", hash))
	assert.Panics(t, func() { MustHash(strings.Repeat("x", database.MaxPasswordBytes+1)) })
}

func TestSetupTestDB_MigratesAndSetsGlobalDB(t *testing.T) {
	db := SetupTestDB()
	assert.Same(t, db, database.DB)
	for _, m := range []any{&models.StatusPage{}, &models.User{}, &models.Subscriber{}, &models.PageInvitation{}} {
		assert.True(t, db.Migrator().HasTable(m), "Tabelle fehlt für %T", m)
	}

	// Jeder Aufruf liefert eine eigene, leere Datenbank.
	require.NoError(t, db.Create(&models.StatusPage{Name: "A", Slug: "a-setup"}).Error)
	other := SetupTestDB()
	var n int64
	require.NoError(t, other.Model(&models.StatusPage{}).Count(&n).Error)
	assert.Zero(t, n)
}

func TestSetupTestDB_JWTSecretDefaultOnlyWhenUnset(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	SetupTestDB()
	assert.NotEmpty(t, os.Getenv("JWT_SECRET"))

	t.Setenv("JWT_SECRET", "eigener-test-wert")
	SetupTestDB()
	assert.Equal(t, "eigener-test-wert", os.Getenv("JWT_SECRET"))
}

func TestSetupRouter_PutsDatabaseInContext(t *testing.T) {
	db := SetupTestDB()
	r := SetupRouter()
	var got any
	r.GET("/x", func(c *gin.Context) { got, _ = c.Get("db") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Same(t, db, got)
}
