package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Strukturwächter gegen SQL-Injection durch Zeichenkettenbau: Die Fragmente,
// die an Where/Raw/Exec/Order/Joins/Select/Having/Group gehen, bestehen aus
// Literalen und benannten Konstanten. Wo ein Fragment aus einer Variablen
// zusammengesetzt wird (z. B. ein Spaltenname), steht die Stelle unten in der
// Liste und wurde geprüft: Der Wert stammt aus dem Code, nie aus einer
// Anfrage. Eine neue Stelle schlägt hier an und braucht dieselbe Prüfung;
// Nutzereingaben gehören immer als ?-Parameter in die Abfrage.
var sqlCallMethods = map[string]bool{
	"Where": true, "Not": true, "Or": true, "Raw": true, "Exec": true,
	"Order": true, "Joins": true, "Select": true, "Having": true, "Group": true,
}

// reviewedDynamicSQL: Fundstelle (Datei:Methode:Ausdruck) -> Herkunft des Werts.
var reviewedDynamicSQL = map[string]string{
	"database/admin_notifications.go:Exec:sql":                                                          "Schleife über zwei feste Anweisungen im Code",
	"database/db.go:Exec:`CREATE UNIQUE INDEX IF NOT EXISTS ` + idx.newIndex + ` ON ` + idx.definition": "Tabellen- und Indexnamen aus festen Listen im Code (softDeleteUniqueIndexes, joinTables) bzw. festes SQL",
	"database/db.go:Exec:`DROP INDEX IF EXISTS ` + idx.legacyIndex":                                     "Tabellen- und Indexnamen aus festen Listen im Code (softDeleteUniqueIndexes, joinTables) bzw. festes SQL",
	"database/db.go:Exec:`DROP INDEX IF EXISTS ` + jt.legacyIndex":                                      "Tabellen- und Indexnamen aus festen Listen im Code (softDeleteUniqueIndexes, joinTables) bzw. festes SQL",
	"database/db.go:Exec:sql":                                         "Tabellen- und Indexnamen aus festen Listen im Code (softDeleteUniqueIndexes, joinTables) bzw. festes SQL",
	"database/languages.go:Exec:sql":                                  "festes SQL-Literal im Code",
	"database/ui_overrides.go:Exec:sql":                               "Schleife über zwei feste Anweisungen im Code",
	"database/token_hash.go:Where:\"id = ? AND \"+tc.column+\" = ?\"": "tc.column kommt aus der festen Liste tokenColumns im Code",
	"database/token_hash.go:Where:tc.column+\" IS NOT NULL AND \"+tc.column+\" <> '' AND \"+tc.column+\" NOT LIKE ?\"": "tc.column kommt aus der festen Liste tokenColumns im Code",
	"database/user_status_page_roles.go:Exec:sql":                                                                        "festes SQL-Literal im Code",
	"database/user_status_page_roles.go:Exec:sql #2":                                                                     "festes SQL-Literal im Code",
	"externalstatus/outage.go:Where:\"id = ? AND outage_since IS NOT NULL AND \"+column+\" IS NULL\"":                    "column kommt von den drei festen Aufrufern (outage_*_at), nie aus einer Anfrage",
	"handlers/admin_notification.go:Exec:sql":                                                                            "scope und adminNotifUnreadClause sind feste Literale im Code, Werte gehen als ?-Parameter",
	"handlers/oidc_frontend.go:Where:\"p.deleted_at IS NULL AND \"+p.column+\" = ?\"":                                    "Spaltenname aus der festen Liste ssoEnabledColumns",
	"handlers/page_order.go:Where:mailboxCond+\" AND status = ? AND NOT (\"+orderPendingExpiredSQL+\")\"":                "mailboxCond kommt aus mailboxMatch (feste Fragmente, Werte als ?-Parameter), orderPendingExpiredSQL ist eine feste Konstante",
	"handlers/subscriber.go:Where:\"channel = 'email' AND status_page_id = ? AND \"+clause":                              "clause kommt aus mailboxMatch (feste Fragmente, Werte als ?-Parameter)",
	"handlers/subscriber_confirm.go:Where:\"channel = 'email' AND status = ? AND confirm_token_expiry > ? AND \"+clause": "clause kommt aus mailboxMatch (feste Fragmente, Werte als ?-Parameter)",
	"handlers/translation_admin_list.go:Where:parentColumn+\" = ?\"":                                                     "parentColumn übergeben die drei festen Aufrufer (incident_id, component_id, notice_id)",
	"selfmonitor/monitor.go:Where:\"id = ? AND \"+col+\" = ? AND \"+col+\" <> ''\"":                                      "col übergeben feste Aufrufer im Selbstmonitor (Spaltennamen im Code)",
}

