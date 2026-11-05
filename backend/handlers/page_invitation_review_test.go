package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Security-Review Funde 1-4, je mit Regressionstest.

func (f invFixture) onlyInvitation(t *testing.T) models.PageInvitation {
	t.Helper()
	var invs []models.PageInvitation
	f.db.Find(&invs)
	if len(invs) != 1 {
		t.Fatalf("genau eine Einladung erwartet, %d", len(invs))
	}
	return invs[0]
}

// Fund 1: Mail-Flut durch wiederholtes Einladen derselben Adresse.
func TestPageInvitationReview_NoMailFloodWithin30Minutes(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, tok := f.account(t, "flood@x.example")

	token := f.invite(t, srv, invitee.Email, f.viewer)
	first := f.onlyInvitation(t)
	for i := 0; i < 3; i++ {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": invitee.Email, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusOK)
	}
	expectNoSMTPMessage(t, srv)
	if again := f.onlyInvitation(t); again.TokenHash != first.TokenHash || again.ID != first.ID {
		t.Fatal("Einladung wurde innerhalb von 30 Minuten ersetzt")
	}
	requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token}), http.StatusOK)

	t.Run("gleiche Regel ohne Konto (kein Orakel)", func(t *testing.T) {
		a := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "ghost@x.example", "role_id": f.viewer.ID})
		var ghost models.PageInvitation
		f.db.Where("email = ?", "ghost@x.example").First(&ghost)
		b := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "ghost@x.example", "role_id": f.viewer.ID})
		var ghost2 models.PageInvitation
		f.db.Where("email = ?", "ghost@x.example").First(&ghost2)
		known := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": invitee.Email, "role_id": f.viewer.ID})
		if a.Body.String() != b.Body.String() || b.Body.String() != known.Body.String() || ghost.TokenHash != ghost2.TokenHash {
			t.Errorf("unterscheidbar oder ersetzt: %s | %s | %s", a.Body.String(), b.Body.String(), known.Body.String())
		}
	})

	t.Run("nach 30 Minuten neuer Link und neue Mail", func(t *testing.T) {
		backdateInvitations(f.db, 31*time.Minute)
		fresh := f.invite(t, srv, invitee.Email, f.viewer)
		if fresh == token {
			t.Fatal("Token nicht erneuert")
		}
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token}), http.StatusNotFound)
	})
}

// Fund 1: Tageslimit neuer Einladungen je Seite, unabhängig vom Konto.
func TestPageInvitationReview_DailyLimit(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "daily@x.example")
	pid := strconv.Itoa(int(f.page.ID))
	logs := make([]models.AuditLog, 0, 200)
	for range 200 {
		logs = append(logs, models.AuditLog{Action: "statuspage.invitation_created", Category: "config", TargetType: "StatusPage", TargetID: pid})
	}
	f.db.Create(&logs)
	// Einträge anderer Seiten und ältere Einträge zählen nicht.
	f.db.Create(&models.AuditLog{Action: "statuspage.invitation_created", TargetType: "StatusPage", TargetID: strconv.Itoa(int(f.other.ID))})
	for _, email := range []string{"daily@x.example", "niemand@x.example"} {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": email, "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusTooManyRequests)
		requireErrorCode(t, w.Body.Bytes(), "invitation_limit_reached")
	}
	expectNoSMTPMessage(t, srv)
	f.db.Model(&models.AuditLog{}).Where("target_id = ?", pid).Update("created_at", time.Now().Add(-25*time.Hour).Unix())
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "daily@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	awaitSMTPMessage(t, srv)
}

