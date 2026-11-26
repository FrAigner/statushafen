package handlers

import (
	"errors"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
)

// The logo is served as <img src>: only an https URL with a
// host or an absolute path on the own host are allowed; schemes like javascript:
// and data:, unencrypted http and oversized values are rejected.
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

// An address above the RFC 5321 limit of 254 characters is not a valid
// subscriber address (prevents megabyte-sized addresses in the database).
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
	// Length takes precedence over characters; the check stops at the first error.
	assert.Equal(t, codeInputTooLong, codeOf(checkInputTexts(textField{"x\x00", 50}, textField{"toolong", 3})))
}

// Every upper bound on public inputs sits exactly on its value: one byte
// below and at it passes, one above does not. The numbers are written out here
// on purpose so that an unnoticed change to the constants stands out.
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
