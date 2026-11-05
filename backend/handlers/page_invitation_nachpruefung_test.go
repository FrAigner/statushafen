package handlers_test

import (
	"encoding/json"
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

// Folgepunkte aus dem Security-Review. Jeder Test hat
// eine Gegenprobe: Er war gegen den Stand von develop rot (siehe PR-Text).

// countQueries zählt Leseabfragen je Tabelle, solange der Test läuft.
func countQueries(t *testing.T, db *gorm.DB, tables ...string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	name := "count834:" + t.Name()
	_ = db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		for _, tb := range tables {
			if tx.Statement.Table == tb {
				counts[tb]++
			}
		}
	})
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	return counts
}

// failNth lässt die n-te (1-basiert) Leseabfrage auf der Tabelle scheitern.
func failNth(t *testing.T, db *gorm.DB, table string, n int) {
	t.Helper()
	seen := 0
	failOn(t, db, "query", table, func(*gorm.DB) bool {
		seen++
		return seen == n
	})
}

// Punkt 1 (Timing-Orakel): Für bekannte und unbekannte Adressen laufen
// dieselben Abfragen in derselben Anzahl, damit die Laufzeit nichts verrät.
func TestPageInvitationNachpruefung_LookupQueriesIdenticalForKnownAndUnknown(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "known834@x.example")
	tables := []string{"users", "user_status_page_roles", "user_sso_identities"}

	known := countQueries(t, f.db, tables...)
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "known834@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	awaitSMTPMessage(t, srv)
	knownCounts := map[string]int{}
	for k, v := range known {
		knownCounts[k] = v
		known[k] = 0
	}

	w = idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "unknown834@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	expectNoSMTPMessage(t, srv)
	for _, tb := range tables {
		if known[tb] != knownCounts[tb] {
			t.Errorf("Abfragen auf %s: bekannt %d, unbekannt %d (Timing-Orakel)", tb, knownCounts[tb], known[tb])
		}
	}
}

// Punkt 1/3: Scheitert die Kontosuche mit einem DB-Fehler, ist das kein
// "unbekannte Adresse" (sonst entstünde eine Einladung ohne Mail): 500.
func TestPageInvitationNachpruefung_UserLookupDBErrorIs500(t *testing.T) {
	f := newInvFixture(t)
	f.account(t, "lookup-err@x.example")
	// users-Abfragen: 1. Sitzungsprüfung (AuthMiddleware), 2. Anmeldung,
	// 3. isEffectiveGlobalAdmin, 4. Kontosuche.
	failNth(t, f.db, "users", 4)
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "lookup-err@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusInternalServerError)
	if f.invitationCount() != 0 {
		t.Error("Einladung trotz Fehler bei der Kontosuche angelegt")
	}
}

// Punkt 3: Der Betreff mit Umlauten im Seitennamen ist RFC-2047-kodiert.
func TestPageInvitationNachpruefung_MailSubjectEncodedForUmlauts(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.db.Model(&f.page).Update("name", "Müller Größe")
	f.account(t, "umlaut@x.example")
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "umlaut@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	msg := awaitSMTPMessage(t, srv)
	var subject string
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "Subject:") {
			subject = line
		}
	}
	if !strings.HasPrefix(subject, "Subject: =?utf-8?q?") {
		t.Errorf("Betreff nicht RFC-2047-kodiert: %q", subject)
	}
	if strings.ContainsAny(subject, "üöß") {
		t.Errorf("Betreff enthält rohe Umlaute: %q", subject)
	}
}

