package handlers_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Seitenadmins (keine Global Admins) hängen Konten nicht mehr direkt
// an, sondern laden ein; erst der Kontoinhaber macht daraus eine Mitgliedschaft.

var invitationLinkRe = regexp.MustCompile(`/invitation#token=([A-Za-z0-9]+)`)

type invFixture struct {
	db       *gorm.DB
	h        *handlers.Handler
	r        *gin.Engine
	page     models.StatusPage
	other    models.StatusPage
	owner    models.User
	ownerTok string
	ownerRol models.Role
	viewer   models.Role
	strong   models.Role
	gaTok    string
}

func newInvFixture(t *testing.T) invFixture {
	t.Helper()
	return newInvFixtureOn(t, testutils.SetupTestDB())
}

// newInvFixtureOn baut die Fixture auf einer vorhandenen Datenbank (SQLite aus
// SetupTestDB oder, in page_invitation_postgres_test.go, ein echtes Postgres).
func newInvFixtureOn(t *testing.T, db *gorm.DB) invFixture {
	t.Helper()
	h := handlers.NewHandler(db)
	t.Cleanup(h.WaitMails) // Einladungsmails laufen im Hintergrund
	r := testutils.SetupRouter()
	admin := r.Group("/api/admin/pages/:page_id", middleware.AuthMiddleware())
	admin.GET("/users", middleware.RequirePermission("pages:read"), h.GetStatusPageUsersHandler)
	admin.POST("/users", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)
	admin.GET("/invitations", middleware.RequirePermission("pages:read"), h.GetPageInvitationsHandler)
	admin.DELETE("/invitations/:id", middleware.RequirePermission("pages:delete"), h.RevokePageInvitationHandler)
	me := r.Group("/api/invitations", middleware.AuthMiddleware())
	me.POST("/preview", h.PreviewPageInvitationHandler)
	me.POST("/accept", h.AcceptPageInvitationHandler)
	me.POST("/decline", h.DeclinePageInvitationHandler)

	f := invFixture{db: db, h: h, r: r}
	f.page = models.StatusPage{Name: "Seite A", Slug: "seite-a"}
	db.Create(&f.page)
	f.other = models.StatusPage{Name: "Seite B", Slug: "seite-b"}
	db.Create(&f.other)
	f.ownerRol = models.Role{Name: "Page Owner", Permissions: "pages:create,pages:read,pages:update,pages:delete,incidents:read"}
	db.Create(&f.ownerRol)
	f.viewer = models.Role{Name: "Viewer", Permissions: "incidents:read"}
	db.Create(&f.viewer)
	f.strong = models.Role{Name: "Strong", Permissions: "pages:create,pages:read,pages:update,pages:delete,incidents:read,roles:read"}
	db.Create(&f.strong)
	f.owner = models.User{Email: "owner@a.example"}
	db.Create(&f.owner)
	db.Create(&models.UserStatusPageRole{UserID: f.owner.ID, StatusPageID: f.page.ID, RoleID: f.ownerRol.ID})
	f.ownerTok, _ = middleware.GenerateToken(f.owner.ID, f.owner.Email, "", "", false)
	ga := models.User{Email: "ga@example.com", IsGlobalAdmin: true}
	db.Create(&ga)
	f.gaTok, _ = middleware.GenerateToken(ga.ID, ga.Email, "", "", true)
	return f
}

func (f invFixture) usersPath() string {
	return "/api/admin/pages/" + strconv.Itoa(int(f.page.ID)) + "/users"
}

func (f invFixture) invPath() string {
	return "/api/admin/pages/" + strconv.Itoa(int(f.page.ID)) + "/invitations"
}

func (f invFixture) account(t *testing.T, email string) (models.User, string) {
	t.Helper()
	u := models.User{Email: email}
	f.db.Create(&u)
	tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	return u, tok
}

func (f invFixture) isMember(userID uint) bool {
	var n int64
	f.db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", userID, f.page.ID).Count(&n)
	return n > 0
}

func (f invFixture) invitationCount() int64 {
	var n int64
	f.db.Model(&models.PageInvitation{}).Count(&n)
	return n
}

