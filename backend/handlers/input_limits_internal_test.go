package handlers

import (
	"errors"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
)

// Das Logo wird als <img src> ausgeliefert: erlaubt sind nur eine https-URL mit
// Host oder ein absoluter Pfad auf dem eigenen Host; Schemata wie javascript:
// und data:, unverschlüsseltes http und übergroße Werte werden abgelehnt.
func TestValidateBranding_LogoURL(t *testing.T) {
	valid := func(logo string) bool {
		_, ok := validateBranding(&models.StatusPage{LogoURL: logo, PageLayout: "standard"})
		return ok
	}
	for _, ok := range []string{"", "https://example.com/logo.png", "/logo.png", "/uploads/a b.png"} {
		assert.True(t, valid(ok), "erlaubt: %q", ok)
	}
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"http://tracker.example.org/p.gif",
		"//evil.example/logo.png",
		"https:///kein-host.png",
		"ftp://example.com/logo.png",
		"https://example.com/\nlogo.png",
		"https://example.com/" + strings.Repeat("a", 2100),
	} {
		assert.False(t, valid(bad), "abgelehnt: %.40q", bad)
	}
}

// Eine Adresse über der RFC-5321-Grenze von 254 Zeichen ist keine gültige
// Abonnentenadresse (verhindert Megabyte-Adressen in der Datenbank).
func TestIsPlainEmailAddress_RejectsOverlongAddresses(t *testing.T) {
	assert.True(t, isPlainEmailAddress("a@example.com"))
	long := strings.Repeat("a", 250) + "@example.com"
	assert.False(t, isPlainEmailAddress(long))
	assert.True(t, isPlainEmailAddress(strings.Repeat("a", 64)+"@"+strings.Repeat("b", 63)+".example.com"))
}

func TestCheckInputTexts(t *testing.T) {
	codeOf := func(err error) string {
		var ce *codedError
		if errors.As(err, &ce) {
			return ce.code
		}
		return ""
	}
	assert.NoError(t, checkInputTexts(textField{"", 5}, textField{"abcde", 5}, textField{"Zürich", 20}))
	assert.Equal(t, codeInputTooLong, codeOf(checkInputTexts(textField{"abcdef", 5})))
	assert.Equal(t, codeInputTooLong, codeOf(checkInputTexts(textField{"ü", 1})), "Grenze zählt Byte")
	for name, v := range map[string]string{
		"NUL": "a\x00b", "Tab": "a\tb", "Zeilenumbruch": "a\nb", "Wagenrücklauf": "a\rb",
		"DEL": "a\x7fb", "C1-Steuerzeichen": "a\u0085b", "ungültiges UTF-8": "a\xffb",
	} {
		assert.Equal(t, codeInputInvalidCharacters, codeOf(checkInputTexts(textField{v, 50})), name)
	}
	// Länge hat Vorrang vor Zeichen, die Prüfung bricht beim ersten Fehler ab.
	assert.Equal(t, codeInputTooLong, codeOf(checkInputTexts(textField{"x\x00", 50}, textField{"toolong", 3})))
}

// Jede Obergrenze der öffentlichen Eingaben liegt genau auf ihrem Wert: ein Byte
// darunter und darauf geht durch, eines darüber nicht. Die Zahlen stehen hier
// absichtlich ausgeschrieben, damit eine unbemerkte Änderung der Konstanten auffällt.
func TestInputLimits_ExactBoundaries(t *testing.T) {
	a := func(n int) string { return strings.Repeat("a", n) }
	tooLong := func(err error) bool {
		var ce *codedError
		return errors.As(err, &ce) && ce.code == codeInputTooLong
	}
	fields := []struct {
		name  string
		limit int
		check func(value string) error
	}{
		{"E-Mail", 254, func(v string) error { return checkSubscriberTexts(v, "", "", "") }},
		{"Webhook-URL", 2048, func(v string) error { return checkSubscriberTexts("", v, "", "") }},
		{"Komponentenliste", 8192, func(v string) error { return checkSubscriberTexts("", "", v, "") }},
		{"Sprache", 16, func(v string) error { return checkSubscriberTexts("", "", "", v) }},
		{"Slug", 100, func(v string) error { return checkInputTexts(textField{v, maxSlugInputLength}) }},
	}
	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			assert.NoError(t, f.check(a(f.limit-1)))
			assert.NoError(t, f.check(a(f.limit)))
			assert.True(t, tooLong(f.check(a(f.limit+1))), "ein Byte über der Grenze")
		})
	}
	assert.Equal(t, 254, maxEmailLength)
	assert.Equal(t, 120, maxOrderNameLength)
	assert.Equal(t, 60, maxOrderSlugLength)
}
