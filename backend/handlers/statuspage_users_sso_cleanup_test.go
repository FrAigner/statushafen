package handlers_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Seitenbenutzer-Verwaltung (SSO-Verknüpfung beim Entfernen,
// Eindeutigkeit der Seitenrolle, letzter Owner).
type pageUsers struct {
	db       *gorm.DB
	h        *handlers.Handler
	r        http.Handler
	pageA    models.StatusPage
	pageB    models.StatusPage
	ownerRol models.Role
	viewer   models.Role
}

func setupPageUsers(t *testing.T) *pageUsers {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api/admin/pages/:page_id/users")
	g.Use(middleware.AuthMiddleware())
	g.POST("", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	g.DELETE("/:uid", middleware.RequirePermission("pages:delete"), h.RemoveStatusPageUserHandler)
	r.DELETE("/api/roles/:id", h.DeleteRoleHandler)
	r.PUT("/api/users/:id", h.UpdateUserHandler)
	r.POST("/api/users", h.CreateUserHandler)

	// Einladungsmails laufen im Hintergrund; vor dem Schließen der DB abwarten.
	t.Cleanup(h.WaitMails)
	p := &pageUsers{db: db, h: h, r: r,
		pageA:    models.StatusPage{Name: "A", Slug: "a"},
		pageB:    models.StatusPage{Name: "B", Slug: "b"},
		ownerRol: models.Role{Name: "Owner", Permissions: "pages:create,pages:read,pages:update,pages:delete"},
		viewer:   models.Role{Name: "Viewer", Permissions: "pages:read"},
	}
	db.Create(&p.pageA)
	db.Create(&p.pageB)
	db.Create(&p.ownerRol)
	db.Create(&p.viewer)
	return p
}

func (p *pageUsers) user(email string, ga bool) (models.User, string) {
	u := models.User{Email: email, IsGlobalAdmin: ga}
	p.db.Create(&u)
	tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", ga)
	return u, tok
}

func (p *pageUsers) member(u models.User, page models.StatusPage, role models.Role) {
	p.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID})
}

func (p *pageUsers) identity(u models.User, page models.StatusPage, subject string) {
	p.db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: page.ID, Protocol: "oidc", Issuer: "iss", Subject: subject})
}

func (p *pageUsers) identities(u models.User, page models.StatusPage) int64 {
	var n int64
	p.db.Model(&models.UserSSOIdentity{}).Where("user_id = ? AND status_page_id = ?", u.ID, page.ID).Count(&n)
	return n
}

func (p *pageUsers) base(page models.StatusPage) string {
	return "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/users"
}

func (p *pageUsers) activeRoles(u models.User, page models.StatusPage) int64 {
	var n int64
	p.db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", u.ID, page.ID).Count(&n)
	return n
}

func TestRemoveStatusPageUser_DeletesSSOIdentity(t *testing.T) {
	// "anhängbar" heißt jetzt: Einladung mit Mail. Der Versand
	// scheitert hier absichtlich (Port 1) und wird auditiert; das zählt der Test.
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1")
	p := setupPageUsers(t)
	pageC := models.StatusPage{Name: "C", Slug: "c"}
	p.db.Create(&pageC)
	ownerB, tokB := p.user("ownerb@x.example", false)
	p.member(ownerB, p.pageB, p.ownerRol)
	ownerC, tokC := p.user("ownerc@x.example", false)
	p.member(ownerC, pageC, p.ownerRol)
	victim, _ := p.user("victim@x.example", false)
	p.member(victim, p.pageB, p.viewer)
	p.identity(victim, p.pageB, "sub-b")
	p.identity(victim, p.pageA, "sub-a") // andere Seite: bleibt

	// Vorher: Seite C kann das Konto nicht anhängen (Rolle/Identität auf B).
	idorRequest(t, p.r, "POST", p.base(pageC), tokC, map[string]any{"email": victim.Email, "role_id": p.viewer.ID})
	if p.activeRoles(victim, pageC) != 0 || auditCountFor(p.db, "statuspage.invitation_email_failed") != 0 {
		t.Fatal("Vorbedingung: Konto darf nicht angehängt (und nicht angeschrieben) werden")
	}

	w := idorRequest(t, p.r, "DELETE", p.base(p.pageB)+"/"+strconv.Itoa(int(victim.ID)), tokB, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Entfernen: %d %s", w.Code, w.Body.String())
	}
	if p.identities(victim, p.pageB) != 0 {
		t.Error("Seiten-SSO-Verknüpfung auf B blieb verwaist stehen")
	}
	if p.identities(victim, p.pageA) != 1 {
		t.Error("Verknüpfung auf anderer Seite darf nicht angefasst werden")
	}
	// A bleibt als Fremdidentität bestehen; für den Anhänge-Test A entfernen,
	// damit nur die Wirkung von B geprüft wird.
	p.db.Where("status_page_id = ?", p.pageA.ID).Delete(&models.UserSSOIdentity{})
	// Die erste (stille) Einladung ist älter als 30 Minuten, sonst bliebe sie unverändert.
	p.db.Model(&models.PageInvitation{}).Where("1 = 1").Update("created_at", time.Now().Add(-time.Hour))
	idorRequest(t, p.r, "POST", p.base(pageC), tokC, map[string]any{"email": victim.Email, "role_id": p.viewer.ID})
	p.h.WaitMails() // der fehlgeschlagene Versand läuft im Hintergrund
	if p.activeRoles(victim, pageC) != 0 || auditCountFor(p.db, "statuspage.invitation_email_failed") != 1 {
		t.Error("nach dem Entfernen muss das Konto an Seite C einladbar sein (Mailversuch)")
	}
}

