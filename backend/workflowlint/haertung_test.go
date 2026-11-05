package workflowlint

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func haertung(t *testing.T, yaml string) []Finding {
	t.Helper()
	f, err := CheckHardening("wf.yml", []byte(yaml))
	require.NoError(t, err)
	return f
}

const checkoutSHA = "actions/checkout@" + sha + " # v4"

// Ohne permissions bekommt das GITHUB_TOKEN die Standardrechte des Repositorys,
// oft Schreibrechte. Jeder Job braucht eine ausdrückliche Angabe, im Workflow
// oder im Job.
func TestHaertung_PermissionsFehlen(t *testing.T) {
	f := haertung(t, `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Equal(t, "wf.yml", f[0].File)
	require.Contains(t, f[0].Message, `Job "build"`)
	require.Contains(t, f[0].Message, "permissions")
}

func TestHaertung_PermissionsImWorkflowGenuegen(t *testing.T) {
	require.Empty(t, haertung(t, `
on: push
permissions:
  contents: read
jobs:
  a: {runs-on: ubuntu-latest, steps: [{run: echo}]}
  b: {runs-on: ubuntu-latest, steps: [{run: echo}]}
`))
	// Auch ein leerer Block (alle Rechte aus) ist eine ausdrückliche Angabe.
	require.Empty(t, haertung(t, `
on: push
permissions: {}
jobs:
  a: {runs-on: ubuntu-latest, steps: [{run: echo}]}
`))
}

func TestHaertung_PermissionsJeJob(t *testing.T) {
	f := haertung(t, `
on: push
jobs:
  mit:
    runs-on: ubuntu-latest
    permissions: {contents: read}
    steps: [{run: echo}]
  ohne:
    runs-on: ubuntu-latest
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, `Job "ohne"`)
}

func TestHaertung_WiederverwendbarerWorkflowBrauchtPermissions(t *testing.T) {
	f := haertung(t, `
on: push
jobs:
  call:
    uses: ./.github/workflows/b.yml
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, `Job "call"`)
}

func TestHaertung_WriteAllIstVerboten(t *testing.T) {
	f := haertung(t, `
on: push
permissions: write-all
jobs:
  a: {runs-on: ubuntu-latest, steps: [{run: echo}]}
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "write-all")

	f = haertung(t, `
on: push
permissions: {contents: read}
jobs:
  a:
    runs-on: ubuntu-latest
    permissions: write-all
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, `Job "a"`)
	require.Contains(t, f[0].Message, "write-all")

	// auch mit Leerraum in Anführungszeichen
	f = haertung(t, `
on: push
permissions: " write-all "
jobs:
  a: {runs-on: ubuntu-latest, steps: [{run: echo}]}
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "write-all")
}

// actions/checkout legt das Token sonst in .git/config ab, wo es jeder spätere
// Schritt (und jeder Code des Repositorys) lesen kann.
func TestHaertung_CheckoutOhnePersistCredentialsFalse(t *testing.T) {
	kopf := "on: push\npermissions: {contents: read}\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n"
	faelle := []struct {
		name  string
		step  string
		anz   int
		bleib string
	}{
		{"ganz ohne with", "      - uses: " + checkoutSHA + "\n", 1, "persist-credentials"},
		{"with ohne Eintrag", "      - uses: " + checkoutSHA + "\n        with: {fetch-depth: 0}\n", 1, "persist-credentials"},
		{"true", "      - uses: " + checkoutSHA + "\n        with: {persist-credentials: true}\n", 1, "persist-credentials"},
		{"String true", "      - uses: " + checkoutSHA + "\n        with: {persist-credentials: 'true'}\n", 1, "persist-credentials"},
		{"ausdruck statt Wert", "      - uses: " + checkoutSHA + "\n        with: {persist-credentials: '${{ inputs.x }}'}\n", 1, "persist-credentials"},
		{"false", "      - uses: " + checkoutSHA + "\n        with: {persist-credentials: false}\n", 0, ""},
		{"String false", "      - uses: " + checkoutSHA + "\n        with: {persist-credentials: 'false'}\n", 0, ""},
		{"andere Action braucht es nicht", "      - uses: actions/setup-go@" + sha + "\n", 0, ""},
		{"Lauf ohne Action", "      - run: echo\n", 0, ""},
		{"ähnlich benannte Action braucht es nicht", "      - uses: actions/checkout-helper@" + sha + "\n", 0, ""},
	}
	for _, c := range faelle {
		t.Run(c.name, func(t *testing.T) {
			f := haertung(t, kopf+c.step)
			require.Len(t, f, c.anz)
			if c.anz > 0 {
				require.Contains(t, f[0].Message, c.bleib)
				require.Contains(t, f[0].Message, `Job "a"`)
			}
		})
	}
}

func TestHaertung_KaputtesYAMLIstFehler(t *testing.T) {
	_, err := CheckHardening("wf.yml", []byte("on: [\n"))
	require.Error(t, err)
}

func TestHaertung_DirPrueftAlleDateien(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
		[]byte("on: push\njobs: {x: {runs-on: ubuntu-latest, steps: [{run: echo}]}}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.yaml"),
		[]byte("on: push\njobs: {x: {runs-on: ubuntu-latest, steps: [{run: echo}]}}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gut.yml"),
		[]byte("on: push\npermissions: {}\njobs: {x: {runs-on: ubuntu-latest, steps: [{run: echo}]}}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notiz.md"), []byte("kein workflow"), 0o600))
	f, err := CheckHardeningDir(dir)
	require.NoError(t, err)
	require.Len(t, f, 2, "beide Endungen werden geprüft, andere Dateien nicht")
	require.Equal(t, filepath.Join(dir, "a.yml"), f[0].File)
	require.Equal(t, filepath.Join(dir, "b.yaml"), f[1].File)

	_, err = CheckHardeningDir(filepath.Join(dir, "gibt-es-nicht"))
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kaputt.yml"), []byte("on: [\n"), 0o600))
	_, err = CheckHardeningDir(dir)
	require.ErrorContains(t, err, "kaputt.yml")
	require.NoError(t, os.Remove(filepath.Join(dir, "kaputt.yml")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "ordner.yml"), 0o700))
	_, err = CheckHardeningDir(dir)
	require.ErrorContains(t, err, "ordner.yml")
}

func TestHaertung_RunMeldetFehlendePermissions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
		[]byte("on: push\njobs: {x: {runs-on: ubuntu-latest, steps: [{run: echo}]}}\n"), 0o600))
	var out, errOut bytes.Buffer
	require.Equal(t, 1, Run([]string{dir}, &out, &errOut))
	require.Contains(t, errOut.String(), "a.yml: ")
	require.Contains(t, errOut.String(), "permissions")
}

// Die Workflows dieses Repositorys erfüllen die Regeln.
func TestHaertung_EigeneWorkflowsSindSauber(t *testing.T) {
	f, err := CheckHardeningDir(filepath.Join("..", "..", ".github", "workflows"))
	require.NoError(t, err)
	require.Empty(t, f)
}

// Passt eine Datei nicht zum Schema der Härtungsregeln (hier: with ist keine Map),
// ist das ein Lesefehler (Exit 2), kein stiller Erfolg.
func TestHaertung_RunMeldetNichtLesbareStruktur(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
		[]byte("on: push\npermissions: {}\njobs: {x: {runs-on: ubuntu-latest, steps: [{uses: 'actions/setup-go@"+sha+"', with: kaputt}]}}\n"), 0o600))
	var out, errOut bytes.Buffer
	require.Equal(t, 2, Run([]string{dir}, &out, &errOut))
	require.Contains(t, errOut.String(), "a.yml")
}
