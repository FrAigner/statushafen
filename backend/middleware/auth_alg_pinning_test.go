package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// signHS384Token builds a token with the SAME JWT_SECRET as legitimate tokens
// but signed with HS384 instead of HS256 - the signature is cryptographically
// valid for that algorithm+secret, but must still be rejected once the
// expected algorithm is pinned. Shared by both middleware tests
// below since the only difference between them is which middleware/assertion
// is exercised, not how the malicious token is constructed.
func signHS384Token(t *testing.T, email string) string {
	t.Helper()
	testutils.SetupTestDB()
	secret := []byte(os.Getenv("JWT_SECRET"))

	claims := &middleware.Claims{UserID: 1, Email: email}
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign HS384 token: %v", err)
	}
	return signed
}

// TestAuthMiddleware_RejectsNonHS256Algorithm:
// jwt.ParseWithClaims must pin the expected signing algorithm via
// jwt.WithValidMethods instead of accepting whatever algorithm the token
// itself declares in its header.
func TestAuthMiddleware_RejectsNonHS256Algorithm(t *testing.T) {
	signed := signHS384Token(t, "alg-pinning@example.com")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.AuthMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a token signed with a non-HS256 algorithm, got %d: %s", w.Code, w.Body.String())
	}
}

// TestOptionalAuthMiddleware_IgnoresNonHS256Algorithm is the
// OptionalAuthMiddleware counterpart: a non-HS256 token must be silently
// ignored (treated as anonymous), not accepted.
func TestOptionalAuthMiddleware_IgnoresNonHS256Algorithm(t *testing.T) {
	signed := signHS384Token(t, "alg-pinning-optional@example.com")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/optional", middleware.OptionalAuthMiddleware(), func(c *gin.Context) {
		if _, exists := c.Get("userID"); exists {
			c.String(http.StatusOK, "authenticated")
			return
		}
		c.String(http.StatusOK, "anonymous")
	})

	req, _ := http.NewRequest("GET", "/optional", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Body.String() != "anonymous" {
		t.Fatalf("expected the non-HS256 token to be ignored (treated as anonymous), got status %d body %q", w.Code, w.Body.String())
	}
}

// TestAuthMiddleware_AcceptsHS256Token is the regression guard that the
// pinning change did not break the normal, legitimate login path.
func TestAuthMiddleware_AcceptsHS256Token(t *testing.T) {
	db := testutils.SetupTestDB()
	// Das Konto muss existieren: AuthMiddleware gleicht das Token mit der DB ab.
	if err := db.Create(&models.User{Email: "hs256@example.com"}).Error; err != nil {
		t.Fatal(err)
	}

	signed, err := middleware.GenerateToken(1, "hs256@example.com", "", "", false)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.AuthMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a normal HS256 token, got %d: %s", w.Code, w.Body.String())
	}
}