// invite lädt per E-Mail ein und liefert den Token aus der Mail.
func (f invFixture) invite(t *testing.T, srv *fakeSMTPServer, email string, role models.Role) string {
	t.Helper()
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": email, "role_id": role.ID})
	requireStatusCode(t, w, http.StatusOK)
	m := invitationLinkRe.FindStringSubmatch(awaitSMTPMessage(t, srv))
	if m == nil {
		t.Fatal("Einladungslink fehlt in der Mail")
	}
	return m[1]
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func TestPageInvitation_OwnerInvitesInsteadOfAttaching(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")
	f := newInvFixture(t)
	victim, _ := f.account(t, "victim@b.example")

	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": " VICTIM@b.example ", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	if f.isMember(victim.ID) {
		t.Fatal("Konto wurde ohne Zustimmung Mitglied")
	}
	msg := awaitSMTPMessage(t, srv)
	m := invitationLinkRe.FindStringSubmatch(msg)
	// Der Token steht im Fragment (geht nie an den Server und nie in
	// den Referer), nicht in der Query.
	if m == nil || !strings.Contains(msg, "https://status.example.test/invitation#token="+m[1]) {
		t.Fatalf("Mail ohne Einladungslink mit Fragment: %s", msg)
	}
	if strings.Contains(msg, "/invitation?") || strings.Contains(msg, "?token=") {
		t.Errorf("Token darf nicht in der Query stehen: %s", msg)
	}
	if !strings.Contains(msg, "To: victim@b.example") || !strings.Contains(msg, "Seite A") || !strings.Contains(msg, "Viewer") {
		t.Errorf("Mail unvollständig: %s", msg)
	}
	var inv models.PageInvitation
	if err := f.db.First(&inv).Error; err != nil {
		t.Fatal(err)
	}
	if inv.Email != "victim@b.example" || inv.RoleID != f.viewer.ID || inv.InvitedByID != f.owner.ID {
		t.Errorf("Einladung unerwartet: %+v", inv)
	}
	if inv.TokenHash != hashToken(m[1]) || strings.Contains(inv.TokenHash, m[1]) {
		t.Errorf("Token nicht gehasht gespeichert")
	}
	if d := time.Until(inv.ExpiresAt); d < 6*24*time.Hour || d > 7*24*time.Hour+time.Minute {
		t.Errorf("Ablauf unerwartet: %v", d)
	}
	if n := auditCountFor(f.db, "statuspage.invitation_created"); n != 1 {
		t.Errorf("Audit fehlt: %d", n)
	}
	var a models.AuditLog
	f.db.Where("action = ?", "statuspage.invitation_created").First(&a)
	if strings.Contains(a.Details+a.TargetName, "victim") {
		t.Errorf("Audit verrät die Adresse: %+v", a)
	}
}

func TestPageInvitation_SameAnswerForKnownAndUnknownAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "known@x.example")
	sso := models.User{Email: "sso@x.example", SSOProvider: "okta", SSOSubject: "s1"}
	f.db.Create(&sso)

	known := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "known@x.example", "role_id": f.viewer.ID})
	awaitSMTPMessage(t, srv)
	unknown := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "unknown@x.example", "role_id": f.viewer.ID})
	expectNoSMTPMessage(t, srv)
	blocked := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "sso@x.example", "role_id": f.viewer.ID})
	expectNoSMTPMessage(t, srv)
	for _, w := range []*struct {
		code int
		body string
	}{{unknown.Code, unknown.Body.String()}, {blocked.Code, blocked.Body.String()}} {
		if w.code != known.Code || w.body != known.Body.String() {
			t.Errorf("Antworten unterscheiden sich: %d %s vs. %d %s", known.Code, known.Body.String(), w.code, w.body)
		}
	}
	// Die Liste offener Einladungen ist ebenfalls gleich aufgebaut (drei Einträge).
	list := idorRequest(t, f.r, "GET", f.invPath(), f.ownerTok, nil)
	requireStatusCode(t, list, http.StatusOK)
	var rows []map[string]any
	_ = json.Unmarshal(list.Body.Bytes(), &rows)
	if len(rows) != 3 {
		t.Fatalf("erwartet 3 Einladungen, erhalten %s", list.Body.String())
	}
	for _, leak := range []string{"Token", "token", "Hash", "InvitedBy"} {
		if strings.Contains(list.Body.String(), leak) {
			t.Errorf("Liste enthält %q: %s", leak, list.Body.String())
		}
	}
	if rows[0]["RoleName"] != "Viewer" {
		t.Errorf("Rollenname fehlt: %v", rows[0])
	}
}

