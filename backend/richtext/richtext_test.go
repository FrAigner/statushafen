package richtext

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// XSS-Matrix und Formatierung des Mail-Renderers.

var allowedMailTags = map[string]bool{"p": true, "br": true, "strong": true, "em": true, "ul": true, "ol": true, "li": true, "a": true, "code": true, "blockquote": true}

// checkSafeMailHTML prüft ein Renderergebnis mit dem HTML-Tokenizer (nicht mit
// dem Renderer selbst): einzeilig, nur erlaubte Tags, nur style (und beim Link
// href/target/rel), href nur https, rel vollständig, style ohne url()/expression.
func checkSafeMailHTML(out string) []string {
	var problems []string
	if strings.ContainsAny(out, "\r\n") {
		problems = append(problems, "CR/LF im Ergebnis")
	}
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken && tt != html.EndTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		tag := string(name)
		if !allowedMailTags[tag] {
			problems = append(problems, "Tag nicht erlaubt: "+tag)
		}
		for hasAttr {
			var k, v []byte
			k, v, hasAttr = z.TagAttr()
			problems = append(problems, checkAttr(tag, string(k), string(v))...)
		}
	}
	return problems
}

// <ol start="N"> nur mit reiner Zahl (höchstens 9 Ziffern).
var startAttrPattern = regexp.MustCompile(`^[0-9]{1,9}$`)

func checkAttr(tag, key, val string) []string {
	switch {
	case key == "style":
		if strings.Contains(strings.ToLower(val), "url(") || strings.Contains(strings.ToLower(val), "expression") {
			return []string{"gefährlicher style: " + val}
		}
	case tag == "a" && key == "href":
		if !strings.HasPrefix(val, "https://") {
			return []string{"href nicht https: " + val}
		}
	case tag == "ol" && key == "start" && startAttrPattern.MatchString(val):
	case tag == "a" && key == "target" && val == "_blank":
	case tag == "a" && key == "rel" && val == "noopener noreferrer nofollow":
	default:
		return []string{"Attribut nicht erlaubt: " + tag + "@" + key}
	}
	return nil
}