func TestDeleteRoleAndReplaceRoles_DeleteSSOIdentity(t *testing.T) {
	p := setupPageUsers(t)
	a, _ := p.user("a@x.example", false)
	p.member(a, p.pageA, p.viewer)
	p.member(a, p.pageB, p.ownerRol)
	p.identity(a, p.pageA, "a-A")
	p.identity(a, p.pageB, "a-B")

	// Rolle ownerRol löschen -> Mitgliedschaft B endet, Identität B weg, A bleibt.
	w := idorRequest(t, p.r, "DELETE", "/api/roles/"+strconv.Itoa(int(p.ownerRol.ID)), "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Rolle löschen: %d %s", w.Code, w.Body.String())
	}
	if p.identities(a, p.pageB) != 0 || p.identities(a, p.pageA) != 1 {
		t.Errorf("Rolle löschen: Identität B=%d (soll 0), A=%d (soll 1)", p.identities(a, p.pageB), p.identities(a, p.pageA))
	}

	// Konto bearbeiten: Seite A aus den Rollen entfernen -> Identität A weg.
	b, _ := p.user("b@x.example", false)
	p.member(b, p.pageA, p.viewer)
	p.member(b, p.pageB, p.viewer)
	p.identity(b, p.pageA, "b-A")
	p.identity(b, p.pageB, "b-B")
	w = idorRequest(t, p.r, "PUT", "/api/users/"+strconv.Itoa(int(b.ID)), "", map[string]any{
		"email": b.Email,
		"page_roles": []map[string]any{
			{"status_page_id": p.pageB.ID, "role_id": p.viewer.ID},
			{"status_page_id": p.pageB.ID, "role_id": p.viewer.ID}, // Doppelter Eintrag: kein 500
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("Konto bearbeiten: %d %s", w.Code, w.Body.String())
	}
	if p.identities(b, p.pageA) != 0 || p.identities(b, p.pageB) != 1 {
		t.Errorf("Konto bearbeiten: Identität A=%d (soll 0), B=%d (soll 1)", p.identities(b, p.pageA), p.identities(b, p.pageB))
	}
	if p.activeRoles(b, p.pageB) != 1 {
		t.Error("doppelter page_roles-Eintrag muss zu genau einer Zeile werden")
	}

	// Anlegen mit doppelten Seiten: ebenfalls kein 500.
	w = idorRequest(t, p.r, "POST", "/api/users", "", map[string]any{
		"email": "neu@x.example", "password": "secret123-pass",
		"page_roles": []map[string]any{
			{"status_page_id": p.pageA.ID, "role_id": p.viewer.ID},
			{"status_page_id": p.pageA.ID, "role_id": p.viewer.ID},
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("Konto anlegen: %d %s", w.Code, w.Body.String())
	}
}

// Gleichzeitiges Hinzufügen -> Unique-Index schlägt zu, Antwort 200
// mit aktualisierter Rolle statt 500.
func TestAddStatusPageUser_ConcurrentInsertBecomesUpdate(t *testing.T) {
	p := setupPageUsers(t)
	ga, gaTok := p.user("ga@x.example", true)
	_ = ga
	target, _ := p.user("target@x.example", false)

	cbName := "test:race800"
	armed := true
	if err := p.db.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "user_status_page_roles" {
			return
		}
		armed = false
		// Der "andere" Aufruf war schneller: Zeile mit Viewer-Rolle existiert.
		p.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec(
			"INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?)",
			target.ID, p.pageA.ID, p.viewer.ID)
	}); err != nil {
		t.Fatal(err)
	}

	w := idorRequest(t, p.r, "POST", p.base(p.pageA), gaTok, map[string]any{"user_id": target.ID, "role_id": p.ownerRol.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("erwartet 200 (idempotent), war %d %s", w.Code, w.Body.String())
	}
	var rows []models.UserStatusPageRole
	p.db.Where("user_id = ? AND status_page_id = ?", target.ID, p.pageA.ID).Find(&rows)
	if len(rows) != 1 || rows[0].RoleID != p.ownerRol.ID {
		t.Fatalf("genau eine Zeile mit angeforderter Rolle erwartet, war %+v", rows)
	}
}

// Ein zweiter Insert derselben (Konto, Seite) muss am Index scheitern.
func TestUserStatusPageRole_UniqueIndexOnTestDB(t *testing.T) {
	p := setupPageUsers(t)
	u, _ := p.user("u@x.example", false)
	p.member(u, p.pageA, p.viewer)
	if err := p.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.pageA.ID, RoleID: p.ownerRol.ID}).Error; err == nil {
		t.Fatal("Doppelzeile hätte am Unique-Index scheitern müssen")
	}
}

