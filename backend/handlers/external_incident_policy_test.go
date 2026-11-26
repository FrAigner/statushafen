package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator decision: reports without linked components
// stay mirrored but do not trigger subscriber mails; staff members
// get no mails; subscribers get the site's default language.

func TestExternalIncidentMail_OhneKomponentenGespiegeltAberStill(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")

	noComp := extInc("ohne", "investigating", "Allgemeine Störung")
	noComp.Components = nil
	unlinked := extInc("fremd", "investigating", "Andere Komponente")
	unlinked.Components = []externalstatus.IncidentComponent{{ExternalID: "nicht-gekoppelt", Name: "Nicht gekoppelt"}}
	s.setFeed(models.ExternalIncidentKindIncident, noComp, unlinked)
	require.True(t, s.poll().OK)

	drainMails(t, srv, 0)
	var rows int64
	require.NoError(t, s.db.Model(&models.ExternalIncident{}).Where("external_id IN ?", []string{"ohne", "fremd"}).Count(&rows).Error)
	assert.EqualValues(t, 2, rows, "gespiegelt")
	assert.EqualValues(t, 0, s.auditCount("external_incident.notified"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.failed"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.mirrored"), "kein Ereignis ohne gekoppelte Komponente")

	// As a check: the same source with a linked component does report.
	s.setFeed(models.ExternalIncidentKindIncident, noComp, unlinked, extInc("mit", "investigating", "Betrifft VM"))
	require.True(t, s.poll().OK)
	assert.Equal(t, []string{"alle@example.com"}, recipients(drainMails(t, srv, 1)))
}

func TestExternalIncidentMail_MitarbeitendeBekommenKeineMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("abo@example.com", "all")
	var staff []models.User
	require.NoError(t, s.db.Find(&staff).Error)
	require.NotEmpty(t, staff, "Fixture hat Mitarbeitende mit E-Mail-Adresse")
	// A template with audience staff changes nothing here either.
	require.NoError(t, s.db.Create(&models.NotificationTemplate{
		StatusPageID: s.page.ID, Kind: "external_incident_created", Audience: "staff", Language: "de",
		Subject: "Intern {{.Title}}", BodyHTML: "<p>intern</p>",
	}).Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "Läuft"))
	require.True(t, s.poll().OK)
	mails := drainMails(t, srv, 1)
	assert.Equal(t, []string{"abo@example.com"}, recipients(mails))
	assert.NotContains(t, mails[0].subject, "Intern", "Staff-Vorlage wird für Abonnenten nicht benutzt")
}

func TestExternalIncidentMail_AbonnentenBekommenStandardspracheDerSeite(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("abo@example.com", "all")
	// Staff language is irrelevant: A staff member with English does not change the subscription mail.
	require.NoError(t, s.db.Model(&models.User{}).Where("1 = 1").Update("preferred_language", "en-GB").Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "Wir prüfen"))
	require.True(t, s.poll().OK)
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "[EXTERN]", "Standardsprache der Seite ist Deutsch")
	assert.NotContains(t, m.subject, "[EXTERNAL]")
}

// The cleanup run writes an audit entry with the count.
func TestMirroredIncidentsPruned_SchreibtAuditEintrag(t *testing.T) {
	s := newIncidentSetup(t)
	s.h.MirroredIncidentsPruned(context.Background(), 7, 90*24*time.Hour)

	var entry models.AuditLog
	require.NoError(t, s.db.Where("action = ?", "external_incident.pruned").First(&entry).Error)
	assert.Equal(t, "system", entry.ActorEmail)
	assert.Equal(t, "ExternalIncident", entry.TargetType)
	assert.Equal(t, "count=7 retention_days=90", entry.Details)
}
