package middleware_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Prüfung von exp und Audience muss auch greifen, wenn das Konto im Token
// existiert. Ein Token für eine unbekannte Nutzer-ID scheitert ohnehin an der
// Kontosuche und würde den Fehler der Token-Prüfung verdecken.
func TestAuthMiddleware_TokenWithoutExpirationIsRejectedForExistingAccount(t *testing.T) {
	db := testutils.SetupTestDB()
	user := models.User{Email: "konto@example.com"}
	require.NoError(t, db.Create(&user).Error)

	good, err := middleware.GenerateToken(user.ID, user.Email, "admin", "", false)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, doGet(authRouter(), "/protected", good).Code, "Gegenprobe: das Konto wird gefunden")

	noExp := signRaw(t, &middleware.Claims{UserID: user.ID, Email: user.Email})
	assert.Equal(t, http.StatusUnauthorized, doGet(authRouter(), "/protected", noExp).Code)
	assert.Equal(t, "anon", doGet(authRouter(), "/optional", noExp).Body.String())
}

// Ein Token mit Subscriber-Audience trägt zwar ein exp, ist aber nie eine
// Sitzung, auch nicht mit der ID eines vorhandenen Kontos.
func TestAuthMiddleware_SubscriberAudienceIsRejectedEvenWithExistingAccountID(t *testing.T) {
	db := testutils.SetupTestDB()
	user := models.User{Email: "konto2@example.com"}
	require.NoError(t, db.Create(&user).Error)

	mixed := signRaw(t, &middleware.Claims{
		UserID: user.ID, Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"subscriber"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	assert.Equal(t, http.StatusUnauthorized, doGet(authRouter(), "/protected", mixed).Code)
	assert.Equal(t, "anon", doGet(authRouter(), "/optional", mixed).Body.String())
}

// Subscriber-ID und Seiten-ID sind beide Pflicht: Fehlt eine, ist das Token
// ungültig (ein Login-Token trüge beide als 0).
func TestParseSubscriberToken_BothIDsAreRequired(t *testing.T) {
	testutils.SetupTestDB()
	claimsWith := func(subscriberID, pageID uint) *middleware.SubscriberClaims {
		return &middleware.SubscriberClaims{SubscriberID: subscriberID, StatusPageID: pageID,
			RegisteredClaims: jwt.RegisteredClaims{
				Audience:  jwt.ClaimStrings{"subscriber"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}}
	}
	_, err := middleware.ParseSubscriberToken(signRaw(t, claimsWith(7, 9)))
	require.NoError(t, err, "Gegenprobe: beide gesetzt")
	_, err = middleware.ParseSubscriberToken(signRaw(t, claimsWith(0, 9)))
	assert.Error(t, err, "ohne Subscriber-ID")
	_, err = middleware.ParseSubscriberToken(signRaw(t, claimsWith(7, 0)))
	assert.Error(t, err, "ohne Seiten-ID")
	_, err = middleware.ParseSubscriberToken(signRaw(t, claimsWith(0, 0)))
	assert.Error(t, err, "ohne beide")
}

// Der Algorithmus ist auf HS256 festgelegt: Ein mit demselben Geheimnis, aber
// per HS384 signiertes Subscriber-Token wird abgelehnt, statt den vom Token
// behaupteten Algorithmus zu übernehmen.
func TestParseSubscriberToken_OnlyHS256IsAccepted(t *testing.T) {
	testutils.SetupTestDB()
	claims := &middleware.SubscriberClaims{SubscriberID: 7, StatusPageID: 9,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"subscriber"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}}
	_, err := middleware.ParseSubscriberToken(signRaw(t, claims))
	require.NoError(t, err, "Gegenprobe: HS256")

	hs384, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(os.Getenv("JWT_SECRET")))
	require.NoError(t, err)
	_, err = middleware.ParseSubscriberToken(hs384)
	assert.Error(t, err)
}
