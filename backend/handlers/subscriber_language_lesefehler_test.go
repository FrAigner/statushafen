package handlers_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Review, Punkt 5: Ein Lesefehler bei der Sprachprüfung ist nicht
// "ungültig". Er darf eine gültige Sprache nicht still durch "" ersetzen.

func failLanguageReads(db *gorm.DB, name, secret string) {
	_ = db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "status_page_languages" {
			_ = tx.AddError(errors.New("kaputt " + secret))
		}
	})
}

func TestAdminAbonnent_LesefehlerBeiSprachpruefung_Antwortet500UndBehaeltSprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	buf := captureAuditLog(t)
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "l703-dberr"}
	require.NoError(t, db.Create(&page).Error)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	addLangSubscriber(t, db, page.ID, "l703-dberr@example.com", "en")
	var sub models.Subscriber
	require.NoError(t, db.Where("email = ?", "l703-dberr@example.com").First(&sub).Error)
	r := newAdminSubRouter(db)
	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"
	failLanguageReads(db, "l703:fail-read", "hunter2-geheim")

	w := adminSubCall(r, http.MethodPut, base+"/"+strconv.Itoa(int(sub.ID)),
		`{"Channel":"email","Email":"l703-dberr@example.com","SubscribedComponentIDs":"all","Language":"en"}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "server_error")

	var after models.Subscriber
	require.NoError(t, db.First(&after, sub.ID).Error)
	assert.Equal(t, "en", after.Language, "gültige Sprache bleibt erhalten")

	w = adminSubCall(r, http.MethodPost, base, `{"Channel":"email","Email":"l703-new@example.com","SubscribedComponentIDs":"all","Language":"en"}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var n int64
	require.NoError(t, db.Model(&models.Subscriber{}).Where("email = ?", "l703-new@example.com").Count(&n).Error)
	assert.Zero(t, n, "nichts angelegt")

	assert.NotContains(t, strings.ToLower(buf.String()), "hunter2", "Fehlertext mit Geheimnis steht nicht im Log")
	assert.Contains(t, buf.String(), "Abonnentensprache nicht prüfbar")
}
