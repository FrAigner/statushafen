package handlers

import (
	"strings"
	"testing"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
)

// reine Funktionen der Vorfall-Benachrichtigung.

func TestExternalIncidentKind_Tabelle(t *testing.T) {
	const inc, maint = models.ExternalIncidentKindIncident, models.ExternalIncidentKindMaintenance
	cases := []struct {
		kind  string
		event externalstatus.IncidentEventKind
		want  string
	}{
		{inc, externalstatus.IncidentCreated, NotificationKindExternalIncidentCreated},
		{inc, externalstatus.IncidentUpdated, NotificationKindExternalIncidentUpdated},
		{inc, externalstatus.IncidentResolved, NotificationKindExternalIncidentResolved},
		{maint, externalstatus.IncidentCreated, NotificationKindExternalMaintenanceCreated},
		{maint, externalstatus.IncidentUpdated, NotificationKindExternalMaintenanceUpdated},
		{maint, externalstatus.IncidentResolved, NotificationKindExternalMaintenanceResolved},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		got := externalIncidentKind(tc.kind, tc.event)
		assert.Equal(t, tc.want, got, tc.kind+"/"+string(tc.event))
		assert.True(t, validNotificationKinds[got], "Vorlage muss editierbar sein: "+got)
		seen[got] = true
	}
	assert.Len(t, seen, 6, "sechs verschiedene Vorlagen")
}

func TestExternalIntro_AlleKombinationen(t *testing.T) {
	seen := map[string]bool{}
	for _, de := range []bool{true, false} {
		for _, kind := range []string{models.ExternalIncidentKindIncident, models.ExternalIncidentKindMaintenance} {
			for _, ev := range []externalstatus.IncidentEventKind{externalstatus.IncidentCreated, externalstatus.IncidentUpdated, externalstatus.IncidentResolved} {
				text := externalIntro(kind, ev, de)
				assert.NotEmpty(t, text)
				assert.False(t, seen[text], "jede Kombination hat einen eigenen Text: "+text)
				seen[text] = true
			}
		}
	}
	assert.Len(t, seen, 12)
	assert.Contains(t, externalIntro(models.ExternalIncidentKindMaintenance, externalstatus.IncidentCreated, true), "Wartung")
	assert.Contains(t, externalIntro(models.ExternalIncidentKindMaintenance, externalstatus.IncidentCreated, false), "maintenance")
}

func TestExternalStatusLabel_Tabelle(t *testing.T) {
	cases := []struct{ status, lang, want string }{
		{"in_progress", "de", "In Arbeit"},
		{"in_progress", "en", "In progress"},
		{"completed", "de", "Abgeschlossen"},
		{"completed", "en", "Completed"},
		{"scheduled", "de", "Geplant"},
		{"investigating", "en", "Investigating"},
		{"resolved", "de", "Behoben"},
		{"was-neues", "de", "was-neues"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, externalStatusLabel(tc.status, tc.lang), tc.status+"/"+tc.lang)
	}
}

func TestEscapeMultiline(t *testing.T) {
	assert.Equal(t, "a<br>b<br>c<br>d", escapeMultiline("a\nb\r\nc\rd"))
	assert.Equal(t, "&lt;script&gt;<br>x", escapeMultiline("<script>\nx"))
	assert.NotContains(t, escapeMultiline("1\n2\r\n3"), "\n")
}

func TestTruncateAudit(t *testing.T) {
	assert.Equal(t, "kurz", truncateAudit("kurz"))
	assert.Equal(t, "a b", truncateAudit("a\nb"))
	long := truncateAudit(strings.Repeat("ä", 400))
	assert.LessOrEqual(t, len(long), 300)
	assert.True(t, strings.HasSuffix(long, "ä"), "UTF-8 nicht zerteilt")
}
