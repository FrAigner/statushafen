// Package workflowlint prüft die GitHub-Actions-Workflows auf zwei Regeln:
//
//  1. Kein Job, den ein Pull Request auslösen kann, läuft auf einem
//     self-hosted Runner. Erlaubt sind nur GitHub-gehostete Runner. Sonst
//     führt Code aus einem fremden PR auf einer eigenen Maschine aus (bei
//     einem öffentlichen Repository kann jeder einen PR stellen).
//  2. Jede fremde Action und jeder fremde wiederverwendbare Workflow ist per
//     vollständigem Commit-SHA gepinnt, nicht per Tag oder Branch.
//
// Dazu kommen die Härtungsregeln in haertung.go: ausdrückliche permissions und
// persist-credentials: false bei jedem Checkout.
//
// Ein PR kann diese Prüfung im eigenen Branch abschalten (pull_request nutzt
// die Workflow-Dateien des PRs). Sie verhindert also Versehen, keinen Angriff
// mit Schreibrechten; dagegen hilft nur, dass die Agent-Tokens keine
// workflow-Berechtigung haben.
package workflowlint

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding ist ein Verstoß in einer Workflow-Datei.
type Finding struct {
	File    string
	Message string
}

var (
	pinned   = regexp.MustCompile(`@[0-9a-f]{40}$`)
	gehostet = regexp.MustCompile(`^(ubuntu|windows|macos)-(latest|slim|\d+(\.\d+)?)(-(arm|intel|large|xlarge))?$`)
	// Ein Glied einer &&-Kette, das bei PR-Ereignissen immer falsch ist (z. B. ein Deploy-Job nur für push).
	nurPush  = regexp.MustCompile(`^github\.event_name\s*==\s*['"]push['"]$`)
	ausdruck = regexp.MustCompile(`^\$\{\{(.*)\}\}$`)
	// Argumentlose Statusfunktionen sind harmlose Glieder einer &&-Kette.
	statusFunktion = regexp.MustCompile(`\b(success|always|failure|cancelled)\(\)`)
)

// On und RunsOn sind any statt yaml.Node: Beim Dekodieren nach
// any löst yaml.v3 Aliase und Merge-Keys auf. Ein yaml.Node behielt sie roh,
// sodass runs-on: *anker als leer (= erlaubt) galt und ein Alias-Element den
// Ankernamen statt des Werts lieferte (Anker &ubuntu-latest → "ubuntu-latest").
type workflow struct {
	On   any            `yaml:"on"`
	Jobs map[string]job `yaml:"jobs"`
}

type job struct {
	If     string `yaml:"if"`
	RunsOn any    `yaml:"runs-on"`
	Uses   string `yaml:"uses"`
	Steps  []struct {
		Uses string `yaml:"uses"`
	} `yaml:"steps"`
}

// CheckWorkflow prüft eine Workflow-Datei; name erscheint in den Befunden.
func CheckWorkflow(name string, data []byte) ([]Finding, error) {
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return pruefeWorkflow(name, wf, triggeredByPR(wf.On), ""), nil
}

// pruefeWorkflow prüft einen geparsten Workflow. pr sagt, ob ein PR ihn auslöst;
// aufrufer nennt bei wiederverwendbaren Workflows den PR-Job, der ihn einbindet.
func pruefeWorkflow(name string, wf workflow, pr bool, aufrufer string) []Finding {
	var findings []Finding
	add := func(format string, args ...any) {
		findings = append(findings, Finding{File: name, Message: fmt.Sprintf(format, args...)})
	}
	for _, id := range jobIDs(wf) {
		j := wf.Jobs[id]
		if pr && !nieBeiPR(j.If) {
			labels := runnerLabels(j.RunsOn)
			if !erlaubterPRRunner(labels) {
				von := ""
				if aufrufer != "" {
					von = " (aufgerufen von " + aufrufer + ")"
				}
				add("Job %q läuft bei PR-Ereignissen auf self-hosted Runner %v (erlaubt sind nur GitHub-gehostete Runner)%s", id, labels, von)
			}
		}
		if j.Uses != "" && !pinnedOrLocal(j.Uses) {
			add("Job %q: wiederverwendbarer Workflow %s ist nicht per Commit-SHA gepinnt", id, j.Uses)
		}
		for _, s := range j.Steps {
			if s.Uses != "" && !pinnedOrLocal(s.Uses) {
				add("Job %q: Action %s ist nicht per Commit-SHA gepinnt", id, s.Uses)
			}
		}
	}
	return findings
}

