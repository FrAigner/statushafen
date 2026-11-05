package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Tests für DELETE /api/me (Konto löschen, DSGVO Art. 17).

const meDeletePassword = "richtiges-passwort"

type meDeleteEnv struct {
	db        *gorm.DB
	r         *gin.Engine
	adminRole models.Role
	viewRole  models.Role
}

func newMeDeleteEnv(t *testing.T) meDeleteEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.DELETE("/me", h.DeleteMeHandler)

	adminRole := models.Role{Name: "Page Admin", Permissions: "incidents:write, pages:write"}
	require.NoError(t, db.Create(&adminRole).Error)
	viewRole := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&viewRole).Error)
	return meDeleteEnv{db: db, r: r, adminRole: adminRole, viewRole: viewRole}
}

func (e meDeleteEnv) createUser(t *testing.T, email string, withPassword bool) models.User {
	t.Helper()
	u := models.User{Email: email}
	if withPassword {
		u.PasswordHash = testutils.MustHash(meDeletePassword)
	}
	require.NoError(t, e.db.Create(&u).Error)
	return u
}

func (e meDeleteEnv) createPage(t *testing.T, name string) models.StatusPage {
	t.Helper()
	p := models.StatusPage{Name: name, Slug: strings.ToLower(strings.ReplaceAll(name, " ", "-"))}
	require.NoError(t, e.db.Create(&p).Error)
	return p
}

func (e meDeleteEnv) assign(t *testing.T, u models.User, p models.StatusPage, role models.Role) {
	t.Helper()
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.ID, RoleID: role.ID}).Error)
}

func (e meDeleteEnv) deleteMe(t *testing.T, u models.User, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	token, err := middleware.GenerateToken(u.ID, u.Email, "", "", u.IsGlobalAdmin)
	require.NoError(t, err)
	var buf *bytes.Buffer
	switch b := body.(type) {
	case string:
		buf = bytes.NewBufferString(b)
	default:
		raw, _ := json.Marshal(b)
		buf = bytes.NewBuffer(raw)
	}
	req, _ := http.NewRequest("DELETE", "/api/me", buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e meDeleteEnv) userExists(u models.User) bool {
	var n int64
	e.db.Unscoped().Model(&models.User{}).Where("id = ?", u.ID).Count(&n)
	return n > 0
}

func pagesFromBody(t *testing.T, w *httptest.ResponseRecorder, key string) []map[string]interface{} {
	t.Helper()
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	raw, ok := parsed[key].([]interface{})
	require.True(t, ok, "Feld %q fehlt oder ist keine Liste: %s", key, w.Body.String())
	out := make([]map[string]interface{}, len(raw))
	for i, p := range raw {
		out[i] = p.(map[string]interface{})
	}
	return out
}

func codeFromBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	code, _ := parsed["code"].(string)
	return code
}

func TestDeleteMeHandler_OhneLogin401(t *testing.T) {
	e := newMeDeleteEnv(t)
	req, _ := http.NewRequest("DELETE", "/api/me", nil)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteMeHandler_UngueltigerBody400(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := e.createUser(t, "body@example.com", true)
	w := e.deleteMe(t, u, "{kein json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, e.userExists(u))
}

func TestDeleteMeHandler_UserZwischenzeitlichGeloescht401(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := e.createUser(t, "weg@example.com", false)
	e.db.Unscoped().Delete(&u)
	w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteMeHandler_Bestaetigung(t *testing.T) {
	cases := []struct {
		name         string
		withPassword bool
		body         map[string]interface{}
		want         int
	}{
		{"falsche E-Mail", false, map[string]interface{}{"confirm_email": "andere@example.com"}, http.StatusForbidden},
		{"fehlende E-Mail", true, map[string]interface{}{"password": meDeletePassword}, http.StatusForbidden},
		{"falsches Passwort", true, map[string]interface{}{"confirm_email": "confirm@example.com", "password": "falsch"}, http.StatusForbidden},
		{"fehlendes Passwort bei Passwort-Konto", true, map[string]interface{}{"confirm_email": "confirm@example.com"}, http.StatusForbidden},
		{"Passwort-Konto korrekt", true, map[string]interface{}{"confirm_email": "confirm@example.com", "password": meDeletePassword}, http.StatusOK},
		{"SSO-Konto ohne Passwort, E-Mail in anderer Schreibweise", false, map[string]interface{}{"confirm_email": "  Confirm@Example.com "}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newMeDeleteEnv(t)
			u := e.createUser(t, "confirm@example.com", tc.withPassword)
			w := e.deleteMe(t, u, tc.body)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.want == http.StatusForbidden {
				assert.Equal(t, "confirmation_invalid", codeFromBody(t, w))
				assert.True(t, e.userExists(u), "bei abgelehnter Bestätigung darf nichts gelöscht werden")
			} else {
				assert.False(t, e.userExists(u))
			}
		})
	}
}

func TestDeleteMeHandler_LetzterGlobalAdmin409(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := models.User{Email: "ga@example.com", IsGlobalAdmin: true}
	require.NoError(t, e.db.Create(&u).Error)
	// Ein soft-gelöschter zweiter Global Admin zählt nicht.
	gone := models.User{Email: "ga-weg@example.com", IsGlobalAdmin: true}
	require.NoError(t, e.db.Create(&gone).Error)
	require.NoError(t, e.db.Delete(&gone).Error)

	w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "last_global_admin", codeFromBody(t, w))
	assert.True(t, e.userExists(u))

	other := models.User{Email: "ga2@example.com", IsGlobalAdmin: true}
	require.NoError(t, e.db.Create(&other).Error)
	w = e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, e.userExists(u))
}

