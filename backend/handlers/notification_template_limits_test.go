package handlers_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Vorlagen-Engine darf keine Konstrukte ausführen, mit denen sich aus
// einer kurzen Vorlage riesige Ausgaben oder lange Laufzeiten erzeugen lassen
// (Schleifen, Unterschablonen, printf mit Breite). Erlaubt sind nur Feldzugriffe
// auf die dokumentierten Platzhalter und einfache Bedingungen.

func TestValidateNotificationTemplate_RejectsAmplifyingConstructs(t *testing.T) {
	cases := map[string]string{
		"range über Zahl":         `{{range 2000}}{{printf "%1000000s" "x"}}{{end}}`,
		"verschachteltes range":   `{{range 3000}}{{range 3000}}{{end}}{{end}}`,
		"printf mit Breite":       `{{printf "%1000000s" "x"}}`,
		"template-Aufruf":         `{{define "a"}}x{{end}}{{template "a"}}`,
		"block":                   `{{block "a" .}}x{{end}}`,
		"define ohne Aufruf":      `{{define "a"}}x{{end}}`,
		"Variablenzuweisung":      `{{$x := .Title}}{{$x}}`,
		"Punkt allein":            `{{.}}`,
		"Funktion len":            `{{len .Message}}`,
		"Funktion html":           `{{html .Message}}`,
		"range über Feld":         `{{range .Title}}x{{end}}`,
		"with":                    `{{with .Title}}x{{end}}`,
		"Methodenkette am Feld":   `{{.Title.Foo}}`,
		"unbekannter Platzhalter": `{{.GibtEsNicht}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := handlers.ValidateNotificationTemplate("S", body)
			require.Error(t, err, "Body muss abgelehnt werden")
			err = handlers.ValidateNotificationTemplate(body, "<p>x</p>")
			require.Error(t, err, "Betreff muss abgelehnt werden")
		})
	}
}

func TestValidateNotificationTemplate_AcceptsDocumentedForms(t *testing.T) {
	ok := []string{
		`[{{.StatusLabel}}] {{.Title}}`,
		`<p>{{.Message}}</p><p>Affected: {{.ComponentNames}}</p><a href="{{.FrontendURL}}">Open</a>{{.MessageHTML}}`,
		`{{if .Level}}Level: {{.Level}}{{else}}kein Level{{end}}`,
		`{{if eq .Status "resolved"}}fertig{{end}}`,
		`{{if and .SourceName (not .SourceURL)}}x{{end}}`,
		`{{ .IncidentTitle }}`,
	}
	for _, body := range ok {
		assert.NoError(t, handlers.ValidateNotificationTemplate("S {{.Title}}", body), body)
	}
}

func TestValidateNotificationTemplate_LengthLimits(t *testing.T) {
	tooLongBody := strings.Repeat("a", handlers.MaxTemplateBodyLen+1)
	err := handlers.ValidateNotificationTemplate("S", tooLongBody)
	assert.Equal(t, "template_too_long", handlers.TemplateErrorCode(err))

	tooLongSubject := strings.Repeat("a", handlers.MaxTemplateSubjectLen+1)
	err = handlers.ValidateNotificationTemplate(tooLongSubject, "<p>x</p>")
	assert.Equal(t, "template_too_long", handlers.TemplateErrorCode(err))

	// Gegenprobe: genau am Limit ist erlaubt.
	assert.NoError(t, handlers.ValidateNotificationTemplate(strings.Repeat("a", handlers.MaxTemplateSubjectLen), strings.Repeat("a", handlers.MaxTemplateBodyLen)))
}

func TestFehlercodes_VorlagenLimits(t *testing.T) {
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	pid := strconv.Itoa(int(page.ID))
	url := "/api/pages/" + pid + "/notification-templates/incident_created/subscriber/de"
	bomb := `{{range 2000}}{{printf "%1000000s" "x"}}{{end}}`

	runCodeCases(t, r, []codeCase{
		{"Vorschau: Verstärkungs-Vorlage", "POST", url + "/preview", adminTok, map[string]any{"Subject": "S", "BodyHTML": bomb}, 400, "template_construct_not_allowed"},
		{"Speichern: Verstärkungs-Vorlage", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": bomb}, 400, "template_construct_not_allowed"},
		{"Speichern: Betreff mit range", "PUT", url, adminTok, map[string]any{"Subject": "{{range 5}}x{{end}}", "BodyHTML": "<p>x</p>"}, 400, "template_construct_not_allowed"},
		{"Vorschau: zu lang", "POST", url + "/preview", adminTok, map[string]any{"Subject": "S", "BodyHTML": strings.Repeat("a", handlers.MaxTemplateBodyLen+1)}, 400, "template_too_long"},
		{"Speichern: zu lang", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": strings.Repeat("a", handlers.MaxTemplateBodyLen+1)}, 400, "template_too_long"},
		// Gegenprobe: normale Vorlage funktioniert weiter.
		{"Vorschau: normale Vorlage", "POST", url + "/preview", adminTok, map[string]any{"Subject": "[{{.StatusLabel}}] {{.Title}}", "BodyHTML": "<p>{{.Message}}</p>"}, 200, ""},
	})
}
