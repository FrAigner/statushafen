package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Rest: Das Projekt duzt. Nutzer sehen Fehlermeldungen,
// Mails und Vorlagentexte aus dem Backend; dieser Test durchsucht alle
// String-Literale (keine Kommentare, keine Testdateien) nach förmlicher Anrede.
//
// Konservative Regel: großes "Sie" mitten im Satz (nach einem kleingeschriebenen
// Wort), Imperativ am Satzanfang ("Stellen Sie", "Bitte lesen Sie") und das
// Possessiv "Ihr/Ihre/Ihren/..." bzw. "Ihnen". Pronomen "Sie" am Satzanfang für
// Dinge bleibt erlaubt. Berechtigte Treffer stehen in anredeErlaubt.
var anredeErlaubt = []string{
	"Ihr zuletzt bekannter Status", // Status der externen Quelle
	"Ihr aktueller Status",         // dito
}

var anredeRegexe = []*regexp.Regexp{
	regexp.MustCompile(`[\p{Ll}\d,;)] Sie\b`),
	regexp.MustCompile(`\p{Lu}\p{Ll}+(?:en|et) Sie\b`),
	regexp.MustCompile(`\b(?:Ihr(?:e|en|em|er|es)?|Ihnen)\b`),
}

func anredeTreffer(text string) []string {
	for _, ok := range anredeErlaubt {
		text = strings.ReplaceAll(text, ok, "")
	}
	var hits []string
	for _, re := range anredeRegexe {
		if m := re.FindString(text); m != "" {
			hits = append(hits, m)
		}
	}
	return hits
}

func TestAnredeTreffer_Gegenprobe(t *testing.T) {
	for _, s := range []string{
		"Stellen Sie sicher, dass es klappt.",
		"Bitte lesen Sie die neue Version durch.",
		"Wenn Sie fertig sind.",
		"Betrifft Ihre Komponenten.",
	} {
		if len(anredeTreffer(s)) == 0 {
			t.Errorf("Anrede nicht erkannt: %q", s)
		}
	}
	for _, s := range []string{
		"Sie gelten sofort.",
		"Fertig. Sie wird gefüllt.",
		"Ihr zuletzt bekannter Status zählt nicht.",
		"Bitte lies die neue Version durch.",
	} {
		if h := anredeTreffer(s); len(h) != 0 {
			t.Errorf("zu Unrecht erkannt: %q -> %v", s, h)
		}
	}
}

func TestBackendTexteDuzen(t *testing.T) {
	fset := token.NewFileSet()
	var treffer []string
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if h := anredeTreffer(s); len(h) > 0 {
				treffer = append(treffer, fset.Position(lit.Pos()).String()+": "+strings.Join(h, ", "))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range treffer {
		t.Errorf("förmliche Anrede im Backend-Text: %s", h)
	}
}
