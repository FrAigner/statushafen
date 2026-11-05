package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Caddy legt HTTP Basic Auth vor alle Seiten und belegt damit den
// Authorization-Header. Das Login-Token kommt deshalb über X-Auth-Token,
// "Authorization: Bearer" bleibt als Fallback für API-Clients bestehen.

func doGetHeaders(path string, headers map[string]string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("GET", path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	authRouter().ServeHTTP(w, req)
	return w
}

func TestAuthTokenHeader(t *testing.T) {
	db := testutils.SetupTestDB()
	// Das Konto muss existieren: AuthMiddleware gleicht das Token mit der DB ab.
	if err := db.Create(&models.User{Email: "a@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	valid, err := middleware.GenerateToken(1, "a@example.com", "Viewer", "", false)
	if err != nil {
		t.Fatal(err)
	}
	const basic = "Basic dXNlcjpwYXNz"

	cases := []struct {
		name         string
		headers      map[string]string
		wantRequired int
		wantOptional string
	}{
		{"X-Auth-Token gültig", map[string]string{middleware.AuthTokenHeader: valid}, http.StatusOK, "user"},
		{"X-Auth-Token mit Leerzeichen", map[string]string{middleware.AuthTokenHeader: " " + valid + " "}, http.StatusOK, "user"},
		{"X-Auth-Token ungültig", map[string]string{middleware.AuthTokenHeader: "kaputt"}, http.StatusUnauthorized, "anon"},
		{"X-Auth-Token gültig neben Basic", map[string]string{middleware.AuthTokenHeader: valid, "Authorization": basic}, http.StatusOK, "user"},
		{"X-Auth-Token hat Vorrang vor gültigem Bearer", map[string]string{middleware.AuthTokenHeader: "kaputt", "Authorization": "Bearer " + valid}, http.StatusUnauthorized, "anon"},
		{"Fallback Bearer gültig", map[string]string{"Authorization": "Bearer " + valid}, http.StatusOK, "user"},
		{"Fallback Bearer ungültig", map[string]string{"Authorization": "Bearer kaputt"}, http.StatusUnauthorized, "anon"},
		{"leerer X-Auth-Token fällt auf Bearer zurück", map[string]string{middleware.AuthTokenHeader: "  ", "Authorization": "Bearer " + valid}, http.StatusOK, "user"},
		{"nur Basic von Caddy", map[string]string{"Authorization": basic}, http.StatusUnauthorized, "anon"},
		{"gar kein Header", map[string]string{}, http.StatusUnauthorized, "anon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := doGetHeaders("/protected", tc.headers); w.Code != tc.wantRequired {
				t.Errorf("AuthMiddleware: want %d, got %d (%s)", tc.wantRequired, w.Code, w.Body.String())
			}
			w := doGetHeaders("/optional", tc.headers)
			if w.Code != http.StatusOK || w.Body.String() != tc.wantOptional {
				t.Errorf("OptionalAuthMiddleware: want 200 %q, got %d %q", tc.wantOptional, w.Code, w.Body.String())
			}
		})
	}
}
