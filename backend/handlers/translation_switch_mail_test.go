package handlers_test

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With the switch off, notification mails go out in the page default language
// only: a subscriber's stored preference (en) and an internal user's UI language
// (en) are ignored, but kept.
func TestTranslationSwitchOff_MailsInPageDefaultLanguage(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "switch-off-mail", "switch-off-admin@example.com")
	setPageDefaultLanguage(t, db, page.ID, "de")
	require.NoError(t, db.Where("code = ?", "en").FirstOrCreate(&models.Language{Code: "en", Name: "English", IsActive: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
	addLangSubscriber(t, db, page.ID, "switch-off-sub-en@example.com", "en")
	require.NoError(t, db.Create(&models.User{Email: "switch-off-staff-en@example.com", IsGlobalAdmin: true, PreferredLanguage: "en"}).Error)
	inc := seedIncident(t, db, page.ID, "Latenz", "Wir prüfen.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	got := collectMailsByRecipient(t, srv, 3, "switch-off-sub-en@example.com", "switch-off-staff-en@example.com", "switch-off-admin@example.com")
	for addr, msg := range got {
		assert.Contains(t, msg, "Status-Update für", addr)
		assert.NotContains(t, msg, "Status update for", addr)
	}
	var sub models.Subscriber
	require.NoError(t, db.Where("email = ?", "switch-off-sub-en@example.com").First(&sub).Error)
	assert.Equal(t, "en", sub.Language, "the stored preference is kept")
}
