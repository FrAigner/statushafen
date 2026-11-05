package handlers_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// ── Review-Nachbesserung 2: F1–F4 ──────────────────────────────────

// F1: Zieht ein Admin die Einladung zwischen Vorprüfung und Transaktion zurück
// (Grabstein), darf die Annahme nicht mehr durchgehen. Der Verbrauch löschte
// früher Unscoped nach ID und traf auch den Grabstein (RowsAffected = 1).
func TestPageInvitationReview_AcceptLosesAgainstConcurrentRevoke(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	u, tok := f.account(t, "race@x.example")
	token := f.invite(t, srv, u.Email, f.viewer)
	armed := true
	name := "revrace799"
	_ = f.db.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "page_invitations" {
			return
		}
		armed = false
		s := f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
		s.Exec("UPDATE page_invitations SET deleted_at = CURRENT_TIMESTAMP")
	})
	t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
	w := idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": token})
	requireStatusCode(t, w, http.StatusNotFound)
	if f.isMember(u.ID) {
		t.Fatal("zurückgezogene Einladung wurde angenommen")
	}
	if n := auditCountFor(f.db, "statuspage.invitation_accepted"); n != 0 {
		t.Errorf("Audit trotz abgelehnter Annahme: %d", n)
	}
}

// O1: Verbraucht ein gleichzeitiges Annehmen die Einladung zwischen Laden und
// Zurückziehen, trifft das Löschen 0 Zeilen: 404 ohne Audit statt 200.
func TestPageInvitationReview_RevokeLosesAgainstConcurrentAccept(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	u, _ := f.account(t, "o1@x.example")
	f.invite(t, srv, u.Email, f.viewer)
	inv := f.onlyInvitation(t)
	armed := true
	name := "revokerace799"
	_ = f.db.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "page_invitations" {
			return
		}
		armed = false
		f.db.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).Exec("DELETE FROM page_invitations")
	})
	t.Cleanup(func() { _ = f.db.Callback().Delete().Remove(name) })
	w := idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil)
	requireStatusCode(t, w, http.StatusNotFound)
	if auditCountFor(f.db, "statuspage.invitation_revoked") != 0 {
		t.Error("Audit für nicht erfolgtes Zurückziehen")
	}
}

// openInvitations zählt nicht weich gelöschte Einladungen.
func (f invFixture) openInvitations() int64 {
	var n int64
	f.db.Model(&models.PageInvitation{}).Count(&n)
	return n
}