func TestDeleteMeHandler_AlleinigerSeitenAdmin(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := e.createUser(t, "solo@example.com", false)
	other := e.createUser(t, "other@example.com", false)
	softDeleted := e.createUser(t, "soft@example.com", false)

	solePage := e.createPage(t, "Solo Page")
	e.assign(t, u, solePage, e.adminRole)
	e.assign(t, other, solePage, e.viewRole) // Viewer zählt nicht als Admin

	sharedPage := e.createPage(t, "Shared Page")
	e.assign(t, u, sharedPage, e.adminRole)
	e.assign(t, other, sharedPage, e.adminRole)

	viewerPage := e.createPage(t, "Viewer Page")
	e.assign(t, u, viewerPage, e.viewRole)

	// Der zweite Admin wurde per Admin-Oberfläche soft-gelöscht, seine
	// Rollenzeile blieb liegen - er verwaltet die Seite faktisch nicht mehr.
	orphanPage := e.createPage(t, "Orphan Page")
	e.assign(t, u, orphanPage, e.adminRole)
	e.assign(t, softDeleted, orphanPage, e.adminRole)
	require.NoError(t, e.db.Delete(&softDeleted).Error)

	// Rollenzeile auf eine bereits gelöschte Seite: keine Rückfrage nötig.
	deletedPage := e.createPage(t, "Deleted Page")
	e.assign(t, u, deletedPage, e.adminRole)
	require.NoError(t, e.db.Delete(&deletedPage).Error)

	t.Run("ohne Bestätigung 409 mit Liste der Seiten", func(t *testing.T) {
		w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email})
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, "sole_admin_pages", codeFromBody(t, w))
		pages := pagesFromBody(t, w, "pages")
		require.Len(t, pages, 2)
		names := []string{pages[0]["name"].(string), pages[1]["name"].(string)}
		assert.ElementsMatch(t, []string{"Solo Page", "Orphan Page"}, names)
		for _, p := range pages {
			if p["name"] == "Solo Page" {
				assert.Equal(t, float64(solePage.ID), p["id"])
				assert.Equal(t, "solo-page", p["slug"])
			}
		}
		assert.True(t, e.userExists(u))
	})

	t.Run("unvollständige Bestätigung 409, nichts gelöscht", func(t *testing.T) {
		w := e.deleteMe(t, u, map[string]interface{}{
			"confirm_email":           u.Email,
			"confirm_delete_page_ids": []uint{solePage.ID},
		})
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.True(t, e.userExists(u))
		var n int64
		e.db.Model(&models.StatusPage{}).Where("id = ?", solePage.ID).Count(&n)
		assert.Equal(t, int64(1), n)
	})

	t.Run("mit Bestätigung werden genau die Seiten gelöscht", func(t *testing.T) {
		w := e.deleteMe(t, u, map[string]interface{}{
			"confirm_email": u.Email,
			// sharedPage ist nicht betroffen und wird trotz Nennung nicht gelöscht.
			"confirm_delete_page_ids": []uint{orphanPage.ID, solePage.ID, sharedPage.ID},
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		deleted := pagesFromBody(t, w, "deleted_pages")
		assert.Len(t, deleted, 2)
		assert.False(t, e.userExists(u))

		for _, p := range []models.StatusPage{solePage, orphanPage} {
			var n int64
			e.db.Model(&models.StatusPage{}).Where("id = ?", p.ID).Count(&n)
			assert.Equal(t, int64(0), n, "Seite %s muss gelöscht sein", p.Name)
		}
		for _, p := range []models.StatusPage{sharedPage, viewerPage} {
			var n int64
			e.db.Model(&models.StatusPage{}).Where("id = ?", p.ID).Count(&n)
			assert.Equal(t, int64(1), n, "Seite %s darf nicht gelöscht sein", p.Name)
		}

		var logs []models.AuditLog
		e.db.Where("action = ?", "statuspage.deleted").Find(&logs)
		require.Len(t, logs, 2)
		for _, l := range logs {
			assert.Nil(t, l.ActorID)
			assert.Equal(t, handlers.DeletedUserMarker, l.ActorEmail)
			assert.Contains(t, l.Details, "account_deleted")
		}
	})
}

func TestDeleteMeHandler_LoeschtUndAnonymisiert(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := e.createUser(t, "gdpr@example.com", true)
	other := e.createUser(t, "keep@example.com", false)
	page := e.createPage(t, "GDPR Page")
	e.assign(t, u, page, e.viewRole)
	e.assign(t, other, page, e.adminRole)

	tag := models.Tag{Name: "eu"}
	require.NoError(t, e.db.Create(&tag).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: u.ID, TagID: tag.ID}).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: other.ID, TagID: tag.ID}).Error)
	require.NoError(t, e.db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: page.ID, Issuer: "iss", Subject: "sub-u"}).Error)
	require.NoError(t, e.db.Create(&models.UserSSOIdentity{UserID: other.ID, StatusPageID: page.ID, Issuer: "iss", Subject: "sub-o"}).Error)
	require.NoError(t, e.db.Create(&models.OIDCLoginState{State: "s-u", Nonce: "n", CodeVerifier: "v", StatusPageID: page.ID, LinkUserID: u.ID}).Error)
	require.NoError(t, e.db.Create(&models.OIDCLoginState{State: "s-o", Nonce: "n", CodeVerifier: "v", StatusPageID: page.ID, LinkUserID: other.ID}).Error)
	// offene SAML-Verknüpfungen des Kontos verfallen mit.
	require.NoError(t, e.db.Create(&models.SAMLLoginState{State: "saml-u", RequestID: "r", StatusPageID: page.ID, LinkUserID: u.ID}).Error)
	require.NoError(t, e.db.Create(&models.SAMLLoginState{State: "saml-o", RequestID: "r", StatusPageID: page.ID, LinkUserID: other.ID}).Error)

	require.NoError(t, e.db.Create(&models.Subscriber{StatusPageID: page.ID, Email: u.Email, Channel: "email", CreatedByUserID: &u.ID}).Error)
	require.NoError(t, e.db.Create(&models.Subscriber{StatusPageID: page.ID, Email: other.Email, Channel: "email", CreatedByUserID: &other.ID}).Error)

	require.NoError(t, e.db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorID: &u.ID, ActorEmail: u.Email, ActorRole: "Viewer", IP: "10.0.0.1", TargetName: "INC-1", Details: `{"by":"gdpr@example.com"}`}).Error)
	require.NoError(t, e.db.Create(&models.AuditLog{Action: "user.created", Category: "user", ActorID: &other.ID, ActorEmail: other.Email, IP: "10.0.0.2", TargetType: "User", TargetID: strconv.FormatUint(uint64(u.ID), 10), TargetName: u.Email}).Error)
	require.NoError(t, e.db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorID: &other.ID, ActorEmail: other.Email, IP: "10.0.0.2", TargetName: "INC-2"}).Error)

	incident := models.Incident{Title: "Ausfall", Message: "m", StatusPageID: page.ID}
	require.NoError(t, e.db.Create(&incident).Error)
	require.NoError(t, e.db.Create(&models.IncidentUpdate{IncidentID: incident.ID, Status: "investigating", Message: "m", AuthorEmail: u.Email}).Error)
	require.NoError(t, e.db.Create(&models.IncidentUpdate{IncidentID: incident.ID, Status: "resolved", Message: "m", AuthorEmail: other.Email}).Error)
	pm := models.PostMortem{IncidentID: incident.ID}
	require.NoError(t, e.db.Create(&pm).Error)
	require.NoError(t, e.db.Create(&models.ActionItem{PostMortemID: pm.ID, Text: "fix", Owner: u.Email}).Error)
	require.NoError(t, e.db.Create(&models.ActionItem{PostMortemID: pm.ID, Text: "fix2", Owner: other.Email}).Error)

	require.NoError(t, e.db.Create(&models.PageOrder{CompanyName: "GDPR GmbH", Email: u.Email, UserID: &u.ID, StatusPageID: &page.ID, RequestIP: "10.0.0.1"}).Error)
	require.NoError(t, e.db.Create(&models.PageOrder{CompanyName: "Other GmbH", Email: other.Email, UserID: &other.ID, RequestIP: "10.0.0.2"}).Error)

	w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email, "password": meDeletePassword})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, pagesFromBody(t, w, "deleted_pages"))

	countUnscoped := func(model interface{}, where string, args ...interface{}) int64 {
		var n int64
		e.db.Unscoped().Model(model).Where(where, args...).Count(&n)
		return n
	}

	assert.False(t, e.userExists(u), "User muss hart gelöscht sein")
	assert.True(t, e.userExists(other))
	assert.Equal(t, int64(0), countUnscoped(&models.UserStatusPageRole{}, "user_id = ?", u.ID))
	assert.Equal(t, int64(1), countUnscoped(&models.UserStatusPageRole{}, "user_id = ?", other.ID))
	assert.Equal(t, int64(0), countUnscoped(&models.UserTag{}, "user_id = ?", u.ID))
	assert.Equal(t, int64(1), countUnscoped(&models.UserTag{}, "user_id = ?", other.ID))
	assert.Equal(t, int64(0), countUnscoped(&models.UserSSOIdentity{}, "user_id = ?", u.ID))
	assert.Equal(t, int64(1), countUnscoped(&models.UserSSOIdentity{}, "user_id = ?", other.ID))
	assert.Equal(t, int64(0), countUnscoped(&models.OIDCLoginState{}, "link_user_id = ?", u.ID))
	assert.Equal(t, int64(1), countUnscoped(&models.OIDCLoginState{}, "link_user_id = ?", other.ID))
	assert.Equal(t, int64(0), countUnscoped(&models.SAMLLoginState{}, "link_user_id = ?", u.ID))
	assert.Equal(t, int64(1), countUnscoped(&models.SAMLLoginState{}, "link_user_id = ?", other.ID))
	assert.Equal(t, int64(0), countUnscoped(&models.Subscriber{}, "email = ?", u.Email))
	assert.Equal(t, int64(1), countUnscoped(&models.Subscriber{}, "email = ?", other.Email))
	// Seite bleibt erhalten: other ist dort weiterhin Admin.
	assert.Equal(t, int64(1), countUnscoped(&models.StatusPage{}, "id = ? AND deleted_at IS NULL", page.ID))

	// Kein Datensatz außer dem neuen Löscheintrag darf die E-Mail noch enthalten.
	var logs []models.AuditLog
	e.db.Order("id").Find(&logs)
	require.Len(t, logs, 4)
	assert.Nil(t, logs[0].ActorID)
	assert.Equal(t, handlers.DeletedUserMarker, logs[0].ActorEmail)
	assert.Empty(t, logs[0].IP)
	assert.Empty(t, logs[0].ActorRole)
	assert.Equal(t, "INC-1", logs[0].TargetName, "Eintrag bleibt inhaltlich erhalten")
	assert.Equal(t, `{"by":"`+handlers.DeletedUserMarker+`"}`, logs[0].Details)
	assert.Equal(t, handlers.DeletedUserMarker, logs[1].TargetName)
	assert.Equal(t, other.Email, logs[1].ActorEmail, "fremder Akteur bleibt unverändert")
	assert.Equal(t, "10.0.0.2", logs[1].IP)
	assert.Equal(t, other.Email, logs[2].ActorEmail)
	assert.Equal(t, "user.account_deleted", logs[3].Action)
	assert.Nil(t, logs[3].ActorID)
	assert.Equal(t, handlers.DeletedUserMarker, logs[3].ActorEmail)
	assert.Equal(t, "User", logs[3].TargetType)
	assert.Equal(t, strconv.FormatUint(uint64(u.ID), 10), logs[3].TargetID)
	assert.Empty(t, logs[3].IP)
	for _, l := range logs {
		raw, _ := json.Marshal(l)
		assert.NotContains(t, string(raw), u.Email)
	}

	var updates []models.IncidentUpdate
	e.db.Order("id").Find(&updates)
	assert.Equal(t, handlers.DeletedUserMarker, updates[0].AuthorEmail)
	assert.Equal(t, other.Email, updates[1].AuthorEmail)

	var items []models.ActionItem
	e.db.Order("id").Find(&items)
	assert.Equal(t, handlers.DeletedUserMarker, items[0].Owner)
	assert.Equal(t, other.Email, items[1].Owner)

	var orders []models.PageOrder
	e.db.Order("id").Find(&orders)
	assert.Nil(t, orders[0].UserID)
	assert.Equal(t, handlers.DeletedUserMarker, orders[0].Email)
	assert.Empty(t, orders[0].RequestIP)
	assert.Equal(t, "GDPR GmbH", orders[0].CompanyName)
	assert.Equal(t, other.Email, orders[1].Email)
	assert.Equal(t, "10.0.0.2", orders[1].RequestIP)

	// Die Adresse ist wieder frei (harte Löschung, Unique-Index blockiert nicht).
	assert.NoError(t, e.db.Create(&models.User{Email: u.Email}).Error)
}

