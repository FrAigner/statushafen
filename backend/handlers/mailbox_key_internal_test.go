package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Die Gmail-Domains stehen zusammengesetzt im Test, damit die Repo-Prüfung auf
// private Postfachadressen nicht anschlägt (es sind Beispiele, keine Konten).
const (
	gmailDom   = "gmail.com"
	gmail      = "@" + gmailDom
	googlemail = "@googlemail.com"
)

// Gmail ignoriert Punkte im lokalen Teil und behandelt googlemail.com als
// Alias von gmail.com; beides gehört in den Postfach-Schlüssel, damit
// Schreibvarianten desselben Postfachs das Limit nicht umgehen. Bei anderen
// Domains sind Punkte Teil der Adresse (Gegenprobe).
func TestMailboxKey_GmailAequivalenzUndUnicode(t *testing.T) {
	cases := []struct{ in, want, why string }{
		{"a.b" + gmail, "ab" + gmail, "Punkt bei gmail.com"},
		{"AB" + "@GMAIL.COM", "ab" + gmail, "Großschreibung"},
		{"a.b.c+tag" + gmail, "abc" + gmail, "Punkte und +tag"},
		{"ab" + googlemail, "ab" + gmail, "googlemail.com ist gmail.com"},
		{"a.b+x" + "@GoogleMail.com", "ab" + gmail, "Alias, Punkt und +tag"},
		{"a.b@example.com", "a.b@example.com", "andere Domain: Punkt bleibt"},
		{"a.b@example.org", "a.b@example.org", "andere Domain: Punkt bleibt"},
		{"a.b@mail." + gmailDom, "a.b@mail." + gmailDom, "Subdomain von gmail.com ist nicht gmail.com"},
		{"a.b@g.mail.com", "a.b@g.mail.com", "ähnliche Domain ohne Punkte-Regel"},
		{"a.b@" + gmailDom + ".example.org", "a.b@" + gmailDom + ".example.org", "gmail.com nur als ganze Domain"},
		{"." + gmail, "." + gmail, "nur Punkte: nicht zu leer kürzen"},
		{"+x" + gmail, "+x" + gmail, "leerer Rest vor +tag: nicht kürzen"},
		{"ａｂ@ｅｘａｍｐｌｅ．ｃｏｍ", "ab@example.com", "NFKC: Vollbreitenzeichen"},
		{"ＡＢ.ｃ@ｇｍａｉｌ．ｃｏｍ", "abc" + gmail, "NFKC wirkt vor der Gmail-Regel"},
		{"user@Bücher.example", "user@xn--bcher-kva.example", "IDN-Domain als Punycode"},
		{"user@xn--bcher-kva.example", "user@xn--bcher-kva.example", "Punycode-Eingabe bleibt gleich"},
		{"user@BÜCHER.example", "user@xn--bcher-kva.example", "Großschreibung der IDN-Domain"},
		{"user@A_B.Example", "user@a_b.example", "nicht wandelbare Domain: nur klein schreiben"},
		{"ohne-at", "ohne-at", "kein @"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, mailboxKey(tc.in), tc.why+": "+tc.in)
	}
	assert.NotEqual(t, mailboxKey("a.b@example.com"), mailboxKey("ab@example.com"), "Gegenprobe: bei anderen Domains sind a.b und ab verschieden")
}

// Das Bestätigungs-Limit je Postfach zählt auch gespeicherte Gmail-Varianten
// (Punkte, googlemail.com, +tag), nicht aber ähnlich aussehende fremde Adressen.
func TestConfirmationMailBudget_ZaehltGmailVarianten(t *testing.T) {
	db := testutils.SetupTestDB()
	h := &Handler{DB: db}
	page := models.StatusPage{Name: "Limit Seite", Slug: "limit-seite"}
	require.NoError(t, db.Create(&page).Error)
	exp := time.Now().Add(time.Hour)
	add := func(email string) {
		require.NoError(t, db.Create(&models.Subscriber{
			StatusPageID: page.ID, Channel: "email", Email: email,
			Status: models.SubscriberStatusPending, ConfirmTokenExpiry: &exp,
		}).Error)
	}

	for _, e := range []string{"vic.tim" + gmail, "v.i.c.t.i.m+1" + googlemail, "Victim" + "@GMAIL.com"} {
		add(e)
	}
	assert.True(t, h.confirmationMailBudgetExhausted("victim"+gmail), "drei Varianten desselben Gmail-Postfachs")
	assert.True(t, h.confirmationMailBudgetExhausted("v.ictim"+googlemail), "auch von der Alias-Seite aus gesehen")

	// Gegenprobe: gleiche Zeichen bei anderen Domains sind andere Postfächer.
	for _, e := range []string{"vic.tim@example.com", "v.i.c.t.i.m@example.com", "victim@g.mail.com", "vic.tim@mail." + gmailDom} {
		add(e)
	}
	assert.False(t, h.confirmationMailBudgetExhausted("victim@example.com"), "example.com: Punkte zählen")
	assert.False(t, h.confirmationMailBudgetExhausted("victim@mail."+gmailDom), "Subdomain von gmail.com ist eigenes Postfach")
}

