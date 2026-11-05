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

	"github.com/gin-gonic/gin"
)

// Hilfe-Tooltips sind pro Benutzer serverseitig abschaltbar
// (User.HideHelpTooltips, PATCH /api/me {"help_tooltips": bool}), Default an.

func meJSON(t *testing.T, h *handlers.Handler, userID uint) map[string]any {
	t.Helper()
	r := testutils.SetupRouter()
	r.Use(mockUserIDMiddleware)
	r.GET("/api/me", h.MeHandler)
	req, _ := http.NewRequest("GET", "/api/me", nil)
	req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(userID)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	return resp
}

func loginJSON(t *testing.T, h *handlers.Handler, body string) map[string]any {
	t.Helper()
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	req, _ := http.NewRequest("POST", "/api/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	return resp
}

func TestHelpTooltips_DefaultAnInLoginUndMe(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "help472@example.com", PasswordHash: testutils.MustHash("pw472")}
	db.Create(&u)
	const login = `{"email":"help472@example.com","password":"pw472"}`

	if got := meJSON(t, h, u.ID)["help_tooltips"]; got != true {
		t.Errorf("GET /api/me help_tooltips = %v, erwartet true (Default an)", got)
	}
	if got := loginJSON(t, h, login)["help_tooltips"]; got != true {
		t.Errorf("Login help_tooltips = %v, erwartet true", got)
	}

	// Ausgeschaltet: Login liefert false.
	db.Model(&models.User{}).Where("id = ?", u.ID).Update("hide_help_tooltips", true)
	if got := loginJSON(t, h, login)["help_tooltips"]; got != false {
		t.Errorf("Login help_tooltips = %v, erwartet false", got)
	}
}

func TestHelpTooltips_PatchSpeichertNurDieseSpalte(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "patch472@example.com", PreferredLanguage: "de"}
	db.Create(&u)
	var before models.User
	db.First(&before, u.ID)

	w := patchMeLanguage(t, h, u.ID, `{"help_tooltips":false}`)
	requireStatusCode(t, w, http.StatusOK)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !reflect.DeepEqual(resp, map[string]any{"help_tooltips": false}) {
		t.Errorf("Antwort = %v", resp)
	}

	var after models.User
	db.First(&after, u.ID)
	if !after.HideHelpTooltips {
		t.Fatal("HideHelpTooltips nicht gespeichert")
	}
	if got := meJSON(t, h, u.ID)["help_tooltips"]; got != false {
		t.Errorf("GET /api/me help_tooltips = %v, erwartet false", got)
	}
	// Nur hide_help_tooltips darf sich ändern (Sprache bleibt).
	after.HideHelpTooltips, after.UpdatedAt = before.HideHelpTooltips, before.UpdatedAt
	if !reflect.DeepEqual(after, before) {
		t.Errorf("andere Felder verändert: vorher %+v, nachher %+v", before, after)
	}

	requireStatusCode(t, patchMeLanguage(t, h, u.ID, `{"help_tooltips":true}`), http.StatusOK)
	if got := meJSON(t, h, u.ID)["help_tooltips"]; got != true {
		t.Errorf("nach Wiedereinschalten help_tooltips = %v", got)
	}
}

func TestHelpTooltips_PatchBeideFelder(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "both472@example.com", PreferredLanguage: "de"}
	db.Create(&u)

	w := patchMeLanguage(t, h, u.ID, `{"help_tooltips":false,"preferred_language":"en"}`)
	requireStatusCode(t, w, http.StatusOK)
	var after models.User
	db.First(&after, u.ID)
	if !after.HideHelpTooltips || after.PreferredLanguage != "en" {
		t.Errorf("User nach PATCH = %+v", after)
	}

	// Ungültige Sprache: nichts wird geschrieben, auch nicht die Tooltips.
	requireStatusCode(t, patchMeLanguage(t, h, u.ID, `{"help_tooltips":true,"preferred_language":"fr"}`), http.StatusBadRequest)
	db.First(&after, u.ID)
	if !after.HideHelpTooltips {
		t.Error("help_tooltips trotz 400 geändert")
	}
}

func TestHelpTooltips_PatchFehler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	u := models.User{Email: "err472@example.com"}
	db.Create(&u)

	for name, body := range map[string]string{
		"kein bool": `{"help_tooltips":"nein"}`,
		"null":      `{"help_tooltips":null}`,
		"leer":      `{}`,
		"kaputt":    `{"help_tooltips":`,
	} {
		t.Run(name, func(t *testing.T) {
			requireStatusCode(t, patchMeLanguage(t, h, u.ID, body), http.StatusBadRequest)
		})
	}

	t.Run("ohne Auth 401", func(t *testing.T) {
		r := testutils.SetupRouter()
		r.PATCH("/api/me", h.UpdateMeHandler)
		req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBufferString(`{"help_tooltips":false}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("ungültige Sitzung 500", func(t *testing.T) {
		r := testutils.SetupRouter()
		r.PATCH("/api/me", func(c *gin.Context) { c.Set("userID", "kein-uint") }, h.UpdateMeHandler)
		req, _ := http.NewRequest("PATCH", "/api/me", bytes.NewBufferString(`{"help_tooltips":false}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusInternalServerError)
	})

	t.Run("unbekannter User 401", func(t *testing.T) {
		requireStatusCode(t, patchMeLanguage(t, h, 474747, `{"help_tooltips":false}`), http.StatusUnauthorized)
	})

	t.Run("DB-Fehler 500 ohne Interna", func(t *testing.T) {
		db.Exec("CREATE TRIGGER fail472 BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'secret-internal-472'); END")
		w := patchMeLanguage(t, h, u.ID, `{"help_tooltips":false}`)
		requireStatusCode(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "secret-internal-472") {
			t.Errorf("Antwort verrät Interna: %s", w.Body.String())
		}
	})
}