// H3: Wird das Konto zwischen Vorprüfung und Transaktion zum Global Admin bzw.
// bekommt eine SSO-Verknüpfung, greift die Ein-Seiten-Grenze in der Transaktion.
func TestPageInvitationNachpruefung_AcceptRechecksAccountInTransaction(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	cases := map[string]string{
		"Global Admin": "UPDATE users SET is_global_admin = 1 WHERE id = ?",
		"SSO":          "UPDATE users SET sso_provider = 'okta', sso_subject = 's' WHERE id = ?",
	}
	for name, stmt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "h3-"+strings.ReplaceAll(strings.ToLower(name), " ", "")+"@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			armed := true
			cb := "h3834" + name
			_ = f.db.Callback().Delete().Before("gorm:delete").Register(cb, func(tx *gorm.DB) {
				if !armed || tx.Statement.Table != "page_invitations" {
					return
				}
				armed = false
				f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec(stmt, u.ID)
			})
			t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(cb) })
			w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusConflict)
			requireErrorCode(t, w.Body.Bytes(), "invitation_account_conflict")
			if f.isMember(u.ID) {
				t.Fatal("Ein-Seiten-Grenze im Wettlauf umgangen")
			}
		})
	}
}

// F5: Verliert der Einladende seine Rechte oder wird die Rolle gelöscht,
// zwischen Vorprüfung und Transaktion, entsteht keine Mitgliedschaft.
func TestPageInvitationNachpruefung_AcceptRechecksRoleAndInviterInTransaction(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	cases := map[string]string{
		"Rolle gelöscht":         "UPDATE roles SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?",
		"Einladender entrechtet": "DELETE FROM user_status_page_roles WHERE user_id = (SELECT invited_by_id FROM page_invitations WHERE id > ? LIMIT 1)",
	}
	for name, stmt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "f5-"+strconv.Itoa(len(name))+"@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			arg := f.viewer.ID
			if strings.HasPrefix(name, "Einladender") {
				arg = 0
			}
			armed := true
			cb := "f5834" + name
			_ = f.db.Callback().Delete().Before("gorm:delete").Register(cb, func(tx *gorm.DB) {
				if !armed || tx.Statement.Table != "page_invitations" {
					return
				}
				armed = false
				f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec(stmt, arg)
			})
			t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(cb) })
			w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusNotFound)
			if f.isMember(u.ID) {
				t.Fatal("Mitgliedschaft trotz gelöschter Rolle bzw. entrechtetem Einladenden")
			}
			if n := auditCountFor(f.db, "statuspage.invitation_accepted"); n != 0 {
				t.Errorf("Audit trotz abgelehnter Annahme: %d", n)
			}
		})
	}
}

// O2: Scheitert die Prüfung "nur global vergebbare Rolle" mit einem DB-Fehler,
// wird gesperrt (500), nicht durchgelassen. Die zweite roles-Abfrage ist sie.
func TestPageInvitationNachpruefung_GlobalOnlyRoleCheckFailsClosed(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Run("Vorschau der Einladung", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "o2@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		failNth(t, f.db, "roles", 2)
		w := idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusInternalServerError)
	})
	t.Run("Einladen als Seitenadmin", func(t *testing.T) {
		f := newInvFixture(t)
		f.account(t, "o2b@x.example")
		// roles-Abfragen: 1. Route, 2. gewählte Rolle, 3. Rechte des Aufrufers (Preload), 4. "nur global".
		failNth(t, f.db, "roles", 4)
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "o2b@x.example", "role_id": f.viewer.ID})
		requireStatusCode(t, w, http.StatusInternalServerError)
		if f.invitationCount() != 0 {
			t.Error("Einladung trotz Fehler der Rollenprüfung")
		}
	})
	t.Run("Zuweisen als Global Admin", func(t *testing.T) {
		f := newInvFixture(t)
		u, _ := f.account(t, "o2c@x.example")
		// roles-Abfragen: 1. gewählte Rolle, 2. pageRolesAssignable (Global Admin: keine Routenprüfung).
		failNth(t, f.db, "roles", 2)
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.gaTok, map[string]any{"user_id": u.ID, "role_id": f.viewer.ID})
		if w.Code == http.StatusOK || w.Code == http.StatusCreated {
			t.Fatalf("Rolle trotz Fehler der Rollenprüfung vergeben: %d", w.Code)
		}
		if f.isMember(u.ID) {
			t.Fatal("Mitgliedschaft trotz Fehler der Rollenprüfung")
		}
	})
}