// Fund 3: Erneutes Einladen ersetzt keine Einladung mit stärkerer Rolle.
func TestPageInvitationReview_ReinviteKeepsStrongerInvitation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	inv := models.PageInvitation{StatusPageID: f.page.ID, Email: "boss@x.example", RoleID: f.strong.ID, TokenHash: hashToken("stark"), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: 1}
	f.db.Create(&inv)
	backdateInvitations(f.db, time.Hour)
	unknown := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "neutral@x.example", "role_id": f.viewer.ID})
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "boss@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	if w.Body.String() != unknown.Body.String() {
		t.Errorf("Antwort unterscheidbar: %s vs %s", w.Body.String(), unknown.Body.String())
	}
	var after models.PageInvitation
	f.db.Where("email = ?", "boss@x.example").First(&after)
	if after.TokenHash != inv.TokenHash || after.RoleID != f.strong.ID {
		t.Fatalf("stärkere Einladung ersetzt: %+v", after)
	}
	// Wer die Rolle abdeckt (Global Admin über den direkten Weg betrifft das nicht),
	// darf ersetzen: Owner mit starker Rolle.
	f.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", f.owner.ID).Update("role_id", f.strong.ID)
	w = idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "boss@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	var replaced models.PageInvitation
	f.db.Where("email = ?", "boss@x.example").First(&replaced)
	if replaced.TokenHash == inv.TokenHash || replaced.RoleID != f.viewer.ID {
		t.Fatalf("Ersetzen durch berechtigten Aufrufer fehlgeschlagen: %+v", replaced)
	}
}

// Fund 2: Einladungen verschwinden mit dem Konto; ein neu angelegtes Konto mit
// derselben Adresse kann einen alten Link nicht nutzen.
func TestPageInvitationReview_AccountDeletionAndReuse(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()

	t.Run("neues Konto mit derselben Adresse kann nicht annehmen", func(t *testing.T) {
		f := newInvFixture(t)
		old, _ := f.account(t, "reuse@x.example")
		token := f.invite(t, srv, old.Email, f.viewer)
		f.db.Unscoped().Delete(&old) // Konto weg, Einladung (in diesem Test) bewusst stehen gelassen
		fresh, freshTok := f.account(t, "reuse@x.example")
		w := idorRequest(t, f.r, "POST", "/api/invitations/accept", freshTok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
		if f.isMember(fresh.ID) {
			t.Fatal("neues Konto wurde über den alten Link Mitglied")
		}
	})
	t.Run("Einladung an eine Adresse ohne Konto ist nie annehmbar", func(t *testing.T) {
		f := newInvFixture(t)
		raw := "ohne-konto"
		f.db.Create(&models.PageInvitation{StatusPageID: f.page.ID, Email: "later@x.example", RoleID: f.viewer.ID, TokenHash: hashToken(raw), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: f.owner.ID})
		_, tok := f.account(t, "later@x.example")
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": raw}), http.StatusNotFound)
	})

	t.Run("DELETE /api/me löscht Einladungen an und von dem Konto", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)
		r := testutils.SetupRouter()
		g := r.Group("/api", middleware.AuthMiddleware())
		g.DELETE("/me", h.DeleteMeHandler)
		u := models.User{Email: "Me@X.example"}
		db.Create(&u)
		other := models.User{Email: "other@x.example"}
		db.Create(&other)
		seedInvitations(db, u, other)
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		w := idorRequest(t, r, "DELETE", "/api/me", tok, map[string]any{"confirm_email": u.Email})
		requireStatusCode(t, w, http.StatusOK)
		assertOnlyForeignInvitation(t, db)
	})
	t.Run("DELETE /api/users/:id löscht Einladungen an und von dem Konto", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)
		r := testutils.SetupRouter()
		r.DELETE("/api/users/:id", h.DeleteUserHandler)
		db.Create(&models.User{Email: "root@x.example", IsGlobalAdmin: true})
		u := models.User{Email: "Me@X.example"}
		db.Create(&u)
		other := models.User{Email: "other@x.example"}
		db.Create(&other)
		seedInvitations(db, u, other)
		w := idorRequest(t, r, "DELETE", "/api/users/"+strconv.Itoa(int(u.ID)), "", nil)
		requireStatusCode(t, w, http.StatusOK)
		assertOnlyForeignInvitation(t, db)
	})
	t.Run("Fehler beim Löschen der Einladungen bricht DELETE /api/users ab", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)
		r := testutils.SetupRouter()
		r.DELETE("/api/users/:id", h.DeleteUserHandler)
		db.Create(&models.User{Email: "root@x.example", IsGlobalAdmin: true})
		u := models.User{Email: "keep@x.example"}
		db.Create(&u)
		failOn(t, db, "delete", "page_invitations", nil)
		requireStatusCode(t, idorRequest(t, r, "DELETE", "/api/users/"+strconv.Itoa(int(u.ID)), "", nil), http.StatusInternalServerError)
		var n int64
		db.Model(&models.User{}).Where("id = ?", u.ID).Count(&n)
		if n != 1 {
			t.Fatal("Konto trotz Fehler gelöscht")
		}
	})
}