func TestPageUsers_LastOwnerCannotLockOutSelf(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	self := strconv.Itoa(int(owner.ID))

	// Entfernen als letzter Owner: 409.
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+self, tok, nil)
	if w.Code != http.StatusConflict || !contains(w.Body.String(), "last_page_owner") {
		t.Fatalf("Entfernen: erwartet 409 last_page_owner, war %d %s", w.Code, w.Body.String())
	}
	// Herabstufen: 409.
	w = idorRequest(t, p.r, "POST", p.base(p.pageA), tok, map[string]any{"user_id": owner.ID, "role_id": p.viewer.ID})
	if w.Code != http.StatusConflict || !contains(w.Body.String(), "last_page_owner") {
		t.Fatalf("Herabstufen: erwartet 409 last_page_owner, war %d %s", w.Code, w.Body.String())
	}
	if p.activeRoles(owner, p.pageA) != 1 {
		t.Fatal("Rolle darf nicht verändert worden sein")
	}
	// Rolle mit gleichem Recht zu ändern bleibt erlaubt.
	other := models.Role{Name: "Owner2", Permissions: p.ownerRol.Permissions}
	p.db.Create(&other)
	w = idorRequest(t, p.r, "POST", p.base(p.pageA), tok, map[string]any{"user_id": owner.ID, "role_id": other.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("Wechsel auf gleichwertige Rolle: %d %s", w.Code, w.Body.String())
	}

	// Mit zweitem Owner ist Selbst-Entfernen erlaubt.
	second, _ := p.user("second@x.example", false)
	p.member(second, p.pageA, p.ownerRol)
	w = idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+self, tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Entfernen mit zweitem Owner: %d %s", w.Code, w.Body.String())
	}
}

