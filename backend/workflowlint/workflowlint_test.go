package workflowlint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func pruefe(t *testing.T, yaml string) []Finding {
	t.Helper()
	f, err := CheckWorkflow("wf.yml", []byte(yaml))
	require.NoError(t, err)
	return f
}

func TestSelfHostedBeiPullRequestIstVerboten(t *testing.T) {
	f := pruefe(t, `
on: [pull_request]
jobs:
  review:
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Equal(t, "wf.yml", f[0].File)
	require.Contains(t, f[0].Message, "review")
	require.Contains(t, f[0].Message, "self-hosted")
	// The message names only GitHub-hosted runners as allowed, no exception.
	require.Contains(t, f[0].Message, "nur GitHub-gehostete Runner")
}

// There is no exception for own runners: even an internal CI label (ci-intern)
// is a violation on PR events.
func TestEigenerRunnerOhneAusnahmeBeiPullRequestIstVerboten(t *testing.T) {
	f := pruefe(t, `
on:
  pull_request:
jobs:
  build:
    runs-on: [self-hosted, ci-intern]
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "ci-intern")
}

func TestPullRequestTargetZaehltAuch(t *testing.T) {
	require.Len(t, pruefe(t, `
on: {pull_request_target: {types: [opened]}}
jobs:
  x:
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`), 1)
}

func TestRunsOnAlsEinzelnerString(t *testing.T) {
	require.Len(t, pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: self-hosted
    steps: [{run: echo}]
`), 1)
}

func TestRunsOnMitLabelsObjekt(t *testing.T) {
	require.Len(t, pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: {group: prod, labels: [self-hosted, custom-runner]}
    steps: [{run: echo}]
`), 1)
}

func TestRunsOnMitLabelsObjektOhneGruppe(t *testing.T) {
	// labels as first key: the labels themselves decide, not the neighbor key.
	f := pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: {labels: [self-hosted, custom-runner]}
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "custom-runner")
	require.Len(t, pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: {labels: [self-hosted, ci-intern]}
    steps: [{run: echo}]
`), 1)
}

func TestRunnerGruppeOhneLabelsIstSelfHosted(t *testing.T) {
	// Runner groups contain only self-hosted runners.
	require.Len(t, pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: {group: prod}
    steps: [{run: echo}]
`), 1)
}

func TestWiederverwendbarerWorkflowBeiPullRequestOhneRunsOn(t *testing.T) {
	require.Empty(t, pruefe(t, `
on: pull_request
jobs:
  x:
    uses: ./.github/workflows/a.yml
`))
}

func TestSelfHostedOhnePullRequestIstErlaubt(t *testing.T) {
	require.Empty(t, pruefe(t, `
on:
  push: {branches: [main]}
  workflow_dispatch:
jobs:
  deploy:
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`))
}

func TestJobNurBeiPushIstErlaubt(t *testing.T) {
	// A deploy job only for push runs on an own runner, but never for a PR.
	require.Empty(t, pruefe(t, `
on: [push, pull_request]
jobs:
  deploy:
    if: github.event_name == 'push'
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`))
	require.Empty(t, pruefe(t, `
on: [push, pull_request]
jobs:
  deploy:
    if: ${{ github.event_name == "push" }}
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`))
}

func TestPushAusnahmeNurWennBedingungBeiPRFalschIst(t *testing.T) {
	// Review finding 3: the exception may only apply if the if:
	// is certainly false on a PR, i.e. a pure &&-chain with
	// github.event_name == 'push' as one link.
	faelle := []struct {
		bedingung string
		befunde   int
	}{
		{`github.event_name == 'push'`, 0},
		{`${{ github.event_name == 'push' }}`, 0},
		{`github.event_name == 'push' && success()`, 0},
		{`success() && github.event_name == "push"`, 0},
		{`github.event_name == 'push' || github.event_name == 'pull_request'`, 1},
		{`${{ !(github.event_name == 'push') }}`, 1},
		{`github.event_name != 'push' || github.event_name == 'push'`, 1},
		{`(github.event_name == 'push' || true) && success()`, 1},
		{`github.event_name == 'pushed'`, 1},
		{`!(success() && github.event_name == 'push' && success())`, 1},
		{`${{ !(success() && github.event_name == 'push' && always()) }}`, 1},
		{`!(github.event_name == 'pull_request' && github.event_name == 'push' && true)`, 1},
		{`always() && github.event_name == 'push' && failure() && cancelled()`, 0},
		{`contains(github.event_name, 'push')`, 1},
	}
	for _, f := range faelle {
		t.Run(f.bedingung, func(t *testing.T) {
			require.Len(t, pruefe(t, `
on: pull_request
jobs:
  deploy:
    if: "`+strings.ReplaceAll(f.bedingung, `"`, `\"`)+`"
    runs-on: custom-runner
    steps: [{run: echo}]
`), f.befunde)
		})
	}
}

