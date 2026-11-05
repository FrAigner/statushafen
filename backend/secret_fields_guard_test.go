package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Zwei Strukturwächter für Geheimnisse:
//
//  1. Ein Modellfeld, das einen Token trägt, speichert nur dessen Hash (Name
//     endet auf Hash) oder ist Metadaten (Ablauf, Version). Ein Feld mit dem
//     Klartext eines Zufalls-Tokens stünde im Backup und in jedem Lesezugriff
//     auf die Datenbank. Ausnahmen stehen in der Liste und haben einen Grund.
//  2. Ein Log-Aufruf (slog) bekommt keine Variable, deren Name auf ein Geheimnis
//     hindeutet (Token, Secret, Passwort, Verifier, Authorization). Meldungen
//     und Fehler gehen über logging.SanitizeErr; hier geht es um Werte, die ein
//     Aufrufer versehentlich als Feld mitgibt.

var allowedPlaintextTokenFields = map[string]string{
	"CustomDomainVerificationToken": "öffentlicher Wert: steht als TXT-Eintrag im DNS der Domain und beweist nur den Besitz, kein Zugang",
	"TokenVersion":                  "Zähler zum Widerruf von Sitzungen, kein Token",
}

var tokenFieldName = regexp.MustCompile(`(?i)token`)
var tokenFieldMetadata = regexp.MustCompile(`(?i)(hash|expiry|expires|version)$`)

func TestModelTokenFieldsStoreOnlyHashes(t *testing.T) {
	files, err := filepath.Glob("models/*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if !tokenFieldName.MatchString(name.Name) {
						continue
					}
					checked++
					if tokenFieldMetadata.MatchString(name.Name) {
						continue
					}
					_, allowed := allowedPlaintextTokenFields[name.Name]
					assert.True(t, allowed, "%s: das Feld %s trägt vermutlich einen Token im Klartext. Nur den Hash speichern (models.HashToken, Feldname auf Hash) oder mit Grund in die Ausnahmen", path, name.Name)
				}
			}
			return true
		})
	}
	assert.GreaterOrEqual(t, checked, 6, "die Token-Felder der Modelle wurden gefunden")
}

var secretArgName = regexp.MustCompile(`(?i)(token|secret|passw|verifier|authorization|credential|apikey|api_key)`)

func TestSlogCallsGetNoSecretVariables(t *testing.T) {
	var offenders []string
	slogCalls := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || (strings.HasPrefix(d.Name(), ".") && path != ".") {
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
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "slog" || !isSlogLevel(sel.Sel.Name) {
				return true
			}
			slogCalls++
			for _, arg := range call.Args[1:] {
				if lit, isLit := arg.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					continue // Schlüssel und feste Texte
				}
				text := string(src[fset.Position(arg.Pos()).Offset:fset.Position(arg.End()).Offset])
				if secretArgName.MatchString(text) {
					offenders = append(offenders, filepath.ToSlash(path)+":"+text)
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, slogCalls, 50, "die Log-Aufrufe wurden gefunden")
	assert.Empty(t, offenders, "ein Log-Aufruf bekommt eine Variable mit Geheimnis im Namen")
}

func isSlogLevel(name string) bool {
	return name == "Debug" || name == "Info" || name == "Warn" || name == "Error"
}

// Gegenprobe: Die Prüfer schlagen an, wenn der Schutz fehlt, und lassen Hashes,
// Metadaten und feste Texte zu.
func TestSecretGuards_DetectViolations(t *testing.T) {
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		require.NoError(t, err)
		return f
	}
	violations := func(src string) (bad []string) {
		ast.Inspect(parse(src), func(n ast.Node) bool {
			if st, ok := n.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if tokenFieldName.MatchString(name.Name) && !tokenFieldMetadata.MatchString(name.Name) {
							if _, allowed := allowedPlaintextTokenFields[name.Name]; !allowed {
								bad = append(bad, name.Name)
							}
						}
					}
				}
			}
			return true
		})
		return bad
	}
	assert.Equal(t, []string{"ConfirmToken"}, violations("package m\ntype O struct{ ConfirmToken string }"))
	assert.Equal(t, []string{"WebhookToken"}, violations("package m\ntype O struct{ WebhookToken string }"))
	assert.Empty(t, violations("package m\ntype O struct{ ConfirmTokenHash string; ConfirmTokenExpiry int; TokenVersion uint }"))

	assert.True(t, secretArgName.MatchString("sub.ConfirmToken"))
	assert.True(t, secretArgName.MatchString("clientSecret"))
	assert.True(t, secretArgName.MatchString("st.CodeVerifier"))
	assert.False(t, secretArgName.MatchString("logging.SanitizeErr(err)"))
	assert.False(t, secretArgName.MatchString("sub.ID"))
}
