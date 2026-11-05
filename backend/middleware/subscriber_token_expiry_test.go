package middleware_test

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/testutils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Neue Subscriber-Tokens laufen ab: Der Link in einer Mail ist 400 Tage gültig
// (jede neue Benachrichtigung bringt einen frischen Link). Ein geleaktes oder
// weitergeleitetes Exemplar bleibt so nicht für immer verwendbar.
func TestGenerateSubscriberToken_TraegtAblaufNachVierhundertTagen(t *testing.T) {
	testutils.SetupTestDB()
	tok, err := middleware.GenerateSubscriberToken(7, 9)
	require.NoError(t, err)

	claims := &middleware.SubscriberClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(tok, claims)
	require.NoError(t, err)
	require.NotNil(t, claims.ExpiresAt, "exp fehlt")
	require.NotNil(t, claims.IssuedAt, "iat fehlt")
	assert.WithinDuration(t, time.Now().Add(400*24*time.Hour), claims.ExpiresAt.Time, time.Minute)
	assert.WithinDuration(t, time.Now(), claims.IssuedAt.Time, time.Minute)
}

// Ein Subscriber-Token braucht immer ein Ablaufdatum und die Subscriber-
// Audience. Eine Übergangsregel für Tokens ohne exp oder ohne aud gibt es
// nicht: Sie würden einen geleakten Link dauerhaft brauchbar halten.
func TestParseSubscriberToken_AblaufIstPflicht(t *testing.T) {
	testutils.SetupTestDB()
	at := func(y int, m time.Month, d int) func() time.Time {
		return func() time.Time { return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
	}

	t.Run("Token ohne exp wird zu keiner Zeit akzeptiert", func(t *testing.T) {
		noExp := signRaw(t, &middleware.SubscriberClaims{SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"subscriber"}}})
		for _, clock := range []func() time.Time{at(2026, time.November, 6), at(2027, time.December, 9), at(2030, time.January, 1)} {
			middleware.SetSubscriberTokenClockForTests(t.Cleanup, clock)
			_, err := middleware.ParseSubscriberToken(noExp)
			assert.Error(t, err)
		}
	})
	t.Run("Token ohne aud wird abgelehnt", func(t *testing.T) {
		middleware.SetSubscriberTokenClockForTests(t.Cleanup, at(2026, time.November, 6))
		noAud := signRaw(t, &middleware.SubscriberClaims{SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Date(2027, time.June, 1, 0, 0, 0, 0, time.UTC))}})
		_, err := middleware.ParseSubscriberToken(noAud)
		assert.Error(t, err)
	})
	t.Run("neuer Token gilt knapp vor dem Ablauf und ist danach abgelehnt", func(t *testing.T) {
		middleware.SetSubscriberTokenClockForTests(t.Cleanup, at(2026, time.November, 6))
		tok, err := middleware.GenerateSubscriberToken(7, 9)
		require.NoError(t, err)

		middleware.SetSubscriberTokenClockForTests(t.Cleanup, at(2027, time.December, 9))
		_, err = middleware.ParseSubscriberToken(tok)
		assert.NoError(t, err, "399 Tage nach Ausstellung")

		middleware.SetSubscriberTokenClockForTests(t.Cleanup, at(2027, time.December, 12))
		_, err = middleware.ParseSubscriberToken(tok)
		assert.Error(t, err, "nach 400 Tagen abgelaufen")
	})
	t.Run("Token mit exp und aud gilt bis zu seinem exp", func(t *testing.T) {
		middleware.SetSubscriberTokenClockForTests(t.Cleanup, at(2028, time.January, 1))
		tok := signRaw(t, &middleware.SubscriberClaims{SubscriberID: 7, StatusPageID: 9,
			RegisteredClaims: jwt.RegisteredClaims{
				Audience:  jwt.ClaimStrings{"subscriber"},
				ExpiresAt: jwt.NewNumericDate(time.Date(2028, time.June, 1, 0, 0, 0, 0, time.UTC)),
			}})
		_, err := middleware.ParseSubscriberToken(tok)
		assert.NoError(t, err)
	})
}