func TestGitHubGehosteterRunnerBeiPullRequestIstErlaubt(t *testing.T) {
	require.Empty(t, pruefe(t, `
on: pull_request
jobs:
  x:
    runs-on: ubuntu-latest
    steps: [{run: echo}]
`))
}

func TestNichtGepinnteActionIstVerboten(t *testing.T) {
	f := pruefe(t, `
on: push
jobs:
  x:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: snyk/actions/node@master
      - uses: actions/setup-go@`+sha+` # v5
`)
	require.Len(t, f, 2)
	require.Contains(t, f[0].Message, "actions/checkout@v4")
	require.Contains(t, f[1].Message, "snyk/actions/node@master")
}

func TestKurzerOderGrossgeschriebenerHashGiltNichtAlsPin(t *testing.T) {
	require.Len(t, pruefe(t, `
on: push
jobs:
  x:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@0123456
      - uses: actions/checkout@0123456789ABCDEF0123456789ABCDEF01234567
`), 2)
}

func TestLokaleActionsUndDockerSindErlaubt(t *testing.T) {
	require.Empty(t, pruefe(t, `
on: push
jobs:
  x:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/setup
      - uses: docker://alpine:3.20
      - run: echo
`))
}

func TestWiederverwendbarerWorkflowMussGepinntSein(t *testing.T) {
	require.Len(t, pruefe(t, `
on: push
jobs:
  x:
    uses: org/repo/.github/workflows/a.yml@main
`), 1)
	require.Empty(t, pruefe(t, `
on: push
jobs:
  x:
    uses: ./.github/workflows/a.yml
`))
}

func TestKaputtesYAMLIstFehler(t *testing.T) {
	_, err := CheckWorkflow("wf.yml", []byte("on: [\n"))
	require.Error(t, err)
}

func TestCheckDirLiestYmlUndYaml(t *testing.T) {
	dir := t.TempDir()
	schreibe := func(name, inhalt string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(inhalt), 0o600))
	}
	schreibe("a.yml", "on: pull_request\njobs: {x: {runs-on: [self-hosted, custom-runner], steps: [{run: echo}]}}\n")
	schreibe("b.yaml", "on: push\njobs: {x: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n")
	schreibe("notiz.md", "kein workflow")

	f, err := CheckDir(dir)
	require.NoError(t, err)
	require.Len(t, f, 2)
	require.Equal(t, filepath.Join(dir, "a.yml"), f[0].File)
	require.Equal(t, filepath.Join(dir, "b.yaml"), f[1].File)
}

func TestCheckDirFehlerBeiFehlendemOrdner(t *testing.T) {
	_, err := CheckDir(filepath.Join(t.TempDir(), "gibt-es-nicht"))
	require.Error(t, err)
}

func TestCheckDirFehlerBeiKaputterDatei(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"), []byte("on: [\n"), 0o600))
	_, err := CheckDir(dir)
	require.ErrorContains(t, err, "a.yml")
}

func TestCheckDirFehlerBeiUnlesbarerDatei(t *testing.T) {
	dir := t.TempDir()
	// A folder with a .yml name cannot be read as a file (also applies as root).
	require.NoError(t, os.Mkdir(filepath.Join(dir, "a.yml"), 0o700))
	_, err := CheckDir(dir)
	require.ErrorContains(t, err, "a.yml")
}

func TestRunOhneBefundeExit0(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
		[]byte("on: push\npermissions: {contents: read}\njobs: {x: {runs-on: ubuntu-latest, steps: [{run: echo}]}}\n"), 0o600))
	var out, errOut bytes.Buffer
	require.Equal(t, 0, Run([]string{dir}, &out, &errOut))
	require.Contains(t, out.String(), "keine Befunde")
}