var xssMatrix = map[string]string{
	"script":                 `<script>alert(1)</script>`,
	"script in Text":         "Hallo <script>alert(1)</script> Welt",
	"javascript-Link":        `[klick](javascript:alert(1))`,
	"javascript Grossschr.":  `[klick](JaVaScRiPt:alert(1))`,
	"javascript Whitespace":  "[klick]( \tjavascript:alert(1) )",
	"javascript Zeilenumbr.": "[klick](java\nscript:alert(1))",
	"javascript Tab":         "[klick](java\tscript:alert(1))",
	"javascript Entität":     `[klick](&#106;avascript:alert(1))`,
	"javascript Autolink":    `<javascript:alert(1)>`,
	"data-URI":               `[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
	"vbscript":               `[x](vbscript:msgbox(1))`,
	"http-Link":              `[x](http://example.com/a)`,
	"http bare":              `http://example.com/a`,
	"ftp":                    `[x](ftp://example.com)`,
	"protokollrelativ":       `[x](//evil.example/a)`,
	"Zugangsdaten":           `[x](https://trusted.example@evil.example/)`,
	"onerror img":            `<img src=x onerror=alert(1)>`,
	"img src":                `<img src="https://evil.example/pixel.png">`,
	"Markdown-Bild":          `![bild](https://evil.example/pixel.png)`,
	"Bild javascript":        `![x](javascript:alert(1))`,
	"onclick":                `<a href="https://x.de" onclick="alert(1)">x</a>`,
	"onmouseover":            `<div onmouseover="alert(1)">hover</div>`,
	"Roh-HTML iframe":        `<iframe src="https://evil.example"></iframe>`,
	"svg onload":             `<svg onload=alert(1)>`,
	"style-Block":            `<style>body{background:url(https://evil.example)}</style>`,
	"Entitäten":              `&lt;script&gt;alert(1)&lt;/script&gt; &#60;script&#62;`,
	"verschachtelt":          `<<script>script>alert(1)<</script>/script>`,
	"verschachtelt a":        `<a href="x"><a href="javascript:alert(1)">x</a></a>`,
	"Titel-Attribut":         `[x](https://example.com "t\" onmouseover=\"alert(1)")`,
	"Attribut-Ausbruch":      `[x](https://example.com/"onmouseover="alert(1))`,
	"Link-Text HTML":         `[<script>alert(1)</script>](https://example.com)`,
	"Fett mit HTML":          `**<img src=x onerror=alert(1)>**`,
	"Code mit HTML":          "`<script>alert(1)</script>`",
	"Codeblock":              "```\n<script>alert(1)</script>\n```",
	"Zitat":                  `> <script>alert(1)</script>`,
	"Liste":                  "- <script>alert(1)</script>\n- [x](javascript:alert(1))",
	"CR LF Injektion":        "Zeile\r\nBcc: evil@example.com\r\n\r\n<b>x</b>",
	"NUL und Steuerzeichen":  "a\x00b\x1b[31m<script>",
	"Überschrift":            `# <script>alert(1)</script>`,
	"Tabelle":                "| a | b |\n|---|---|\n| <script> | x |",
	"Autolink mit Quote":     `https://example.com/"><script>alert(1)</script>`,
}

func TestToMailHTML_XSSMatrix(t *testing.T) {
	for name, in := range xssMatrix {
		t.Run(name, func(t *testing.T) {
			out := ToMailHTML(in)
			assert.Empty(t, checkSafeMailHTML(out), "Eingabe %q ergab %q", in, out)
			assert.NotContains(t, strings.ToLower(out), "<script")
			assert.NotContains(t, strings.ToLower(out), "<img")
			assert.NotContains(t, strings.ToLower(out), "href=\"javascript")
			assert.NotContains(t, strings.ToLower(out), "href=\"data:")
			assert.NotContains(t, strings.ToLower(out), "href=\"http:")
		})
	}
}

// Gegenprobe: Der Prüfer schlägt bei ungefiltertem Durchreichen (alter Stand
// bzw. Renderer ohne Whitelist) an. Ohne diesen Test könnte die Matrix leer
// laufen.
func TestCheckSafeMailHTML_ErkenntUngefiltertesHTML(t *testing.T) {
	unsafe := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<a href="http://example.com">x</a>`,
		`<a href="https://x.de" onclick="alert(1)">x</a>`,
		`<p style="background:url(https://evil.example)">x</p>`,
		"<p>zwei\nZeilen</p>",
	}
	for _, in := range unsafe {
		assert.NotEmpty(t, checkSafeMailHTML(in), in)
	}
}

func TestToMailHTML_Links(t *testing.T) {
	out := ToMailHTML(`[Status](https://status.example.com/a?x=1&y=2) und https://example.org/pfad.`)
	assert.Contains(t, out, `<a href="https://status.example.com/a?x=1&amp;y=2" target="_blank" rel="noopener noreferrer nofollow"`)
	assert.Contains(t, out, `>Status</a>`)
	assert.Contains(t, out, `<a href="https://example.org/pfad" `)
	assert.Contains(t, out, `>https://example.org/pfad</a>.`)
}

func TestToMailHTML_NichtHttpsLinkWirdText(t *testing.T) {
	out := ToMailHTML(`[x](http://example.com) [y](javascript:alert(1))`)
	assert.NotContains(t, out, "<a ")
	assert.Contains(t, out, "x (http://example.com)")
	assert.Contains(t, out, "y (javascript:alert(1))")
}

func TestToMailHTML_Formatierung(t *testing.T) {
	out := ToMailHTML("Das ist **fett**, *kursiv*, __fett2__, _kursiv2_ und ***beides***.\n\nZweiter `Code` Absatz")
	assert.Contains(t, out, "<strong>fett</strong>")
	assert.Contains(t, out, "<em>kursiv</em>")
	assert.Contains(t, out, "<strong>fett2</strong>")
	assert.Contains(t, out, "<em>kursiv2</em>")
	assert.Contains(t, out, "<em><strong>beides</strong></em>")
	assert.Contains(t, out, ">Code</code>")
	assert.Equal(t, 2, strings.Count(out, "<p "))
}

func TestToMailHTML_ListenUndZitat(t *testing.T) {
	out := ToMailHTML("Einleitung\n- eins\n- **zwei**\n\n1. a\n2. b\n\n> zitiert\n> zweite Zeile")
	assert.Contains(t, out, "<ul ")
	assert.Equal(t, 2, strings.Count(out[strings.Index(out, "<ul"):strings.Index(out, "</ul>")], "<li "))
	assert.Contains(t, out, "<ol ")
	assert.Contains(t, out, "<blockquote ")
	assert.Contains(t, out, "zitiert<br>zweite Zeile")
	assert.Empty(t, checkSafeMailHTML(out))
}

func TestToMailHTML_LeerzeilenZwischenListeneintraegen(t *testing.T) {
	out := ToMailHTML("- a\n\n- b\n\nDanach")
	assert.Equal(t, 1, strings.Count(out, "<ul "))
	assert.Equal(t, 2, strings.Count(out, "<li "))
	assert.Contains(t, out, "Danach")
}

func TestToMailHTML_Zaunblock(t *testing.T) {
	out := ToMailHTML("```\nzeile1\nzeile2\n```\nDanach")
	assert.Contains(t, out, ">zeile1<br>zeile2</code>")
	assert.Contains(t, out, "Danach")
	// Nicht geschlossener Zaun läuft bis zum Textende.
	assert.Contains(t, ToMailHTML("```\nrest"), ">rest</code>")
}

// Altdaten: Klartext bleibt lesbar, einfache Zeilenumbrüche bleiben erhalten.
func TestToMailHTML_Altdaten(t *testing.T) {
	cases := map[string][]string{
		"Wir untersuchen das Problem.\nBitte um Geduld.": {"Wir untersuchen das Problem.<br>Bitte um Geduld."},
		"snake_case_name und a*b*c":                      {"snake_case_name", "a<em>b</em>c"},
		"2 * 3 = 6 und 5 * 4":                            {"2 * 3 = 6 und 5 * 4"},
		"# Kein Titel":                                   {"# Kein Titel"},
		"## Zwei\nZeile":                                 {"## Zwei<br>Zeile"},
		"Text\n---\nMehr":                                {">Text</p>", ">---</p>", ">Mehr</p>"},
		"* * *":                                          {"* * *"},
		"Preis: 5 € - 10 €":                              {"Preis: 5 € - 10 €"},
		"Stand 2024. Das Jahr war gut.":                  {"Stand 2024. Das Jahr war gut."},
		"Zeile\n2024. Jahr":                              {"Zeile<br>2024. Jahr"},
		"Unabgeschlossen **fett":                         {"Unabgeschlossen **fett"},
		"a & b < c > d \"q\" 'r'":                        {"a &amp; b &lt; c &gt; d &#34;q&#34; &#39;r&#39;"},
		"Ein Stern * allein _ und ` Backtick":            {"Ein Stern * allein _ und ` Backtick"},
		"    eingerückter Text":                          {"eingerückter Text"},
		"| a | b |\n|---|---|":                           {"| a | b |<br>|---|---|"},
		"C:\\pfad\\datei und \\*kein kursiv\\*":          {"C:\\pfad\\datei und *kein kursiv*"},
	}
	for in, wants := range cases {
		out := ToMailHTML(in)
		for _, want := range wants {
			assert.Contains(t, out, want, "Eingabe %q -> %q", in, out)
		}
		assert.Empty(t, checkSafeMailHTML(out))
	}
}

func TestToMailHTML_ImmerEinzeilig(t *testing.T) {
	in := "a\r\nb\rc\n\n\nd\n- x\n  y\n> q\n```\nc\n```"
	out := ToMailHTML(in)
	assert.NotContains(t, out, "\n")
	assert.NotContains(t, out, "\r")
}

func TestToMailHTML_LeerUndWhitespace(t *testing.T) {
	assert.Equal(t, "", ToMailHTML(""))
	assert.Equal(t, "", ToMailHTML("  \n\t\n"))
}

func TestToMailHTML_UeberlangeAltdatenOhneFormatierung(t *testing.T) {
	in := strings.Repeat("**x** ", MaxRenderLen) // weit über dem Renderlimit
	out := ToMailHTML(in)
	assert.NotContains(t, out, "<strong>")
	assert.Contains(t, out, "**x**")
	assert.Empty(t, checkSafeMailHTML(out))
	// Absätze bleiben auch dann getrennt.
	assert.Equal(t, 2, strings.Count(ToMailHTML(strings.Repeat("a", MaxRenderLen)+"\n\nb"), "<p "))
}

func TestToMailHTML_PathologischeEingabenSindSchnell(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("*a ", 4000),
		strings.Repeat("[", 4000),
		strings.Repeat("[a](", 2000),
		strings.Repeat("`", 4000),
		strings.Repeat("_a_", 3000),
	} {
		out := ToMailHTML(in)
		assert.Empty(t, checkSafeMailHTML(out))
	}
}

func TestToMailHTML_VerschachtelungBegrenzt(t *testing.T) {
	in := strings.Repeat("*a ", 10) + "x" + strings.Repeat(" a*", 10)
	assert.Empty(t, checkSafeMailHTML(ToMailHTML(in)))
	deep := strings.Repeat("**a *b ", 20) + "x" + strings.Repeat(" b* a**", 20)
	assert.Empty(t, checkSafeMailHTML(ToMailHTML(deep)))
}

func TestTooLong(t *testing.T) {
	assert.False(t, TooLong(strings.Repeat("ä", MaxLen)))
	assert.True(t, TooLong(strings.Repeat("ä", MaxLen+1)))
	assert.False(t, TooLong(""))
}

func TestToPlainText(t *testing.T) {
	cases := map[string]string{
		"**fett** und *kursiv*":                          "fett und kursiv",
		"[Status](https://example.com/s) ansehen":        "Status (https://example.com/s) ansehen",
		"https://example.com/s":                          "https://example.com/s",
		"[https://example.com/s](https://example.com/s)": "https://example.com/s",
		"- a\n- b":                 "- a\n- b",
		"1. eins\n2. zwei":         "1. eins\n2. zwei",
		"A\n\nB":                   "A\n\nB",
		"Zeile 1\nZeile 2":         "Zeile 1\nZeile 2",
		"> zitat":                  "> zitat",
		"`code` hier":              "code hier",
		"[x](javascript:alert(1))": "x (javascript:alert(1))",
		"<b>roh</b>":               "<b>roh</b>",
		"- erster\n  weiter":       "- erster\n  weiter",
		"# Titel":                  "# Titel",
		"":                         "",
	}
	for in, want := range cases {
		assert.Equal(t, want, ToPlainText(in), "Eingabe %q", in)
	}
}

func TestToPlainText_KeinHTMLUndGeordneteListeZaehlt(t *testing.T) {
	out := ToPlainText("1. a\n2. b\n3. c\n4. d\n5. e\n6. f\n7. g\n8. h\n9. i\n10. j")
	assert.Contains(t, out, "10. j")
	require.NotContains(t, out, "<")
}

func TestHelfer(t *testing.T) {
	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "120", itoa(120))
	_, _, ok := listMarker("-", false)
	assert.False(t, ok)
	_, _, ok = listMarker("1.", false)
	assert.False(t, ok)
	_, _, ok = listMarker("- ", false)
	assert.False(t, ok)
	_, _, ok = listMarker("12345678901. x", false)
	assert.False(t, ok)
	_, _, ok = listMarker("2. x", true)
	assert.False(t, ok)
	_, _, ok = listMarker("1. x", true)
	assert.True(t, ok)
	assert.True(t, isRule("- - -"))
	assert.True(t, isRule("___"))
	assert.False(t, isRule("--"))
	assert.False(t, isRule("-a-"))
	assert.True(t, validLinkTitle(`"t"`))
	assert.True(t, validLinkTitle("(t)"))
	assert.False(t, validLinkTitle(`x`))
	assert.False(t, validLinkTitle(`"`))
	_, ok = httpsURL("https:///nur-pfad")
	assert.False(t, ok)
	_, ok = httpsURL("https://%zz")
	assert.False(t, ok)
	_, ok = linkDestination("   ")
	assert.False(t, ok)
	_, ok = linkDestination(`https://a.de "titel`)
	assert.False(t, ok)
	d, ok := linkDestination(`https://a.de 'titel'`)
	assert.True(t, ok)
	assert.Equal(t, "https://a.de", d)
}