func jobIDs(wf workflow) []string {
	ids := make([]string, 0, len(wf.Jobs))
	for id := range wf.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// prEvents sind alle Ereignisse, die ungemergten PR-Code ausführen:
// pull_request_review(_comment) laufen auf refs/pull/<N>/merge, merge_group
// auf dem Stand der Merge-Queue. pull_request_target, workflow_run und
// issue_comment laufen zwar im Repo-Kontext, werden aber durch PR-Aktivität
// ausgelöst (PR-CI-Lauf beendet, Kommentar am PR) und checken typischerweise
// den PR-Stand aus.
var prEvents = []string{"pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment", "merge_group", "workflow_run", "issue_comment"}

// triggeredByPR erkennt die prEvents in allen drei Schreibweisen von "on":
// String, Liste oder Map.
func triggeredByPR(on any) bool {
	var events []string
	switch v := on.(type) {
	case string:
		events = []string{v}
	case []any:
		for _, e := range v {
			events = append(events, fmt.Sprint(e))
		}
	default:
		events = mapSchluessel(v)
	}
	for _, e := range prEvents {
		if contains(events, e) {
			return true
		}
	}
	return false
}

// runnerLabels liest runs-on als String, Liste oder {group: …, labels: …}.
// Eine Runner-Gruppe enthält nur self-hosted Runner, auch ohne das Label;
// sie wird als "self-hosted" gezählt und von der Allowlist abgelehnt. Andere
// Schlüssel (etwa Group/Labels in anderer Schreibweise) zählen selbst als
// fremdes Label, statt still ignoriert zu werden.
func runnerLabels(v any) []string {
	var out []string
	switch r := v.(type) {
	case nil:
	case []any:
		for _, e := range r {
			out = append(out, fmt.Sprint(e))
		}
	case map[string]any, map[any]any:
		m := alsMap(r)
		for _, k := range mapSchluessel(m) {
			switch k {
			case "labels":
				out = append(out, runnerLabels(m[k])...)
			case "group":
				out = append(out, "self-hosted")
			default:
				out = append(out, k)
			}
		}
	default:
		out = []string{fmt.Sprint(r)}
	}
	return out
}

// alsMap macht aus einer dekodierten YAML-Map eine map[string]any, für alles
// andere nil. yaml.v3 dekodiert eine Map mit einem Nicht-String-Schlüssel
// (on: {pull_request: …, 1: x}) als map[any]any.
func alsMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, w := range m {
			out[fmt.Sprint(k)] = w
		}
		return out
	}
	return nil
}

