package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// AddStatusPageUserHandler prüfte user_id/role_id nicht; ein Page
// Owner konnte beliebige Konten (auch fremder Mandanten) auf seine Seite setzen
// und danach per GET E-Mail, SSO-Kennung und Admin-Flag auslesen.
func idorRequest(t *testing.T, r http.Handler, method, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if payload != nil {
		_ = json.NewEncoder(&buf).Encode(payload)
	}
	req, _ := http.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestStatusPageUsers_Mandantentrennung(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	// Rechte wie in main.go: lesen = pages:read, hinzufügen = pages:create, entfernen = pages:delete.
	g := r.Group("/api/admin/pages/:page_id/users")
	g.Use(middleware.AuthMiddleware())
	g.GET("", middleware.RequirePermission("pages:read"), h.GetStatusPageUsersHandler)
	g.POST("", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	g.DELETE("/:uid", middleware.RequirePermission("pages:delete"), h.RemoveStatusPageUserHandler)

	page := models.StatusPage{Name: "A", Slug: "a"}
	db.Create(&page)
	base := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/users"

	ownerRole := models.Role{Name: "Page Owner", Permissions: "pages:create,pages:read,pages:update,pages:delete,incidents:create,incidents:read"}
	db.Create(&ownerRole)
	editor := models.Role{Name: "Editor", Permissions: "incidents:create,incidents:read"}
	db.Create(&editor)
	strong := models.Role{Name: "Strong", Permissions: "pages:create,pages:read,pages:update,pages:delete,incidents:create,incidents:read,roles:read"}
	db.Create(&strong)
	gaRole := models.Role{Name: models.GlobalAdminRoleName, Permissions: "pages:write"}
	db.Create(&gaRole)

	owner := models.User{Email: "owner@a.example"}
	db.Create(&owner)
	db.Create(&models.UserStatusPageRole{UserID: owner.ID, StatusPageID: page.ID, RoleID: ownerRole.ID})
	ownerTok, _ := middleware.GenerateToken(owner.ID, owner.Email, "", "", false)

	ga := models.User{Email: "ga@example.com", IsGlobalAdmin: true}
	db.Create(&ga)
	gaTok, _ := middleware.GenerateToken(ga.ID, ga.Email, "", "", true)

	foreign := models.User{Email: "geheim@fremd.example", SSOProvider: "okta", SSOSubject: "sub-123", IsGlobalAdmin: true}
	db.Create(&foreign)

	t.Run("Page Owner kann fremdes Konto nicht per user_id einschleusen und auslesen", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"user_id": foreign.ID, "role_id": ownerRole.ID})
		if w.Code == http.StatusCreated || w.Code == http.StatusOK {
			t.Fatalf("fremde user_id wurde angenommen: %d %s", w.Code, w.Body.String())
		}
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", foreign.ID).Count(&n)
		if n != 0 {
			t.Fatalf("fremdes Konto ist Mitglied geworden")
		}
		list := idorRequest(t, r, "GET", base, ownerTok, nil)
		for _, leak := range []string{"geheim@fremd.example", "sub-123", "okta"} {
			if strings.Contains(list.Body.String(), leak) {
				t.Errorf("Antwort enthält %q: %s", leak, list.Body.String())
			}
		}
	})

	t.Run("Unbekannte user_id/role_id erzeugen keine verwaisten Zeilen (Global Admin)", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, gaTok, map[string]any{"user_id": 99999, "role_id": editor.ID})
		requireStatusCode(t, w, http.StatusNotFound)
		w = idorRequest(t, r, "POST", base, gaTok, map[string]any{"user_id": foreign.ID, "role_id": 88888})
		requireStatusCode(t, w, http.StatusNotFound)
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? OR role_id = ?", 99999, 88888).Count(&n)
		if n != 0 {
			t.Fatalf("verwaiste Zeilen: %d", n)
		}
	})

	t.Run("Unbekannte Seite (Global Admin) -> 404", func(t *testing.T) {
		w := idorRequest(t, r, "POST", "/api/admin/pages/77777/users", gaTok, map[string]any{"user_id": foreign.ID, "role_id": editor.ID})
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Global-Admin-Rolle bleibt nicht vergebbar", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, gaTok, map[string]any{"user_id": foreign.ID, "role_id": gaRole.ID})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Global Admin kann weiterhin per user_id hinzufügen", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, gaTok, map[string]any{"user_id": foreign.ID, "role_id": editor.ID})
		requireStatusCode(t, w, http.StatusCreated)
		// Antwort der Liste ohne sensible Felder, auch für den Global Admin.
		list := idorRequest(t, r, "GET", base, gaTok, nil)
		requireStatusCode(t, list, http.StatusOK)
		for _, leak := range []string{"sub-123", "okta", "IsGlobalAdmin", "SSOSubject", "SSOProvider", "PasswordHash"} {
			if strings.Contains(list.Body.String(), leak) {
				t.Errorf("Antwort enthält %q: %s", leak, list.Body.String())
			}
		}
		if !strings.Contains(list.Body.String(), "geheim@fremd.example") {
			t.Errorf("Mitglieder-E-Mail fehlt für die Verwaltung: %s", list.Body.String())
		}
		idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(foreign.ID)), gaTok, nil)
	})

	t.Run("Page Owner lädt per E-Mail ein statt anzuhängen", func(t *testing.T) {
		// Konten mit SSO-Verknüpfung hängt nur ein Global Admin an.
		db.Model(&models.User{}).Where("id = ?", foreign.ID).Updates(map[string]any{"sso_provider": "", "sso_subject": "", "is_global_admin": false})
		w := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"email": " GEHEIM@fremd.example ", "role_id": editor.ID})
		requireStatusCode(t, w, http.StatusOK)
		var n, inv int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", foreign.ID, page.ID).Count(&n)
		db.Model(&models.PageInvitation{}).Where("email = ? AND status_page_id = ?", "geheim@fremd.example", page.ID).Count(&inv)
		if n != 0 || inv != 1 {
			t.Fatalf("erwartet Einladung statt Mitgliedschaft: Mitglied %d, Einladung %d", n, inv)
		}
		if strings.Contains(w.Body.String(), "sub-123") || strings.Contains(w.Body.String(), "fremd") {
			t.Errorf("Antwort verrät Kontodaten: %s", w.Body.String())
		}
		// Für die folgenden Schritte: Das Konto hat die Einladung angenommen.
		db.Create(&models.UserStatusPageRole{UserID: foreign.ID, StatusPageID: page.ID, RoleID: editor.ID})
	})

	t.Run("Unbekannte E-Mail antwortet wie bekannte (keine Enumeration)", func(t *testing.T) {
		known := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"email": "ga@example.com", "role_id": editor.ID})
		unknown := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"email": "gibtsnicht@nirgends.example", "role_id": editor.ID})
		if known.Code != unknown.Code || known.Body.String() != unknown.Body.String() {
			t.Errorf("unterscheidbar: %d %q vs %d %q", known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
		}
		var n int64
		db.Model(&models.User{}).Where("email = ?", "gibtsnicht@nirgends.example").Count(&n)
		if n != 0 {
			t.Fatal("Konto angelegt")
		}
	})

	t.Run("Page Owner ohne E-Mail für Nicht-Mitglied -> 400 (auch bei unbekannter ID gleich)", func(t *testing.T) {
		fresh := models.User{Email: "fresh@elsewhere.example"}
		db.Create(&fresh)
		a := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"user_id": fresh.ID, "role_id": editor.ID})
		b := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"user_id": 99999, "role_id": editor.ID})
		requireStatusCode(t, a, http.StatusBadRequest)
		if a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Errorf("unterscheidbar: %q vs %q", a.Body.String(), b.Body.String())
		}
	})

	t.Run("Page Owner darf bestehendes Mitglied per user_id umstufen", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"user_id": foreign.ID, "role_id": ownerRole.ID})
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Selbst-Hochstufung auf stärkere Rolle -> 403", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, ownerTok, map[string]any{"user_id": owner.ID, "role_id": strong.ID})
		requireStatusCode(t, w, http.StatusForbidden)
		var ur models.UserStatusPageRole
		db.Where("user_id = ? AND status_page_id = ?", owner.ID, page.ID).First(&ur)
		if ur.RoleID != ownerRole.ID {
			t.Fatalf("Rolle wurde geändert")
		}
		// Auch für andere: stärkere Rolle vergeben -> 403
		w = idorRequest(t, r, "POST", base, ownerTok, map[string]any{"email": "ga@example.com", "role_id": strong.ID})
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Global Admin darf stärkere Rolle vergeben", func(t *testing.T) {
		w := idorRequest(t, r, "POST", base, gaTok, map[string]any{"user_id": owner.ID, "role_id": strong.ID})
		requireStatusCode(t, w, http.StatusOK)
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", owner.ID, page.ID).Update("role_id", ownerRole.ID)
	})

	t.Run("Schwächere Rolle darf stärkeres Mitglied nicht ändern oder entfernen", func(t *testing.T) {
		weak := models.User{Email: "weak@a.example"}
		db.Create(&weak)
		weakRole := models.Role{Name: "Weak", Permissions: "pages:create,pages:read,pages:delete"}
		db.Create(&weakRole)
		db.Create(&models.UserStatusPageRole{UserID: weak.ID, StatusPageID: page.ID, RoleID: weakRole.ID})
		weakTok, _ := middleware.GenerateToken(weak.ID, weak.Email, "", "", false)
		w := idorRequest(t, r, "POST", base, weakTok, map[string]any{"user_id": owner.ID, "role_id": weakRole.ID})
		requireStatusCode(t, w, http.StatusForbidden)
		w = idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(owner.ID)), weakTok, nil)
		requireStatusCode(t, w, http.StatusForbidden)
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", owner.ID, page.ID).Count(&n)
		if n != 1 {
			t.Fatal("Owner wurde entfernt")
		}
	})

	t.Run("Ungültiger JSON-Body -> 400", func(t *testing.T) {
		req, _ := http.NewRequest("POST", base, strings.NewReader("{"))
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

// Review rollenbezogene Prüfungen laufen für Nicht-Global-
// Admins vor der Konto-Suche; die Antwort hängt nie von der E-Mail-Adresse ab.
func TestStatusPageUsers_KeineEnumerationUeberRollen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api/admin/pages/:page_id/users")
	g.Use(middleware.AuthMiddleware())
	g.GET("", middleware.RequirePermission("pages:read"), h.GetStatusPageUsersHandler)
	g.POST("", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	g.DELETE("/:uid", middleware.RequirePermission("pages:delete"), h.RemoveStatusPageUserHandler)

	page := models.StatusPage{Name: "A", Slug: "a"}
	db.Create(&page)
	base := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/users"

	pageOwner := models.Role{Name: "Page Owner", Permissions: database.PageOwnerPermissions}
	db.Create(&pageOwner)
	pageAdmin := models.Role{Name: "Page Admin", Permissions: permissions.MigrateLegacy("components:write,incidents:write,notices:write,subscribers:write,admin_dashboard")}
	db.Create(&pageAdmin)
	tooMuch := models.Role{Name: "Too Much", Permissions: database.PageOwnerPermissions + ",roles:read"}
	db.Create(&tooMuch)
	gaRole := models.Role{Name: models.GlobalAdminRoleName, Permissions: "pages:write"}
	db.Create(&gaRole)
	deleted := models.Role{Name: "Weg", Permissions: "incidents:read"}
	db.Create(&deleted)
	db.Delete(&deleted)

	owner := models.User{Email: "owner@a.example"}
	db.Create(&owner)
	db.Create(&models.UserStatusPageRole{UserID: owner.ID, StatusPageID: page.ID, RoleID: pageOwner.ID})
	tok, _ := middleware.GenerateToken(owner.ID, owner.Email, "", "", false)
	known := models.User{Email: "known@x.example"}
	db.Create(&known)

	same := func(t *testing.T, name string, roleID uint) {
		t.Helper()
		a := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": roleID})
		b := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "unknown@x.example", "role_id": roleID})
		if a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Errorf("%s: bekannt %d %s vs unbekannt %d %s", name, a.Code, a.Body.String(), b.Code, b.Body.String())
		}
	}
	same(t, "nicht abgedeckte Rolle", tooMuch.ID)
	same(t, "Global-Admin-Rolle", gaRole.ID)
	same(t, "unbekannte Rolle", 88888)
	same(t, "gelöschte Rolle", deleted.ID)

	// Rollen-IDs werden nicht verraten: Global-Admin-Rolle, unbekannte und nicht abgedeckte Rolle antworten gleich.
	r1 := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": gaRole.ID})
	r2 := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": 88888})
	r3 := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": tooMuch.ID})
	if r1.Code != http.StatusForbidden || r1.Body.String() != r2.Body.String() || r1.Body.String() != r3.Body.String() {
		t.Errorf("Rollen-ID-Orakel: %d %s | %d %s | %d %s", r1.Code, r1.Body.String(), r2.Code, r2.Body.String(), r3.Code, r3.Body.String())
	}

	t.Run("Page Owner kann Page Owner und Page Admin vergeben (admin_dashboard)", func(t *testing.T) {
		// Rollenwechsel per E-Mail gilt für Mitglieder (Einladung angenommen).
		db.Create(&models.UserStatusPageRole{UserID: known.ID, StatusPageID: page.ID, RoleID: pageAdmin.ID})
		for _, role := range []models.Role{pageOwner, pageAdmin} {
			w := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": role.ID})
			requireStatusCode(t, w, http.StatusOK)
			var ur models.UserStatusPageRole
			db.Where("user_id = ? AND status_page_id = ?", known.ID, page.ID).First(&ur)
			if ur.RoleID != role.ID {
				t.Errorf("Rolle %s nicht gesetzt", role.Name)
			}
		}
	})

	t.Run("Page Owner kann Mitglied mit Page-Owner-Rolle umstufen und entfernen", func(t *testing.T) {
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", known.ID).Update("role_id", pageOwner.ID)
		w := idorRequest(t, r, "POST", base, tok, map[string]any{"user_id": known.ID, "role_id": pageAdmin.ID})
		requireStatusCode(t, w, http.StatusOK)
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", known.ID).Update("role_id", pageOwner.ID)
		w = idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(known.ID)), tok, nil)
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Mitglied mit gelöschter Rolle: nur Global Admin darf ändern/entfernen", func(t *testing.T) {
		m := models.User{Email: "m@x.example"}
		db.Create(&m)
		db.Create(&models.UserStatusPageRole{UserID: m.ID, StatusPageID: page.ID, RoleID: deleted.ID})
		w := idorRequest(t, r, "POST", base, tok, map[string]any{"user_id": m.ID, "role_id": pageAdmin.ID})
		requireStatusCode(t, w, http.StatusForbidden)
		w = idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(m.ID)), tok, nil)
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Audit nennt Ziel-ID und Rolle, keine E-Mail", func(t *testing.T) {
		db.Where("1 = 1").Delete(&models.AuditLog{})
		db.Create(&models.UserStatusPageRole{UserID: known.ID, StatusPageID: page.ID, RoleID: pageOwner.ID})
		w := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "known@x.example", "role_id": pageAdmin.ID})
		requireStatusCode(t, w, http.StatusOK)
		var logs []models.AuditLog
		db.Where("action = ?", "statuspage.user_role_assigned").Find(&logs)
		if len(logs) != 1 || !strings.Contains(logs[0].Details, "user_id="+strconv.Itoa(int(known.ID))) ||
			!strings.Contains(logs[0].Details, "role_id="+strconv.Itoa(int(pageAdmin.ID))) || strings.Contains(logs[0].Details, "@") {
			t.Fatalf("Audit: %+v", logs)
		}
		idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(known.ID)), tok, nil)
		logs = nil
		db.Where("action = ?", "statuspage.user_role_removed").Find(&logs)
		if len(logs) != 1 || !strings.Contains(logs[0].Details, "user_id="+strconv.Itoa(int(known.ID))) {
			t.Fatalf("Audit remove: %+v", logs)
		}
	})
}

