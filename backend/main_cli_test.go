package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// cliTestPassword ist ein reines Testpasswort.
const cliTestPassword = "ein-langes-testpasswort"

func newCLITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/cli.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Role{}, &models.User{}))
	return db
}

func runCreate(t *testing.T, db *gorm.DB, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = runCreateAdmin(args, strings.NewReader(stdin), &o, &e, db)
	return code, o.String(), e.String()
}

func TestCreateAdminCLI_LegtAdminAnUndGibtPasswortNieAus(t *testing.T) {
	db := newCLITestDB(t)
	code, out, errOut := runCreate(t, db, cliTestPassword+"\n", "--email", "admin@example.com", "--password-stdin")

	assert.Zero(t, code, errOut)
	assert.Contains(t, out, "admin@example.com")
	assert.NotContains(t, out+errOut, cliTestPassword)
	var u models.User
	require.NoError(t, db.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.True(t, u.IsGlobalAdmin)
	assert.True(t, database.CheckPasswordHash(cliTestPassword, u.PasswordHash))
}

func TestCreateAdminCLI_FehlerGebenNichtNullZurueckUndSchreibenNichts(t *testing.T) {
	cases := map[string]struct {
		stdin string
		args  []string
	}{
		"ohne E-Mail":                  {cliTestPassword + "\n", []string{"--password-stdin"}},
		"ohne --password-stdin":        {cliTestPassword + "\n", []string{"--email", "admin@example.com"}},
		"leere Eingabe":                {"", []string{"--email", "admin@example.com", "--password-stdin"}},
		"Passwort zu kurz":             {"kurz\n", []string{"--email", "admin@example.com", "--password-stdin"}},
		"unbekanntes Argument":         {cliTestPassword + "\n", []string{"--email", "admin@example.com", "--password-stdin", "--x"}},
		"ungültige Adresse":            {cliTestPassword + "\n", []string{"--email", "nein", "--password-stdin"}},
		"Passwort in der Befehlszeile": {"", []string{"--email", "admin@example.com", "--password", cliTestPassword}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := newCLITestDB(t)
			code, out, errOut := runCreate(t, db, tc.stdin, tc.args...)
			assert.NotZero(t, code)
			assert.NotContains(t, out+errOut, cliTestPassword)
			var n int64
			require.NoError(t, db.Model(&models.User{}).Count(&n).Error)
			assert.Zero(t, n)
		})
	}
}

func TestCreateAdminCLI_VorhandenesKontoGibtFehlercode(t *testing.T) {
	db := newCLITestDB(t)
	require.NoError(t, db.Create(&models.User{Email: "admin@example.com", PasswordHash: "alt"}).Error)
	code, _, errOut := runCreate(t, db, cliTestPassword+"\n", "--email", "admin@example.com", "--password-stdin")
	assert.NotZero(t, code)
	assert.Contains(t, errOut, "bereits")
}

func TestRunCLI_LehntSeedUndUnbekannteBefehleAb(t *testing.T) {
	var o, e bytes.Buffer

	// Das Seeden würde alle Tabellen löschen: create-admin darf nie damit laufen.
	t.Setenv("SEED_DB", "true")
	code := runCLI([]string{"create-admin", "--email", "a@example.com", "--password-stdin"}, strings.NewReader(cliTestPassword+"\n"), &o, &e)
	assert.NotZero(t, code)
	assert.Contains(t, e.String(), "SEED_DB")

	e.Reset()
	t.Setenv("SEED_DB", "")
	code = runCLI([]string{"geheimbefehl"}, strings.NewReader(""), &o, &e)
	assert.NotZero(t, code)
	assert.Contains(t, e.String(), "create-admin")
}

// Der Testlauf übergibt Flags wie -test.v an main(); nur ein Wort ohne
// führenden Bindestrich ist ein Verwaltungsbefehl.
func TestCLIRequested(t *testing.T) {
	assert.False(t, cliRequested(nil))
	assert.False(t, cliRequested([]string{"-test.v"}))
	assert.True(t, cliRequested([]string{"create-admin"}))
	assert.True(t, cliRequested([]string{"unbekannt"}))
}

type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) {
	c.n += len(p)
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// Eine endlose Eingabe ohne Zeilenende darf nicht ungebremst gelesen werden.
func TestCreateAdminCLI_LiestNurBegrenzteMengeVonStdin(t *testing.T) {
	db := newCLITestDB(t)
	src := &countingReader{}
	var o, e bytes.Buffer
	code := runCreateAdmin([]string{"--email", "admin@example.com", "--password-stdin"}, io.LimitReader(src, 1<<20), &o, &e, db)
	assert.NotZero(t, code)
	assert.LessOrEqual(t, src.n, 16*1024)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("lesefehler") }

func TestCreateAdminCLI_LesefehlVonStdinBrichtAb(t *testing.T) {
	db := newCLITestDB(t)
	var o, e bytes.Buffer
	code := runCreateAdmin([]string{"--email", "admin@example.com", "--password-stdin"}, failingReader{}, &o, &e, db)
	assert.NotZero(t, code)
	var n int64
	require.NoError(t, db.Model(&models.User{}).Count(&n).Error)
	assert.Zero(t, n)
}

// runCLI öffnet die Datenbank erst nach den Vorprüfungen; main() reicht den
// Exit-Code des Befehls weiter, statt den Server zu starten.
func TestMain_FuehrtVerwaltungsbefehlAusStattServerZuStarten(t *testing.T) {
	db := newCLITestDB(t)
	t.Setenv("SEED_DB", "")
	prevOpen, prevExit, prevArgs, prevIn := openDatabase, exitProcess, os.Args, os.Stdin
	t.Cleanup(func() { openDatabase, exitProcess, os.Args, os.Stdin = prevOpen, prevExit, prevArgs, prevIn })
	openDatabase = func() *gorm.DB { return db }
	var got = -1
	exitProcess = func(code int) { got = code }

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	_, err = pw.WriteString(cliTestPassword + "\n")
	require.NoError(t, err)
	require.NoError(t, pw.Close())
	os.Stdin = pr
	os.Args = []string{"server", "create-admin", "--email", "admin@example.com", "--password-stdin"}

	main()

	assert.Equal(t, 0, got)
	var n int64
	require.NoError(t, db.Model(&models.User{}).Where("is_global_admin = ?", true).Count(&n).Error)
	assert.EqualValues(t, 1, n)
}