// H5: Scheitert das Laden der Rollennamen, antwortet die Liste mit 500 statt
// mit leeren Rollennamen.
func TestPageInvitationNachpruefung_ListRoleNamesErrorIs500(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	u, _ := f.account(t, "h5@x.example")
	f.invite(t, srv, u.Email, f.viewer)
	// Die erste roles-Abfrage gehört der Rechteprüfung der Route, die zweite den Rollennamen.
	failNth(t, f.db, "roles", 2)
	w := idorRequest(t, f.r, "GET", f.invPath(), f.ownerTok, nil)
	requireStatusCode(t, w, http.StatusInternalServerError)
}

// F6: Eine Einladung mit inzwischen gelöschter Rolle kann der Seitenadmin
// zurückziehen (vorher 403); ein anderer Lesefehler der Rolle ist ein 500.
func TestPageInvitationNachpruefung_RevokeWithDeletedOrUnreadableRole(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Run("Rolle gelöscht -> zurückgezogen", func(t *testing.T) {
		f := newInvFixture(t)
		u, _ := f.account(t, "f6@x.example")
		f.invite(t, srv, u.Email, f.viewer)
		inv := f.onlyInvitation(t)
		f.db.Delete(&f.viewer)
		w := idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil)
		requireStatusCode(t, w, http.StatusOK)
		if f.openInvitations() != 0 {
			t.Error("Einladung nicht zurückgezogen")
		}
	})
	t.Run("Lesefehler der Rolle -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, _ := f.account(t, "f6b@x.example")
		f.invite(t, srv, u.Email, f.viewer)
		inv := f.onlyInvitation(t)
		// Erste roles-Abfrage: Rechteprüfung der Route; zweite: Rolle der Einladung.
		failNth(t, f.db, "roles", 2)
		w := idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		if f.openInvitations() != 1 {
			t.Error("Einladung trotz Fehler zurückgezogen")
		}
	})
}

// H8: expires_at ist für das Aufräumen indiziert.
func TestPageInvitationNachpruefung_ExpiresAtIndexed(t *testing.T) {
	db := testutils.SetupTestDB()
	if !db.Migrator().HasIndex(&models.PageInvitation{}, "ExpiresAt") {
		t.Error("Index auf page_invitations.expires_at fehlt")
	}
}