func TestPageUsers_GlobalAdminMayRemoveLastOwner(t *testing.T) {
	p := setupPageUsers(t)
	owner, _ := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	_, gaTok := p.user("ga@x.example", true)
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(owner.ID)), gaTok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Global Admin darf reparieren: %d %s", w.Code, w.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// failTable lässt jede Operation `op` (create|query|delete|update) auf `table` scheitern.
func failTable(t *testing.T, db *gorm.DB, op, table string) {
	t.Helper()
	name := "test:fail807:" + op + table
	cb := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(gorm.ErrInvalidDB)
		}
	}
	var err error
	switch op {
	case "query":
		err = db.Callback().Query().Before("gorm:query").Register(name, cb)
		t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, cb)
		t.Cleanup(func() { _ = db.Callback().Delete().Remove(name) })
	default:
		err = db.Callback().Create().Before("gorm:create").Register(name, cb)
		t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestPageUsers_DBErrorsReturn500(t *testing.T) {
	for _, tc := range []struct{ op, table string }{
		{"delete", "user_sso_identities"},
		{"delete", "user_status_page_roles"},
		{"query", "user_status_page_roles"},
	} {
		t.Run(tc.op+" "+tc.table, func(t *testing.T) {
			p := setupPageUsers(t)
			u, _ := p.user("u@x.example", false)
			p.member(u, p.pageA, p.ownerRol)
			p.identity(u, p.pageA, "s")
			failTable(t, p.db, tc.op, tc.table)
			w := idorRequest(t, p.r, "DELETE", "/api/roles/"+strconv.Itoa(int(p.ownerRol.ID)), "", nil)
			if w.Code != http.StatusInternalServerError {
				t.Errorf("Rolle löschen: erwartet 500, war %d %s", w.Code, w.Body.String())
			}
			w = idorRequest(t, p.r, "PUT", "/api/users/"+strconv.Itoa(int(u.ID)), "", map[string]any{"email": u.Email, "page_roles": []map[string]any{}})
			if w.Code != http.StatusInternalServerError {
				t.Errorf("Konto bearbeiten: erwartet 500, war %d %s", w.Code, w.Body.String())
			}
			if tc.table == "user_sso_identities" && p.identities(u, p.pageA) != 1 {
				t.Error("Transaktion muss zurückgerollt sein")
			}
		})
	}
}

func TestRemoveStatusPageUser_SSODeleteErrorRollsBack(t *testing.T) {
	p := setupPageUsers(t)
	_, gaTok := p.user("ga@x.example", true)
	u, _ := p.user("u@x.example", false)
	p.member(u, p.pageA, p.viewer)
	p.identity(u, p.pageA, "s")
	failTable(t, p.db, "delete", "user_sso_identities")
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(u.ID)), gaTok, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("erwartet 500, war %d", w.Code)
	}
	if p.activeRoles(u, p.pageA) != 1 {
		t.Error("Rolle muss bei Fehler der Identitätslöschung erhalten bleiben")
	}
}

func TestAddStatusPageUser_RaceVariants(t *testing.T) {
	race := func(p *pageUsers, target models.User, roleID uint) {
		armed := true
		_ = p.db.Callback().Create().Before("gorm:create").Register("test:race800b", func(tx *gorm.DB) {
			if !armed || tx.Statement.Table != "user_status_page_roles" {
				return
			}
			armed = false
			p.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec(
				"INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?)",
				target.ID, p.pageA.ID, roleID)
		})
		t.Cleanup(func() { _ = p.db.Callback().Create().Remove("test:race800b") })
	}

	t.Run("Owner per E-Mail legt keine Zeile an, auch nicht im Wettlauf", func(t *testing.T) {
		p := setupPageUsers(t)
		owner, tok := p.user("owner@x.example", false)
		p.member(owner, p.pageA, p.ownerRol)
		target, _ := p.user("t@x.example", false)
		creates := 0
		_ = p.db.Callback().Create().Before("gorm:create").Register("test:count799", func(tx *gorm.DB) {
			if tx.Statement.Table == "user_status_page_roles" {
				creates++
			}
		})
		t.Cleanup(func() { _ = p.db.Callback().Create().Remove("test:count799") })
		t.Setenv("SMTP_PORT", "1")
		w := idorRequest(t, p.r, "POST", p.base(p.pageA), tok, map[string]any{"email": target.Email, "role_id": p.viewer.ID})
		if w.Code != http.StatusOK || creates != 0 || p.activeRoles(target, p.pageA) != 0 {
			t.Fatalf("Mitgliedschaft ohne Zustimmung: %d %s, Anlegeversuche %d", w.Code, w.Body.String(), creates)
		}
	})

	t.Run("Global Admin per user_id, gleichzeitig angelegt: Rolle wird gesetzt", func(t *testing.T) {
		p := setupPageUsers(t)
		_, gaTok := p.user("ga@x.example", true)
		target, _ := p.user("t@x.example", false)
		race(p, target, p.viewer.ID)
		w := idorRequest(t, p.r, "POST", p.base(p.pageA), gaTok, map[string]any{"user_id": target.ID, "role_id": p.ownerRol.ID})
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var ur models.UserStatusPageRole
		p.db.Where("user_id = ?", target.ID).First(&ur)
		if p.activeRoles(target, p.pageA) != 1 || ur.RoleID != p.ownerRol.ID {
			t.Errorf("genau eine Zeile mit der neuen Rolle erwartet: %+v", ur)
		}
	})

	t.Run("Zeile nach Unique-Fehler nicht lesbar: 500", func(t *testing.T) {
		p := setupPageUsers(t)
		_, gaTok := p.user("ga@x.example", true)
		target, _ := p.user("t@x.example", false)
		_ = p.db.Callback().Create().Before("gorm:create").Register("test:dupnoRow", func(tx *gorm.DB) {
			if tx.Statement.Table == "user_status_page_roles" {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}
		})
		t.Cleanup(func() { _ = p.db.Callback().Create().Remove("test:dupnoRow") })
		w := idorRequest(t, p.r, "POST", p.base(p.pageA), gaTok, map[string]any{"user_id": target.ID, "role_id": p.viewer.ID})
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("erwartet 500, war %d", w.Code)
		}
	})
}

