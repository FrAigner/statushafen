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
	// Der Platzhalter darf die Ausgabegrenze nicht durch Länge sprengen.
	data := NotificationTemplateData{Message: strings.Repeat("x", maxTemplateSubjectOutput+1)}
	_, err := executeStringTemplate("subject", "{{.Message}}", data)
	assert.Equal(t, "template_output_too_large", TemplateErrorCode(err))

	// Gegenprobe: unter der Grenze wird gerendert.
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

// Bereits gespeicherte, nach den neuen Regeln unzulässige Vorlagen dürfen den
// Versand nicht unterbinden: es greift die Standardvorlage, und das Log nennt
// weder den Vorlagentext noch Platzhalterwerte.
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

// Eine leere Vorlage hat keinen Baum und ist gültig; es werden keine
// Pflichtplatzhalter gemeldet.
func TestParseAllowedTemplateFields_EmptyTemplateIsValid(t *testing.T) {
	tmpl, used, err := parseAllowedTemplateFields("body", "", MaxTemplateBodyLen)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Empty(t, used)
}

// checkTemplateNode wird vom Parser nie mit einem nil-Interface gerufen; die
// Zusicherung hält fest, dass ein solcher Knoten als leer gilt, statt zu
// paniken.
func TestCheckTemplateNode_NilNodeIsAccepted(t *testing.T) {
	require.NoError(t, checkTemplateNode(nil, 0, map[string]bool{}))
}

// Fehler innerhalb einer Bedingung oder ihres Rumpfes werden gemeldet; Gegenprobe
// ist jeweils die gleiche Form mit erlaubtem Inhalt.
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

// Eine Bedingung ohne Befehl kann der Parser nicht erzeugen; checkTemplateCondition
// lehnt sie trotzdem ab, falls ein Baum von Hand gebaut wird.
func TestCheckTemplateCondition_RejectsMalformedPipes(t *testing.T) {
	assert.Equal(t, "template_construct_not_allowed", TemplateErrorCode(checkTemplateCondition(nil)))
	emptyCmd := &parse.PipeNode{Cmds: []*parse.CommandNode{{}}}
	assert.Equal(t, "template_construct_not_allowed", TemplateErrorCode(checkTemplateCondition(emptyCmd)))
}

// Ein Fehler beim Ausführen (hier: Vergleich unterschiedlicher Typen) kommt als
// gewöhnlicher Fehler zurück, nicht als Ausgabegrenze.
func TestExecuteStringTemplate_ExecutionErrorIsReturned(t *testing.T) {
	_, err := executeStringTemplate("subject", `{{if eq .Title 1}}x{{end}}`, NotificationTemplateData{Title: "t"})
	require.Error(t, err)
	assert.Equal(t, "", TemplateErrorCode(err))
}
