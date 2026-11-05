package logging

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const webhookURL = "https://chat.googleapis.com/v1/spaces/AAA/messages?key=GEHEIMKEY&token=GEHEIMTOKEN"

func TestSanitizeErr_URLErrorOhneURL(t *testing.T) {
	_, err := http.Get("http://127.0.0.1:1/bot123:GEHEIMTOKEN/getUpdates?key=GEHEIMKEY") //nolint:noctx,bodyclose // nur der Fehlertext zählt
	assert.Error(t, err)
	got := SanitizeErr(err)
	assert.NotContains(t, got, "GEHEIMTOKEN")
	assert.NotContains(t, got, "GEHEIMKEY")
	assert.Contains(t, got, "connect")

	ue := &url.Error{Op: "Post", URL: webhookURL, Err: errors.New("EOF")}
	assert.Equal(t, `Post "[URL]": EOF`, SanitizeErr(ue))
	assert.Equal(t, "", SanitizeErr(nil))
}

func TestSanitizeText(t *testing.T) {
	assert.Equal(t, "fetch https://chat.googleapis.com/[REDACTED] failed", SanitizeText("fetch "+webhookURL+" failed"))
	assert.Equal(t, "https://h.example/[REDACTED]", SanitizeText("https://user:pw@h.example/p?token=x"))
	assert.Equal(t, "kein url text", SanitizeText("kein url text"))
	assert.Equal(t, "Key (email)=([REDACTED]) already exists", SanitizeText("Key (email)=(a@b.de) already exists"))
	assert.Equal(t, "Key (a)=([REDACTED])", SanitizeText("Key (a)=(wert)"))
}

func TestLogger_BereinigtStringsUndFehler(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"}))
	l.Warn("SAML metadata fetch failed", "url", webhookURL,
		"error", &url.Error{Op: "Get", URL: webhookURL, Err: errors.New("timeout")})
	out := buf.String()
	assert.False(t, strings.Contains(out, "GEHEIMKEY") || strings.Contains(out, "GEHEIMTOKEN"), out)
	assert.Contains(t, out, "chat.googleapis.com")
}

func TestSanitizeText_UnparsbareURL(t *testing.T) {
	assert.Equal(t, "x [REDACTED] y", SanitizeText("x http://[::1 y"))
	assert.Equal(t, "x [REDACTED] y", SanitizeText("x http:///pfad?token=geheim y"))
}

// SMTP-Server wiederholen die Empfängeradresse oft im Fehlertext; sie darf
// weder in Freitext noch in Fehlerattributen im Klartext im Log stehen.
func TestSanitizeText_MasksEmailAddresses(t *testing.T) {
	assert.Equal(t, "550 5.1.1 <v***@example.org>: Recipient rejected", SanitizeText("550 5.1.1 <victim@example.org>: Recipient rejected"))
	assert.Equal(t, "a***@example.com und b***@example.net", SanitizeText("anna@example.com und bob+tag@example.net"))
	// Gegenprobe: Text ohne Adresse und ein einzelnes @ bleiben unverändert.
	assert.Equal(t, "kein Treffer @ hier", SanitizeText("kein Treffer @ hier"))
}

func TestRedact_ErrorWithRecipientAddressIsMasked(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"}))
	l.Error("Mailversand fehlgeschlagen", "error", errors.New("rcpt victim@example.org: 550 no such user"), "detail", "to=victim@example.org")
	out := buf.String()
	assert.NotContains(t, out, "victim@example.org")
	assert.Contains(t, out, "v***@example.org")
}

func TestURLForLog(t *testing.T) {
	cases := map[string]string{
		"https://idp.example.com/metadata?token=GEHEIM#GEHEIM": "https://idp.example.com/metadata",
		"https://user:GEHEIM@idp.example.com:8443/a/b?x=1":     "https://idp.example.com:8443/a/b",
		"https://idp.example.com":                              "https://idp.example.com",
		"keine url":                                            Redacted,
		"://kaputt":                                            Redacted,
	}
	for in, want := range cases {
		got := URLForLog(in)
		assert.Equal(t, want, got, in)
		if strings.Contains(in, "?") {
			assert.NotContains(t, got, "?", in)
		}
	}
}
