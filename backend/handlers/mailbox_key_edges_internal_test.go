package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// With multiple @, the last one separates the domain, also for Gmail: the part before it
// belongs to the local part and loses its dots there.
func TestMailboxKey_LetztesATrenntDieDomainAuchBeiGmail(t *testing.T) {
	assert.Equal(t, "ab@c"+gmail, mailboxKey("a.b@c"+gmail))
	assert.Equal(t, "ab@c"+gmail, mailboxKey("A.B@c"+googlemail))
}

// The lookup of stored variants follows the same split: with multiple @
// it matches the dotted variant of the same Gmail mailbox.
func TestMailboxMatch_MehrereATreffenDiePunktVarianteBeiGmail(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Create(&models.Subscriber{Channel: "email", Email: "a.b@c" + gmail}).Error)
	clause, args := mailboxMatch("ab@c" + gmail)
	var n int64
	require.NoError(t, db.Model(&models.Subscriber{}).Where("channel = 'email' AND "+clause, args...).Count(&n).Error)
	assert.EqualValues(t, 1, n)
}

// The Gmail rule (dots are dropped) applies only to the entire domain gmail.com
// or googlemail.com. A domain that looks like that after removing the dots
// (".gmail.com" with a leading dot) is a different mailbox.
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

// isASCII splits at 0x80: 0x7f is still ASCII, a single byte from 0x80 on is not.
func TestIsASCII_Grenze(t *testing.T) {
	assert.True(t, isASCII("a\x7f"))
	assert.False(t, isASCII("a\x80"))
	assert.False(t, isASCII("\xff"))
	assert.True(t, isASCII(""))
}
