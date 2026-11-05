package handlers_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Ohne aktive Mitgliedschaft des Ziels darf ein Seiten-Owner
// über DELETE /api/admin/pages/:p/users/:uid keine Seiten-SSO-Verknüpfungen
// fremder Konten löschen (es gibt keine Rolle, gegen die seine Rechte geprüft
// würden). 404 wie bei einem unbekannten Konto, nichts wird gelöscht, kein
// Audit-Eintrag.
func TestRemoveStatusPageUser_NonMemberOwnerGets404(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	stranger, _ := p.user("stranger@x.example", false)
	p.identity(stranger, p.pageA, "sub-stranger") // Verknüpfung ohne Rolle

	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(stranger.ID)), tok, nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "user_not_found") {
		t.Fatalf("erwartet 404 user_not_found, war %d %s", w.Code, w.Body.String())
	}
	if p.identities(stranger, p.pageA) != 1 {
		t.Error("fremde Verknüpfung ohne Mitgliedschaft darf der Owner nicht löschen")
	}
	var n int64
	p.db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_removed").Count(&n)
	if n != 0 {
		t.Errorf("kein removed-Audit bei Ablehnung, waren %d", n)
	}
}

// Ein Global Admin darf verwaiste Verknüpfungen ohne
// Mitgliedschaft weiter entfernen; der Audit-Eintrag nennt die Anzahl.
func TestRemoveStatusPageUser_GlobalAdminRemovesOrphanIdentity(t *testing.T) {
	p := setupPageUsers(t)
	_, gaTok := p.user("ga@x.example", true)
	stranger, _ := p.user("stranger@x.example", false)
	p.identity(stranger, p.pageA, "sub-stranger")
	p.identity(stranger, p.pageB, "sub-b") // andere Seite: bleibt

	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(stranger.ID)), gaTok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("erwartet 200, war %d %s", w.Code, w.Body.String())
	}
	if p.identities(stranger, p.pageA) != 0 || p.identities(stranger, p.pageB) != 1 {
		t.Error("nur die Verknüpfung auf A darf gelöscht werden")
	}
	requireRemovedAudit(t, p, "sso_identities_removed=1")
}

// Beim normalen Entfernen eines Mitglieds nennt der
// Audit-Eintrag die mitgelöschte Verknüpfung (bzw. 0, wenn keine bestand).
func TestRemoveStatusPageUser_AuditCountsSSOIdentities(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	linked, _ := p.user("linked@x.example", false)
	p.member(linked, p.pageA, p.viewer)
	p.identity(linked, p.pageA, "sub-linked")
	plain, _ := p.user("plain@x.example", false)
	p.member(plain, p.pageA, p.viewer)

	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(linked.ID)), tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Entfernen: %d %s", w.Code, w.Body.String())
	}
	requireRemovedAudit(t, p, "role_id="+strconv.Itoa(int(p.viewer.ID))+" sso_identities_removed=1")

	p.db.Where("1 = 1").Delete(&models.AuditLog{})
	w = idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(plain.ID)), tok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Entfernen: %d %s", w.Code, w.Body.String())
	}
	requireRemovedAudit(t, p, "sso_identities_removed=0")
}

func requireRemovedAudit(t *testing.T, p *pageUsers, want string) {
	t.Helper()
	var logs []models.AuditLog
	p.db.Where("action = ?", "statuspage.user_role_removed").Find(&logs)
	if len(logs) != 1 || !strings.Contains(logs[0].Details, want) {
		t.Fatalf("Audit soll %q enthalten: %+v", want, logs)
	}
}

// Review: Keine Aufzählung von Konten. Unbekanntes Konto,
// existierendes Nicht-Mitglied und Konto mit nur soft-gelöschter
// Mitgliedszeile liefern einem Nicht-Global-Admin denselben Status und Body.
func TestRemoveStatusPageUser_NoEnumeration(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	stranger, _ := p.user("stranger@x.example", false)
	former, _ := p.user("former@x.example", false)
	p.member(former, p.pageA, p.viewer)
	p.db.Where("user_id = ?", former.ID).Delete(&models.UserStatusPageRole{}) // soft
	p.identity(former, p.pageA, "sub-former")

	var bodies []string
	for _, uid := range []uint{999999, stranger.ID, former.ID} {
		w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(uid)), tok, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("uid %d: erwartet 404, war %d %s", uid, w.Code, w.Body.String())
		}
		bodies = append(bodies, w.Body.String())
	}
	if bodies[0] != bodies[1] || bodies[1] != bodies[2] || !strings.Contains(bodies[0], `"user_not_found"`) {
		t.Fatalf("Antworten unterscheiden sich: %q", bodies)
	}
	if p.identities(former, p.pageA) != 1 {
		t.Error("Verknüpfung des früheren Mitglieds darf der Owner nicht löschen")
	}
}

// Review, TOCTOU: Verschwindet die Mitgliedschaft zwischen
// Prüfung und Löschen (paralleles Entfernen), rollt der Handler für einen
// Nicht-Global-Admin zurück und antwortet 404; die Verknüpfung bleibt, kein Audit.
func TestRemoveStatusPageUser_MembershipVanishesBeforeDelete(t *testing.T) {
	p := setupPageUsers(t)
	owner, tok := p.user("owner@x.example", false)
	p.member(owner, p.pageA, p.ownerRol)
	victim, _ := p.user("victim@x.example", false)
	p.member(victim, p.pageA, p.viewer)
	p.identity(victim, p.pageA, "sub-victim")

	// Nach dem Nachschlagen der Zielmitgliedschaft (erste Abfrage mit der
	// Ziel-ID) entfernt ein "paralleler" Aufruf die Zeile.
	fired := false
	victimID := fmt.Sprint(victim.ID)
	if err := p.db.Callback().Query().After("gorm:query").Register("vanish821", func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "user_status_page_roles" || len(tx.Statement.Vars) == 0 || fmt.Sprint(tx.Statement.Vars[0]) != victimID {
			return
		}
		fired = true
		p.db.Session(&gorm.Session{NewDB: true}).Exec("DELETE FROM user_status_page_roles WHERE user_id = ?", victim.ID)
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.db.Callback().Query().Remove("vanish821") }()

	w := idorRequest(t, p.r, "DELETE", p.base(p.pageA)+"/"+strconv.Itoa(int(victim.ID)), tok, nil)
	if !fired {
		t.Fatal("Vorbedingung: Rennen wurde nicht ausgelöst")
	}
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "user_not_found") {
		t.Fatalf("erwartet 404 user_not_found, war %d %s", w.Code, w.Body.String())
	}
	if p.identities(victim, p.pageA) != 1 {
		t.Error("Transaktion muss zurückgerollt sein, die Verknüpfung bleibt")
	}
	var n int64
	p.db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_removed").Count(&n)
	if n != 0 {
		t.Errorf("kein removed-Audit bei Ablehnung, waren %d", n)
	}
}
