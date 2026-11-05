package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Sprachen aus dem Katalog löschen.

func langDeleteSetup(t *testing.T) (*gorm.DB, http.Handler, map[string]models.Language, models.StatusPage) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/languages", h.CreateLanguageHandler)
	r.DELETE("/api/languages/:id", h.DeleteLanguageHandler)

	langs := map[string]models.Language{}
	for _, code := range []string{"de", "en", "fr"} {
		l := models.Language{Code: code, Name: code, IsActive: true}
		require.NoError(t, db.Create(&l).Error)
		langs[code] = l
	}
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
	return db, r, langs, page
}

func delLang(r http.Handler, id string) (int, map[string]any) {
	w := doTagRequest(r, "DELETE", "/api/languages/"+id, nil)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestDeleteLanguageHandler_StandardspracheWirdBlockiert(t *testing.T) {
	db, r, langs, page := langDeleteSetup(t)
	code, body := delLang(r, itoa(langs["de"].ID))
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "language_is_default", body["code"])
	assert.EqualValues(t, 1, body["count"])
	assert.Equal(t, []any{page.Name}, body["pages"])
	var n int64
	db.Model(&models.Language{}).Where("code = ?", "de").Count(&n)
	assert.EqualValues(t, 1, n, "Sprache muss bestehen bleiben")
}

func TestDeleteLanguageHandler_InBenutzungDurchMailVorlageBlockiert(t *testing.T) {
	db, r, langs, page := langDeleteSetup(t)
	require.NoError(t, db.Create(&models.NotificationTemplate{StatusPageID: page.ID, Kind: "incident", Audience: "staff", Language: "en", Subject: "s", BodyHTML: "b"}).Error)
	code, body := delLang(r, itoa(langs["en"].ID))
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "language_in_use", body["code"])
	assert.EqualValues(t, 1, body["count"])
	_, hasPages := body["pages"]
	assert.False(t, hasPages)
	var n int64
	db.Model(&models.StatusPageLanguage{}).Where("language_code = ?", "en").Count(&n)
	assert.EqualValues(t, 1, n, "Zuordnung darf bei Ablehnung nicht verschwinden")
}

func TestDeleteLanguageHandler_EntferntZuordnungUndSpracheUndSchreibtAudit(t *testing.T) {
	db, r, langs, _ := langDeleteSetup(t)
	// Übersetzungen bleiben in der DB.
	require.NoError(t, db.Create(&models.IncidentTranslation{IncidentID: 1, LanguageCode: "en", Title: "t", Message: "m"}).Error)
	code, _ := delLang(r, itoa(langs["en"].ID))
	assert.Equal(t, http.StatusNoContent, code)

	var n int64
	db.Model(&models.Language{}).Where("code = ?", "en").Count(&n)
	assert.Zero(t, n)
	db.Model(&models.StatusPageLanguage{}).Where("language_code = ?", "en").Count(&n)
	assert.Zero(t, n, "Zuordnung zur Seite muss entfernt sein")
	db.Model(&models.StatusPageLanguage{}).Where("language_code = ?", "de").Count(&n)
	assert.EqualValues(t, 1, n, "andere Zuordnungen bleiben")
	db.Model(&models.IncidentTranslation{}).Where("language_code = ?", "en").Count(&n)
	assert.EqualValues(t, 1, n, "gespeicherte Übersetzung bleibt in der DB")
	db.Model(&models.AuditLog{}).Where("action = ? AND target_name = ?", "language.deleted", "en").Count(&n)
	assert.EqualValues(t, 1, n)
}

func TestDeleteLanguageHandler_GleicherCodeLaesstSichNeuAnlegen(t *testing.T) {
	db, r, langs, _ := langDeleteSetup(t)
	code, _ := delLang(r, itoa(langs["fr"].ID))
	require.Equal(t, http.StatusNoContent, code)

	w := doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "fr", "name": "Français neu"})
	require.Equal(t, http.StatusCreated, w.Code)
	var got models.Language
	require.NoError(t, db.Where("code = ?", "fr").First(&got).Error)
	assert.Equal(t, "Français neu", got.Name)
	assert.Equal(t, langs["fr"].ID, got.ID, "die Grabstein-Zeile lebt wieder auf")
	var n int64
	db.Unscoped().Model(&models.Language{}).Where("code = ?", "fr").Count(&n)
	assert.EqualValues(t, 1, n)

	// Und ein zweites Anlegen ist wieder ein Duplikat.
	w = doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "fr", "name": "x"})
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDeleteLanguageHandler_UngueltigeUndUnbekannteId(t *testing.T) {
	_, r, _, _ := langDeleteSetup(t)
	code, body := delLang(r, "abc")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "invalid_id", body["code"])
	code, body = delLang(r, "9999")
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "language_not_found", body["code"])
}

func TestDeleteLanguageHandler_DatenbankfehlerLiefernFehler500(t *testing.T) {
	steps := []struct {
		name   string
		break_ func(db *gorm.DB)
	}{
		{"Sprache laden", func(db *gorm.DB) { require.NoError(t, db.Migrator().DropTable(&models.Language{})) }},
		{"Standardseiten prüfen", func(db *gorm.DB) { require.NoError(t, db.Migrator().DropTable(&models.StatusPageLanguage{})) }},
		{"Vorlagen zählen", func(db *gorm.DB) { require.NoError(t, db.Migrator().DropTable(&models.NotificationTemplate{})) }},
		{"Zuordnung löschen", func(db *gorm.DB) {
			db.Exec("CREATE TRIGGER no_upd BEFORE UPDATE ON status_page_languages BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
		}},
		{"Sprache löschen", func(db *gorm.DB) {
			db.Exec("CREATE TRIGGER no_upd BEFORE UPDATE ON languages BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			db, r, langs, _ := langDeleteSetup(t)
			id := itoa(langs["en"].ID)
			s.break_(db)
			code, body := delLang(r, id)
			assert.Equal(t, http.StatusInternalServerError, code)
			assert.Equal(t, "server_error", body["code"])
		})
	}
}

func TestCreateLanguageHandler_DatenbankfehlerBeiGrabsteinPruefung(t *testing.T) {
	db, r, langs, _ := langDeleteSetup(t)
	code, _ := delLang(r, itoa(langs["fr"].ID))
	require.Equal(t, http.StatusNoContent, code)
	// Wiederbeleben scheitert: 500 statt stillem Verlust.
	db.Exec("CREATE TRIGGER no_upd BEFORE UPDATE ON languages BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	w := doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "fr", "name": "x"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Review-Fund: scheitert nur die Abfrage nach einer gelöschten Sprache (nicht die Duplikatprüfung davor),
// antwortet Anlegen mit 500 statt eine zweite Zeile zu versuchen.
func TestCreateLanguageHandler_DatenbankfehlerBeiAbfrageGeloeschterSprache(t *testing.T) {
	db, r, _, _ := langDeleteSetup(t)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:tomb_fail", func(tx *gorm.DB) {
		if tx.Statement.SQL.Len() == 0 && tx.Statement.Unscoped {
			_ = tx.AddError(gorm.ErrInvalidDB)
		}
	}))
	w := doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "pl", "name": "polski"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "server_error", body["code"])
}
