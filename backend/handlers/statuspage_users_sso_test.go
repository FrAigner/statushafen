package handlers_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Ein Page Owner darf per E-Mail kein Konto an seine Seite hängen,
// das schon eine Rolle auf einer anderen Seite oder eine SSO-Verknüpfung hat
// (das Opfer verlöre sonst die SSO-Anmeldung der eigenen Seite).
func TestStatusPageUsers_SSOKonten(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // keine echte Mail (anhängbare Konten bekommen eine Einladung)
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	t.Cleanup(h.WaitMails) // Einladungsmails laufen im Hintergrund
	r := testutils.SetupRouter()
	g := r.Group("/api/admin/pages/:page_id/users")
	g.Use(middleware.AuthMiddleware())
	g.POST("", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	g.DELETE("/:uid", middleware.RequirePermission("pages:delete"), h.RemoveStatusPageUserHandler)

	pageA := models.StatusPage{Name: "A", Slug: "a"}
	pageB := models.StatusPage{Name: "B", Slug: "b"}
	db.Create(&pageA)
	db.Create(&pageB)
	baseA := "/api/admin/pages/" + strconv.Itoa(int(pageA.ID)) + "/users"
	role := models.Role{Name: "Owner", Permissions: "pages:create,pages:read,pages:delete"}
	db.Create(&role)
	owner := models.User{Email: "owner@a.example"}
	db.Create(&owner)
	db.Create(&models.UserStatusPageRole{UserID: owner.ID, StatusPageID: pageA.ID, RoleID: role.ID})
	tok, _ := middleware.GenerateToken(owner.ID, owner.Email, "", "", false)
	ga := models.User{Email: "ga@x.example", IsGlobalAdmin: true}
	db.Create(&ga)
	gaTok, _ := middleware.GenerateToken(ga.ID, ga.Email, "", "", true)

	otherPage := models.User{Email: "opfer-b@x.example"}
	db.Create(&otherPage)
	db.Create(&models.UserStatusPageRole{UserID: otherPage.ID, StatusPageID: pageB.ID, RoleID: role.ID})
	sso := models.User{Email: "sso@x.example", SSOProvider: "okta", SSOSubject: "s1"}
	db.Create(&sso)
	plain := models.User{Email: "plain@x.example"}
	db.Create(&plain)

	count := func(uid uint) int64 {
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", uid, pageA.ID).Count(&n)
		return n
	}
	// Statt anzuhängen entsteht eine Einladung; eine Mail bekommt nur
	// ein anhängbares Konto (Versuch scheitert hier an SMTP_PORT=1 und wird auditiert).
	invited := func(email string) bool {
		var n int64
		db.Model(&models.PageInvitation{}).Where("email = ? AND status_page_id = ?", email, pageA.ID).Count(&n)
		return n == 1
	}
	mailFailures := func() int64 {
		h.WaitMails() // der Versand läuft im Hintergrund
		return auditCountFor(db, "statuspage.invitation_email_failed")
	}

	unknown := idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": "niemand@x.example", "role_id": role.ID})
	for name, u := range map[string]models.User{"Rolle auf anderer Seite": otherPage, "SSO-Verknüpfung": sso} {
		w := idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": u.Email, "role_id": role.ID})
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() {
			t.Errorf("%s: unterscheidbar von unbekannt: %d %s vs %d %s", name, w.Code, w.Body.String(), unknown.Code, unknown.Body.String())
		}
		if count(u.ID) != 0 {
			t.Errorf("%s: Konto wurde angehängt", name)
		}
	}

	t.Run("Seiten-SSO-Verknüpfung auf anderer Seite, Global-Admin-Ziel und DB-Fehler sperren neutral", func(t *testing.T) {
		pageSSO := models.User{Email: "pagesso@x.example"}
		onlyA := models.User{Email: "onlya@x.example"}
		gaTarget := models.User{Email: "gatarget@x.example", IsGlobalAdmin: true}
		failing := models.User{Email: "failing@x.example"}
		for _, u := range []*models.User{&pageSSO, &onlyA, &gaTarget, &failing} {
			db.Create(u)
		}
		db.Create(&models.UserSSOIdentity{UserID: pageSSO.ID, StatusPageID: pageB.ID, Protocol: "oidc", Issuer: "i", Subject: "s-b"})
		db.Create(&models.UserSSOIdentity{UserID: onlyA.ID, StatusPageID: pageA.ID, Protocol: "oidc", Issuer: "i", Subject: "s-a"})
		for name, u := range map[string]models.User{"Seiten-SSO auf B": pageSSO, "Global-Admin-Ziel": gaTarget} {
			w := idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": u.Email, "role_id": role.ID})
			if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() {
				t.Errorf("%s: unterscheidbar: %d %s", name, w.Code, w.Body.String())
			}
			if count(u.ID) != 0 {
				t.Errorf("%s: Konto wurde angehängt", name)
			}
		}
		before := mailFailures()
		idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": onlyA.Email, "role_id": role.ID})
		if count(onlyA.ID) != 0 || !invited(onlyA.Email) || mailFailures() != before+1 {
			t.Error("Verknüpfung nur mit Seite A darf nicht sperren (Einladung mit Mailversuch erwartet)")
		}
		// DB-Fehler bei der Prüfung "Rolle auf anderer Seite": nie fail-open.
		_ = db.Callback().Query().Before("gorm:query").Register("fail803b", func(tx *gorm.DB) {
			if tx.Statement.Table == "user_status_page_roles" && strings.Contains(fmt.Sprintf("%+v", tx.Statement.Clauses["WHERE"].Expression), "<>") {
				_ = tx.AddError(errors.New("boom"))
			}
		})
		w := idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": failing.Email, "role_id": role.ID})
		_ = db.Callback().Query().Remove("fail803b")
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() || count(failing.ID) != 0 {
			t.Errorf("DB-Fehler: fail-open: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("DB-Fehler bei der Prüfung der Seiten-SSO-Verknüpfung sperrt (fail closed)", func(t *testing.T) {
		u := models.User{Email: "failsso@x.example"}
		db.Create(&u)
		_ = db.Callback().Query().Before("gorm:query").Register("fail803c", func(tx *gorm.DB) {
			if tx.Statement.Table == "user_sso_identities" {
				_ = tx.AddError(errors.New("boom"))
			}
		})
		w := idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": u.Email, "role_id": role.ID})
		_ = db.Callback().Query().Remove("fail803c")
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() || count(u.ID) != 0 {
			t.Errorf("fail-open: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("Einfaches Konto ohne Rolle/SSO wird eingeladen statt angehängt", func(t *testing.T) {
		before := mailFailures()
		idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": plain.Email, "role_id": role.ID})
		if count(plain.ID) != 0 || !invited(plain.Email) || mailFailures() != before+1 {
			t.Fatal("Einladung mit Mailversuch erwartet, keine Mitgliedschaft")
		}
	})

	t.Run("Umstufen eines Mitglieds mit SSO-Verknüpfung per E-Mail bleibt möglich", func(t *testing.T) {
		db.Create(&models.UserStatusPageRole{UserID: sso.ID, StatusPageID: pageA.ID, RoleID: role.ID})
		low := models.Role{Name: "Low", Permissions: "pages:read"}
		db.Create(&low)
		idorRequest(t, r, "POST", baseA, tok, map[string]any{"email": sso.Email, "role_id": low.ID})
		var ur models.UserStatusPageRole
		db.Where("user_id = ? AND status_page_id = ?", sso.ID, pageA.ID).First(&ur)
		if ur.RoleID != low.ID {
			t.Fatal("Umstufen blockiert")
		}
		db.Where("user_id = ?", sso.ID).Delete(&models.UserStatusPageRole{})
	})

	t.Run("Global Admin darf beliebige Konten hinzufügen", func(t *testing.T) {
		for _, u := range []models.User{otherPage, sso} {
			w := idorRequest(t, r, "POST", baseA, gaTok, map[string]any{"email": u.Email, "role_id": role.ID})
			requireStatusCode(t, w, http.StatusCreated)
			if count(u.ID) != 1 {
				t.Errorf("%s nicht hinzugefügt", u.Email)
			}
		}
	})

	t.Run("Versuch mit unbekannter Adresse wird ohne E-Mail im Audit vermerkt", func(t *testing.T) {
		// als statuspage.invitation_created (gleich für jede Adresse).
		var logs []models.AuditLog
		db.Where("action = ?", "statuspage.invitation_created").Find(&logs)
		if len(logs) == 0 {
			t.Fatal("kein Audit-Eintrag")
		}
		for _, l := range logs {
			if strings.Contains(l.Details+l.TargetName, "@") {
				t.Errorf("Audit enthält Daten: %+v", l)
			}
		}
	})

	t.Run("Leerer Body (ohne role_id) -> 400 statt 403", func(t *testing.T) {
		w := idorRequest(t, r, "POST", baseA, tok, map[string]any{})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Fehler beim Löschen -> 500, kein removed-Audit", func(t *testing.T) {
		victim := models.User{Email: "v@x.example"}
		db.Create(&victim)
		db.Create(&models.UserStatusPageRole{UserID: victim.ID, StatusPageID: pageA.ID, RoleID: role.ID})
		_ = db.Callback().Delete().Before("gorm:delete").Register("fail803", func(tx *gorm.DB) {
			if tx.Statement.Table == "user_status_page_roles" {
				_ = tx.AddError(errors.New("boom"))
			}
		})
		defer func() { _ = db.Callback().Delete().Remove("fail803") }()
		w := idorRequest(t, r, "DELETE", baseA+"/"+strconv.Itoa(int(victim.ID)), tok, nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_removed").Count(&n)
		if n != 0 {
			t.Fatalf("removed-Audit trotz Fehler: %d", n)
		}
	})
}