// mapSchluessel liefert die Schlüssel von alsMap(v) sortiert.
func mapSchluessel(v any) []string {
	var keys []string
	for k := range alsMap(v) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// erlaubterPRRunner ist eine Allowlist: Erlaubt sind nur Labelsätze aus
// ausschließlich GitHub-gehosteten Labels. Alles andere (eigene Labels
// wie "custom-runner", Runner-Gruppen, nicht auflösbare Ausdrücke) zählt als
// Verstoß, denn GitHub ordnet einen Job auch ohne das Label self-hosted einem
// Runner mit passenden Labels zu. GitHub vergleicht Labels ohne Beachtung
// der Groß- und Kleinschreibung. Ohne runs-on (wiederverwendbarer Workflow)
// ist die Liste leer und damit erlaubt.
func erlaubterPRRunner(labels []string) bool {
	hosted := true
	for _, l := range labels {
		if !githubGehostet(strings.ToLower(l)) {
			hosted = false
		}
	}
	return hosted
}

// githubGehostet erkennt die Labels der GitHub-gehosteten Standard-Runner
// (ubuntu-latest, ubuntu-24.04-arm, windows-2025, macos-15-intel, …) exakt.
// Ein reiner Präfix-Vergleich ließ ubuntu-${{ matrix.x }} und eigene Labels
// wie ubuntu-custom durch. Larger Runner mit frei gewählten
// Namen fallen bewusst durch und müssten hier ergänzt werden.
func githubGehostet(label string) bool {
	return gehostet.MatchString(label)
}

// nieBeiPR erkennt ein if:, das bei PR-Ereignissen sicher falsch ist: eine reine
// &&-Kette, von der ein Glied genau github.event_name == 'push' ist.
// Mit || ist die Kette nicht mehr sicher falsch; Negation oder Klammern
// irgendwo in der Bedingung (Befund 4) → keine Ausnahme, denn dann ist der Split an
// && nicht mehr korrekt. Im Zweifel wird der Job geprüft.
func nieBeiPR(bedingung string) bool {
	b := strings.TrimSpace(bedingung)
	if m := ausdruck.FindStringSubmatch(b); m != nil {
		b = m[1]
	}
	if strings.Contains(b, "||") {
		return false
	}
	if strings.ContainsAny(statusFunktion.ReplaceAllString(b, "x"), "!()") {
		return false
	}
	for _, glied := range strings.Split(b, "&&") {
		if nurPush.MatchString(strings.TrimSpace(glied)) {
			return true
		}
	}
	return false
}

func pinnedOrLocal(uses string) bool {
	return strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "docker://") || pinned.MatchString(uses)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// lokalerWorkflow ist das Präfix von uses: bei wiederverwendbaren Workflows im selben Repo.
const lokalerWorkflow = "./.github/workflows/"

type datei struct {
	pfad string
	wf   workflow
}

// CheckDir prüft alle *.yml/*.yaml in dir. Ein wiederverwendbarer Workflow, den
// ein PR-Job per uses: ./.github/workflows/<datei> einbindet (auch über mehrere
// Ebenen), läuft im Kontext des PRs und zählt selbst als PR-ausgelöst.
func CheckDir(dir string) ([]Finding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	dateien := map[string]datei{}
	var namen []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var wf workflow
		if err := yaml.Unmarshal(data, &wf); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		dateien[e.Name()] = datei{pfad: path, wf: wf}
		namen = append(namen, e.Name())
	}
	sort.Strings(namen)

	// aufrufer: Datei → PR-Job, über den sie erreicht wird ("" = direkt PR-ausgelöst).
	aufrufer := map[string]string{}
	var queue []string
	for _, n := range namen {
		if triggeredByPR(dateien[n].wf.On) {
			aufrufer[n] = ""
			queue = append(queue, n)
		}
	}
	var findings []Finding
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		d := dateien[n]
		for _, id := range jobIDs(d.wf) {
			j := d.wf.Jobs[id]
			// Jeder lokale Aufruf (./…) zählt; einer außerhalb von
			// ./.github/workflows/ (z. B. ./.GitHub/…) blieb sonst ungeprüft.
			// Er behält nach TrimPrefix sein "./…" und passt auf keinen Dateinamen.
			if !strings.HasPrefix(j.Uses, "./") || nieBeiPR(j.If) {
				continue
			}
			ziel := strings.TrimPrefix(j.Uses, lokalerWorkflow)
			if _, ok := dateien[ziel]; !ok {
				findings = append(findings, Finding{File: d.pfad, Message: fmt.Sprintf("Job %q: wiederverwendbarer Workflow %s existiert nicht und kann nicht geprüft werden", id, j.Uses)})
				continue
			}
			if _, gesehen := aufrufer[ziel]; !gesehen {
				aufrufer[ziel] = n + "/" + id
				queue = append(queue, ziel)
			}
		}
	}
	for _, n := range namen {
		von, pr := aufrufer[n]
		findings = append(findings, pruefeWorkflow(dateien[n].pfad, dateien[n].wf, pr, von)...)
	}
	return findings, nil
}

// Run ist die CLI (cmd/workflowlint): 0 = sauber, 1 = Befunde, 2 = Aufruf-
// oder Lesefehler. Die Logik steht hier statt in package main, weil gremlins
// Mutanten in cmd/ dem falschen Paket zuordnet (siehe CONTRIBUTING.md).
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "Aufruf: workflowlint <workflow-ordner>")
		return 2
	}
	findings, err := CheckDir(args[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Fehler:", err)
		return 2
	}
	haertung, err := CheckHardeningDir(args[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Fehler:", err)
		return 2
	}
	findings = append(findings, haertung...)
	for _, f := range findings {
		_, _ = fmt.Fprintf(stderr, "%s: %s\n", f.File, f.Message)
	}
	if len(findings) > 0 {
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "workflowlint: keine Befunde")
	return 0
}