// H4/F7: Der Datenexport nennt nur offene, dem eigenen Konto zugeordnete
// Einladungen, lädt die Seitennamen mit einer Abfrage und meldet DB-Fehler.
func TestPageInvitationNachpruefung_ExportInvitations(t *testing.T) {
	setup := func(t *testing.T) (*gorm.DB, http.Handler, models.User, string) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)
		r := testutils.SetupRouter()
		g := r.Group("/api", middleware.AuthMiddleware())
		g.GET("/me/export", h.MeExportHandler)
		u := models.User{Email: "Exp834@X.example"}
		db.Create(&u)
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		return db, r, u, tok
	}
	mk := func(db *gorm.DB, pageID uint, email, hash string, userID uint, exp time.Time) {
		db.Create(&models.PageInvitation{StatusPageID: pageID, Email: email, RoleID: 1, TokenHash: hash, ExpiresAt: exp, InvitedByID: 1, InvitedUserID: userID})
	}

	t.Run("nur offene Einladungen an das eigene Konto", func(t *testing.T) {
		db, r, u, tok := setup(t)
		var pages []models.StatusPage
		for i, n := range []string{"Offen A", "Offen B", "Abgelaufen", "Ohne Konto"} {
			p := models.StatusPage{Name: n, Slug: "exp834-" + strconv.Itoa(i)}
			db.Create(&p)
			pages = append(pages, p)
		}
		future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
		mk(db, pages[0].ID, "exp834@x.example", "h1", u.ID, future)
		mk(db, pages[1].ID, "exp834@x.example", "h2", u.ID, future)
		mk(db, pages[2].ID, "exp834@x.example", "h3", u.ID, past)
		mk(db, pages[3].ID, "exp834@x.example", "h4", 0, future)
		queries := countQueries(t, db, "status_pages")
		w := idorRequest(t, r, "GET", "/api/me/export", tok, nil)
		requireStatusCode(t, w, http.StatusOK)
		var out struct {
			Invitations []struct {
				StatusPageName string `json:"status_page_name"`
			} `json:"invitations"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Invitations) != 2 {
			t.Fatalf("erwartet 2 offene Einladungen, erhalten %s", w.Body.String())
		}
		for _, inv := range out.Invitations {
			if inv.StatusPageName != "Offen A" && inv.StatusPageName != "Offen B" {
				t.Errorf("unerwartete Einladung im Export: %q", inv.StatusPageName)
			}
		}
		// Rollen-Preload des Exports (leer) zählt nicht; Seitennamen der Einladungen: eine Abfrage.
		if queries["status_pages"] > 1 {
			t.Errorf("N+1: %d Abfragen auf status_pages", queries["status_pages"])
		}
	})

	for _, table := range []string{"page_invitations", "user_status_page_roles", "subscribers", "audit_logs"} {
		t.Run("DB-Fehler auf "+table+" -> 500", func(t *testing.T) {
			db, r, _, tok := setup(t)
			failOn(t, db, "query", table, nil)
			w := idorRequest(t, r, "GET", "/api/me/export", tok, nil)
			requireStatusCode(t, w, http.StatusInternalServerError)
		})
	}
	t.Run("DB-Fehler auf status_pages der Einladungen -> 500", func(t *testing.T) {
		db, r, u, tok := setup(t)
		p := models.StatusPage{Name: "P", Slug: "exp834-p"}
		db.Create(&p)
		mk(db, p.ID, "exp834@x.example", "h9", u.ID, time.Now().Add(time.Hour))
		failOn(t, db, "query", "status_pages", nil)
		w := idorRequest(t, r, "GET", "/api/me/export", tok, nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
	})
}

// O2/F5 (Abdeckung): Eine Einladung auf die globale Rolle "Global Admin" ist
// gegenstandslos, und DB-Fehler bei der erneuten Prüfung in der Annahme-
// Transaktion enden fail closed mit 500 (Einladung bleibt, keine Mitgliedschaft).
func TestPageInvitationNachpruefung_AcceptTransactionFailsClosedAndGlobalAdminRole(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Run("Global-Admin-Rolle -> 404", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "gar834@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		ga := models.Role{Name: models.GlobalAdminRoleName, Permissions: "incidents:read"}
		f.db.Create(&ga)
		f.db.Model(&models.PageInvitation{}).Where("1 = 1").Update("role_id", ga.ID)
		w := idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
	})
	for _, table := range []string{"users", "status_pages"} {
		t.Run("DB-Fehler auf "+table+" in der Transaktion -> 500", func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "txerr-"+table+"@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			inTx := false
			cb := "txerr834" + table
			_ = f.db.Callback().Delete().After("gorm:delete").Register(cb, func(tx *gorm.DB) {
				if tx.Statement.Table == "page_invitations" {
					inTx = true
				}
			})
			t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(cb) })
			failOn(t, f.db, "query", table, func(*gorm.DB) bool { return inTx })
			w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusInternalServerError)
			if f.openInvitations() != 1 || f.isMember(u.ID) {
				t.Fatal("Einladung verbraucht oder Mitgliedschaft trotz DB-Fehler")
			}
		})
	}
}

// Review-Fund 5: Scheitert die Mitgliedschaftsabfrage der Kontosuche, gilt nicht
// "kein Mitglied" (fail closed): 500, keine Einladung, keine Mail.
func TestPageInvitationNachpruefung_MembershipCountErrorIs500(t *testing.T) {
	f := newInvFixture(t)
	f.account(t, "count-err@x.example")
	// user_status_page_roles-Abfragen: 1. Route, 2. Rechte des Aufrufers, 3. Rolle auf anderer Seite, 4. Mitgliedschaft.
	failNth(t, f.db, "user_status_page_roles", 4)
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "count-err@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusInternalServerError)
	if f.invitationCount() != 0 {
		t.Error("Einladung trotz Fehler der Mitgliedschaftsabfrage angelegt")
	}
}