func TestRunMitBefundenExit1(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
		[]byte("on: push\njobs: {x: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n"), 0o600))
	var out, errOut bytes.Buffer
	require.Equal(t, 1, Run([]string{dir}, &out, &errOut))
	require.Contains(t, errOut.String(), "a.yml: ")
	require.Contains(t, errOut.String(), "actions/checkout@v4")
}

func TestRunAufruffehlerExit2(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 2, Run(nil, &out, &errOut))
	require.Contains(t, errOut.String(), "Aufruf")
	require.Equal(t, 2, Run([]string{filepath.Join(t.TempDir(), "fehlt")}, &out, &errOut))
	require.Contains(t, errOut.String(), "Fehler")
}

// Allowlist instead of blocklist. A label without
// "self-hosted" suffices for GitHub to assign to an own runner.
func TestPullRequestJobNurAufErlaubtenRunnern(t *testing.T) {
	faelle := []struct {
		name    string
		runsOn  string
		befunde int
	}{
		{"eigenes Label als String", "custom-runner", 1},
		{"eigenes Label in Liste", "[custom-runner]", 1},
		{"Schreibweise Self-Hosted", "[Self-Hosted, custom-runner]", 1},
		{"Ausdruck nicht auflösbar", "${{ matrix.runner }}", 1},
		{"Ausdruck in Liste", "[\"${{ matrix.runner }}\"]", 1},
		{"Runner-Gruppe", "{group: prod}", 1},
		{"ubuntu", "ubuntu-latest", 0},
		{"ubuntu groß geschrieben", "Ubuntu-22.04", 0},
		{"windows", "windows-latest", 0},
		{"macos", "macos-14", 0},
		{"mehrere gehostete Labels", "[ubuntu-latest, ubuntu-22.04]", 0},
		{"gehostet mit fremdem Label", "[ubuntu-latest, custom-runner]", 1},
		{"internes CI-Label", "[self-hosted, ci-intern]", 1},
		{"internes CI-Label groß geschrieben", "[Self-Hosted, CI-INTERN]", 1},
		{"leeres Labels-Objekt", "{labels: []}", 0},
	}
	for _, c := range faelle {
		t.Run(c.name, func(t *testing.T) {
			f := pruefe(t, "on: pull_request\njobs:\n  x:\n    runs-on: "+c.runsOn+"\n    steps: [{run: echo}]\n")
			require.Len(t, f, c.befunde)
		})
	}
}

// Also events other than pull_request and
// pull_request_target execute unmerged PR code.
func TestAllePREreignisseWerdenGeprueft(t *testing.T) {
	faelle := []struct {
		name    string
		on      string
		befunde int
	}{
		{"pull_request_review als String", "pull_request_review", 1},
		{"pull_request_review_comment in Liste", "[pull_request_review_comment]", 1},
		{"merge_group als Map", "{merge_group: {}}", 1},
		{"pull_request_review mit types", "{pull_request_review: {types: [submitted]}}", 1},
		{"push", "push", 0},
		{"issues", "[issues]", 0},
		{"Ereignis mit gleichem Präfix", "pull_request_labeled", 0},
	}
	for _, c := range faelle {
		t.Run(c.name, func(t *testing.T) {
			f := pruefe(t, "on: "+c.on+"\njobs:\n  x:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n")
			require.Len(t, f, c.befunde)
		})
	}
}

// dirMit puts workflow files into a temp directory and checks it (review finding 5).
func dirMit(t *testing.T, dateien map[string]string) []Finding {
	t.Helper()
	dir := t.TempDir()
	for name, inhalt := range dateien {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(inhalt), 0o600))
	}
	f, err := CheckDir(dir)
	require.NoError(t, err)
	return f
}

const aufrufer = `
on: pull_request
jobs:
  call:
    uses: ./.github/workflows/b.yml
`