func TestPageUsers_OwnerLookupError(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	// Vierte Abfrage auf user_status_page_roles (nach Rechteprüfung, Mitglied und eigenen Rechten) = Owner-Suche.
	n := 0
	_ = p.db.Callback().Query().Before("gorm:query").Register("test:ownerlookup", func(tx *gorm.DB) {
		if tx.Statement.Table != "user_status_page_roles" {
			return
		}
		n++
		if n == 4 {
			_ = tx.AddError(gorm.ErrInvalidDB)
		}
	})
	t.Cleanup(func() { _ = p.db.Callback().Query().Remove("test:ownerlookup") })
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(owner.ID)), tok, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("bei DB-Fehler fail closed (500), war %d %s", w.Code, w.Body.String())
	}
	if p.activeRoles(owner, p.pageA) != 1 {
		t.Fatal("Mitglied darf bei DB-Fehler nicht entfernt werden")
	}
}

// Wer pages:update nie hatte, verliert durch Selbst-Entfernen keinen Owner.
func TestPageUsers_NonOwnerMaySelfRemove(t *testing.T) {
	p := setupPageUsers(t)
	role := models.Role{Name: "Nur Benutzer", Permissions: "pages:create,pages:read,pages:delete"}
	p.db.Create(&role)
	u, tok := p.user("u@x.example", false)
	p.member(u, p.pageA, role)
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(u.ID)), tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// Ein soft-gelöschtes Konto zählt nicht als weiterer Owner (wie soleAdminPages).
func TestPageUsers_DeletedAccountIsNoOtherOwner(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	ghost, _ := p.user("ghost@x.example", false)
	p.member(ghost, p.pageA, p.ownerRol)
	p.db.Delete(&ghost)
	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(owner.ID)), tok, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("gelöschtes Konto darf nicht zählen: %d %s", w.Code, w.Body.String())
	}
}

// Auto-Join: parallele SPA-Anfragen -> eine verliert gegen den Unique-Index und
// muss trotzdem Erfolg melden (Mitgliedschaft existiert).
func TestAutoJoin_ConcurrentInsertIsSuccess(t *testing.T) {
	p := setupPageUsers(t)
	h := handlers.NewHandler(p.db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)
	role := models.Role{Name: "AJ", Permissions: "components:read"}
	p.db.Create(&role)
	page := models.StatusPage{Name: "Priv", Slug: "priv800", IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "x.example"}
	p.db.Create(&page)
	u, tok := p.user("aj@x.example", false)
	armed := true
	_ = p.db.Callback().Create().Before("gorm:create").Register("test:ajrace", func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "user_status_page_roles" {
			return
		}
		armed = false
		p.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec(
			"INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?)",
			u.ID, page.ID, role.ID)
	})
	t.Cleanup(func() { _ = p.db.Callback().Create().Remove("test:ajrace") })
	w := idorRequest(t, r, "GET", "/api/public/pages/priv800/dashboard", tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("erwartet 200, war %d %s", w.Code, w.Body.String())
	}
}
