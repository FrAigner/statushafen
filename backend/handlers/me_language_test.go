package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// TestUpdateMeHandler prüft (i18n-Infrastruktur), dass der
// PATCH-artige Endpunkt an /api/me die bevorzugte Sprache eines eingeloggten
// Users korrekt in der DB persistiert und in der Antwort zurückliefert.
func TestUpdateMeHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	// Mock der JWT-Middleware, analog zu den anderen /api/me-Tests: userID wird
	// direkt über einen Test-Header statt einem echten Token gesetzt.
	r.Use(mockUserIDMiddleware)

	r.GET("/api/me", h.MeHandler)
	r.PATCH("/api/me", h.UpdateMeHandler)

	testUser := models.User{
		Email: "lang_test@example.com",
	}
	db.Create(&testUser)

	t.Run("PATCH /api/me speichert PreferredLanguage", func(t *testing.T) {
		payload := map[string]string{"preferred_language": "de"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(testUser.ID)))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response body: %v", err)
		}
		if resp["preferred_language"] != "de" {
			t.Errorf("Expected preferred_language 'de' in response, got %q", resp["preferred_language"])
		}

		// In der DB nachschauen: Persistenz muss über Requests hinweg bestehen bleiben.
		var reloaded models.User
		if err := db.First(&reloaded, testUser.ID).Error; err != nil {
			t.Fatalf("Failed to reload user: %v", err)
		}
		if reloaded.PreferredLanguage != "de" {
			t.Errorf("Expected PreferredLanguage 'de' in DB, got %q", reloaded.PreferredLanguage)
		}
	})

	t.Run("GET /api/me liefert die gespeicherte Sprache mit", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me", nil)
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(testUser.ID)))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response body: %v", err)
		}
		if resp["preferred_language"] != "de" {
			t.Errorf("Expected preferred_language 'de', got %v", resp["preferred_language"])
		}
	})

	t.Run("PATCH /api/me ohne Auth gibt 401", func(t *testing.T) {
		payload := map[string]string{"preferred_language": "en"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("PATCH /api/me ohne preferred_language gibt 400", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBuffer([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(testUser.ID)))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

func patchMeLanguage(t *testing.T, h *handlers.Handler, userID uint, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := testutils.SetupRouter()
	r.Use(mockUserIDMiddleware)
	r.PATCH("/api/me", h.UpdateMeHandler)
	req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(userID)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// PATCH /api/me muss für alle Kontoarten 200 liefern und darf
// ausschließlich die Spalte preferred_language schreiben (kein Save des
// ganzen Users, der an Constraints anderer Spalten scheitern kann).
func TestUpdateMeHandler_AlleKontoarten(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	role := models.Role{Name: "editor387"}
	db.Create(&role)
	users := map[string]models.User{
		"globaladmin": {Email: "ga387@example.com", IsGlobalAdmin: true},
		"sso":         {Email: "sso387@example.com", SSOProvider: "oidc", SSOSubject: "sub-387"},
		"seitenadmin": {Email: "sa387@example.com", RoleID: role.ID},
		"normal":      {Email: "user387@example.com", PasswordHash: "hash"},
	}
	for name, u := range users {
		t.Run(name, func(t *testing.T) {
			u.PreferredLanguage = "de"
			db.Create(&u)
			var before models.User
			db.First(&before, u.ID)

			w := patchMeLanguage(t, h, u.ID, `{"preferred_language":"en"}`)
			requireStatusCode(t, w, http.StatusOK)
			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp["preferred_language"] != "en" {
				t.Fatalf("unerwartete Antwort: %s", w.Body.String())
			}

			var after models.User
			db.First(&after, u.ID)
			if after.PreferredLanguage != "en" {
				t.Errorf("PreferredLanguage in DB = %q, erwartet en", after.PreferredLanguage)
			}
			// Nur preferred_language darf sich ändern.
			after.PreferredLanguage, after.UpdatedAt = before.PreferredLanguage, before.UpdatedAt
			if !reflect.DeepEqual(after, before) {
				t.Errorf("andere Felder wurden verändert: vorher %+v, nachher %+v", before, after)
			}
		})
	}
}

// Das Konto mit gesetztem SSO-Feld ohne Passwort/Rolle darf den
// Save nicht mehr über Zusatz-Spalten scheitern lassen; ein Update auf eine
// Spalte reicht auch dann, wenn andere Spalten in der DB nicht zum Modell
// passen (hier: Zeile mit einer Rolle, die es nicht mehr gibt).
func TestUpdateMeHandler_VerwaisteRolle(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "orphan387@example.com"}
	db.Create(&u)
	db.Exec("PRAGMA foreign_keys = OFF")
	db.Exec("UPDATE users SET role_id = 9999 WHERE id = ?", u.ID)

	w := patchMeLanguage(t, h, u.ID, `{"preferred_language":"en"}`)
	requireStatusCode(t, w, http.StatusOK)
}

func TestUpdateMeHandler_UngueltigeSprache(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "val387@example.com", PreferredLanguage: "de"}
	db.Create(&u)

	for name, body := range map[string]string{
		"fr":        `{"preferred_language":"fr"}`,
		"leer":      `{"preferred_language":""}`,
		"zu lang":   `{"preferred_language":"` + strings.Repeat("x", 5000) + `"}`,
		"grossbuch": `{"preferred_language":"EN"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := patchMeLanguage(t, h, u.ID, body)
			requireStatusCode(t, w, http.StatusBadRequest)
			if !strings.Contains(w.Body.String(), "Unsupported language") && !strings.Contains(w.Body.String(), "Invalid request") {
				t.Errorf("keine klare Fehlermeldung: %s", w.Body.String())
			}
			var after models.User
			db.First(&after, u.ID)
			if after.PreferredLanguage != "de" {
				t.Errorf("Sprache wurde trotz 400 geändert: %q", after.PreferredLanguage)
			}
		})
	}
}

func TestUpdateMeHandler_UnbekannterUser(t *testing.T) {
	h := handlers.NewHandler(testutils.SetupTestDB())
	w := patchMeLanguage(t, h, 424242, `{"preferred_language":"en"}`)
	requireStatusCode(t, w, http.StatusUnauthorized)
}

// Ein echter DB-Fehler bleibt 500, die Antwort verrät keine
// Interna (Tabellenname, SQL-Text).
func TestUpdateMeHandler_DBFehler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "dberr387@example.com"}
	db.Create(&u)
	db.Exec("CREATE TRIGGER fail387 BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'secret-internal-387'); END")

	w := patchMeLanguage(t, h, u.ID, `{"preferred_language":"en"}`)
	requireStatusCode(t, w, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "secret-internal-387") || strings.Contains(w.Body.String(), "users") {
		t.Errorf("Antwort verrät Interna: %s", w.Body.String())
	}
}