func TestPageInvitation_AcceptFlow(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, inviteeTok := f.account(t, "invitee@x.example")
	_, strangerTok := f.account(t, "stranger@x.example")
	token := f.invite(t, srv, "invitee@x.example", f.viewer)

	t.Run("ohne Token -> 400", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", inviteeTok, map[string]any{})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireErrorCode(t, w.Body.Bytes(), "invitation_token_missing")
	})
	t.Run("unbekannter Token und fremdes Konto -> gleiche 404", func(t *testing.T) {
		a := idorRequest(t, f.r, "POST", "/api/invitations/accept", inviteeTok, map[string]any{"token": "FALSCH"})
		b := idorRequest(t, f.r, "POST", "/api/invitations/accept", strangerTok, map[string]any{"token": token})
		requireStatusCode(t, a, http.StatusNotFound)
		if a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Errorf("Antworten unterscheiden sich: %s vs. %s", a.Body.String(), b.Body.String())
		}
		if f.isMember(invitee.ID) || f.invitationCount() != 1 {
			t.Fatal("Fehlversuche dürfen nichts ändern")
		}
	})
	t.Run("Vorschau verbraucht den Token nicht", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", "/api/invitations/preview", inviteeTok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `"page_name":"Seite A"`) || !strings.Contains(w.Body.String(), `"role_name":"Viewer"`) {
			t.Errorf("Vorschau unvollständig: %s", w.Body.String())
		}
		if f.invitationCount() != 1 || f.isMember(invitee.ID) {
			t.Fatal("Vorschau hat etwas geändert")
		}
	})
	t.Run("Annehmen legt die Mitgliedschaft an, einmalig", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", inviteeTok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusOK)
		var ur models.UserStatusPageRole
		if err := f.db.Where("user_id = ? AND status_page_id = ?", invitee.ID, f.page.ID).First(&ur).Error; err != nil || ur.RoleID != f.viewer.ID {
			t.Fatalf("Mitgliedschaft fehlt/falsch: %+v %v", ur, err)
		}
		if f.invitationCount() != 0 {
			t.Error("Einladung nicht verbraucht")
		}
		if auditCountFor(f.db, "statuspage.invitation_accepted") != 1 {
			t.Error("Audit fehlt")
		}
		again := idorRequest(t, f.r, "POST", "/api/invitations/accept", inviteeTok, map[string]any{"token": token})
		requireStatusCode(t, again, http.StatusNotFound)
	})
}

func TestPageInvitation_Decline(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, tok := f.account(t, "invitee@x.example")
	_, strangerTok := f.account(t, "stranger@x.example")
	token := f.invite(t, srv, invitee.Email, f.viewer)

	w := idorRequest(t, f.r, "POST", "/api/invitations/decline", strangerTok, map[string]any{"token": token})
	requireStatusCode(t, w, http.StatusNotFound)
	w = idorRequest(t, f.r, "POST", "/api/invitations/decline", tok, map[string]any{"token": token})
	requireStatusCode(t, w, http.StatusOK)
	if f.isMember(invitee.ID) || f.invitationCount() != 0 {
		t.Fatal("Ablehnen muss die Einladung löschen und darf nichts anlegen")
	}
	if auditCountFor(f.db, "statuspage.invitation_declined") != 1 {
		t.Error("Audit fehlt")
	}
	w = idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
	requireStatusCode(t, w, http.StatusNotFound)
}

