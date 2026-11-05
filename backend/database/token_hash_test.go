package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Bestandsdaten halten Webhook-, Bestätigungs- und Passwort-Token im Klartext.
// Die Migration ersetzt sie durch den Hash; der Klartext aus Mails und Admin-
// Oberfläche bleibt danach einlösbar. Ein zweiter Lauf ändert nichts mehr
// (kein Doppel-Hashen), und leere Werte bleiben leer.
func TestHashLegacyTokens_HashtKlartextEinmalig(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.PageOrder{}))

	// Ältere Zeilen mit Klartext direkt per SQL, ohne die Modell-Felder.
	exp := time.Now().Add(time.Hour)
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('A', 'a', 'klartext-webhook', ?, ?)`, exp, exp).Error)
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('B', 'b', '', ?, ?)`, exp, exp).Error)
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, created_at, updated_at) VALUES ('C', 'c', ?, ?)`, exp, exp).Error)
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('D', 'd', ?, ?, ?)`, models.HashToken("schon-gehasht"), exp, exp).Error)
	require.NoError(t, db.Exec(`INSERT INTO page_orders (company_name, email, status, confirm_token, set_password_token, created_at, updated_at) VALUES ('O1', 'o1@example.com', 'pending_confirmation', 'klartext-bestaetigung', NULL, ?, ?)`, exp, exp).Error)
	require.NoError(t, db.Exec(`INSERT INTO page_orders (company_name, email, status, confirm_token, set_password_token, created_at, updated_at) VALUES ('O2', 'o2@example.com', 'confirmed', '', 'klartext-passwort', ?, ?)`, exp, exp).Error)

	var updatedBefore time.Time
	require.NoError(t, db.Raw(`SELECT updated_at FROM page_orders WHERE email = 'o1@example.com'`).Scan(&updatedBefore).Error)

	require.NoError(t, hashLegacyTokens(db))
	snapshot := func() []string {
		var rows []string
		require.NoError(t, db.Raw(`SELECT 'p:' || slug || ':' || coalesce(webhook_token, '<null>') FROM status_pages ORDER BY id`).Scan(&rows).Error)
		var orders []string
		require.NoError(t, db.Raw(`SELECT 'o:' || email || ':' || coalesce(confirm_token, '<null>') || ':' || coalesce(set_password_token, '<null>') FROM page_orders ORDER BY id`).Scan(&orders).Error)
		return append(rows, orders...)
	}
	first := snapshot()

	assert.Equal(t, []string{
		"p:a:" + models.HashToken("klartext-webhook"),
		"p:b:",
		"p:c:<null>",
		"p:d:" + models.HashToken("schon-gehasht"),
		"o:o1@example.com:" + models.HashToken("klartext-bestaetigung") + ":<null>",
		"o:o2@example.com::" + models.HashToken("klartext-passwort"),
	}, first)

	require.NoError(t, hashLegacyTokens(db))
	assert.Equal(t, first, snapshot(), "der zweite Lauf ändert nichts (Hashes werden nicht erneut gehasht)")

	var updatedAfter time.Time
	require.NoError(t, db.Raw(`SELECT updated_at FROM page_orders WHERE email = 'o1@example.com'`).Scan(&updatedAfter).Error)
	assert.True(t, updatedBefore.Equal(updatedAfter), "die Migration fasst updated_at nicht an")
}

// Auch ein gelöschtes (Soft Delete) Datenbankobjekt wird umgestellt, und ein
// Fehler (fehlende Tabelle) kommt als Fehler zurück statt still zu verschwinden.
func TestHashLegacyTokens_FehlerUndSoftDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens2.db")), &gorm.Config{})
	require.NoError(t, err)
	assert.Error(t, hashLegacyTokens(db), "ohne Tabellen ein Fehler")

	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.PageOrder{}))
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, deleted_at, created_at, updated_at) VALUES ('Weg', 'weg', 'klartext-weg', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, hashLegacyTokens(db))
	var v string
	require.NoError(t, db.Raw(`SELECT webhook_token FROM status_pages WHERE slug = 'weg'`).Scan(&v).Error)
	assert.Equal(t, models.HashToken("klartext-weg"), v, "Soft-Delete-Zeilen werden ungefiltert mitgenommen")
}

// Scheitert die Umstellung, loggt der Start nur und läuft weiter; der nächste
// Start versucht es erneut.
func TestMigrateLegacyTokens_FehlerIstNichtFatal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens3.db")), &gorm.Config{})
	require.NoError(t, err)
	out := captureLog(t)
	migrateLegacyTokens(db)
	assert.Contains(t, out.String(), "Failed to hash legacy tokens")
}

// Scheitert das Schreiben eines Hashes, kommt der Fehler zurück und die
// Zeile bleibt unverändert (der nächste Start versucht es erneut).
func TestHashLegacyTokens_SchreibfehlerKommtZurueck(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens4.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.PageOrder{}))
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('A', 'a', 'klartext', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_token_update", func(tx *gorm.DB) {
		_ = tx.AddError(assert.AnError)
	}))

	err = hashLegacyTokens(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status_pages.webhook_token")

	var v string
	require.NoError(t, db.Raw(`SELECT webhook_token FROM status_pages`).Scan(&v).Error)
	assert.Equal(t, "klartext", v, "die Zeile bleibt unverändert")
}

// Die Umstellung schreibt jede Zeile nur, wenn sie noch den gelesenen
// Klartext trägt. Hat zwischen Lesen und Schreiben ein anderer Prozess (oder
// ein Admin, der den Token erneuert) den Wert geändert, darf der Hash des
// alten Klartexts den neuen Token nicht überschreiben.
func TestHashLegacyTokens_UeberschreibtKeinenZwischenzeitlichGeaendertenWert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens5.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.PageOrder{}))
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('A', 'a', 'klartext-alt', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)

	fresh := models.HashToken("frisch-erneuert")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:concurrent_rotation", func(tx *gorm.DB) {
		_ = tx.Session(&gorm.Session{NewDB: true}).Exec(`UPDATE status_pages SET webhook_token = ? WHERE slug = 'a'`, fresh).Error
	}))

	require.NoError(t, hashLegacyTokens(db))
	var v string
	require.NoError(t, db.Raw(`SELECT webhook_token FROM status_pages WHERE slug = 'a'`).Scan(&v).Error)
	assert.Equal(t, fresh, v, "der zwischenzeitlich erneuerte Token bleibt erhalten")
}
