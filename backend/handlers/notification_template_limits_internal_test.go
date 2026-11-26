package handlers

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"text/template/parse"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitedWriter_AbortsAboveLimit(t *testing.T) {
	w := &limitedWriter{max: 5}
	_, err := w.Write([]byte("abc"))
	require.NoError(t, err)
	_, err = w.Write([]byte("def"))
	require.Error(t, err)
	assert.True(t, w.exceeded)
	assert.Equal(t, "abc", w.buf.String())
}

func TestExecuteStringTemplate_OutputLimit(t *testing.T) {
	// The placeholder must not blow the output limit through its length.
	data := NotificationTemplateData{Message: strings.Repeat("x", maxTemplateSubjectOutput+1)}
	_, err := executeStringTemplate("subject", "{{.Message}}", data)
	assert.Equal(t, "template_output_too_large", TemplateErrorCode(err))

	// Control check: below the limit it is rendered.
	out, err := executeStringTemplate("subject", "{{.Title}}", NotificationTemplateData{Title: "ok"})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
}

func TestRunWithTimeout_ReturnsCodedErrorWhenTooSlow(t *testing.T) {
	_, err := runWithTimeout(20*time.Millisecond, func() (string, error) {
		time.Sleep(300 * time.Millisecond)
		return "spät", nil
	})
	assert.Equal(t, "template_timeout", TemplateErrorCode(err))

	s, err := runWithTimeout(time.Second, func() (string, error) { return "schnell", nil })
	require.NoError(t, err)
	assert.Equal(t, "schnell", s)
}

// Templates that were already saved and are invalid under the new rules must not
// block sending: the default template applies, and the log mentions
// neither the template text nor placeholder values.
func TestApplyNotificationTemplate_StoredForbiddenTemplateFallsBackWithoutLeak(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	tmpl := &models.NotificationTemplate{
		Subject:  "S",
		BodyHTML: `{{range 100000}}{{printf "%1000000s" "geheim-vorlagentext"}}{{end}}`,
	}
	subject, body := applyNotificationTemplate(tmpl, NotificationTemplateData{Title: "geheim-titel"}, "DEFAULT-S", "DEFAULT-B")
	assert.Equal(t, "DEFAULT-S", subject)
	assert.Equal(t, "DEFAULT-B", body)
	assert.Contains(t, buf.String(), "template_construct_not_allowed")
	assert.NotContains(t, buf.String(), "geheim")
}

func TestTemplateErrorCode_NonTemplateErrorHasNoCode(t *testing.T) {
	assert.Equal(t, "", TemplateErrorCode(errors.New("irgendein Fehler")))
	assert.Equal(t, "template_too_long", TemplateErrorCode(newCodedError("template_too_long", "x")))
}

func TestOutputLimitError_Message(t *testing.T) {
	assert.Contains(t, outputLimitError{}.Error(), "limit")
}

// An empty template has no tree and is valid; no
// required placeholder is reported.
func TestParseAllowedTemplateFields_EmptyTemplateIsValid(t *testing.T) {
	tmpl, used, err := parseAllowedTemplateFields("body", "", MaxTemplateBodyLen)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Empty(t, used)
}

// checkTemplateNode is never called by the parser with a nil interface; the
// assertion records that such a node counts as empty instead of
// paniken.
func TestCheckTemplateNode_NilNodeIsAccepted(t *testing.T) {
	require.NoError(t, checkTemplateNode(nil, 0, map[string]bool{}))
}

// Errors inside a condition or its body are reported; the control check
// is in each case the same form with allowed content.
func TestParseAllowedTemplate_RejectsForbiddenConstructsInsideConditions(t *testing.T) {
	cases := []struct {
		name, raw, wantCode string
	}{
		{"Variablendeklaration in der Bedingung", `{{if $x := .Title}}a{{end}}`, "template_construct_not_allowed"},
		{"Pipeline in der Bedingung", `{{if .Title | not}}a{{end}}`, "template_construct_not_allowed"},
		{"nicht erlaubte Funktion", `{{if len .Title}}a{{end}}`, "template_construct_not_allowed"},
		{"unbekannter Platzhalter als Funktionsargument", `{{if eq .Title .GibtEsNicht}}a{{end}}`, "template_invalid"},
		{"Methodenaufruf mit Argument", `{{if .Title .Message}}a{{end}}`, "template_construct_not_allowed"},
		{"Fehler im Rumpf der Bedingung", `{{if .Title}}{{len .Title}}{{end}}`, "template_construct_not_allowed"},
		{"Fehler im Sonst-Zweig", `{{if .Title}}a{{else}}{{.Title.Foo}}{{end}}`, "template_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseAllowedTemplateFields("body", tc.raw, MaxTemplateBodyLen)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, TemplateErrorCode(err))
		})
	}

	_, _, err := parseAllowedTemplateFields("body", `{{if eq .Title "x"}}{{.Message}}{{else}}{{.Title}}{{end}}`, MaxTemplateBodyLen)
	require.NoError(t, err)
}

// A condition without a command cannot be produced by the parser; checkTemplateCondition
// rejects it anyway in case a tree is built by hand.
func TestCheckTemplateCondition_RejectsMalformedPipes(t *testing.T) {
	assert.Equal(t, "template_construct_not_allowed", TemplateErrorCode(checkTemplateCondition(nil)))
	emptyCmd := &parse.PipeNode{Cmds: []*parse.CommandNode{{}}}
	assert.Equal(t, "template_construct_not_allowed", TemplateErrorCode(checkTemplateCondition(emptyCmd)))
}

// An error during execution (here: comparing different types) comes back as an
// ordinary error, not as an output limit.
func TestExecuteStringTemplate_ExecutionErrorIsReturned(t *testing.T) {
	_, err := executeStringTemplate("subject", `{{if eq .Title 1}}x{{end}}`, NotificationTemplateData{Title: "t"})
	require.Error(t, err)
	assert.Equal(t, "", TemplateErrorCode(err))
}
