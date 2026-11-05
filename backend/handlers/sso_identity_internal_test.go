package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Identität wird über Seite, Protokoll, Issuer, Subject und Format
// gesucht. Ein leeres Subject (oder Issuer) darf nie als "beliebig" gelten:
// GORMs Struktur-Where ignorierte Nullwerte und lieferte sonst die erste
// verknüpfte Identität der Seite, also fremde Konten.
func TestResolveSSOUser_EmptySubjectOrIssuerNeverMatchesAnotherIdentity(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)

	page := models.StatusPage{Name: "SSO", Slug: "sso-identity"}
	require.NoError(t, db.Create(&page).Error)
	role := models.Role{Name: "Viewer SSO", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	victim := models.User{Email: "opfer@example.com"}
	require.NoError(t, db.Create(&victim).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: victim.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
	ident := models.UserSSOIdentity{UserID: victim.ID, StatusPageID: page.ID, Protocol: "oidc", Issuer: "https://idp.example.com", Subject: "sub-opfer"}
	require.NoError(t, db.Create(&ident).Error)

	t.Run("Gegenprobe: bekannte Identität meldet das richtige Konto an", func(t *testing.T) {
		user, e := h.resolveSSOUser(0, models.UserSSOIdentity{StatusPageID: page.ID, Protocol: "oidc", Issuer: ident.Issuer, Subject: ident.Subject}, "x@example.com")
		require.Nil(t, e)
		assert.Equal(t, victim.ID, user.ID)
	})

	for name, key := range map[string]models.UserSSOIdentity{
		"leeres Subject":                       {StatusPageID: page.ID, Protocol: "oidc", Issuer: ident.Issuer, Subject: ""},
		"leeres Subject und leerer Issuer":     {StatusPageID: page.ID, Protocol: "oidc"},
		"nur Subject, Issuer leer":             {StatusPageID: page.ID, Protocol: "oidc", Subject: ident.Subject},
		"leeres Protokoll und Subject":         {StatusPageID: page.ID, Issuer: ident.Issuer},
		"Subject bekannt, Format abweichend":   {StatusPageID: page.ID, Protocol: "oidc", Issuer: ident.Issuer, Subject: ident.Subject, SubjectFormat: "urn:x"},
		"Subject unbekannt, sonst vollständig": {StatusPageID: page.ID, Protocol: "oidc", Issuer: ident.Issuer, Subject: "unbekannt"},
	} {
		t.Run(name, func(t *testing.T) {
			user, e := h.resolveSSOUser(0, key, "angreifer@example.com")
			require.NotNil(t, e, "darf kein Konto liefern, bekam %d", user.ID)
			assert.Zero(t, user.ID)
		})
	}

	t.Run("Verknüpfen mit einem leeren Schlüsselteil legt keine Identität an", func(t *testing.T) {
		other := models.User{Email: "link@example.com"}
		require.NoError(t, db.Create(&other).Error)
		full := models.UserSSOIdentity{StatusPageID: page.ID, Protocol: "oidc", Issuer: ident.Issuer, Subject: "sub-neu"}
		for name, blank := range map[string]func(k *models.UserSSOIdentity){
			"Seite":     func(k *models.UserSSOIdentity) { k.StatusPageID = 0 },
			"Protokoll": func(k *models.UserSSOIdentity) { k.Protocol = "" },
			"Issuer":    func(k *models.UserSSOIdentity) { k.Issuer = "" },
			"Subject":   func(k *models.UserSSOIdentity) { k.Subject = "" },
		} {
			key := full
			blank(&key)
			_, e := h.resolveSSOUser(other.ID, key, "link@example.com")
			require.NotNil(t, e, name)
			var n int64
			db.Model(&models.UserSSOIdentity{}).Where("user_id = ?", other.ID).Count(&n)
			assert.Zero(t, n, "%s leer: keine Identität angelegt", name)
		}
	})

	t.Run("die E-Mail anderer Identitäten bleibt unverändert", func(t *testing.T) {
		_, _ = h.resolveSSOUser(0, models.UserSSOIdentity{StatusPageID: page.ID, Protocol: "oidc"}, "ueberschrieben@example.com")
		var got models.UserSSOIdentity
		require.NoError(t, db.First(&got, ident.ID).Error)
		assert.NotEqual(t, "ueberschrieben@example.com", got.Email)
	})
}

// Jede der fünf Schlüsselspalten (Seite, Protokoll, Issuer, Subject, Format)
// zählt beim Suchen und beim Aktualisieren der E-Mail. Der Test legt je Spalte
// eine Identität an, die sich nur in dieser einen Spalte unterscheidet; keine
// darf als die bekannte gelten, und ihre Daten bleiben unberührt.
func TestResolveSSOUser_EveryKeyColumnSeparatesIdentities(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)

	page := models.StatusPage{Name: "SSO Spalten", Slug: "sso-spalten"}
	otherPage := models.StatusPage{Name: "SSO Spalten 2", Slug: "sso-spalten-2"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&otherPage).Error)
	owner := models.User{Email: "inhaberin@example.com"}
	require.NoError(t, db.Create(&owner).Error)

	known := models.UserSSOIdentity{
		UserID: owner.ID, StatusPageID: page.ID, Protocol: "oidc",
		Issuer: "https://idp.example.com", Subject: "sub-bekannt", SubjectFormat: "urn:fmt:a", Email: "alt@example.com",
	}
	require.NoError(t, db.Create(&known).Error)

	differ := map[string]func(k models.UserSSOIdentity) models.UserSSOIdentity{
		"Seite":     func(k models.UserSSOIdentity) models.UserSSOIdentity { k.StatusPageID = otherPage.ID; return k },
		"Protokoll": func(k models.UserSSOIdentity) models.UserSSOIdentity { k.Protocol = "saml"; return k },
		"Issuer":    func(k models.UserSSOIdentity) models.UserSSOIdentity { k.Issuer = "https://idp.example.org"; return k },
		"Subject":   func(k models.UserSSOIdentity) models.UserSSOIdentity { k.Subject = "sub-anders"; return k },
		"Format":    func(k models.UserSSOIdentity) models.UserSSOIdentity { k.SubjectFormat = "urn:fmt:b"; return k },
		// GORMs Struktur-Where ließe ein leeres Format weg und träfe damit die Identität mit Format.
		"Format leer": func(k models.UserSSOIdentity) models.UserSSOIdentity { k.SubjectFormat = ""; return k },
	}

	t.Run("Gegenprobe: der volle Schlüssel findet die Identität und aktualisiert nur sie", func(t *testing.T) {
		user, e := h.resolveSSOUser(0, known, "neu@example.com")
		require.Nil(t, e)
		assert.Equal(t, owner.ID, user.ID)
		var got models.UserSSOIdentity
		require.NoError(t, db.First(&got, known.ID).Error)
		assert.Equal(t, "neu@example.com", got.Email)
	})

	for column, mutate := range differ {
		t.Run("nur "+column+" abweichend: keine Anmeldung", func(t *testing.T) {
			require.NoError(t, db.Model(&models.UserSSOIdentity{}).Where("id = ?", known.ID).Update("email", "alt@example.com").Error)
			key := mutate(known)
			key.ID, key.UserID, key.Email = 0, 0, ""
			user, e := h.resolveSSOUser(0, key, "angreifer@example.com")
			require.NotNil(t, e)
			assert.Zero(t, user.ID)
			var got models.UserSSOIdentity
			require.NoError(t, db.First(&got, known.ID).Error)
			assert.Equal(t, "alt@example.com", got.Email, "die E-Mail der bekannten Identität bleibt")
		})
	}

	// Die E-Mail-Aktualisierung trifft nur die Identität mit dem vollen Schlüssel,
	// auch wenn sich Geschwister-Identitäten in genau einer Spalte unterscheiden.
	t.Run("die Aktualisierung der E-Mail trifft keine Geschwister-Identität", func(t *testing.T) {
		sibling := map[string]uint{}
		for column, mutate := range differ {
			s := mutate(known)
			s.ID, s.Email, s.UserID = 0, "geschwister-"+column+"@example.com", owner.ID
			require.NoError(t, db.Create(&s).Error, column)
			sibling[column] = s.ID
		}
		_, e := h.resolveSSOUser(0, known, "gezielt@example.com")
		require.Nil(t, e)
		for column, id := range sibling {
			var got models.UserSSOIdentity
			require.NoError(t, db.First(&got, id).Error)
			assert.Equal(t, "geschwister-"+column+"@example.com", got.Email, "Geschwister (%s) unverändert", column)
		}
	})
}
