package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// An auto-join default role must not modify anything (create,
// update, delete - and the legacy value write) and must not read subscribers.
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

// Someone also counts as a "site admin" in the sense of "delete account" if they
// have pages:update (formerly pages:write); the legacy value still counts.
func TestPermissionListContains_NutztDasRechtemodell(t *testing.T) {
	assert.True(t, permissionListContains("pages:update", pagesWritePermission))
	assert.True(t, permissionListContains("pages:write", pagesWritePermission), "Altwert")
	assert.False(t, permissionListContains("pages:read,pages:create", pagesWritePermission))
	assert.True(t, permissionListContains("external_sources:update", permComponentsWrite))
	assert.True(t, permissionListContains("components:write", permComponentsWrite), "Altwert components:write war früher an die externen Quellen gekoppelt")
	assert.False(t, permissionListContains("components:update", permComponentsWrite), "Neu-Recht koppelt nicht")
}