// seedInvitations: an u (Adresse in anderer Schreibweise), von u ausgesprochen,
// an u als Konto, und eine fremde, die bleiben muss.
func seedInvitations(db *gorm.DB, u, other models.User) {
	exp := time.Now().Add(time.Hour)
	rows := []models.PageInvitation{
		{StatusPageID: 1, Email: "me@x.example", RoleID: 1, TokenHash: hashToken("a"), ExpiresAt: exp, InvitedByID: other.ID},
		{StatusPageID: 1, Email: "dritte@x.example", RoleID: 1, TokenHash: hashToken("b"), ExpiresAt: exp, InvitedByID: u.ID},
		{StatusPageID: 1, Email: "fremd@x.example", RoleID: 1, TokenHash: hashToken("c"), ExpiresAt: exp, InvitedByID: other.ID},
	}
	db.Create(&rows)
}

func assertOnlyForeignInvitation(t *testing.T, db *gorm.DB) {
	t.Helper()
	var left []models.PageInvitation
	db.Unscoped().Find(&left)
	if len(left) != 1 || left[0].Email != "fremd@x.example" {
		t.Fatalf("erwartet nur die fremde Einladung, übrig: %+v", left)
	}
}

// Fund 2: abgelaufene Einladungen räumt der Wartungs-Tick seitenübergreifend ab.
func TestPageInvitationReview_PruneExpired(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	db.Create(&[]models.PageInvitation{
		{StatusPageID: 1, Email: "alt@x.example", RoleID: 1, TokenHash: hashToken("alt"), ExpiresAt: time.Now().Add(-time.Minute), InvitedByID: 1},
		{StatusPageID: 2, Email: "alt2@x.example", RoleID: 1, TokenHash: hashToken("alt2"), ExpiresAt: time.Now().Add(-time.Hour), InvitedByID: 1},
		{StatusPageID: 1, Email: "neu@x.example", RoleID: 1, TokenHash: hashToken("neu"), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: 1},
	})
	h.RunMaintenanceTickForTest()
	h.RunMaintenanceTickForTest() // idempotent
	var left []models.PageInvitation
	db.Unscoped().Find(&left)
	if len(left) != 1 || left[0].Email != "neu@x.example" {
		t.Fatalf("Bereinigung falsch: %+v", left)
	}
	t.Run("Fehler wird nur geloggt", func(t *testing.T) {
		buf := captureLog(t)
		failOn(t, db, "delete", "page_invitations", nil)
		h.RunMaintenanceTickForTest()
		if !strings.Contains(buf.String(), "Einladungen: Bereinigung fehlgeschlagen") {
			t.Errorf("Fehler nicht geloggt: %s", buf.String())
		}
	})
}