const aufgerufenEigenerRunner = `
on: workflow_call
jobs:
  deploy:
    runs-on: [self-hosted, custom-runner]
    steps: [{run: echo}]
`

func TestAufgerufenerWorkflowErbtPRAusloesung(t *testing.T) {
	f := dirMit(t, map[string]string{"a.yml": aufrufer, "b.yml": aufgerufenEigenerRunner})
	require.Len(t, f, 1)
	require.Contains(t, f[0].File, "b.yml")
	require.Contains(t, f[0].Message, "deploy")
	require.Contains(t, f[0].Message, "aufgerufen von a.yml/call")
}

func TestAufrufUeberMehrereEbenen(t *testing.T) {
	f := dirMit(t, map[string]string{
		"a.yml": aufrufer,
		"b.yml": "on: workflow_call\njobs:\n  mid:\n    uses: ./.github/workflows/c.yml\n",
		"c.yml": aufgerufenEigenerRunner,
	})
	require.Len(t, f, 1)
	require.Contains(t, f[0].File, "c.yml")
	require.Contains(t, f[0].Message, "aufgerufen von b.yml/mid")
}

func TestAufrufOhnePRIstErlaubt(t *testing.T) {
	require.Empty(t, dirMit(t, map[string]string{
		"a.yml": strings.Replace(aufrufer, "pull_request", "push", 1),
		"b.yml": aufgerufenEigenerRunner,
	}))
	require.Empty(t, dirMit(t, map[string]string{
		"a.yml": strings.Replace(aufrufer, "    uses:", "    if: github.event_name == 'push'\n    uses:", 1),
		"b.yml": aufgerufenEigenerRunner,
	}))
}

func TestAufgerufenerWorkflowAufErlaubtemRunnerIstErlaubt(t *testing.T) {
	for _, runner := range []string{"ubuntu-latest", "[ubuntu-latest, ubuntu-22.04]"} {
		require.Empty(t, dirMit(t, map[string]string{
			"a.yml": aufrufer,
			"b.yml": "on: workflow_call\njobs:\n  x:\n    runs-on: " + runner + "\n    steps: [{run: echo}]\n",
		}), runner)
	}
}

func TestAufrufZyklusEndet(t *testing.T) {
	f := dirMit(t, map[string]string{
		"a.yml": aufrufer,
		"b.yml": "on: workflow_call\njobs:\n  back:\n    uses: ./.github/workflows/a.yml\n  deploy:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n",
	})
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "deploy")
}

func TestAufrufAufFehlendeDateiIstBefund(t *testing.T) {
	f := dirMit(t, map[string]string{"a.yml": strings.Replace(aufrufer, "b.yml", "fehlt.yml", 1)})
	require.Len(t, f, 1)
	require.Contains(t, f[0].File, "a.yml")
	require.Contains(t, f[0].Message, "fehlt.yml")
}

func TestAufrufAufFehlendeDateiOhnePRIstKeinBefund(t *testing.T) {
	require.Empty(t, dirMit(t, map[string]string{"a.yml": strings.Replace(strings.Replace(aufrufer, "b.yml", "fehlt.yml", 1), "pull_request", "push", 1)}))
}

func TestAufrufMitNichtLokalemPfadWirdNichtVerfolgt(t *testing.T) {
	require.Empty(t, dirMit(t, map[string]string{
		"a.yml": "on: pull_request\njobs:\n  call:\n    uses: o/r/.github/workflows/x.yml@" + sha + "\n",
	}))
}

func TestAufrufMitYamlEndungUndMehrfachAufruf(t *testing.T) {
	f := dirMit(t, map[string]string{
		"a.yml":  "on: pull_request\njobs:\n  eins:\n    uses: ./.github/workflows/b.yaml\n  zwei:\n    uses: ./.github/workflows/b.yaml\n",
		"b.yaml": aufgerufenEigenerRunner,
	})
	require.Len(t, f, 1)
}

// The lint was deliberately probed for bypasses. Every finding
// is here as its own test; the non-findings are secured by the tests below.