// F2: Ein vorübergehender DB-Fehler darf eine gültige Einladung nicht
// verbrauchen: 500, kein Schreibzugriff, kein Audit. Nur "nicht gefunden" ist
// gegenstandslos (404 und Grabstein).
func TestPageInvitationReview_TransientDBErrorsFailClosed(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	cases := []struct {
		name, path, table string
		afterPage         bool // Fehler erst, nachdem invitationContext die Seite geladen hat
	}{
		{"Vorschau: Seite", "preview", "status_pages", false},
		{"Annehmen: Seite", "accept", "status_pages", false},
		{"Vorschau: Rolle", "preview", "roles", false},
		{"Vorschau: Einladender", "preview", "users", true},
		{"Annehmen: Einladender", "accept", "users", true},
		{"Vorschau: Rolle des Einladenden", "preview", "user_status_page_roles", true},
		{"Annehmen: Rolle des Einladenden", "accept", "user_status_page_roles", true},
		{"Vorschau: Token-Abfrage", "preview", "page_invitations", false},
		{"Ablehnen: Token-Abfrage", "decline", "page_invitations", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "trans@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			pageLoaded := !tc.afterPage
			if tc.afterPage {
				cb := "pageLoaded799"
				_ = f.db.Callback().Query().After("gorm:query").Register(cb, func(tx *gorm.DB) {
					if tx.Statement.Table == "status_pages" {
						pageLoaded = true
					}
				})
				t.Cleanup(func() { _ = f.db.Callback().Query().Remove(cb) })
			}
			once := true
			failOn(t, f.db, "query", tc.table, func(*gorm.DB) bool {
				if !pageLoaded || !once {
					return false
				}
				once = false
				return true
			})
			w := idorRequest(t, f.r, "POST", "/api/invitations/"+tc.path, tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusInternalServerError)
			if f.openInvitations() != 1 {
				t.Fatal("Einladung durch transienten DB-Fehler verbraucht")
			}
			if f.isMember(u.ID) {
				t.Fatal("Mitgliedschaft trotz DB-Fehler")
			}
			for _, a := range []string{"statuspage.invitation_accepted", "statuspage.invitation_declined"} {
				if n := auditCountFor(f.db, a); n != 0 {
					t.Errorf("Audit %s trotz Fehler", a)
				}
			}
		})
	}

	// Konto-Abfrage in loadOwnInvitation: Der erste users-Zugriff gehört ggf. der
	// Anmeldung, deshalb den ersten scheitern lassen, der nach dem Token kommt.
	for _, path := range []string{"preview", "accept"} {
		t.Run(path+": Konto-Abfrage", func(t *testing.T) {
			f := newInvFixture(t)
			u, tok := f.account(t, "trans-user@x.example")
			token := f.invite(t, srv, u.Email, f.viewer)
			tokenLoaded := false
			cb := "tokenLoaded799"
			_ = f.db.Callback().Query().After("gorm:query").Register(cb, func(tx *gorm.DB) {
				if tx.Statement.Table == "page_invitations" {
					tokenLoaded = true
				}
			})
			t.Cleanup(func() { _ = f.db.Callback().Query().Remove(cb) })
			once := true
			failOn(t, f.db, "query", "users", func(*gorm.DB) bool {
				if !tokenLoaded || !once {
					return false
				}
				once = false
				return true
			})
			w := idorRequest(t, f.r, "POST", "/api/invitations/"+path, tok, map[string]any{"token": token})
			requireStatusCode(t, w, http.StatusInternalServerError)
			if f.openInvitations() != 1 || f.isMember(u.ID) {
				t.Fatal("Einladung verbraucht oder Mitgliedschaft trotz DB-Fehler")
			}
		})
	}

	t.Run("Zurückziehen: Laden der Einladung scheitert -> 500", func(t *testing.T) {
		f := newInvFixture(t)
		u, _ := f.account(t, "revoke-err@x.example")
		f.invite(t, srv, u.Email, f.viewer)
		inv := f.onlyInvitation(t)
		failOn(t, f.db, "query", "page_invitations", nil)
		w := idorRequest(t, f.r, "DELETE", f.invPath()+"/"+strconv.Itoa(int(inv.ID)), f.ownerTok, nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		if auditCountFor(f.db, "statuspage.invitation_revoked") != 0 {
			t.Error("Audit trotz Fehler")
		}
	})
	t.Run("Zurückziehen: unbekannte ID bleibt 404", func(t *testing.T) {
		f := newInvFixture(t)
		w := idorRequest(t, f.r, "DELETE", f.invPath()+"/9999", f.ownerTok, nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	// Gegenproben: "nicht gefunden" bleibt gegenstandslos (404, Grabstein).
	t.Run("Einladender ohne Rechte: 404 und Grabstein", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "gone@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		f.db.Where("user_id = ?", f.owner.ID).Delete(&models.UserStatusPageRole{})
		w := idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token})
		requireStatusCode(t, w, http.StatusNotFound)
		if f.openInvitations() != 0 {
			t.Error("gegenstandslose Einladung nicht weich gelöscht")
		}
	})
	t.Run("Einladender gelöscht: 404 und Grabstein", func(t *testing.T) {
		f := newInvFixture(t)
		u, tok := f.account(t, "gone2@x.example")
		token := f.invite(t, srv, u.Email, f.viewer)
		f.db.Delete(&f.owner)
		requireStatusCode(t, idorRequest(t, f.r, "POST", "/api/invitations/preview", tok, map[string]any{"token": token}), http.StatusNotFound)
		if f.openInvitations() != 0 {
			t.Error("gegenstandslose Einladung nicht weich gelöscht")
		}
	})
}

// F4: Ein DB-Fehler beim Prüfen der alten Rolle macht die alte Einladung nicht
// ersetzbar (fail closed: 500, die alte Einladung bleibt).
func TestPageInvitationReview_ReinviteRoleLookupErrorFailsClosed(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	u, _ := f.account(t, "reinvite-err@x.example")
	f.invite(t, srv, u.Email, f.viewer)
	backdateInvitations(f.db, 31*time.Minute)
	old := f.onlyInvitation(t)
	listed := false
	cb := "listed799"
	_ = f.db.Callback().Query().After("gorm:query").Register(cb, func(tx *gorm.DB) {
		if tx.Statement.Table == "page_invitations" {
			listed = true
		}
	})
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(cb) })
	failOn(t, f.db, "query", "roles", func(*gorm.DB) bool { return listed })
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": u.Email, "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusInternalServerError)
	expectNoSMTPMessage(t, srv)
	if got := f.onlyInvitation(t); got.ID != old.ID || got.TokenHash != old.TokenHash {
		t.Errorf("alte Einladung trotz DB-Fehler ersetzt: %+v", got)
	}
}