// Fund 2: Datenexport enthält eigene offene Einladungen (ohne Token/Einladenden).
func TestPageInvitationReview_ExportContainsOwnInvitations(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api", middleware.AuthMiddleware())
	g.GET("/me/export", h.MeExportHandler)
	page := models.StatusPage{Name: "Exportseite", Slug: "exp"}
	db.Create(&page)
	u := models.User{Email: "Exp@X.example"}
	db.Create(&u)
	exp := time.Now().Add(time.Hour)
	db.Create(&models.PageInvitation{StatusPageID: page.ID, Email: "exp@x.example", RoleID: 7, TokenHash: hashToken("geheim-export"), ExpiresAt: exp, InvitedByID: 4242, InvitedUserID: u.ID}) // F7: nur Einladungen an dieses Konto
	db.Create(&models.PageInvitation{StatusPageID: page.ID, Email: "fremd@x.example", RoleID: 7, TokenHash: hashToken("x"), ExpiresAt: exp, InvitedByID: 1})
	tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	w := idorRequest(t, r, "GET", "/api/me/export", tok, nil)
	requireStatusCode(t, w, http.StatusOK)
	body := w.Body.String()
	for _, want := range []string{`"invitations":[{`, `"status_page_name":"Exportseite"`, `"role_id":7`, `"expires_at"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Export ohne %s: %s", want, body)
		}
	}
	for _, leak := range []string{"fremd@x.example", hashToken("geheim-export"), "4242", "TokenHash", "invited"} {
		if strings.Contains(body, leak) {
			t.Errorf("Export enthält %q", leak)
		}
	}
}

// Fund 4: Rolle auf anderer Seite entsteht zwischen Vorprüfung und Annehmen.
func TestPageInvitationReview_AcceptRechecksOtherPageInTransaction(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	for _, kind := range []string{"role", "sso"} {
		t.Run(kind, func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "toctou-"+kind+"@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			name := "toctou799" + kind
			armed := true
			// Unmittelbar vor dem Verbrauchen der Einladung (nach der Vorprüfung)
			// vergibt ein "gleichzeitiger" Aufruf eine Rolle auf Seite B.
			_ = f.db.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
				if !armed || tx.Statement.Table != "page_invitations" {
					return
				}
				armed = false
				s := f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
				if kind == "role" {
					s.Exec("INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?)", u.ID, f.other.ID, f.viewer.ID)
				} else {
					s.Exec("INSERT INTO user_sso_identities (created_at, user_id, status_page_id, protocol, issuer, subject) VALUES (CURRENT_TIMESTAMP, ?, ?, 'oidc', 'i', 's')", u.ID, f.other.ID)
				}
			})
			t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
			w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusConflict)
			requireErrorCode(t, w.Body.Bytes(), "invitation_account_conflict")
			if f.isMember(u.ID) {
				t.Fatal("Ein-Seiten-Grenze im Wettlauf umgangen")
			}
		})
	}
	t.Run("DB-Fehler bei der erneuten Prüfung -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "toctou-err@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		inTx := false
		name := "toctou799err"
		_ = f.db.Callback().Delete().After("gorm:delete").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "page_invitations" {
				inTx = true
			}
		})
		t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
		for _, table := range []string{"user_status_page_roles", "user_sso_identities"} {
			failOn(t, f.db, "query", table, func(*gorm.DB) bool { return inTx })
		}
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token}), http.StatusInternalServerError)
	})
	t.Run("DB-Fehler nur bei der SSO-Prüfung -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "toctou-sso-err@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		inTx := false
		name := "toctou799ssoerr"
		_ = f.db.Callback().Delete().After("gorm:delete").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "page_invitations" {
				inTx = true
			}
		})
		t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
		failOn(t, f.db, "query", "user_sso_identities", func(*gorm.DB) bool { return inTx })
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token}), http.StatusInternalServerError)
	})
}

// Zählfehler der Obergrenzen enden fail closed mit 500.
func TestPageInvitationReview_LimitCountErrors(t *testing.T) {
	for _, table := range []string{"page_invitations", "audit_logs"} {
		t.Run(table, func(t *testing.T) {
			f := newInvFixture(t)
			failOn(t, f.db, "query", table, func(tx *gorm.DB) bool {
				_, isCount := tx.Statement.Dest.(*int64)
				return isCount
			})
			w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "count@x.example", "role_id": f.viewer.ID})
			requireStatusCode(t, w, http.StatusInternalServerError)
			if f.invitationCount() != 0 {
				t.Fatal("trotz Fehler angelegt")
			}
		})
	}
}

// Review B1: Zurückziehen hebt die 30-Minuten-Sperre nicht auf.
// Früher löschte DELETE /invitations/:id die Zeile endgültig; POST -> DELETE ->
// POST erzeugte so beliebig viele Mails an dieselbe Adresse.
func TestPageInvitationReview_RevokeDoesNotLiftResendLock(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, tok := f.account(t, "revoke@x.example")
	token := f.invite(t, srv, invitee.Email, f.viewer)
	revoke := func() {
		t.Helper()
		var inv models.PageInvitation
		if err := f.db.Where("email = ?", invitee.Email).First(&inv).Error; err != nil {
			t.Fatalf("keine offene Einladung: %v", err)
		}
		requireStatusCode(t, idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil), http.StatusOK)
	}
	revoke()

	// Zurückgezogen: nicht in der Liste, nicht annehmbar, nicht im Export.
	w := idorRequest(t, f.r, "GET", f.invPath(), f.ownerTok, nil)
	requireStatusCode(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), invitee.Email) {
		t.Errorf("zurückgezogene Einladung in der Liste: %s", w.Body.String())
	}
	for _, p := range []string{"/api/invitations/preview", "/api/invitations/accept"} {
		requireStatusCode(t, idorRequest(t, f.r, "POST", p, tok, map[string]any{"token": token}), http.StatusNotFound)
	}

	unknown := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "neu-b1@x.example", "role_id": f.viewer.ID})
	expectNoSMTPMessage(t, srv) // Adresse ohne Konto: nie eine Mail
	again := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": invitee.Email, "role_id": f.viewer.ID})
	requireStatusCode(t, again, http.StatusOK)
	if again.Body.String() != unknown.Body.String() {
		t.Errorf("Antwort unterscheidbar: %s vs %s", again.Body.String(), unknown.Body.String())
	}
	expectNoSMTPMessage(t, srv)
	var open int64
	f.db.Model(&models.PageInvitation{}).Where("email = ?", invitee.Email).Count(&open)
	if open != 0 {
		t.Errorf("innerhalb der Sperre neue offene Einladung angelegt: %d", open)
	}

	t.Run("nach 30 Minuten wieder einladbar", func(t *testing.T) {
		backdateInvitations(f.db, 31*time.Minute)
		fresh := f.invite(t, srv, invitee.Email, f.viewer)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": fresh}), http.StatusOK)
		var rows int64
		f.db.Unscoped().Model(&models.PageInvitation{}).Where("email = ?", invitee.Email).Count(&rows)
		if rows != 1 {
			t.Errorf("alte zurückgezogene Zeile nicht ersetzt: %d Zeilen", rows)
		}
	})
}

// Review B1: zurückgezogene Zeilen bleiben nur so lange, wie die Sperre gilt.
func TestPageInvitationReview_PruneRevoked(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	later := time.Now().Add(time.Hour)
	db.Create(&[]models.PageInvitation{
		{StatusPageID: 1, Email: "alt-zurueck@x.example", RoleID: 1, TokenHash: hashToken("r1"), ExpiresAt: later, InvitedByID: 1},
		{StatusPageID: 1, Email: "neu-zurueck@x.example", RoleID: 1, TokenHash: hashToken("r2"), ExpiresAt: later, InvitedByID: 1},
		{StatusPageID: 1, Email: "offen@x.example", RoleID: 1, TokenHash: hashToken("r3"), ExpiresAt: later, InvitedByID: 1},
	})
	backdateInvitations(db, 31*time.Minute)
	db.Model(&models.PageInvitation{}).Where("email = ?", "neu-zurueck@x.example").Update("created_at", time.Now())
	db.Where("email LIKE ?", "%zurueck%").Delete(&models.PageInvitation{})
	h.RunMaintenanceTickForTest()
	var left []string
	db.Unscoped().Model(&models.PageInvitation{}).Order("email").Pluck("email", &left)
	if strings.Join(left, ",") != "neu-zurueck@x.example,offen@x.example" {
		t.Fatalf("Bereinigung falsch: %v", left)
	}
}

// Review B1: Auch Ablehnen hebt die Sperre je (Seite, Adresse) nicht auf; der
// Einladende bekommt keine zweite Mail, wenn der Eingeladene abgelehnt hat.
func TestPageInvitationReview_DeclineDoesNotLiftResendLock(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	invitee, tok := f.account(t, "decline@x.example")
	token := f.invite(t, srv, invitee.Email, f.viewer)
	requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/decline", tok, map[string]any{"token": token}), http.StatusOK)
	// Schreibweise der Adresse ändert den Schlüssel nicht (Normalisierung).
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "  DECLINE@X.example ", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	expectNoSMTPMessage(t, srv)
	var open int64
	f.db.Model(&models.PageInvitation{}).Where("email = ?", invitee.Email).Count(&open)
	if open != 0 {
		t.Errorf("innerhalb der Sperre neue offene Einladung: %d", open)
	}
}
