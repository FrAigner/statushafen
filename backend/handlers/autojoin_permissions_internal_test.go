package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Eine Auto-Join-Standardrolle darf nichts verändern (create,
// update, delete - und den Altwert write) und keine Abonnenten lesen.
func TestRoleSafeAsDefault_VerlangtNurLesen(t *testing.T) {
	cases := map[string]bool{
		"components:read,incidents:read,notices:read": true,
		"components:read,external_sources:read":       true,
		"":                                            true,
		"admin_dashboard":                             true,
		"components:create":                           false,
		"notices:update":                              false,
		"incidents:delete":                            false,
		"components:read,incident_templates:create":     false,
		"components:write":                              false,
		"subscribers:read":                              false,
		"components:read,subscribers:read":              false,
		"notification_templates:update,components:read": false,
	}
	for perms, want := range cases {
		assert.Equal(t, want, roleSafeAsDefault(perms), perms)
	}
}

// Auch ein „Seiten-Admin“ im Sinne von „Konto löschen“ ist, wer
// pages:update hat (früher pages:write); der Altwert zählt weiter.
func TestPermissionListContains_NutztDasRechtemodell(t *testing.T) {
	assert.True(t, permissionListContains("pages:update", pagesWritePermission))
	assert.True(t, permissionListContains("pages:write", pagesWritePermission), "Altwert")
	assert.False(t, permissionListContains("pages:read,pages:create", pagesWritePermission))
	assert.True(t, permissionListContains("external_sources:update", permComponentsWrite))
	assert.True(t, permissionListContains("components:write", permComponentsWrite), "Altwert components:write war früher an die externen Quellen gekoppelt")
	assert.False(t, permissionListContains("components:update", permComponentsWrite), "Neu-Recht koppelt nicht")
}
