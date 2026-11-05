package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Bei mehreren @ trennt das letzte die Domain, auch bei Gmail: Der Teil davor
// gehört zum lokalen Teil und verliert dort seine Punkte.
func TestMailboxKey_LetztesATrenntDieDomainAuchBeiGmail(t *testing.T) {
	assert.Equal(t, "ab@c"+gmail, mailboxKey("a.b@c"+gmail))
	assert.Equal(t, "ab@c"+gmail, mailboxKey("A.B@c"+googlemail))
}

// Die Suche nach gespeicherten Varianten folgt demselben Schnitt: Mit mehreren @
// trifft sie die Punkt-Variante desselben Gmail-Postfachs.
func TestMailboxMatch_MehrereATreffenDiePunktVarianteBeiGmail(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Create(&models.Subscriber{Channel: "email", Email: "a.b@c" + gmail}).Error)
	clause, args := mailboxMatch("ab@c" + gmail)
	var n int64
	require.NoError(t, db.Model(&models.Subscriber{}).Where("channel = 'email' AND "+clause, args...).Count(&n).Error)
	assert.EqualValues(t, 1, n)
}

// Die Gmail-Regel (Punkte entfallen) gilt nur für die ganze Domain gmail.com
// bzw. googlemail.com. Eine Domain, die nach Entfernen der Punkte so aussieht
// (".gmail.com" mit führendem Punkt), ist ein anderes Postfach.
func TestMailboxMatch_GmailDomainMussGanzUebereinstimmen(t *testing.T) {
	db := testutils.SetupTestDB()
	for _, e := range []string{"victim@." + gmailDom, "victim@.googlemail.com"} {
		require.NoError(t, db.Create(&models.Subscriber{Channel: "email", Email: e}).Error)
	}
	count := func(query string) int64 {
		clause, args := mailboxMatch(query)
		var n int64
		require.NoError(t, db.Model(&models.Subscriber{}).Where("channel = 'email' AND "+clause, args...).Count(&n).Error)
		return n
	}
	assert.EqualValues(t, 0, count("victim"+gmail), "Domain mit führendem Punkt ist nicht gmail.com")
	assert.EqualValues(t, 0, count("victim"+googlemail))

	require.NoError(t, db.Create(&models.Subscriber{Channel: "email", Email: "vic.tim" + gmail}).Error)
	assert.EqualValues(t, 1, count("victim"+gmail), "Gegenprobe: die echte Gmail-Variante wird gefunden")
}

// isASCII trennt bei 0x80: 0x7f ist noch ASCII, ein einzelnes Byte ab 0x80 nicht.
func TestIsASCII_Grenze(t *testing.T) {
	assert.True(t, isASCII("a\x7f"))
	assert.False(t, isASCII("a\x80"))
	assert.False(t, isASCII("\xff"))
	assert.True(t, isASCII(""))
}