// dynamicSQLSites liefert alle Aufrufe, deren erstes Argument nicht nur aus
// Literalen und benannten Konstanten besteht, als "Datei:Methode:Ausdruck".
func dynamicSQLSites(t *testing.T, root string) []string {
	t.Helper()
	type parsed struct {
		path string
		fset *token.FileSet
		file *ast.File
		src  []byte
	}
	consts := map[string]map[string]bool{} // Paketverzeichnis -> Konstantennamen
	var files []parsed
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || (strings.HasPrefix(d.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		dir := filepath.Dir(path)
		if consts[dir] == nil {
			consts[dir] = map[string]bool{}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				for _, sp := range gd.Specs {
					for _, name := range sp.(*ast.ValueSpec).Names {
						consts[dir][name.Name] = true
					}
				}
			}
			return true
		})
		files = append(files, parsed{filepath.ToSlash(path), fset, f, src})
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)

	var sites []string
	for _, pf := range files {
		known := consts[filepath.Dir(filepath.FromSlash(pf.path))]
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !sqlCallMethods[sel.Sel.Name] {
				return true
			}
			if isStaticSQL(call.Args[0], known) {
				return true
			}
			from, to := pf.fset.Position(call.Args[0].Pos()).Offset, pf.fset.Position(call.Args[0].End()).Offset
			sites = append(sites, strings.TrimPrefix(pf.path, "./")+":"+sel.Sel.Name+":"+string(pf.src[from:to]))
			return true
		})
	}
	sort.Strings(sites)
	// Gleiche Ausdrücke in einer Datei werden durchnummeriert (" #2", ...), damit
	// eine weitere Stelle mit demselben Text nicht unter dem alten Eintrag durchgeht.
	seen := map[string]int{}
	for i, s := range sites {
		seen[s]++
		if seen[s] > 1 {
			sites[i] = s + " #" + strconv.Itoa(seen[s])
		}
	}
	return sites
}

// isStaticSQL: Literal, benannte Konstante oder Verkettung nur daraus. Nicht
// zur SQL-Methode gehörende Aufrufe (z. B. Where(gormExpr), Select([]string{...}))
// bleiben außen vor, solange sie keine Zeichenkette bauen.
func isStaticSQL(e ast.Expr, consts map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return consts[v.Name]
	case *ast.ParenExpr:
		return isStaticSQL(v.X, consts)
	case *ast.BinaryExpr:
		return isStaticOperand(v.X, consts) && isStaticOperand(v.Y, consts)
	case *ast.CompositeLit, *ast.UnaryExpr, *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr, *ast.FuncLit:
		// Strukturen und Typen (Select([]string{..}), Where(&model), Where(user.ID))
		// enthalten keinen Zeichenkettenbau auf oberster Ebene.
		return true
	case *ast.CallExpr:
		// Sprintf & Co. bauen Zeichenketten; Konvertierungen und Konstruktoren
		// wie gorm.Expr(...) oder clause.Eq{...} nicht.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Join" || sel.Sel.Name == "Replace") {
			return false
		}
		return true
	}
	return true
}

// isStaticOperand: Operand einer Verkettung. Feldzugriffe, Indizes und Aufrufe
// sind hier dynamisch (jt.legacyIndex, p.column, f(x)), nur Literale, benannte
// Konstanten und Verkettungen daraus nicht.
func isStaticOperand(e ast.Expr, consts map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return consts[v.Name]
	case *ast.ParenExpr:
		return isStaticOperand(v.X, consts)
	case *ast.BinaryExpr:
		return isStaticOperand(v.X, consts) && isStaticOperand(v.Y, consts)
	}
	return false
}

func TestSQL_NoDynamicFragmentsOutsideReviewedSites(t *testing.T) {
	got := dynamicSQLSites(t, ".")
	var unexpected []string
	for _, s := range got {
		if _, ok := reviewedDynamicSQL[s]; !ok {
			unexpected = append(unexpected, s)
		}
	}
	assert.Empty(t, unexpected, "SQL nicht aus Variablen zusammensetzen; Nutzereingaben nur als ?-Parameter (oder die Stelle prüfen und eintragen)")
	for site := range reviewedDynamicSQL {
		assert.Contains(t, got, site, "Eintrag veraltet, bitte entfernen")
	}
}

// Gegenprobe: Der Prüfer erkennt Verkettung mit einer Variablen und Sprintf,
// lässt Literale, Konstanten und Konstantenverkettung durch.
func TestDynamicSQLSites_DetectsConcatenation(t *testing.T) {
	dir := t.TempDir()
	src := `package x
const whereOK = "a = ?"
func f(db DB, name string, id int) {
	db.Where("a = ?", id)
	db.Where(whereOK, id)
	db.Where("x = ? AND " + whereOK)
	db.Where("a = " + name)
	db.Exec("DROP INDEX " + obj.index)
	db.Where(fmt.Sprintf("a = %s", name))
	db.Order(name)
	db.Select([]string{"a"})
	db.Where(&model{})
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600))
	got := dynamicSQLSites(t, dir)
	require.Len(t, got, 4, "%v", got)
	assert.Contains(t, got[0], `Exec:"DROP INDEX " + obj.index`)
	assert.Contains(t, got[1], `Order:name`)
	assert.Contains(t, got[2], `Where:"a = " + name`)
	assert.Contains(t, got[3], `Where:fmt.Sprintf(`)
}
