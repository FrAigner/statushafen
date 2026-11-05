package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Subscriber-Tokens (lange gültig, 400 Tage) dürfen nie als
// Login-Token durchgehen, und Login-Tokens nicht als Subscriber-Token.

func authRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.AuthMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.GET("/optional", middleware.OptionalAuthMiddleware(), func(c *gin.Context) {
		if _, ok := c.Get("userID"); ok {
			c.String(http.StatusOK, "user")
			return
		}
		c.String(http.StatusOK, "anon")
	})
	return r
}

func doGet(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func signRaw(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestAuthMiddleware_RejectsSubscriberToken(t *testing.T) {
	testutils.SetupTestDB()
	tok, err := middleware.GenerateSubscriberToken(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if w := doGet(authRouter(), "/protected", tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("Subscriber-Token an AuthMiddleware: want 401, got %d", w.Code)
	}
}

func TestOptionalAuthMiddleware_IgnoresSubscriberToken(t *testing.T) {
	testutils.SetupTestDB()
	tok, _ := middleware.GenerateSubscriberToken(1, 2)
	if w := doGet(authRouter(), "/optional", tok); w.Body.String() != "anon" {
		t.Fatalf("Subscriber-Token darf keinen User-Kontext setzen, got %q", w.Body.String())
	}
}

func TestAuthMiddleware_RejectsLegacySubscriberTokenWithoutAudience(t *testing.T) {
	testutils.SetupTestDB()
	// Altes Format aus bereits verschickten Mails: weder aud noch exp.
	legacy := signRaw(t, &middleware.SubscriberClaims{SubscriberID: 1, StatusPageID: 2})
	if w := doGet(authRouter(), "/protected", legacy); w.Code != http.StatusUnauthorized {
		t.Fatalf("Legacy-Subscriber-Token: want 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_RequiresExpiration(t *testing.T) {
	testutils.SetupTestDB()
	noExp := signRaw(t, &middleware.Claims{UserID: 1, Email: "a@example.com"})
	if w := doGet(authRouter(), "/protected", noExp); w.Code != http.StatusUnauthorized {
		t.Fatalf("Login-Token ohne exp: want 401, got %d", w.Code)
	}
	if w := doGet(authRouter(), "/optional", noExp); w.Body.String() != "anon" {
		t.Fatalf("Optional: Token ohne exp muss ignoriert werden, got %q", w.Body.String())
	}
}

func TestAuthMiddleware_AcceptsRegularLoginToken(t *testing.T) {
	db := testutils.SetupTestDB()
	// Das Konto muss existieren: AuthMiddleware gleicht das Token mit der DB ab.
	if err := db.Create(&models.User{Email: "a@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	tok, _ := middleware.GenerateToken(1, "a@example.com", "admin", "", false)
	if w := doGet(authRouter(), "/protected", tok); w.Code != http.StatusOK {
		t.Fatalf("regulärer Login-Token: want 200, got %d", w.Code)
	}
	if w := doGet(authRouter(), "/optional", tok); w.Body.String() != "user" {
		t.Fatalf("Optional: regulärer Token muss User setzen, got %q", w.Body.String())
	}
}

func TestParseSubscriberToken(t *testing.T) {
	testutils.SetupTestDB()

	t.Run("roundtrip", func(t *testing.T) {
		tok, _ := middleware.GenerateSubscriberToken(7, 9)
		c, err := middleware.ParseSubscriberToken(tok)
		if err != nil || c.SubscriberID != 7 || c.StatusPageID != 9 {
			t.Fatalf("roundtrip fehlgeschlagen: %+v %v", c, err)
		}
	})
	t.Run("Token ohne aud wird abgelehnt", func(t *testing.T) {
		noAud := signRaw(t, &middleware.SubscriberClaims{SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
		if _, err := middleware.ParseSubscriberToken(noAud); err == nil {
			t.Fatal("Subscriber-Token ohne Audience darf nicht gelten")
		}
	})
	t.Run("Login-Token abgelehnt", func(t *testing.T) {
		login, _ := middleware.GenerateToken(1, "a@example.com", "admin", "", false)
		if _, err := middleware.ParseSubscriberToken(login); err == nil {
			t.Fatal("Login-Token darf kein Subscriber-Token sein")
		}
	})
	t.Run("fremde Audience abgelehnt", func(t *testing.T) {
		other := signRaw(t, &middleware.SubscriberClaims{
			SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"other"}},
		})
		if _, err := middleware.ParseSubscriberToken(other); err == nil {
			t.Fatal("fremde Audience muss abgelehnt werden")
		}
	})
	t.Run("IDs null abgelehnt", func(t *testing.T) {
		for _, c := range []*middleware.SubscriberClaims{{SubscriberID: 0, StatusPageID: 9}, {SubscriberID: 7, StatusPageID: 0}} {
			if _, err := middleware.ParseSubscriberToken(signRaw(t, c)); err == nil {
				t.Fatalf("IDs %+v müssen abgelehnt werden", c)
			}
		}
	})
	t.Run("abgelaufen abgelehnt", func(t *testing.T) {
		exp := signRaw(t, &middleware.SubscriberClaims{
			SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))},
		})
		if _, err := middleware.ParseSubscriberToken(exp); err == nil {
			t.Fatal("abgelaufenes Token muss abgelehnt werden")
		}
	})
	t.Run("Müll abgelehnt", func(t *testing.T) {
		if _, err := middleware.ParseSubscriberToken("kein.jwt.token"); err == nil {
			t.Fatal("ungültiges Token muss abgelehnt werden")
		}
	})
}