func TestToMailHTML_Randfaelle(t *testing.T) {
	cases := map[string]string{
		"> a  \n> b":              "a<br>b",
		"`  `":                    "`  `",
		"`a``b`":                  ">a``b</code>",
		"***a**":                  "*<strong>a</strong>",
		"[](https://a.de)":        ">https://a.de</a>",
		"- a\n1. b":               "<ol ",
		"1. ":                     ">1.</p>",
		"- a\n---":                ">---</p>",
		"![alt](https://a.de)":    "![alt](https://a.de)",
		"[a [b] c](https://a.de)": ">a [b] c</a>",
		"[x]()":                   "[x]()",
		"[x](https://a.de":        "[x](",
		"[x](https://a.de \"t\")": ">x</a>",
		"(https://a.de/x_(y)) ok": ">https://a.de/x_(y)</a>",
	}
	for in, want := range cases {
		out := ToMailHTML(in)
		assert.Contains(t, out, want, "Eingabe %q -> %q", in, out)
		assert.Empty(t, checkSafeMailHTML(out))
	}
}

func TestToMailHTML_UeberlangMitMehrzeiligenAbsaetzen(t *testing.T) {
	in := strings.Repeat("x", MaxRenderLen) + "\n\n\n\nzeile1\nzeile2"
	out := ToMailHTML(in)
	assert.Equal(t, 2, strings.Count(out, "<p "))
	assert.Contains(t, out, "zeile1<br>zeile2")
}
