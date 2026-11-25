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

// The check of exp and audience must also apply when the account exists in the token.
// A token for an unknown user ID fails at the
// account lookup anyway and would mask the error of the token check.
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

// A token with the subscriber audience does carry an exp, but is never a
// session, not even with the ID of an existing account.
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

// Subscriber ID and page ID are both required: if one is missing, the token is
// invalid (a login token would carry both as 0).
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

// The algorithm is pinned to HS256: a subscriber token signed with the same secret but
// with HS384 is rejected instead of adopting the algorithm
// claimed by the token.
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
