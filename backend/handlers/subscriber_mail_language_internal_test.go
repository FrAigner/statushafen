package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reihenfolge gewünschte Sprache -> Seitenstandardsprache -> kein
// Treffer (eingebauter Standardtext). Das Subscriber-Modell speichert heute keine
// Sprache; preferred ist die Sprache des Abonnenten (Subscriber.Language).
func TestSubscriberMailTemplate_Reihenfolge(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "p", Slug: "reihenfolge"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
	// die gewünschte Sprache zählt nur, wenn die Seite sie aktiv hat.
	require.NoError(t, db.Create(&models.Language{Code: "en", Name: "English", IsActive: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
	kind := NotificationKindIncidentCreated
	mk := func(lang string) {
		require.NoError(t, db.Create(&models.NotificationTemplate{StatusPageID: page.ID, Kind: kind, Audience: NotificationAudienceSubscriber, Language: lang, Subject: "S-" + lang, BodyHTML: "B"}).Error)
	}

	tmpl, lang := h.subscriberMailTemplate(page.ID, kind, "en")
	assert.Nil(t, tmpl, "keine Vorlage gepflegt: eingebauter Standardtext")
	assert.Equal(t, "en", lang, "gewünschte Sprache bestimmt den Standardtext")

	mk("de")
	tmpl, lang = h.subscriberMailTemplate(page.ID, kind, "en")
	require.NotNil(t, tmpl)
	assert.Equal(t, "S-de", tmpl.Subject, "Rückfall auf die Seitenstandardsprache")
	assert.Equal(t, "de", lang)

	mk("en")
	tmpl, lang = h.subscriberMailTemplate(page.ID, kind, "en")
	require.NotNil(t, tmpl)
	assert.Equal(t, "S-en", tmpl.Subject, "gewünschte Sprache vor Standardsprache")
	assert.Equal(t, "en", lang)

	tmpl, _ = h.subscriberMailTemplate(page.ID, kind, "de")
	require.NotNil(t, tmpl)
	assert.Equal(t, "S-de", tmpl.Subject, "gleich der Standardsprache: nur einmal geprüft")

	tmpl, _ = h.subscriberMailTemplate(page.ID, "", "")
	assert.Nil(t, tmpl, "leerer Kind: keine Vorlage")
}