func TestPageInvitation_ExpiredAndStale(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, tok := f.account(t, "invitee@x.example")

	t.Run("abgelaufen -> 410 und gelöscht", func(t *testing.T) {
		token := f.invite(t, srv, invitee.Email, f.viewer)
		f.db.Model(&models.PageInvitation{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Minute))
		w := idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusGone)
		requireErrorCode(t, w.Body.Bytes(), "invitation_expired")
		if f.invitationCount() != 0 || f.isMember(invitee.ID) {
			t.Fatal("abgelaufene Einladung bleibt bzw. wirkt")
		}
	})
	t.Run("Einladender hat keine Rechte mehr -> 404", func(t *testing.T) {
		token := f.invite(t, srv, invitee.Email, f.viewer)
		f.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", f.owner.ID).Update("role_id", f.viewer.ID)
		defer f.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", f.owner.ID).Update("role_id", f.ownerRol.ID)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
		if f.isMember(invitee.ID) {
			t.Fatal("Einladung eines entmachteten Admins wirkte")
		}
	})
	t.Run("Rolle gelöscht -> 404", func(t *testing.T) {
		backdateInvitations(f.db, 31*time.Minute) // Grabstein der vorigen Prüfung: Sperre abgelaufen (Review B1)
		tmp := models.Role{Name: "Tmp", Permissions: "incidents:read"}
		f.db.Create(&tmp)
		token := f.invite(t, srv, invitee.Email, tmp)
		f.db.Delete(&tmp)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
	})
	t.Run("Seite gelöscht -> 404", func(t *testing.T) {
		backdateInvitations(f.db, 31*time.Minute) // Grabstein der vorigen Prüfung: Sperre abgelaufen (Review B1)
		token := f.invite(t, srv, invitee.Email, f.viewer)
		f.db.Delete(&f.page)
		defer f.db.Unscoped().Model(&models.StatusPage{}).Where("id = ?", f.page.ID).Update("deleted_at", nil)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
	})
	t.Run("erneutes Einladen macht den alten Link ungültig", func(t *testing.T) {
		backdateInvitations(f.db, 31*time.Minute) // Grabstein der vorigen Prüfung: Sperre abgelaufen (Review B1)
		old := f.invite(t, srv, invitee.Email, f.viewer)
		backdateInvitations(f.db, 31*time.Minute) // Ersetzen erst nach 30 Minuten (Security-Review)
		fresh := f.invite(t, srv, invitee.Email, f.viewer)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": old}), http.StatusNotFound)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": fresh}), http.StatusOK)
	})
}

func TestPageInvitation_AccountConflicts(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)

	t.Run("Rolle auf einer anderen Seite -> 409", func(t *testing.T) {
		u, tok := f.account(t, "multi@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		// Erst nach der Einladung woanders Mitglied geworden.
		f.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.other.ID, RoleID: f.viewer.ID})
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusConflict)
		requireErrorCode(t, w.Body.Bytes(), "invitation_account_conflict")
		if f.isMember(u.ID) {
			t.Fatal("Konto mit Rolle auf anderer Seite wurde Mitglied")
		}
	})
	t.Run("Seiten-SSO-Sitzung -> 409", func(t *testing.T) {
		u, _ := f.account(t, "ssosession@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		ssoTok, _ := middleware.GeneratePageSSOToken(u, "", "oidc", f.other.ID)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", ssoTok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusConflict)
		if f.isMember(u.ID) {
			t.Fatal("Seiten-SSO-Sitzung hat angenommen")
		}
	})
	t.Run("SSO-Verknüpfung nur mit der einladenden Seite: Annehmen erlaubt", func(t *testing.T) {
		u, tok := f.account(t, "ssoa@x.example")
		f.db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: f.page.ID, Protocol: "oidc", Issuer: "i", Subject: "s-a"})
		token := f.invite(t, srv, u.Email, f.viewer)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusOK)
		if !f.isMember(u.ID) {
			t.Fatal("nicht Mitglied geworden")
		}
	})
	t.Run("Global-Admin-Konto -> 409", func(t *testing.T) {
		u := models.User{Email: "wasga@x.example"}
		f.db.Create(&u)
		token := f.invite(t, srv, u.Email, f.viewer)
		f.db.Model(&u).Update("is_global_admin", true)
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", true)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusConflict)
	})
	t.Run("schon Mitglied -> 409, Rolle unverändert, Einladung verbraucht", func(t *testing.T) {
		u, tok := f.account(t, "member@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		f.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.ownerRol.ID})
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusConflict)
		requireErrorCode(t, w.Body.Bytes(), "invitation_already_member")
		var ur models.UserStatusPageRole
		f.db.Where("user_id = ? AND status_page_id = ?", u.ID, f.page.ID).First(&ur)
		if ur.RoleID != f.ownerRol.ID {
			t.Error("Rolle wurde geändert")
		}
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token}), http.StatusNotFound)
	})
}

