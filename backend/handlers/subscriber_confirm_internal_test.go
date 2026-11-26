package handlers

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Without "@" there is no local part that could be matched for "+tag" variants:
// mailboxMatch falls back to exact comparison, without LIKE.
func TestMailboxMatch_WithoutAtSignMatchesExactly(t *testing.T) {
	clause, args := mailboxMatch("  Kein-At ")
	assert.Equal(t, "lower(email) = ?", clause)
	assert.Equal(t, []any{"kein-at"}, args)

	clause, args = mailboxMatch("Opfer+x@Example.com")
	assert.Contains(t, clause, "LIKE")
	require.Len(t, args, 2)
	assert.Equal(t, "opfer@example.com", args[0])
	assert.Equal(t, `opfer+%@example.com`, args[1])
}

// A stored confirmation template that is not (or no longer) usable - it
// cannot be parsed or contains no unconditional confirmation link -
// is not used: the default text applies, and the log names neither
// template text nor placeholder values.
func TestRenderSubscriptionConfirmTemplate_UnusableTemplateFallsBackToDefault(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	data := NotificationTemplateData{Title: "geheim-titel", ConfirmURL: "https://status.example.com/confirm/geheim-token", Language: "de"}

	cases := map[string]string{
		"ohne Bestätigungslink":       `<p>geheim-vorlagentext</p>`,
		"Link nur in Bedingung":       `{{if .Level}}{{.ConfirmURL}}{{end}}geheim-vorlagentext`,
		"nicht erlaubtes Konstrukt":   `{{range .Title}}geheim-vorlagentext {{.ConfirmURL}}{{end}}`,
		"Syntaxfehler in der Vorlage": `{{.ConfirmURL geheim-vorlagentext`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			tmpl := &models.NotificationTemplate{Subject: "S", BodyHTML: body}
			subject, html := h.renderSubscriptionConfirmTemplate(tmpl, data, "DEFAULT-S", "DEFAULT-B")

			assert.Equal(t, "DEFAULT-S", subject)
			assert.Equal(t, "DEFAULT-B", html)
			assert.Contains(t, buf.String(), "unusable")
			assert.NotContains(t, buf.String(), "geheim")
		})
	}

	// Control case: with an unconditional link the custom template is used within the frame.
	tmpl := &models.NotificationTemplate{Subject: "Betreff {{.Title}}", BodyHTML: `<a href="{{.ConfirmURL}}">Los</a>`}
	subject, html := h.renderSubscriptionConfirmTemplate(tmpl, data, "DEFAULT-S", "DEFAULT-B")
	assert.NotEqual(t, "DEFAULT-B", html)
	assert.Contains(t, html, "https://status.example.com/confirm/geheim-token")
	assert.Contains(t, subject, "Betreff")
}

// If the template yields an empty text despite a confirmation link (here: the link
// is empty), the default text is sent instead of a mail without content.
func TestRenderSubscriptionConfirmTemplate_EmptyRenderFallsBackToDefault(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	tmpl := &models.NotificationTemplate{Subject: "S", BodyHTML: `{{.ConfirmURL}}`}

	subject, html := h.renderSubscriptionConfirmTemplate(tmpl, NotificationTemplateData{Language: "de"}, "DEFAULT-S", "DEFAULT-B")

	assert.Equal(t, "DEFAULT-S", subject)
	assert.Equal(t, "DEFAULT-B", html)
}
