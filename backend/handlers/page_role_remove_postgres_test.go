package handlers_test

// Role change (POST /api/admin/pages/:page_id/users on an
// existing member) concurrent with removing the member
// (DELETE .../users/:uid) on a REAL Postgres (READ COMMITTED). Previously
// the UPDATE of Save() waited for the removal's lock, then hit 0
// rows, and the upsert reset deleted_at: the removed membership
// came back with a 200 response. Like the other Postgres tests only with
// MIGRATION_PG_TEST=1 and not as root.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

func roleRemoveSetup(t *testing.T) (invFixture, *gorm.DB) {
	t.Helper()
	f, db := updSetup(t)
	h := handlers.NewHandler(db)
	f.r.DELETE("/api/admin/pages/:page_id/users/:uid", middleware.AuthMiddleware(), h.RemoveStatusPageUserHandler)
	return f, db
}

func activeMemberships(db *gorm.DB, uid uint) int64 {
	var n int64
	db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", uid).Count(&n)
	return n
}

// Deterministically interleaved: the "removal" holds the soft delete of the
// role row uncommitted, the role change runs into the row lock
// and is released only after the commit. Expectation: 404, no active membership.
func TestPageRoleChangePostgres_VsRemoveHeldLock(t *testing.T) {
	f, db := roleRemoveSetup(t)
	u, _ := f.account(t, "held856@x.example")
	if err := db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; err != nil {
		t.Fatal(err)
	}
	blocker := db.Begin()
	if err := blocker.Where("user_id = ? AND status_page_id = ?", u.ID, f.page.ID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
		t.Fatal(err)
	}
	res := updAsync(t, f, http.MethodPost, f.usersPath(), f.gaTok, map[string]any{"user_id": u.ID, "role_id": f.strong.ID})
	time.Sleep(500 * time.Millisecond)
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	r := updWait(t, "Rollenwechsel", res)
	if r.code != http.StatusNotFound || !strings.Contains(r.body, "user_not_found") {
		t.Errorf("erwartet 404 user_not_found, war %d %s", r.code, r.body)
	}
	if n := activeMemberships(db, u.ID); n != 0 {
		t.Errorf("%d aktive Mitgliedschaft(en) nach dem Entfernen (zurückgeholt)", n)
	}
}

// Real handlers against each other, many runs with staggered start: afterwards never an
// active membership (except after 201 = re-creation by the Global Admin), never 5xx (deadlock 40P01), no hang.
func TestPageRoleChangePostgres_VsRemoveRounds(t *testing.T) {
	f, db := roleRemoveSetup(t)
	const rounds = 60
	for i := 0; i < rounds; i++ {
		u, _ := f.account(t, fmt.Sprintf("rr%d@x.example", i))
		if err := db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		var cCode, dCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			cCode = idorRequest(t, f.r, http.MethodPost, f.usersPath(), f.gaTok, map[string]any{"user_id": u.ID, "role_id": f.strong.ID}).Code
		}()
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(time.Duration(i%8) * 700 * time.Microsecond)
			dCode = idorRequest(t, f.r, http.MethodDelete, f.usersPath()+"/"+strconv.Itoa(int(u.ID)), f.gaTok, nil).Code
		}()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatalf("Runde %d: hängt", i)
		}
		if dCode != http.StatusOK {
			t.Fatalf("Runde %d: Entfernen antwortet %d", i, dCode)
		}
		if cCode >= 500 {
			t.Fatalf("Runde %d: Rollenwechsel antwortet %d (Deadlock/Abbruch)", i, cCode)
		}
		// 201: The Global Admin re-added after the removal (legitimate,
		// no member found anymore). With 200/404 the member must be gone.
		if n := activeMemberships(db, u.ID); n != 0 && cCode != http.StatusCreated {
			t.Fatalf("Runde %d: %d aktive Mitgliedschaft(en) nach dem Entfernen (Rollenwechsel: %d)", i, n, cCode)
		}
	}
}