// failOnTable registriert für die Dauer des Tests einen GORM-Callback, der
// Abfragen (kind "query") bzw. Löschungen (kind "delete") auf der Tabelle
// table mit einem simulierten Fehler abbricht. after gibt an, wie viele
// Treffer vorher noch durchgelassen werden.
func failOnTable(t *testing.T, db *gorm.DB, kind, table string, after int) {
	t.Helper()
	name := "test:me_delete_fail_" + kind + "_" + table
	seen := 0
	fn := func(tx *gorm.DB) {
		if tx.Statement.Table == table || (tx.Statement.Schema != nil && tx.Statement.Schema.Table == table) {
			seen++
			if seen > after {
				_ = tx.AddError(errors.New("simulierter DB-Fehler"))
			}
		}
	}
	if kind == "query" {
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, fn))
		t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
		return
	}
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(name, fn))
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(name) })
}

func TestDeleteMeHandler_FehlerBeimErmittelnDerSeiten500(t *testing.T) {
	for _, after := range []int{0, 1} {
		t.Run("Abfrage "+strconv.Itoa(after+1), func(t *testing.T) {
			e := newMeDeleteEnv(t)
			u := e.createUser(t, "err@example.com", false)
			page := e.createPage(t, "Err Page")
			e.assign(t, u, page, e.adminRole)
			failOnTable(t, e.db, "query", "user_status_page_roles", after)

			w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email})
			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.True(t, e.userExists(u))
		})
	}
}

