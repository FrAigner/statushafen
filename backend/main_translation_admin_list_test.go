package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Die Admin-Übersetzungsseiten luden ihre Tabelle über die
// öffentliche Route, die nur bestätigte Übersetzungen liefert - frisch
// angelegte Entwürfe verschwanden. GET .../translations/all liefert dem Admin
// Entwürfe und Bestätigte, die öffentliche Route bleibt bei "nur confirmed",
// und fremde Seiten bleiben gesperrt.
func TestRouter_AdminTranslationListIncludesDrafts(t *testing.T) {
	for _, tc := range translationRouteCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := newRouter(handlers.NewHandler(db))

			own := models.StatusPage{Name: "Eigene", Slug: "eigene-liste-" + tc.name}
			foreign := models.StatusPage{Name: "Fremde", Slug: "fremde-liste-" + tc.name}
			db.Create(&own)
			db.Create(&foreign)
			entity := strings.SplitN(tc.permission, ":", 2)[0]
			reader := models.Role{Name: "Leser " + tc.name, Permissions: entity + ":read"}
			db.Create(&reader)
			user := models.User{Email: "leser-" + tc.name + "@example.com"}
			db.Create(&user)
			db.Create(&models.UserStatusPageRole{UserID: user.ID, StatusPageID: own.ID, RoleID: reader.ID})
			token, _ := middleware.GenerateToken(user.ID, user.Email, "", "", false)

			ownParent, _ := tc.seed(db, own.ID, "fr") // Entwurf
			foreignParent, _ := tc.seed(db, foreign.ID, "fr")
			// Bestätigte Übersetzung an dasselbe Elternobjekt hängen: seed legt
			// stets ein neues Elternobjekt an, deshalb umhängen.
			_, confirmedID := tc.seed(db, own.ID, "es")
			table := map[string][2]string{
				"incident":  {"incident_translations", "incident_id"},
				"component": {"component_translations", "component_id"},
				"notice":    {"general_notice_translations", "general_notice_id"},
			}[tc.name]
			if err := db.Exec("UPDATE "+table[0]+" SET status = 'confirmed', "+table[1]+" = ? WHERE id = ?", ownParent, confirmedID).Error; err != nil {
				t.Fatal(err)
			}

			get := func(path, tok string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				if tok != "" {
					req.Header.Set("Authorization", "Bearer "+tok)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			public := func(parent string) string { return strings.Replace(tc.createPath, "%d", parent, 1) }
			all := func(parent string) string { return public(parent) + "/all" }
			id := func(v uint) string { return strconv.FormatUint(uint64(v), 10) }

			w := get(all(id(ownParent)), token)
			if w.Code != http.StatusOK {
				t.Fatalf("Admin-Liste: Status %d, Body %s", w.Code, w.Body.String())
			}
			if got := statuses(t, w.Body.Bytes()); got != "confirmed:es,draft:fr" {
				t.Errorf("Admin-Liste soll Entwurf und Bestätigte liefern, hat %q", got)
			}

			if got := statuses(t, get(public(id(ownParent)), "").Body.Bytes()); got != "confirmed:es" {
				t.Errorf("öffentliche Liste darf nur Bestätigte liefern, hat %q", got)
			}

			for _, c := range []struct {
				desc, path, token string
				want              int
			}{
				{"fremde Seite", all(id(foreignParent)), token, http.StatusForbidden},
				{"unbekanntes Elternobjekt", all("999999"), token, http.StatusNotFound},
				{"ungültige ID", all("abc"), token, http.StatusBadRequest},
				{"ohne Anmeldung", all(id(ownParent)), "", http.StatusUnauthorized},
			} {
				if got := get(c.path, c.token).Code; got != c.want {
					t.Errorf("%s: GET %s = %d, want %d", c.desc, c.path, got, c.want)
				}
			}
		})
	}
}

// statuses fasst die Antwort als "status:sprache,..." zusammen.
func statuses(t *testing.T, body []byte) string {
	t.Helper()
	var list []struct {
		Status       string
		LanguageCode string
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("Antwort ist keine Liste: %s", body)
	}
	parts := make([]string, 0, len(list))
	for _, tr := range list {
		parts = append(parts, tr.Status+":"+tr.LanguageCode)
	}
	return strings.Join(parts, ",")
}