// F4 (Gegenprobe): Ist die Rolle der alten Einladung gelöscht ("nicht gefunden"),
// bleibt die alte Einladung ersetzbar.
func TestPageInvitationReview_ReinviteWithDeletedOldRoleReplaces(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	u, _ := f.account(t, "reinvite-gone@x.example")
	oldToken := f.invite(t, srv, u.Email, f.viewer)
	backdateInvitations(f.db, 31*time.Minute)
	f.db.Delete(&f.viewer)
	newToken := f.invite(t, srv, u.Email, f.ownerRol)
	if newToken == oldToken {
		t.Error("alte Einladung nicht ersetzt")
	}
	if got := f.onlyInvitation(t); got.RoleID != f.ownerRol.ID {
		t.Errorf("neue Rolle nicht gespeichert: %+v", got)
	}
}

// F3: Scheitert das Sperren der Seitenzeile, endet das Einladen fail closed.
func TestPageInvitationReview_PageLockErrorFailsClosed(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "lock-err@x.example")
	// Nur die Abfrage mit FOR UPDATE (Sperre der Seitenzeile) scheitert.
	failOn(t, f.db, "query", "status_pages", func(tx *gorm.DB) bool {
		_, locking := tx.Statement.Clauses["FOR"]
		return locking
	})
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "lock-err@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusInternalServerError)
	expectNoSMTPMessage(t, srv)
	if f.invitationCount() != 0 {
		t.Error("Einladung trotz Fehler angelegt")
	}
}

// F3: Zwei gleichzeitige Einladungen derselben Adresse: Die zweite scheitert am
// Unique-Index (status_page_id, email) und antwortet neutral, ohne Mail und
// ohne Audit.
func TestPageInvitationReview_ConcurrentInviteSameAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "twice@x.example")
	armed := true
	name := "concurrent799"
	_ = f.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "page_invitations" {
			return
		}
		armed = false
		// Der "gleichzeitige" Aufruf hat die Adresse zwischen Prüfung und Insert
		// angelegt. SQLite sperrt die Tabelle für eine zweite Verbindung, deshalb
		// schreibt der Test in dieselbe Transaktion; das Create darunter scheitert
		// dann wie unter Postgres am Unique-Index. (Die Zeile rollt mit zurück,
		// deshalb gibt es unten keine Zeilenzahl zu prüfen.)
		err := tx.Session(&gorm.Session{NewDB: true}).Exec("INSERT INTO page_invitations (created_at, updated_at, status_page_id, email, role_id, token_hash, expires_at, invited_by_id, invited_user_id) VALUES (CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?, 'konkurrent', ?, ?, 0)",
			f.page.ID, "twice@x.example", f.viewer.ID, time.Now().Add(time.Hour), f.owner.ID).Error
		if err != nil {
			t.Errorf("konkurrierender Insert: %v", err)
		}
	})
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(name) })
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "twice@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusOK)
	expectNoSMTPMessage(t, srv)
	if n := auditCountFor(f.db, "statuspage.invitation_created"); n != 0 {
		t.Errorf("Audit für die unterlegene Einladung: %d", n)
	}
}

// F3: Der Audit-Eintrag, aus dem die Tagesgrenze zählt, entsteht in derselben
// Transaktion wie die Einladung (sonst sieht eine zweite Transaktion zwischen
// Commit und Audit noch den alten Zähler). Scheitert er, entsteht keine Einladung.
func TestPageInvitationReview_AuditInSameTransaction(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixture(t)
	f.account(t, "audit-tx@x.example")
	failOn(t, f.db, "create", "audit_logs", nil)
	w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": "audit-tx@x.example", "role_id": f.viewer.ID})
	requireStatusCode(t, w, http.StatusInternalServerError)
	expectNoSMTPMessage(t, srv)
	if f.invitationCount() != 0 {
		t.Error("Einladung ohne Audit-Eintrag angelegt")
	}
}
