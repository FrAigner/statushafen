package handlers

import (
	"encoding/json"
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
)

// Jede im Backend geschriebene Audit-Aktion braucht eine Beschriftung
// in de.json UND en.json (auditLog.actions.<gruppe>.<schluessel>), sonst zeigt die
// Liste den Rohschluessel. rohwerte.test.js prueft das im Frontend per Regex;
// dieser Test liest per AST (wie scanAuditCategories) und faengt auch Aufrufe
// ausserhalb von handlers/. Erfasst werden:
//   - RecordAudit(c, ACTION, ...) / RecordSystemAudit(ACTION, ...)
//   - Action: ... in AuditLog{}-Literalen (auch models.AuditLog)
//
// Konstanten (const auditActionX = "a.b") werden aufgeloest. Variablen als Aktion
// (z. B. action := "x.y" je nach Zweig) sieht der Test nicht direkt; solche Dateien
// stehen in auditActionVariablen, und dort zaehlen ALLE Literale der Form
// gruppe.schluessel, deren Gruppe in auditLog.actions existiert, als Aktion.
// Jede andere Nicht-Literal-Aktion macht den Test rot.
var auditActionVariablen = map[string]string{
	"selfmonitor/monitor.go":                  "Durchreichung in der Hilfsmethode audit; die Aufrufer sind Literale",
	"handlers/incident_detail.go":             "action je nach Zweig",
	"handlers/maintenance_scheduler.go":       "auditAction je nach Zweig",
	"handlers/notification_template_admin.go": "auditAction je nach Zweig",
	"handlers/oidc_login.go":                  "action je nach Ergebnis",
	"handlers/statuspage_sso.go":              "r.action aus einer Tabelle",
}

var auditActionShape = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)

func scanAuditActions(t *testing.T, root string) (found map[string]string, nonLiteral []string) {
	t.Helper()
	found = map[string]string{}
	fset := token.NewFileSet()
	var files []string
	_ = filepath.WalkDir(root, func(f string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == ".git" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			files = append(files, f)
		}
		return nil
	})
	parsed := map[string]*ast.File{}
	consts := map[string]string{}
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed[f] = file
		ast.Inspect(file, func(n ast.Node) bool {
			if vs, ok := n.(*ast.ValueSpec); ok {
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if u, err := strconv.Unquote(lit.Value); err == nil {
								consts[name.Name] = u
							}
						}
					}
				}
			}
			return true
		})
	}
	variableFiles := map[string]bool{}
	check := func(e ast.Expr, f string) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if u, err := strconv.Unquote(v.Value); err == nil {
					found[u] = f
					return
				}
			}
		case *ast.Ident:
			if u, ok := consts[v.Name]; ok {
				found[u] = f
				return
			}
		}
		rel := strings.TrimPrefix(filepath.ToSlash(f), "../")
		if _, ok := auditActionVariablen[rel]; ok {
			variableFiles[f] = true
			return
		}
		nonLiteral = append(nonLiteral, fset.Position(e.Pos()).String())
	}
	for _, f := range files {
		if filepath.Base(f) == "audit.go" {
			continue
		}
		ast.Inspect(parsed[f], func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				idx := -1
				switch sel.Sel.Name {
				case "RecordAudit":
					idx = 1
				case "RecordSystemAudit", "audit":
					// audit: Hilfsmethode der Selbstueberwachung (monitor.audit(action, ...))
					idx = 0
				}
				if idx >= 0 && len(x.Args) > idx {
					check(x.Args[idx], f)
				}
			case *ast.CompositeLit:
				if !isAuditLogType(x.Type) {
					return true
				}
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Action" {
							check(kv.Value, f)
						}
					}
				}
			}
			return true
		})
	}
	// Variablen-Dateien: Literale der Form gruppe.schluessel einer bekannten Gruppe.
	groups := map[string]bool{}
	for k := range found {
		g, _, _ := strings.Cut(k, ".")
		groups[g] = true
	}
	for f := range variableFiles {
		ast.Inspect(parsed[f], func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if u, err := strconv.Unquote(lit.Value); err == nil && auditActionShape.MatchString(u) {
					if g, _, _ := strings.Cut(u, "."); groups[g] {
						found[u] = f
					}
				}
			}
			return true
		})
	}
	return found, nonLiteral
}

func isAuditLogType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "AuditLog" || v.Name == "mailAuditTarget"
	case *ast.SelectorExpr:
		return v.Sel.Name == "AuditLog"
	}
	return false
}

// missingAuditLabels liefert die Aktionen ohne Beschriftung in messages.
func missingAuditLabels(actions []string, messages map[string]any) []string {
	var missing []string
	for _, a := range actions {
		group, key, ok := strings.Cut(a, ".")
		if !ok {
			missing = append(missing, a)
			continue
		}
		al, _ := messages["auditLog"].(map[string]any)
		acts, _ := al["actions"].(map[string]any)
		g, _ := acts[group].(map[string]any)
		if s, _ := g[key].(string); s == "" {
			missing = append(missing, a)
		}
	}
	return missing
}

func loadLocale(t *testing.T, lang string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "locales", lang+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAuditActions_HaveLabelsInDeAndEn(t *testing.T) {
	found, nonLiteral := scanAuditActions(t, "..")
	if len(found) < 50 {
		t.Fatalf("Scanner findet zu wenige Audit-Aktionen (%d) - Muster kaputt?", len(found))
	}
	for _, pos := range nonLiteral {
		t.Errorf("Audit-Aktion ist weder Literal noch Konstante (%s) - der Test sieht sie nicht; Datei in auditActionVariablen aufnehmen oder Literal verwenden", pos)
	}
	actions := make([]string, 0, len(found))
	for a := range found {
		actions = append(actions, a)
	}
	sort.Strings(actions)
	for _, lang := range []string{"de", "en"} {
		if miss := missingAuditLabels(actions, loadLocale(t, lang)); len(miss) > 0 {
			t.Errorf("%s.json: Audit-Aktionen ohne Beschriftung (auditLog.actions): %v", lang, miss)
		}
	}
}

// Gegenprobe: eine fehlende Beschriftung und ein Fixture-Schreiber muessen auffallen.
func TestAuditActions_Gegenprobe(t *testing.T) {
	de := loadLocale(t, "de")
	if m := missingAuditLabels([]string{"statuspage.created"}, de); len(m) != 0 {
		t.Fatalf("vorhandene Beschriftung als fehlend gemeldet: %v", m)
	}
	if m := missingAuditLabels([]string{"statuspage.gibt_es_nicht", "ohnepunkt"}, de); len(m) != 2 {
		t.Fatalf("fehlende Beschriftungen nicht erkannt: %v", m)
	}
	dir := t.TempDir()
	src := "package x\nfunc f(h interface{ RecordAudit(a ...any) }, v string) {\n" +
		"h.RecordAudit(nil, \"foo.bar\", \"config\")\nh.RecordAudit(nil, v, \"config\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	found, non := scanAuditActions(t, dir)
	if _, ok := found["foo.bar"]; !ok || len(non) != 1 {
		t.Fatalf("Scanner: found=%v nonLiteral=%v", found, non)
	}
}
