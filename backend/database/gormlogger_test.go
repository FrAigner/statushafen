package database

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Weder "SLOW SQL" noch Fehlerzeilen dürfen Parameterwerte
// (Webhook-URL mit key=/token=, Token-Hash, E-Mail) enthalten.
const (
	secretWebhook = "https://chat.googleapis.com/v1/spaces/AAA/messages?key=GEHEIMKEY&token=GEHEIMTOKEN"
	secretToken   = "confirm-token-hash-0123456789abcdef"
	secretEmail   = "anna.geheim@example.com"
)

func openLoggedDB(t *testing.T, buf *bytes.Buffer) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: NewGormLogger(buf)})
	require.NoError(t, err)
	return db
}

func assertNoSecrets(t *testing.T, out string) {
	t.Helper()
	for _, s := range []string{"GEHEIMKEY", "GEHEIMTOKEN", secretToken, secretEmail, secretWebhook} {
		assert.NotContains(t, out, s)
	}
}

func TestGormLogger_FehlerzeileOhneParameterwerte(t *testing.T) {
	var buf bytes.Buffer
	db := openLoggedDB(t, &buf)
	err := db.Exec("UPDATE nicht_vorhanden SET webhook_url = ?, confirm_token_hash = ? WHERE email = ?",
		secretWebhook, secretToken, secretEmail).Error
	require.Error(t, err)
	assert.Contains(t, buf.String(), "nicht_vorhanden")
	assert.Contains(t, buf.String(), "?")
	assertNoSecrets(t, buf.String())
}

func TestGormLogger_SlowSQLOhneParameterwerte(t *testing.T) {
	var buf bytes.Buffer
	db := openLoggedDB(t, &buf)
	require.NoError(t, db.Exec("CREATE TABLE subs (webhook_url TEXT, confirm_token_hash TEXT, email TEXT)").Error)
	buf.Reset()
	slow := func(*gorm.DB) { time.Sleep(gormSlowThreshold + 50*time.Millisecond) }
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:slow", slow))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:slow", slow))
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM subs WHERE webhook_url = ? AND confirm_token_hash = ? AND email = ?",
		secretWebhook, secretToken, secretEmail).Scan(&n).Error)
	assert.Contains(t, buf.String(), "SLOW SQL")
	assertNoSecrets(t, buf.String())
}

func TestGormLogger_LogModeBehaeltSchutz(t *testing.T) {
	var buf bytes.Buffer
	l := NewGormLogger(&buf).LogMode(logger.Info)
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1 WHERE x = ?", 1 }, nil)
	assert.Contains(t, buf.String(), "SELECT 1")
	_, ok := l.(sanitizingLogger)
	assert.True(t, ok)
}

func TestGormLogger_TreiberfehlerOhnePostgresWerte(t *testing.T) {
	var buf bytes.Buffer
	l := NewGormLogger(&buf)
	cause := errors.New(`ERROR: duplicate key value violates unique constraint "uq" (SQLSTATE 23505) Key (email)=(` + secretEmail + `) already exists`)
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "INSERT", 0 }, cause)
	assert.Contains(t, buf.String(), "Key (email)=([REDACTED]) already exists")
	assertNoSecrets(t, buf.String())
}

func TestSanitizedError_BehaeltKette(t *testing.T) {
	e := sanitizedError{msg: "x", cause: gorm.ErrRecordNotFound}
	assert.ErrorIs(t, e, gorm.ErrRecordNotFound)
}
