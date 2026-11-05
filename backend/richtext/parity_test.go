package richtext

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parityFixture struct {
	Limits struct {
		MaxRenderLen int `json:"maxRenderLen"`
		MaxBlockLen  int `json:"maxBlockLen"`
	} `json:"limits"`
	Cases []struct {
		Name      string `json:"name"`
		Prefix    string `json:"prefix"`
		Unit      string `json:"unit"`
		Count     int    `json:"count"`
		Formatted bool   `json:"formatted"`
	} `json:"cases"`
}

func loadParity(t *testing.T) parityFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/parity.json")
	require.NoError(t, err)
	var f parityFixture
	require.NoError(t, json.Unmarshal(raw, &f))
	return f
}

// hasMarkup: das Ergebnis enthält Formatierung (nicht nur Absätze und Umbrüche).
func hasMarkup(html string) bool {
	for _, tag := range []string{"<ul", "<ol", "<li", "<strong", "<em", "<code", "<blockquote", "<a "} {
		if strings.Contains(html, tag) {
			return true
		}
	}
	return false
}

// Fund 9: Seite (Frontend) und Mail (Backend) müssen denselben Text
// gleich formatieren oder gleich unformatiert lassen. Das Frontend prüft dieselbe
// Datei (richText.test.js).
func TestParity_GrenzenUndFaelleWieImFrontend(t *testing.T) {
	f := loadParity(t)
	assert.Equal(t, f.Limits.MaxRenderLen, MaxRenderLen)
	assert.Equal(t, f.Limits.MaxBlockLen, MaxBlockLen)
	assert.Less(t, MaxLen, MaxRenderLen, "erlaubte Texte müssen immer formatiert werden")
	assert.LessOrEqual(t, MaxLen, MaxBlockLen)
	for _, c := range f.Cases {
		in := c.Prefix + strings.Repeat(c.Unit, c.Count)
		assert.Equal(t, c.Formatted, hasMarkup(ToMailHTML(in)), c.Name)
	}
}

func TestToMailHTML_ErlaubteLaengeIstImmerFormatiert(t *testing.T) {
	// 5.000 Zeichen in EINEM Block (Liste ohne Leerzeile) bleiben formatiert.
	in := strings.Repeat("- ab\n", MaxLen/5)
	require.False(t, TooLong(in))
	assert.Contains(t, ToMailHTML(in), "<ul ")
}

// Messung mit großzügiger Zeitgrenze: Der Aufwand am Limit bleibt klein.
func TestToMailHTML_WorstCaseAmLimitIstSchnell(t *testing.T) {
	block := func(unit string, n int) string { return strings.Repeat(unit, n) }
	inputs := []string{
		block("*a ", MaxBlockLen/3),
		block("[a](", MaxBlockLen/4),
		block("_a_", MaxBlockLen/3),
		block("*a _b ", MaxBlockLen/6) + "\n\n" + block("*a ", (MaxRenderLen-MaxBlockLen)/3-1),
	}
	start := time.Now()
	for _, in := range inputs {
		assert.Empty(t, checkSafeMailHTML(ToMailHTML(in)))
		_ = ToPlainText(in)
	}
	assert.Less(t, time.Since(start), 10*time.Second)
}

// Fund 11: eine einzeilige ```...``` ist Inline-Code, kein Zaun;
// nichts darf wegfallen und die folgenden Zeilen bleiben normaler Text.
func TestToMailHTML_EinzeiligeDreierBackticksSindInlineCode(t *testing.T) {
	out := ToMailHTML("```npm install```\nDanach **fett**\n\nNoch ein Absatz")
	assert.Contains(t, out, ">npm install</code>")
	assert.Contains(t, out, "Danach <strong>fett</strong>")
	assert.Contains(t, out, "Noch ein Absatz")
	plain := ToPlainText("```npm install```\nDanach\n\nEnde")
	assert.Equal(t, "npm install\nDanach\n\nEnde", plain)
	// Mitten im Absatz unterbricht so eine Zeile den Absatz nicht.
	out = ToMailHTML("Vorher\n```a``` b\nNachher")
	assert.Equal(t, 1, strings.Count(out, "<p "))
	assert.Contains(t, out, "Vorher<br><code")
	// Echter Zaun mit Infotext bleibt ein Zaun, auch in einer Liste endet er.
	assert.Contains(t, ToMailHTML("```js\nlet a\n```\nEnde"), ">let a</code>")
	assert.Contains(t, ToMailHTML("~~~ a`b\nx\n~~~"), ">x</code>")
	// Nicht geschlossener Zaun: der Rest bleibt sichtbar (kein Textverlust).
	assert.Contains(t, ToPlainText("```\nA\nB"), "A\nB")
}

// Fund 12: '2024. Das Jahr' / '3. Schritt' behalten ihre Nummer.
func TestToMailHTML_ListeBeginntMitIhrerNummer(t *testing.T) {
	out := ToMailHTML("3. Schritt\n4. Weiter")
	assert.Contains(t, out, `<ol start="3" `)
	assert.Equal(t, "3. Schritt\n4. Weiter", ToPlainText("3. Schritt\n4. Weiter"))
	assert.Equal(t, "2024. Das Jahr", ToPlainText("2024. Das Jahr"))
	assert.Contains(t, ToMailHTML("2024. Das Jahr"), `<ol start="2024" `)
	assert.NotContains(t, ToMailHTML("1. a\n2. b"), "start=")
	assert.Contains(t, ToMailHTML("0. a"), `<ol start="0" `)
	// Ein laufender Absatz wird nur von "1." unterbrochen.
	assert.Equal(t, 1, strings.Count(ToMailHTML("Text\n2024. Das Jahr"), "<p "))
	assert.Empty(t, checkSafeMailHTML(ToMailHTML("999999999. x")))
}
