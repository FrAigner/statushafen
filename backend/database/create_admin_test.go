package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// testAdminPassword ist ein reines Testpasswort, kein echtes Geheimnis.
const testAdminPassword = "ein-langes-testpasswort"

func newCreateAdminDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	return db
}

func TestCreateGlobalAdmin_LegtAdminMitPasswortAn(t *testing.T) {
	db := newCreateAdminDB(t)
	require.NoError(t, db.Create(&models.Role{Name: "Global Admin"}).Error)

	require.NoError(t, CreateGlobalAdmin(db, "  Admin@Example.COM ", testAdminPassword))

	var u models.User
	require.NoError(t, db.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.True(t, u.IsGlobalAdmin)
	assert.NotEmpty(t, u.RoleID, "die vorhandene Rolle Global Admin wird zugewiesen")
	assert.True(t, CheckPasswordHash(testAdminPassword, u.PasswordHash))
	assert.NotContains(t, u.PasswordHash, testAdminPassword)
}

func TestCreateGlobalAdmin_OhneRolleTabelleTrotzdemAdmin(t *testing.T) {
	db := newCreateAdminDB(t)
	require.NoError(t, CreateGlobalAdmin(db, "admin@example.com", testAdminPassword))

	var u models.User
	require.NoError(t, db.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.True(t, u.IsGlobalAdmin)
}

func TestCreateGlobalAdmin_UngueltigeEingabenSchreibenNichts(t *testing.T) {
	cases := map[string]struct{ email, pw string }{
		"leere Adresse":         {"", testAdminPassword},
		"keine Adresse":         {"kein-at-zeichen", testAdminPassword},
		"Anzeigename":           {"Name <admin@example.com>", testAdminPassword},
		"zu lange Adresse":      {strings.Repeat("a", 250) + "@example.com", testAdminPassword},
		"Passwort zu kurz":      {"admin@example.com", "kurz"},
		"leeres Passwort":       {"admin@example.com", ""},
		"Passwort über 72 Byte": {"admin@example.com", strings.Repeat("a", MaxPasswordBytes+1)},
		"NUL im Passwort":       {"admin@example.com", "ein-langes\x00testpasswort"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := newCreateAdminDB(t)
			require.Error(t, CreateGlobalAdmin(db, tc.email, tc.pw))
			var n int64
			require.NoError(t, db.Model(&models.User{}).Count(&n).Error)
			assert.Zero(t, n)
		})
	}
}

func TestCreateGlobalAdmin_VorhandenesKontoWirdNichtUeberschrieben(t *testing.T) {
	db := newCreateAdminDB(t)
	require.NoError(t, db.Create(&models.User{Email: "admin@example.com", PasswordHash: "alt"}).Error)

	err := CreateGlobalAdmin(db, "ADMIN@example.com", testAdminPassword)
	assert.ErrorIs(t, err, ErrAdminExists)

	var u models.User
	require.NoError(t, db.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.Equal(t, "alt", u.PasswordHash)
	assert.False(t, u.IsGlobalAdmin)
}

func TestCreateGlobalAdmin_DatenbankfehlerBrichtAb(t *testing.T) {
	db := newCreateAdminDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = CreateGlobalAdmin(db, "admin@example.com", testAdminPassword)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrAdminExists))
}

// Jeder Datenbankfehler mitten im Ablauf bricht ab und legt nichts an.
func TestCreateGlobalAdmin_FehlerInEinzelnenSchrittenBrechenAb(t *testing.T) {
	t.Run("Konten nicht lesbar", func(t *testing.T) {
		db := newCreateAdminDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.User{}))
		err := CreateGlobalAdmin(db, "admin@example.com", testAdminPassword)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "konto prüfen")
	})
	t.Run("Rollen nicht lesbar", func(t *testing.T) {
		db := newCreateAdminDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.Role{}))
		err := CreateGlobalAdmin(db, "admin@example.com", testAdminPassword)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rolle lesen")
		var n int64
		require.NoError(t, db.Model(&models.User{}).Count(&n).Error)
		assert.Zero(t, n)
	})
	t.Run("Anlegen scheitert", func(t *testing.T) {
		db := newCreateAdminDB(t)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail", func(tx *gorm.DB) {
			_ = tx.AddError(errors.New("schreibfehler"))
		}))
		err := CreateGlobalAdmin(db, "admin@example.com", testAdminPassword)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "konto anlegen")
	})
}
