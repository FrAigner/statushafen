package database

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// InitDB stellt Klartext-Token aus Bestandsdaten beim Start auf den Hash um.
// Die Umstellung selbst prüft TestHashLegacyTokens_*; dieser Test hält fest,
// dass der Start sie auch aufruft.
func TestInitDB_HashtKlartextTokenBeimStart(t *testing.T) {
	prevDialector, prevDB := openDialector, DB
	t.Cleanup(func() { openDialector, DB = prevDialector, prevDB })
	openDialector = sqlite.Open
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "tokenstart.db"))
	t.Setenv("SEED_DB", "")

	InitDB() // legt die Tabellen an
	require.NoError(t, DB.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('Alt', 'alt', 'klartext-webhook', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)

	InitDB() // der nächste Start
	var stored string
	require.NoError(t, DB.Raw(`SELECT webhook_token FROM status_pages WHERE slug = 'alt'`).Scan(&stored).Error)
	assert.Equal(t, models.HashToken("klartext-webhook"), stored)
}
