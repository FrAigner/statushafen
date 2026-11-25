package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Links from the SAML era (protocol "saml") stay in the table but have no
// effect: they neither count as a page SSO link on another page (accepting an
// invitation, attaching a page user) nor show up in the profile.
func TestPageSSOIdentity_LegacySAMLRowsDoNotCount(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	user := models.User{Email: "alt-saml@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: user.ID, StatusPageID: 7, Protocol: "saml", Issuer: "i", Subject: "s"}).Error)

	assert.False(t, h.hasSSOIdentityOnOtherPage(user.ID, 3))
	assert.NoError(t, ensureNoRoleOnOtherPage(db, user.ID, 3))

	// Counter-check: an OIDC link on another page still counts.
	require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: user.ID, StatusPageID: 7, Protocol: models.SSOProtocolOIDC, Issuer: "i", Subject: "s"}).Error)
	assert.True(t, h.hasSSOIdentityOnOtherPage(user.ID, 3))
	assert.ErrorIs(t, ensureNoRoleOnOtherPage(db, user.ID, 3), errInvitationOtherPage)
	assert.False(t, h.hasSSOIdentityOnOtherPage(user.ID, 7))
}
