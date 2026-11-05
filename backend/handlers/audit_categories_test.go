package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
)

// Das Frontend zeigt je Audit-Kategorie einen Filter-Chip. Wer im
// Backend eine neue Kategorie schreibt, ohne sie in models.AuditCategories
// und im Frontend einzutragen, würde sie nie filtern können (so fehlten
// Abonnent, Ankündigung und Wartung). Der Test liest das GANZE backend/ per AST
// (ohne Testdateien) und sammelt alle Kategorien, die geschrieben werden:
//   - RecordAudit(c, action, CATEGORY, ...) / RecordSystemAudit(action, CATEGORY, ...)
//   - Category: ... in models.AuditLog{} und mailAuditTarget{}
//
// Literale werden gesammelt. Alles andere (Konstante, Variable) wäre eine
// Kategorie, die der Test nicht sieht, und wird als Fehler gemeldet; erlaubt
// sind nur die Durchreichungen x.Category (stammen aus einem gescannten
// Literal) und audit.go selbst (die Funktionen, die den Parameter weitergeben).
func scanAuditCategories(t *testing.T, root string) (found map[string]string, nonLiteral []string) {
	t.Helper()
	found = map[string]string{}
	fset := token.NewFileSet()
	check := func(e ast.Expr, f string) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if u, err := strconv.Unquote(v.Value); err == nil {
					found[u] = f
					return
				}
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "Category" {
				return
			}
		}
		nonLiteral = append(nonLiteral, fset.Position(e.Pos()).String())
	}
	err := filepath.WalkDir(root, func(f string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == ".git" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") || filepath.Base(f) == "audit.go" {
			return nil
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				idx := -1
				switch sel.Sel.Name {
				case "RecordAudit":
					idx = 2
				case "RecordSystemAudit":
					idx = 1
				}
				if idx >= 0 && len(x.Args) > idx {
					check(x.Args[idx], f)
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && id.Name == "Category" {
					check(x.Value, f)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found, nonLiteral
}

func TestAuditCategories_CoverEveryWrittenCategory(t *testing.T) {
	found, nonLiteral := scanAuditCategories(t, "..")
	if len(found) < 6 {
		t.Fatalf("Scanner findet zu wenige Kategorien (%v) - Muster kaputt?", found)
	}
	for _, pos := range nonLiteral {
		t.Errorf("Audit-Kategorie ist kein String-Literal (%s) - der Test sieht sie nicht", pos)
	}
	for cat, file := range found {
		if !models.IsAuditCategory(cat) {
			t.Errorf("Audit-Kategorie %q (%s) fehlt in models.AuditCategories", cat, file)
		}
	}
	// Gegenrichtung: kein toter Eintrag in der Liste.
	for _, cat := range models.AuditCategories {
		if _, ok := found[cat]; !ok {
			t.Errorf("models.AuditCategories enthält %q, das nirgends geschrieben wird", cat)
		}
	}
}

// Gegenprobe: ein Fixture mit Konstante als Kategorie und einem Schreiber in
// einem fremden Paket muss auffallen.
func TestScanAuditCategories_FindetKonstantenUndFremdePakete(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "database"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package database\nconst cat = \"x\"\nfunc f(h interface{ RecordSystemAudit(a, c, d, e, f, g string) }) {\n" +
		"\th.RecordSystemAudit(\"a\", cat, \"T\", \"1\", \"n\", \"\")\n\th.RecordSystemAudit(\"a\", \"fremd\", \"T\", \"1\", \"n\", \"\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "database", "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found, nonLiteral := scanAuditCategories(t, dir)
	if len(nonLiteral) != 1 {
		t.Errorf("Konstante nicht gemeldet: %v", nonLiteral)
	}
	if _, ok := found["fremd"]; !ok {
		t.Errorf("Schreiber im Paket database nicht erfasst: %v", found)
	}
}

// Die Chips der Oberfläche (CATS in AdminAuditLog.vue) und
// models.AuditCategories müssen übereinstimmen. Liest die Vue-Datei wie
// database/system_capabilities_test.go die Hilfeseiten; bei 0 Treffern schlägt
// der Test fehl statt still grün zu sein.
func TestAuditCategories_MatchFrontendChips(t *testing.T) {
	raw, err := os.ReadFile("../../frontend/src/views/admin/AdminAuditLog.vue")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "const CATS = computed(")
	if start < 0 {
		t.Fatal("CATS in AdminAuditLog.vue nicht gefunden")
	}
	end := strings.Index(src[start:], "])")
	if end < 0 {
		t.Fatal("Ende von CATS nicht gefunden")
	}
	ids := regexp.MustCompile(`\{\s*id:\s*'([a-z]+)'`).FindAllStringSubmatch(src[start:start+end], -1)
	if len(ids) == 0 {
		t.Fatal("keine Chip-IDs in CATS gefunden")
	}
	chips := map[string]bool{}
	for _, m := range ids {
		chips[m[1]] = true
		if !models.IsAuditCategory(m[1]) {
			t.Errorf("Chip %q hat keine Entsprechung in models.AuditCategories", m[1])
		}
	}
	for _, cat := range models.AuditCategories {
		if !chips[cat] {
			t.Errorf("Kategorie %q hat keinen Chip in AdminAuditLog.vue", cat)
		}
	}
}

// Die OpenAPI-Beschreibung des Filters nennt genau die gültigen Kategorien.
func TestAuditCategories_MatchOpenAPIEnum(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`name: category, in: query[^\n]*enum: \[([^\]]*)\]`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("enum für den Audit-Parameter category fehlt in openapi.yaml")
	}
	var got []string
	for _, v := range strings.Split(m[1], ",") {
		got = append(got, strings.TrimSpace(v))
	}
	sort.Strings(got)
	want := append([]string{}, models.AuditCategories...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("openapi enum %v != models.AuditCategories %v", got, want)
	}
}