// Finding 1: a YAML alias in runs-on arrived as an AliasNode and yielded no
// labels, hence "allowed". A job with a push exception anchors the runner, a
// second one without an exception takes it over via alias.
func TestRunsOnAlsAliasWirdAufgeloest(t *testing.T) {
	f := pruefe(t, `
on: [push, pull_request]
jobs:
  a:
    if: github.event_name == 'push'
    runs-on: &prod [self-hosted, custom-runner]
    steps: [{run: echo}]
  b:
    runs-on: *prod
    steps: [{run: echo}]
`)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, `"b"`)
	require.Contains(t, f[0].Message, "custom-runner")
}

// Finding 2: an alias as a list element yielded the name of the anchor instead of
// its value. An anchor named ubuntu-latest thus unlocked the allowlist.
func TestAliasNameGiltNichtAlsLabel(t *testing.T) {
	require.Len(t, pruefe(t, `
on: [push, pull_request]
jobs:
  a:
    if: github.event_name == 'push'
    runs-on: &ubuntu-latest custom-runner
    steps: [{run: echo}]
  b:
    runs-on: [*ubuntu-latest]
    steps: [{run: echo}]
`), 1)
}

// Finding 3: an alias in on: went undetected, the workflow counted as not
// PR-triggered. The same applies to aliases in the event list, aliases as
// keys and merge keys in the on: map.
func TestOnAlsAliasOderMergeKeyWirdAufgeloest(t *testing.T) {
	faelle := map[string]string{
		"on als Alias":       "name: &ev pull_request\non: *ev\n",
		"Alias in Liste":     "name: &pull_request x\nrun-name: &ev pull_request\non: [push, *ev]\n",
		"Merge-Key in on":    "env: &t {pull_request: {}}\non: {<<: *t, push: {}}\n",
		"Alias als Ereignis": "name: &ev pull_request\non: {*ev : {}}\n",
	}
	for name, kopf := range faelle {
		t.Run(name, func(t *testing.T) {
			require.Len(t, pruefe(t, kopf+"jobs:\n  x:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n"), 1)
		})
	}
}

// Finding 4: merge key in a runs-on object. Labels and runner group come
// from the anchor and were overlooked.
func TestRunsOnObjektMitMergeKey(t *testing.T) {
	require.Len(t, pruefe(t, `
on: [push, pull_request]
jobs:
  a:
    if: github.event_name == 'push'
    runs-on: &r {group: prod}
    steps: [{run: echo}]
  b:
    runs-on: {<<: *r}
    steps: [{run: echo}]
`), 1)
}

// Finding 5: unknown keys in the runs-on object (e.g. Group/Labels in
// a different spelling) were ignored; the object counted as empty and thus
// allowed. Whether GitHub accepts them is undocumented, so a violation.
func TestRunsOnObjektMitUnbekanntemSchluessel(t *testing.T) {
	for _, runsOn := range []string{"{Group: prod}", "{Labels: [self-hosted, custom-runner]}", "{labels: [ubuntu-latest], runner: custom-runner}"} {
		require.Len(t, pruefe(t, "on: pull_request\njobs:\n  x:\n    runs-on: "+runsOn+"\n    steps: [{run: echo}]\n"), 1, runsOn)
	}
}

// Finding 6: the hosted labels were only recognized by prefix. That let
// expressions like ubuntu-${{ matrix.x }} and custom labels like ubuntu-custom
// through, although GitHub can assign them to a self-hosted runner.
func TestGehosteteLabelsNurExakt(t *testing.T) {
	faelle := map[string]int{
		"ubuntu-${{ matrix.version }}": 1,
		"windows-${{ inputs.os }}":     1,
		"ubuntu-custom":                1,
		"macos-prod":                   1,
		"ubuntu-":                      1,
		"ubuntu-latest":                0,
		"ubuntu-24.04":                 0,
		"ubuntu-24.04-arm":             0,
		"ubuntu-slim":                  0,
		"windows-2025":                 0,
		"windows-11-arm":               0,
		"macos-15-intel":               0,
		"macos-14-xlarge":              0,
		"macos-latest-large":           0,
	}
	for label, befunde := range faelle {
		t.Run(label, func(t *testing.T) {
			require.Len(t, pruefe(t, "on: pull_request\njobs:\n  x:\n    runs-on: \""+label+"\"\n    steps: [{run: echo}]\n"), befunde)
		})
	}
}