func TestPageInvitation_AdminPaths(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	other, _ := f.account(t, "other@x.example")

	t.Run("per user_id für Nicht-Mitglied weiter 400", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"user_id": other.ID, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusBadRequest)
		if f.isMember(other.ID) || f.invitationCount() != 0 {
			t.Fatal("user_id-Weg legte etwas an")
		}
	})
	t.Run("Global Admin ordnet weiter direkt zu", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.gaTok, map[string]any{"email": other.Email, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusCreated)
		if !f.isMember(other.ID) || f.invitationCount() != 0 {
			t.Fatal("Global Admin sollte direkt zuordnen")
		}
	})
	t.Run("Zurückziehen", func(t *testing.T) {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "weg@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		var inv models.PageInvitation
		f.db.First(&inv)
		path := f.invPath() + "/" + strconv.Itoa(int(inv.ID))
		otherPagePath := "/api/admin/pages/" + strconv.Itoa(int(f.other.ID)) + "/invitations/" + strconv.Itoa(int(inv.ID))
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", otherPagePath, f.gaTok, nil), http.StatusNotFound)
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", f.invPath()+"/abc", f.ownerTok, nil), http.StatusBadRequest)
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", path, f.ownerTok, nil), http.StatusOK)
		if f.invitationCount() != 0 || auditCountFor(f.db, "statuspage.invitation_revoked") != 1 {
			t.Fatal("Zurückziehen wirkte nicht")
		}
	})
	t.Run("stärkere Einladung zieht nur zurück, wer die Rolle abdeckt", func(t *testing.T) {
		inv := models.PageInvitation{StatusPageID: f.page.ID, Email: "s@x.example", RoleID: f.strong.ID, TokenHash: hashToken("x"), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: 1}
		f.db.Create(&inv)
		path := f.invPath() + "/" + strconv.Itoa(int(inv.ID))
		w := idorRequest(t, f.r, "DELETE", path, f.ownerTok, nil)
		requireStatusCode(t, w, http.StatusForbidden)
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", path, f.gaTok, nil), http.StatusOK)
	})
	t.Run("ungültige page_id", func(t *testing.T) {
		requireStatusCode(t, idorRequest(t, f.r, "GET", "/api/admin/pages/abc/invitations", f.gaTok, nil), http.StatusBadRequest)
	})
	t.Run("Obergrenze offener Einladungen -> 429 unabhängig vom Konto", func(t *testing.T) {
		rows := make([]models.PageInvitation, 0, 100)
		for i := range 100 {
			rows = append(rows, models.PageInvitation{StatusPageID: f.page.ID, Email: "m" + strconv.Itoa(i) + "@x.example", RoleID: f.viewer.ID, TokenHash: hashToken("t" + strconv.Itoa(i)), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: f.owner.ID})
		}
		f.db.Create(&rows)
		f.account(t, "known2@x.example")
		for _, email := range []string{"known2@x.example", "niemand@x.example"} {
			w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": email, "role_id": f.viewer.ID})
			requireStatusCode(t, w, http.StatusTooManyRequests)
			requireErrorCode(t, w.Body.Bytes(), "invitation_limit_reached")
		}
		expectNoSMTPMessage(t, srv)
		// Abgelaufene zählen nicht mit und werden beim nächsten Einladen entfernt.
		f.db.Model(&models.PageInvitation{}).Where("email LIKE ?", "m%").Update("expires_at", time.Now().Add(-time.Minute))
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "niemand@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		if f.invitationCount() != 1 {
			t.Errorf("abgelaufene Einladungen nicht aufgeräumt: %d", f.invitationCount())
		}
	})
}

