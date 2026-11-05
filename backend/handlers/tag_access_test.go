package handlers

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
)

// testContextForUser builds a minimal gin.Context with "userID" set, as
// AuthMiddleware would after parsing a valid JWT - internal package test, so
// it can call the unexported Handler methods in tag_access.go directly
// instead of going through a full HTTP router.
func testContextForUser(userID uint) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if userID != 0 {
		c.Set("userID", userID)
	}
	return c
}

func newTagAccessTestHandler(t *testing.T) *Handler {
	t.Helper()
	db := testutils.SetupTestDB()
	return NewHandler(db)
}

// TestUserHasTagAccess covers the "tags are an optional, additional
// narrowing filter on top of RequirePermission/UserStatusPageRole, not a
// replacement for it" decision: the check must only actually
// restrict when BOTH the user and the resource carry at least one tag - a
// Page Admin/Editor without any tags assigned must not be locked out of
// resources they already have role-based write access to.
func TestUserHasTagAccess(t *testing.T) {
	h := newTagAccessTestHandler(t)

	page := models.StatusPage{Name: "Page", Slug: "tag-access-page"}
	h.DB.Create(&page)

	globalTag := models.Tag{Name: "global"}
	h.DB.Create(&globalTag)
	deTag := models.Tag{Name: "DE", StatusPageID: &page.ID}
	h.DB.Create(&deTag)
	usTag := models.Tag{Name: "US", StatusPageID: &page.ID}
	h.DB.Create(&usTag)

	globalAdmin := models.User{Email: "admin@example.com", IsGlobalAdmin: true}
	h.DB.Create(&globalAdmin)

	noTagsUser := models.User{Email: "no-tags@example.com"}
	h.DB.Create(&noTagsUser)

	deUser := models.User{Email: "de-user@example.com"}
	h.DB.Create(&deUser)
	h.DB.Create(&models.UserTag{UserID: deUser.ID, TagID: deTag.ID})

	globalTagUser := models.User{Email: "global-tag-user@example.com"}
	h.DB.Create(&globalTagUser)
	h.DB.Create(&models.UserTag{UserID: globalTagUser.ID, TagID: globalTag.ID})

	tests := []struct {
		name         string
		userID       uint
		resourceTags []models.Tag
		want         bool
	}{
		{"Global Admin darf immer, egal welche Tags", globalAdmin.ID, []models.Tag{usTag}, true},
		// Tags sperren nicht eigenständig aus - das ist Aufgabe von
		// RequirePermission/UserStatusPageRole (main.go), die diese Handler-Ebene
		// nie ohne vorherige Auth erreichen. userHasTagAccess behandelt "keine
		// Tags beim Aufrufer" konsistent für authentifizierte wie anonyme Caller.
		{"Anonym (userID 0) wird von der Tag-Prüfung NICHT eigenständig abgelehnt", 0, []models.Tag{usTag}, true},
		{"User ohne Tags darf Ressource ohne Tags verwalten", noTagsUser.ID, nil, true},
		{"User ohne Tags darf auch Ressource MIT Tags verwalten (Tags sind optional, kein Ausschluss)", noTagsUser.ID, []models.Tag{usTag}, true},
		{"User mit Tag darf Ressource ohne Tags verwalten", deUser.ID, nil, true},
		{"User mit DE-Tag gegen Ressource mit DE-Tag -> erlaubt", deUser.ID, []models.Tag{deTag}, true},
		{"User mit DE-Tag gegen Ressource mit US-Tag (keine Überschneidung) -> verweigert", deUser.ID, []models.Tag{usTag}, false},
		{"User mit globalem Tag gegen Ressource mit US-Tag -> erlaubt (Wildcard)", globalTagUser.ID, []models.Tag{usTag}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testContextForUser(tc.userID)
			got := h.userHasTagAccess(c, tc.resourceTags)
			if got != tc.want {
				t.Errorf("userHasTagAccess() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveTagsByIDs covers the Create/Update-time validation:
// tags must exist and be either global or scoped to the exact target status
// page - a tag from a DIFFERENT status page must be rejected, otherwise an
// admin with rights on page A could make a page-B resource visible via a
// tag that page B never granted.
func TestResolveTagsByIDs(t *testing.T) {
	h := newTagAccessTestHandler(t)

	pageA := models.StatusPage{Name: "Page A", Slug: "resolve-tags-a"}
	h.DB.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B", Slug: "resolve-tags-b"}
	h.DB.Create(&pageB)

	globalTag := models.Tag{Name: "global"}
	h.DB.Create(&globalTag)
	aTag := models.Tag{Name: "A-only", StatusPageID: &pageA.ID}
	h.DB.Create(&aTag)
	bTag := models.Tag{Name: "B-only", StatusPageID: &pageB.ID}
	h.DB.Create(&bTag)

	t.Run("leere IDs -> keine Tags, kein Fehler", func(t *testing.T) {
		tags, err := h.resolveTagsByIDs(nil, pageA.ID)
		if err != nil || len(tags) != 0 {
			t.Fatalf("expected no error and no tags, got tags=%v err=%v", tags, err)
		}
	})

	t.Run("globaler Tag ist für jede Statusseite gültig", func(t *testing.T) {
		tags, err := h.resolveTagsByIDs([]uint{globalTag.ID}, pageA.ID)
		if err != nil || len(tags) != 1 {
			t.Fatalf("expected global tag to resolve, got tags=%v err=%v", tags, err)
		}
	})

	t.Run("seiteneigener Tag ist gültig", func(t *testing.T) {
		tags, err := h.resolveTagsByIDs([]uint{aTag.ID}, pageA.ID)
		if err != nil || len(tags) != 1 {
			t.Fatalf("expected page A's own tag to resolve, got tags=%v err=%v", tags, err)
		}
	})

	t.Run("fremder Seiten-Tag wird abgelehnt", func(t *testing.T) {
		_, err := h.resolveTagsByIDs([]uint{bTag.ID}, pageA.ID)
		if err == nil {
			t.Fatal("expected an error when resolving page B's tag for page A, got nil")
		}
	})

	t.Run("nicht existierende ID wird abgelehnt", func(t *testing.T) {
		_, err := h.resolveTagsByIDs([]uint{999999}, pageA.ID)
		if err == nil {
			t.Fatal("expected an error for a non-existent tag ID, got nil")
		}
	})
}

// TestResolveGlobalTagsByIDs covers the User-specific variant:
// User-Tags sind bewusst seitenübergreifend, ein seitengebundener Tag darf
// einem User nicht zugewiesen werden können.
func TestResolveGlobalTagsByIDs(t *testing.T) {
	h := newTagAccessTestHandler(t)

	page := models.StatusPage{Name: "Page", Slug: "resolve-global-tags-page"}
	h.DB.Create(&page)

	globalTag := models.Tag{Name: "global"}
	h.DB.Create(&globalTag)
	scopedTag := models.Tag{Name: "scoped", StatusPageID: &page.ID}
	h.DB.Create(&scopedTag)

	t.Run("globaler Tag ist für User gültig", func(t *testing.T) {
		tags, err := h.resolveGlobalTagsByIDs([]uint{globalTag.ID})
		if err != nil || len(tags) != 1 {
			t.Fatalf("expected global tag to resolve, got tags=%v err=%v", tags, err)
		}
	})

	t.Run("seitengebundener Tag wird für User abgelehnt", func(t *testing.T) {
		_, err := h.resolveGlobalTagsByIDs([]uint{scopedTag.ID})
		if err == nil {
			t.Fatal("expected an error when assigning a page-scoped tag to a user, got nil")
		}
	})
}

// Die Fehlermeldung beim Zuweisen eines Tags nennt weder den Namen eines Tags
// einer anderen Statusseite noch unterscheidet sie sich von der Antwort auf
// eine unbekannte ID: Beides wäre ein Orakel über Tags fremder Mandanten.
func TestResolveTagsByIDs_FremderTagVerraetNichts(t *testing.T) {
	h := newTagAccessTestHandler(t)

	pageA := models.StatusPage{Name: "Page A", Slug: "leak-tags-a"}
	h.DB.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B", Slug: "leak-tags-b"}
	h.DB.Create(&pageB)
	foreign := models.Tag{Name: "Kunde-Geheimprojekt", StatusPageID: &pageB.ID}
	h.DB.Create(&foreign)

	describe := func(err error) (code, msg string) {
		var ce *codedError
		if !errors.As(err, &ce) {
			t.Fatalf("erwartet codedError, bekam %v", err)
		}
		return ce.code, ce.msg
	}

	_, errForeign := h.resolveTagsByIDs([]uint{foreign.ID}, pageA.ID)
	_, errUnknown := h.resolveTagsByIDs([]uint{999999}, pageA.ID)
	codeForeign, msgForeign := describe(errForeign)
	codeUnknown, msgUnknown := describe(errUnknown)

	if strings.Contains(msgForeign, foreign.Name) {
		t.Errorf("Meldung nennt den Tag-Namen eines fremden Mandanten: %q", msgForeign)
	}
	if codeForeign != codeUnknown || msgForeign != msgUnknown {
		t.Errorf("fremder Tag (%s, %q) und unbekannte ID (%s, %q) sind unterscheidbar", codeForeign, msgForeign, codeUnknown, msgUnknown)
	}

	// Gegenprobe: der eigene Tag der Seite lässt sich weiterhin zuweisen.
	own := models.Tag{Name: "eigen", StatusPageID: &pageA.ID}
	h.DB.Create(&own)
	if tags, err := h.resolveTagsByIDs([]uint{own.ID}, pageA.ID); err != nil || len(tags) != 1 {
		t.Fatalf("eigener Tag muss gültig bleiben, got tags=%v err=%v", tags, err)
	}

	// Auch die Variante für Benutzer-Tags nennt den Namen des Tags nicht.
	_, errBound := h.resolveGlobalTagsByIDs([]uint{foreign.ID})
	if _, msg := describe(errBound); strings.Contains(msg, foreign.Name) {
		t.Errorf("Meldung für Benutzer-Tags nennt den Tag-Namen: %q", msg)
	}
}