// Review E-Mail-Weg bei stärkerem Mitglied neutral; DB-Fehler beim
// Entfernen brechen ab (fail closed).
func TestStatusPageUsers_StaerkeresMitgliedUndDBFehler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api/admin/pages/:page_id/users")
	g.Use(middleware.AuthMiddleware())
	g.POST("", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	g.DELETE("/:uid", middleware.RequirePermission("pages:delete"), h.RemoveStatusPageUserHandler)

	page := models.StatusPage{Name: "A", Slug: "a"}
	db.Create(&page)
	base := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/users"
	weak := models.Role{Name: "Weak", Permissions: "pages:create,pages:read,pages:delete"}
	strong := models.Role{Name: "Strong", Permissions: "pages:create,pages:read,pages:delete,roles:read"}
	db.Create(&weak)
	db.Create(&strong)
	actor := models.User{Email: "actor@a.example"}
	db.Create(&actor)
	db.Create(&models.UserStatusPageRole{UserID: actor.ID, StatusPageID: page.ID, RoleID: weak.ID})
	tok, _ := middleware.GenerateToken(actor.ID, actor.Email, "", "", false)
	boss := models.User{Email: "boss@a.example"}
	db.Create(&boss)
	db.Create(&models.UserStatusPageRole{UserID: boss.ID, StatusPageID: page.ID, RoleID: strong.ID})

	known := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "boss@a.example", "role_id": weak.ID})
	unknown := idorRequest(t, r, "POST", base, tok, map[string]any{"email": "nobody@a.example", "role_id": weak.ID})
	if known.Code != http.StatusOK || known.Code != unknown.Code || known.Body.String() != unknown.Body.String() {
		t.Errorf("stärkeres Mitglied unterscheidbar: %d %s vs %d %s", known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
	var ur models.UserStatusPageRole
	db.Where("user_id = ?", boss.ID).First(&ur)
	if ur.RoleID != strong.ID {
		t.Error("Rolle des stärkeren Mitglieds wurde geändert")
	}

	t.Run("DB-Fehler bei der Rechteprüfung löscht nichts", func(t *testing.T) {
		victim := models.User{Email: "v@a.example"}
		db.Create(&victim)
		db.Create(&models.UserStatusPageRole{UserID: victim.ID, StatusPageID: page.ID, RoleID: strong.ID})
		_ = db.Callback().Query().Before("gorm:query").Register("fail791", func(tx *gorm.DB) {
			// Nur die Abfrage nach dem Zielmitglied (erstes Argument = uid) schlägt fehl.
			if tx.Statement.Table == "user_status_page_roles" && strings.Contains(fmt.Sprintf("%+v", tx.Statement.Clauses["WHERE"].Expression), "["+strconv.Itoa(int(victim.ID))+" ") {
				_ = tx.AddError(errors.New("boom"))
			}
		})
		defer func() { _ = db.Callback().Query().Remove("fail791") }()
		w := idorRequest(t, r, "DELETE", base+"/"+strconv.Itoa(int(victim.ID)), tok, nil)
		_ = db.Callback().Query().Remove("fail791")
		requireStatusCode(t, w, http.StatusInternalServerError)
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", victim.ID).Count(&n)
		if n != 1 {
			t.Fatal("Mitglied wurde trotz DB-Fehler gelöscht")
		}
	})
}