// Mail und Log: kein Token im Log, CR/LF im Seitennamen verhindert den Versand,
// englische Mail für englischsprachige Konten.
func TestPageInvitation_MailAndLogging(t *testing.T) {
	t.Run("Versandfehler: Token und Link nicht im Log", func(t *testing.T) {
		t.Setenv("SMTP_HOST", "127.0.0.1")
		t.Setenv("SMTP_PORT", "1") // nichts lauscht -> Fehler
		buf := captureLog(t)
		f := newInvFixture(t)
		f.account(t, "log@x.example")
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "log@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		f.h.WaitMails()
		if auditCountFor(f.db, "statuspage.invitation_email_failed") != 1 {
			t.Fatalf("Versandfehler nicht auditiert; Log: %s", buf.String())
		}
		var inv models.PageInvitation
		f.db.First(&inv)
		logText := buf.String()
		if strings.Contains(logText, "token=") || strings.Contains(logText, inv.TokenHash) || strings.Contains(logText, "log@x.example") {
			t.Errorf("Log enthält Geheimnis oder Adresse: %s", logText)
		}
		var a models.AuditLog
		f.db.Where("action = ?", "statuspage.invitation_email_failed").First(&a)
		if strings.Contains(a.Details, "token") {
			t.Errorf("Audit enthält Token: %+v", a)
		}
	})
	t.Run("CR/LF im Seitennamen: keine Mail", func(t *testing.T) {
		srv := startFakeSMTPServerWithEnv(t)
		defer srv.close()
		f := newInvFixture(t)
		f.db.Model(&f.page).Update("name", "Böse\r\nBcc: x@evil.example")
		f.account(t, "crlf@x.example")
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "crlf@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		expectNoSMTPMessage(t, srv)
	})
	t.Run("englisches Konto bekommt englische Mail, Namen maskiert", func(t *testing.T) {
		srv := startFakeSMTPServerWithEnv(t)
		defer srv.close()
		f := newInvFixture(t)
		f.db.Model(&f.page).Update("name", "<b>A&B</b>")
		u := models.User{Email: "en@x.example", PreferredLanguage: "en"}
		f.db.Create(&u)
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": u.Email, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		msg := awaitSMTPMessage(t, srv)
		if !strings.Contains(msg, "You have been invited") || !strings.Contains(msg, "&lt;b&gt;A&amp;B&lt;/b&gt;") {
			t.Errorf("Mail unerwartet: %s", msg)
		}
	})
	t.Run("Versand abgeschaltet: keine Mail, Einladung bleibt", func(t *testing.T) {
		srv := startFakeSMTPServerWithEnv(t)
		defer srv.close()
		f := newInvFixture(t)
		disableEmail(t, f.db)
		f.account(t, "off@x.example")
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "off@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
		expectNoSMTPMessage(t, srv)
		if f.invitationCount() != 1 {
			t.Error("Einladung fehlt")
		}
	})
}

// failOn registriert einen Fehler für Abfragen (kind: query/create/delete) auf
// der Tabelle, solange cond true liefert, und räumt am Testende auf.
func failOn(t *testing.T, db *gorm.DB, kind, table string, cond func(*gorm.DB) bool) {
	t.Helper()
	name := "fail799:" + kind + ":" + table + ":" + t.Name()
	fn := func(tx *gorm.DB) {
		if tx.Statement.Table == table && (cond == nil || cond(tx)) {
			_ = tx.AddError(errors.New("boom"))
		}
	}
	switch kind {
	case "query":
		_ = db.Callback().Query().Before("gorm:query").Register(name, fn)
		t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	case "create":
		_ = db.Callback().Create().Before("gorm:create").Register(name, fn)
		t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
	case "delete":
		_ = db.Callback().Delete().Before("gorm:delete").Register(name, fn)
		t.Cleanup(func() { _ = db.Callback().Delete().Remove(name) })
	}
}