// Finding 7: workflow_run and issue_comment run in the repo context and are
// triggered by PR activity (completion of the PR CI run or comment on the
// PR). They typically check out the PR state (head_sha, ChatOps) and
// therefore count as a PR event, like pull_request_target.
func TestWorkflowRunUndIssueCommentZaehlenAlsPR(t *testing.T) {
	for _, on := range []string{"workflow_run", "{workflow_run: {workflows: [CI], types: [completed]}}", "[issue_comment]"} {
		require.Len(t, pruefe(t, "on: "+on+"\njobs:\n  x:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n"), 1, on)
	}
}

// Finding 8: a local call with a different spelling of the path
// (./.GitHub/workflows/…) was neither followed nor reported. Every local
// job uses: outside ./.github/workflows/ is a finding.
func TestLokalerAufrufAusserhalbVonWorkflowsIstBefund(t *testing.T) {
	f := dirMit(t, map[string]string{
		"a.yml": strings.Replace(aufrufer, "./.github/workflows/b.yml", "./.GitHub/workflows/b.yml", 1),
		"b.yml": aufgerufenEigenerRunner,
	})
	require.Len(t, f, 1)
	require.Contains(t, f[0].File, "a.yml")
}

// Non-findings, recorded as regression tests.

func TestMapMitNichtStringSchluessel(t *testing.T) {
	// yaml.v3 decodes such maps as map[any]any.
	require.Len(t, pruefe(t, "on: {pull_request: {}, 1: x}\njobs:\n  x:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n"), 1)
	f := pruefe(t, "on: pull_request\njobs:\n  x:\n    runs-on: {1: x, labels: [ubuntu-latest]}\n    steps: [{run: echo}]\n")
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "[1 ubuntu-latest]")
	// Without on: or with a value that names no event, nothing is PR-triggered.
	for _, on := range []string{"", "on:\n", "on: 1\n"} {
		require.Empty(t, pruefe(t, on+"jobs:\n  x:\n    runs-on: custom-runner\n    steps: [{run: echo}]\n"), on)
	}
}

func TestMergeKeyImJobWirdAufgeloest(t *testing.T) {
	// yaml.v3 applies merge keys when decoding into structs.
	require.Len(t, pruefe(t, `
on: [push, pull_request]
jobs:
  a: &basis
    if: github.event_name == 'push'
    runs-on: custom-runner
    steps: [{run: echo}]
  b:
    <<: *basis
    if: "true"
`), 1)
}

func TestAliasImIfWirdAufgeloest(t *testing.T) {
	require.Len(t, pruefe(t, `
name: &nie "github.event_name == 'pushed'"
on: pull_request
jobs:
  x:
    if: *nie
    runs-on: custom-runner
    steps: [{run: echo}]
`), 1)
}

func TestMatrixUndAusdrueckeImRunsOn(t *testing.T) {
	for _, runsOn := range []string{
		"${{ matrix.os }}",
		`"${{ fromJSON('[\"self-hosted\",\"intern\"]') }}"`,
		`{group: "${{ inputs.g }}"}`,
		`{labels: "${{ matrix.l }}"}`,
	} {
		require.Len(t, pruefe(t, "on: pull_request\njobs:\n  x:\n    strategy: {matrix: {os: [ubuntu-latest]}}\n    runs-on: "+runsOn+"\n    steps: [{run: echo}]\n"), 1, runsOn)
	}
}

func TestAufgerufenerWorkflowMitAliasImRunsOn(t *testing.T) {
	f := dirMit(t, map[string]string{
		"a.yml": aufrufer,
		"b.yml": "on: workflow_call\njobs:\n  a:\n    if: github.event_name == 'push'\n    runs-on: &r custom-runner\n    steps: [{run: echo}]\n  b:\n    runs-on: *r\n    steps: [{run: echo}]\n",
	})
	require.Len(t, f, 1)
	require.Contains(t, f[0].Message, "aufgerufen von a.yml/call")
}