// Nicht-ASCII-Eingaben stehen roh in der Datenbank. Ihre unveränderte
// Wiederholung zählt weiter zum Limit, obwohl der Schlüssel (NFKC) anders
// aussieht als der gespeicherte Wert.
func TestMailboxMatch_NichtASCIIEingabeBleibtAuffindbar(t *testing.T) {
	clause, args := mailboxMatch("ＶＩＣ@example.com")
	assert.Contains(t, clause, "lower(email) = ? OR lower(email) = ?")
	assert.Equal(t, []any{"vic@example.com", "ｖｉｃ@example.com", "vic+%@example.com"}, args)

	// reine ASCII-Eingabe: keine zusätzliche Roh-Bedingung
	_, args = mailboxMatch("VIC@example.com")
	assert.Len(t, args, 2)

	assert.True(t, isASCII("abc@example.com"))
	assert.False(t, isASCII("äbc@example.com"))

	db := testutils.SetupTestDB()
	h := &Handler{DB: db}
	page := models.StatusPage{Name: "Roh Seite", Slug: "roh-seite"}
	require.NoError(t, db.Create(&page).Error)
	exp := time.Now().Add(time.Hour)
	for i := 0; i < maxConfirmationMailsPerMailbox; i++ {
		require.NoError(t, db.Create(&models.Subscriber{
			StatusPageID: page.ID, Channel: "email", Email: "ｖｉｃ@example.com",
			Status: models.SubscriberStatusPending, ConfirmTokenExpiry: &exp,
		}).Error)
	}
	assert.True(t, h.confirmationMailBudgetExhausted("ＶＩＣ@example.com"), "dieselbe Eingabe wiederholt: Limit greift")
	assert.False(t, h.confirmationMailBudgetExhausted("andere@example.com"), "Gegenprobe")
}

// Die Suche nach gespeicherten Schreibvarianten trifft genau das Postfach der
// Eingabe: Zeichen mit Bedeutung in LIKE (_ und %) stehen als Text, beide
// Gmail-Domains zählen, und fremde Adressen mit ähnlicher Form nicht.
func TestMailboxMatch_FindetNurDasEigenePostfach(t *testing.T) {
	cases := []struct {
		stored, query string
		match         bool
		why           string
	}{
		{"axb+t@example.com", "a_b@example.com", false, "Unterstrich ist kein Platzhalter"},
		{"a_b+t@example.com", "a_b@example.com", true, "Unterstrich als Text, +tag"},
		{"ab+1@example.com", "a%b@example.com", false, "Prozentzeichen ist kein Platzhalter"},
		{"axb+t" + gmail, "a_b" + gmail, false, "Gmail: Unterstrich ist kein Platzhalter"},
		{"a_b+t" + gmail, "a_b" + gmail, true, "Gmail: Unterstrich als Text, +tag"},
		{"ab+1" + gmail, "a%b" + gmail, false, "Gmail: Prozentzeichen ist kein Platzhalter"},
		{"victim" + googlemail, "vict.im" + gmail, true, "gespeichert googlemail.com ohne Punkt und +tag, gesucht mit Punkt bei gmail.com"},
		{"victim" + gmail, "victim" + googlemail, true, "gespeichert gmail.com, gesucht über googlemail.com"},
		{"vic.tim+x" + gmail, "victim" + gmail, true, "gespeichert mit Punkt und +tag"},
		{"vic.tim+x" + googlemail, "victim" + gmail, true, "gespeichert googlemail.com mit Punkt und +tag"},
		{"victim@g.mail.com", "victim" + gmail, false, "g.mail.com ist nicht gmail.com"},
		{"victim" + gmail, "other" + gmail, false, "anderes Postfach"},
	}
	for _, tc := range cases {
		db := testutils.SetupTestDB()
		require.NoError(t, db.Create(&models.Subscriber{Channel: "email", Email: tc.stored}).Error)
		clause, args := mailboxMatch(tc.query)
		var n int64
		require.NoError(t, db.Model(&models.Subscriber{}).Where("channel = 'email' AND "+clause, args...).Count(&n).Error, tc.why)
		assert.Equal(t, tc.match, n == 1, "%s (gespeichert %s, gesucht %s)", tc.why, tc.stored, tc.query)
	}

	// Ohne verwertbares @ gilt nur der exakte Vergleich.
	for _, in := range []string{"ohne-at", "@" + gmailDom, "@example.com"} {
		clause, args := mailboxMatch(in)
		assert.Equal(t, "lower(email) = ?", clause, in)
		assert.Equal(t, []any{in}, args, in)
	}
}

// Schlüssel in Grenzfällen: mehrere @ (der letzte trennt die Domain), leere Domain.
func TestMailboxKey_Grenzfaelle(t *testing.T) {
	assert.Equal(t, "a@b@example.com", mailboxKey("a@b@Example.com"), "der letzte @ trennt die Domain")
	assert.Equal(t, "user@", mailboxKey("User@"), "leere Domain bleibt leer")
	assert.Equal(t, "ab"+gmail, mailboxKey("a.b+x"+gmail))
}
