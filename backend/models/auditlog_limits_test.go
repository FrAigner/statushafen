package models_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Audit fields partly come from user input (email at login, honeypot).
// The hook truncates them centrally on save, whichever path creates the entry.
func TestAuditLog_BeforeCreateTruncatesFields(t *testing.T) {
	db := testutils.SetupTestDB()
	huge := strings.Repeat("x", 2<<20)
	entry := models.AuditLog{
		Action: huge, Category: huge,
		ActorEmail: huge, ActorRole: huge, TargetType: huge, TargetID: huge, TargetName: huge, IP: huge, Details: huge,
	}
	require.NoError(t, db.Create(&entry).Error)

	var stored models.AuditLog
	require.NoError(t, db.First(&stored, entry.ID).Error)
	assert.Equal(t, 255, utf8.RuneCountInString(stored.TargetName))
	assert.Equal(t, 255, utf8.RuneCountInString(stored.TargetID))
	assert.Equal(t, 254, utf8.RuneCountInString(stored.ActorEmail))
	// Each field is truncated to exactly its limit (not just "somehow smaller"),
	// so that the removal of a single truncation is noticed.
	assert.Equal(t, 255, utf8.RuneCountInString(stored.ActorRole))
	assert.Equal(t, 128, utf8.RuneCountInString(stored.Action))
	assert.Equal(t, 64, utf8.RuneCountInString(stored.Category))
	assert.Equal(t, 64, utf8.RuneCountInString(stored.TargetType))
	assert.Equal(t, 64, utf8.RuneCountInString(stored.IP))
	assert.Equal(t, 4000, utf8.RuneCountInString(stored.Details))
}

func TestAuditLog_TruncationIsUTF8Safe(t *testing.T) {
	db := testutils.SetupTestDB()
	// 300 umlauts = 600 bytes: a cut by bytes would split a character.
	entry := models.AuditLog{Action: "a", Category: "auth", TargetName: strings.Repeat("ä", 300)}
	require.NoError(t, db.Create(&entry).Error)
	var stored models.AuditLog
	require.NoError(t, db.First(&stored, entry.ID).Error)
	assert.True(t, utf8.ValidString(stored.TargetName))
	assert.Equal(t, strings.Repeat("ä", 255), stored.TargetName)

	// Invalid UTF-8 sequences (Postgres rejects them in text) are replaced.
	bad := models.AuditLog{Action: "a", Category: "auth", TargetName: "ab\xff\xfecd"}
	require.NoError(t, db.Create(&bad).Error)
	var storedBad models.AuditLog
	require.NoError(t, db.First(&storedBad, bad.ID).Error)
	assert.True(t, utf8.ValidString(storedBad.TargetName))
	// A contiguous invalid sequence becomes a single replacement character.
	assert.Equal(t, "ab\uFFFDcd", storedBad.TargetName)
}

func TestAuditLog_NormalValuesStayUntouched(t *testing.T) {
	db := testutils.SetupTestDB()
	entry := models.AuditLog{
		Action: "incident.created", Category: "incident", ActorEmail: "a@example.com", ActorRole: "Page Admin",
		TargetType: "Incident", TargetID: "42", TargetName: strings.Repeat("n", 255), IP: "2001:db8::1", Details: `{"a":"b"}`,
	}
	want := entry
	require.NoError(t, db.Create(&entry).Error)
	var stored models.AuditLog
	require.NoError(t, db.First(&stored, entry.ID).Error)
	assert.Equal(t, want.TargetName, stored.TargetName, "genau 255 Zeichen bleiben")
	assert.Equal(t, want.ActorEmail, stored.ActorEmail)
	assert.Equal(t, want.IP, stored.IP)
	assert.Equal(t, want.Details, stored.Details)
	assert.Equal(t, want.TargetType, stored.TargetType)
}

func TestIsAuditCategory(t *testing.T) {
	for _, c := range models.AuditCategories {
		assert.True(t, models.IsAuditCategory(c), c)
	}
	for _, bad := range []string{"", "AUTH", "unbekannt", "auth "} {
		assert.False(t, models.IsAuditCategory(bad), bad)
	}
}