func TestDeleteMeHandler_TransaktionsfehlerRolltZurueck(t *testing.T) {
	e := newMeDeleteEnv(t)
	u := e.createUser(t, "tx@example.com", false)
	page := e.createPage(t, "Tx Page")
	e.assign(t, u, page, e.adminRole)
	require.NoError(t, e.db.Create(&models.Subscriber{StatusPageID: page.ID, Email: u.Email, Channel: "email", CreatedByUserID: &u.ID}).Error)
	require.NoError(t, e.db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorID: &u.ID, ActorEmail: u.Email}).Error)
	// Der letzte Schritt (User löschen) scheitert: alles davor muss zurückgerollt werden.
	failOnTable(t, e.db, "delete", "users", 0)

	w := e.deleteMe(t, u, map[string]interface{}{"confirm_email": u.Email, "confirm_delete_page_ids": []uint{page.ID}})
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	assert.True(t, e.userExists(u))
	var n int64
	e.db.Model(&models.Subscriber{}).Where("created_by_user_id = ?", u.ID).Count(&n)
	assert.Equal(t, int64(1), n)
	e.db.Model(&models.StatusPage{}).Where("id = ?", page.ID).Count(&n)
	assert.Equal(t, int64(1), n)
	var log models.AuditLog
	require.NoError(t, e.db.First(&log).Error)
	assert.Equal(t, u.Email, log.ActorEmail)
	e.db.Model(&models.AuditLog{}).Count(&n)
	assert.Equal(t, int64(1), n, "keine neuen Audit-Einträge nach Rollback")
}