// DB-Fehler enden fail closed mit 500 und ohne Mitgliedschaft.
func TestPageInvitation_DBErrors(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()

	t.Run("Einladen: Löschen/Zählen/Anlegen scheitert -> 500", func(t *testing.T) {
		for _, kind := range []string{"delete", "query", "create"} {
			t.Run(kind, func(t *testing.T) {
				f := newInvFixture(t)
				failOn(t, f.db, kind, "page_invitations", nil)
				w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "x@x.example", "role_id": f.viewer.ID})
				requireStatusCode(t, w, http.StatusInternalServerError)
			})
		}
	})
	t.Run("Liste scheitert -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		failOn(t, f.db, "query", "page_invitations", nil)
		requireStatusCode(t, idorRequest(t, f.r, "GET", f.invPath(), f.ownerTok, nil), http.StatusInternalServerError)
	})
	t.Run("Zurückziehen/Ablehnen: Löschen scheitert -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "d@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		var inv models.PageInvitation
		f.db.First(&inv)
		failOn(t, f.db, "delete", "page_invitations", nil)
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil), http.StatusInternalServerError)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/decline", tok, map[string]any{"token": token}), http.StatusInternalServerError)
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusInternalServerError)
		if f.isMember(u.ID) {
			t.Fatal("Mitglied trotz Fehler")
		}
	})
	t.Run("Annehmen: Mitgliedsprüfung scheitert -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "q@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		// Erst die Zählung in der Transaktion (Bedingung ohne "<>": nicht die Prüfung "andere Seite").
		failOn(t, f.db, "query", "user_status_page_roles", func(tx *gorm.DB) bool {
			_, isCount := tx.Statement.Dest.(*int64)
			return isCount && !strings.Contains(fmt.Sprintf("%+v", tx.Statement.Clauses["WHERE"].Expression), "<>")
		})
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token}), http.StatusInternalServerError)
	})
	t.Run("Annehmen: Unique-Verletzung -> 409 schon Mitglied", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "race@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		name := "race799"
		_ = f.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "user_status_page_roles" {
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}
		})
		t.Cleanup(func() { _ = f.db.Callback().Create().Remove(name) })
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusConflict)
		requireErrorCode(t, w.Body.Bytes(), "invitation_already_member")
	})
	t.Run("Annehmen: Einladung gleichzeitig verbraucht -> 404", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "twice@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		// Unmittelbar vor dem Löschen in der Transaktion löscht ein "zweiter Aufruf".
		name := "twice799"
		_ = f.db.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "page_invitations" {
				f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec("DELETE FROM page_invitations")
			}
		})
		t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
		if f.isMember(u.ID) {
			t.Fatal("doppelt verbrauchte Einladung wirkte")
		}
	})
	t.Run("Einladender gelöscht, Global Admin oder nicht mehr Mitglied", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "inv@x.example")
		mk := func(by uint) string {
			raw := "tok-" + strconv.Itoa(int(by)) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			inv := models.PageInvitation{StatusPageID: f.page.ID, Email: u.Email, RoleID: f.viewer.ID, TokenHash: hashToken(raw), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: by}
			// Review F3: je (Seite, Adresse) gibt es nur noch eine Zeile.
			f.db.Unscoped().Where("email = ?", u.Email).Delete(&models.PageInvitation{})
			f.db.Create(&inv)
			f.db.Model(&inv).Update("invited_user_id", u.ID)
			return raw
		}
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": mk(99999)}), http.StatusNotFound)
		outsider, _ := f.account(t, "outsider@x.example")
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": mk(outsider.ID)}), http.StatusNotFound)
		var ga models.User
		f.db.Where("is_global_admin = ?", true).First(&ga)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": mk(ga.ID)}), http.StatusOK)
	})
	t.Run("keine nackte Adresse -> 400, nichts gespeichert, unabhängig vom Konto", func(t *testing.T) {
		f := newInvFixture(t)
		f.db.Create(&models.User{Email: "Name <odd@x.example>"})
		for _, email := range []string{"name <odd@x.example>", "keine-adresse", "a@x.example\r\nBcc: b@x.example"} {
			w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": email, "role_id": f.viewer.ID})
			requireStatusCode(t, w, http.StatusBadRequest)
			requireErrorCode(t, w.Body.Bytes(), "email_invalid")
		}
		expectNoSMTPMessage(t, srv)
		if f.invitationCount() != 0 {
			t.Fatal("ungültige Adresse gespeichert")
		}
	})
	t.Run("Mitglied wird zwischen Prüfung und Zuweisung entfernt (user_id) -> 400", func(t *testing.T) {
		f := newInvFixture(t)
		u, _ := f.account(t, "gone@x.example")
		f.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID})
		name := "gone799"
		armed := true
		_ = f.db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			if _, isCount := tx.Statement.Dest.(*int64); armed && isCount && tx.Statement.Table == "user_status_page_roles" {
				armed = false
				f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec("DELETE FROM user_status_page_roles WHERE user_id = ?", u.ID)
			}
		})
		t.Cleanup(func() { _ = f.db.Callback().Query().Remove(name) })
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"user_id": u.ID, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireErrorCode(t, w.Body.Bytes(), "user_email_required")
		if f.isMember(u.ID) || f.invitationCount() != 0 {
			t.Fatal("am Einladungsweg vorbei angelegt")
		}
	})
}

// backdateInvitations setzt alle Einladungen um d in die Vergangenheit.
func backdateInvitations(db *gorm.DB, d time.Duration) {
	// Unscoped: auch zurückgezogene (soft-gelöschte) Zeilen (Review B1).
	db.Unscoped().Model(&models.PageInvitation{}).Where("1 = 1").Update("created_at", time.Now().Add(-d))
}
