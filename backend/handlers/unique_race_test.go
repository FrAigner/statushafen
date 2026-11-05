package handlers_test

import (
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Zwischen der Vorabprüfung (SELECT) und dem Insert kann eine
// zweite Anfrage denselben Namen/Slug anlegen; dann greift der Unique-Index.
// Der Handler antwortete mit 500 statt 409. Die Tests erzwingen diese Lücke
// deterministisch: ein GORM-Callback legt direkt vor dem ersten Schreibzugriff
// auf die Zieltabelle die kollidierende Zeile an (gleiche Transaktion).
func forceConflictOnce(t *testing.T, db *gorm.DB, table, insertSQL string) {
	t.Helper()
	var once sync.Once
	name := "test:race:" + table
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		once.Do(func() {
			// Direkt über die Verbindung der laufenden Transaktion, ein
			// verschachtelter GORM-Aufruf würde in SQLite blockieren.
			_, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context, insertSQL)
			require.NoError(t, err)
		})
	}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, hook))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
}

func setupUniqueRaceRouter(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	db, r := setupSoftDeleteReuseRouter(t)
	h := handlers.NewHandler(db)
	r.POST("/api/admin/roles", h.CreateRoleHandler)
	r.PUT("/api/admin/roles/:id", h.UpdateRoleHandler)
	r.POST("/api/admin/languages", h.CreateLanguageHandler)
	r.POST("/api/admin/users", h.CreateUserHandler)
	return db, r
}

func TestUniqueRace_CreateStatusPageSlug(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	forceConflictOnce(t, db, "status_pages", "INSERT INTO status_pages (name, slug, page_layout, created_at, updated_at) VALUES ('P', 'race', 'standard', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	w := doTagRequest(r, http.MethodPost, "/api/admin/pages", map[string]any{"Name": "Meine", "Slug": "race"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_taken")
}

func TestUniqueRace_UpdateStatusPageSlug(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	page := models.StatusPage{Name: "Alt", Slug: "alt"}
	require.NoError(t, db.Create(&page).Error)
	// Save läuft als UPDATE; der Konflikt entsteht durch einen parallel
	// angelegten Datensatz mit dem Ziel-Slug.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:race:upd", func(tx *gorm.DB) {
		if tx.Statement.Table == "status_pages" {
			_, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context,
				"INSERT INTO status_pages (name, slug, page_layout, created_at, updated_at) VALUES ('P', 'neu', 'standard', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
			require.NoError(t, err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:race:upd") })
	w := doTagRequest(r, http.MethodPut, "/api/admin/pages/"+strconv.FormatUint(uint64(page.ID), 10), map[string]any{"Name": "Alt", "Slug": "neu"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_taken")
}

func TestUniqueRace_CreateTagName(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	forceConflictOnce(t, db, "tags", "INSERT INTO tags (name, created_at, updated_at) VALUES ('Race', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	w := doTagRequest(r, http.MethodPost, "/api/tags", map[string]any{"name": "race"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "tag_name_taken")
}

func TestUniqueRace_UpdateTagName(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	tag := models.Tag{Name: "alt"}
	require.NoError(t, db.Create(&tag).Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:race:tagupd", func(tx *gorm.DB) {
		if tx.Statement.Table == "tags" {
			_, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context,
				"INSERT INTO tags (name, created_at, updated_at) VALUES ('neu', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
			require.NoError(t, err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:race:tagupd") })
	w := doTagRequest(r, http.MethodPut, "/api/tags/"+strconv.FormatUint(uint64(tag.ID), 10), map[string]any{"name": "neu"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "tag_name_taken")
}

func TestUniqueRace_CreateRoleName(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	forceConflictOnce(t, db, "roles", "INSERT INTO roles (name, permissions, created_at, updated_at) VALUES ('Race', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	w := doTagRequest(r, http.MethodPost, "/api/admin/roles", map[string]any{"name": "Race"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "role_name_taken")
}

// Ohne Vorabprüfung antwortete das Ändern auf einen vergebenen Namen schon
// sequentiell mit 500.
func TestUpdateRole_DuplicateNameReturns409(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	a, b := models.Role{Name: "A"}, models.Role{Name: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	w := doTagRequest(r, http.MethodPut, "/api/admin/roles/"+strconv.FormatUint(uint64(b.ID), 10), map[string]any{"name": "A"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "role_name_taken")
}

func TestUniqueRace_CreateLanguageCode(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	forceConflictOnce(t, db, "languages", "INSERT INTO languages (code, name, is_active, created_at, updated_at) VALUES ('xx', 'X', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	w := doTagRequest(r, http.MethodPost, "/api/admin/languages", map[string]any{"code": "xx", "name": "Xx"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

func TestUniqueRace_CreateUserEmail(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	forceConflictOnce(t, db, "users", "INSERT INTO users (email, created_at, updated_at) VALUES ('race@example.com', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	w := doTagRequest(r, http.MethodPost, "/api/admin/users", map[string]any{"email": "race@example.com", "password": "geheim-genug-123"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// Bestellung bestätigen: Slug bzw. E-Mail werden in der Transaktion geprüft,
// aber erst der Index fängt die Race zwischen Prüfung und Insert ab.
func TestUniqueRace_ConfirmOrder(t *testing.T) {
	cases := []struct {
		name, table, insertSQL, msg, slug string
	}{
		{"Slug", "status_pages",
			"INSERT INTO status_pages (name, slug, page_layout, created_at, updated_at) VALUES ('P', 'order-race', 'standard', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			"Slug wird bereits", "order-race"},
		{"E-Mail", "users",
			"INSERT INTO users (email, created_at, updated_at) VALUES ('order-race@example.com', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			"existiert bereits ein Konto", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := setupUniqueRaceRouter(t)
			r := setupOrderRouter(handlers.NewHandler(db))
			seedPageAdminRole(t, db)
			order := createPendingOrder(t, db, "Race GmbH", "order-race@example.com", tc.slug)
			forceConflictOnce(t, db, tc.table, tc.insertSQL)
			w := doTagRequest(r, http.MethodPost, "/api/public/orders/confirm", map[string]any{"token": plainToken(order.ConfirmTokenHash)})
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestUpdateUser_DuplicateEmailReturns409(t *testing.T) {
	db, r := setupUniqueRaceRouter(t)
	r.PUT("/api/admin/users/:id", handlers.NewHandler(db).UpdateUserHandler)
	a := models.User{Email: "a@example.com"}
	b := models.User{Email: "b@example.com"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	w := doTagRequest(r, http.MethodPut, "/api/admin/users/"+strconv.FormatUint(uint64(b.ID), 10), map[string]any{"email": "a@example.com"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "email_taken")
}
