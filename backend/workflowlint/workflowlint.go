// Package workflowlint checks the GitHub Actions workflows against two rules:
//
//  1. No job that a pull request can trigger runs on a
//     self-hosted runner. Only GitHub-hosted runners are allowed. Otherwise
//     code from a foreign PR runs on an own machine (with
//     a public repository anyone can open a PR).
//  2. Every third-party action and every third-party reusable workflow is pinned
//     by full commit SHA, not by tag or branch.
//
// In addition there are the hardening rules in haertung.go: explicit permissions and
// persist-credentials: false on every checkout.
//
// A PR can disable this check in its own branch (pull_request uses
// the PR's workflow files). It therefore prevents mistakes, not an attack
// with write permissions; the only defense against that is that the agent tokens have no
// workflow permission.
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

// Finding is a violation in a workflow file.
type Finding struct {
	File    string
	Message string
}

var (
	pinned   = regexp.MustCompile(`@[0-9a-f]{40}$`)
	gehostet = regexp.MustCompile(`^(ubuntu|windows|macos)-(latest|slim|\d+(\.\d+)?)(-(arm|intel|large|xlarge))?$`)
	// A link of an &&-chain that is always false on PR events (e.g. a deploy job only for push).
	nurPush  = regexp.MustCompile(`^github\.event_name\s*==\s*['"]push['"]$`)
	ausdruck = regexp.MustCompile(`^\$\{\{(.*)\}\}$`)
	// Argument-less status functions are harmless links of an &&-chain.
	statusFunktion = regexp.MustCompile(`\b(success|always|failure|cancelled)\(\)`)
)

// On and RunsOn are any instead of yaml.Node: when decoding into
// any, yaml.v3 resolves aliases and merge keys. A yaml.Node kept them raw,
// so runs-on: *anchor counted as empty (= allowed) and an alias element yielded
// the anchor name instead of the value (anchor &ubuntu-latest → "ubuntu-latest").
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

// CheckWorkflow checks a workflow file; name appears in the findings.
func CheckWorkflow(name string, data []byte) ([]Finding, error) {
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return pruefeWorkflow(name, wf, triggeredByPR(wf.On), ""), nil
}

// pruefeWorkflow checks a parsed workflow. pr says whether a PR triggers it;
// aufrufer names, for reusable workflows, the PR job that includes it.
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

// prEvents are all events that execute unmerged PR code:
// pull_request_review(_comment) run on refs/pull/<N>/merge, merge_group
// on the state of the merge queue. pull_request_target, workflow_run and
// issue_comment do run in the repo context, but are triggered by PR activity
// (PR CI run finished, comment on the PR) and typically check
// out the PR state.
var prEvents = []string{"pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment", "merge_group", "workflow_run", "issue_comment"}

// triggeredByPR detects the prEvents in all three notations of "on":
// string, list or map.
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

// runnerLabels reads runs-on as a string, list or {group: …, labels: …}.
// A runner group contains only self-hosted runners, even without the label;
// it is counted as "self-hosted" and rejected by the allowlist. Other
// keys (e.g. Group/Labels in a different spelling) themselves count as a
// foreign label, instead of being silently ignored.
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

// alsMap turns a decoded YAML map into a map[string]any, for everything
// else nil. yaml.v3 decodes a map with a non-string key
// (on: {pull_request: …, 1: x}) as map[any]any.
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

// mapSchluessel returns the keys of alsMap(v) sorted.
func mapSchluessel(v any) []string {
	var keys []string
	for k := range alsMap(v) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// erlaubterPRRunner is an allowlist: only label sets consisting
// exclusively of GitHub-hosted labels are allowed. Everything else (custom labels
// like "custom-runner", runner groups, unresolvable expressions) counts as a
// violation, because GitHub assigns a job to a runner with matching labels
// even without the self-hosted label. GitHub compares labels case-insensitively.
// Without runs-on (reusable workflow)
// the list is empty and therefore allowed.
func erlaubterPRRunner(labels []string) bool {
	hosted := true
	for _, l := range labels {
		if !githubGehostet(strings.ToLower(l)) {
			hosted = false
		}
	}
	return hosted
}

// githubGehostet recognizes the labels of the GitHub-hosted standard runners
// (ubuntu-latest, ubuntu-24.04-arm, windows-2025, macos-15-intel, …) exactly.
// A plain prefix comparison let ubuntu-${{ matrix.x }} and custom labels
// like ubuntu-custom through. Larger runners with freely chosen
// names deliberately fall through and would have to be added here.
func githubGehostet(label string) bool {
	return gehostet.MatchString(label)
}

// nieBeiPR detects an if: that is certainly false on PR events: a pure
// &&-chain of which one link is exactly github.event_name == 'push'.
// With || the chain is no longer certainly false; negation or parentheses
// anywhere in the condition (finding 4) → no exception, because then the split at
// && is no longer correct. When in doubt, the job is checked.
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

// lokalerWorkflow is the prefix of uses: for reusable workflows in the same repo.
const lokalerWorkflow = "./.github/workflows/"

type datei struct {
	pfad string
	wf   workflow
}

// CheckDir checks all *.yml/*.yaml in dir. A reusable workflow that
// a PR job includes via uses: ./.github/workflows/<file> (even across several
// levels) runs in the context of the PR and itself counts as PR-triggered.
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

	// aufrufer: file → PR job through which it is reached ("" = directly PR-triggered).
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
			// Every local call (./…) counts; one outside
			// ./.github/workflows/ (e.g. ./.GitHub/…) would otherwise remain unchecked.
			// After TrimPrefix it keeps its "./…" and matches no file name.
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

// Run is the CLI (cmd/workflowlint): 0 = clean, 1 = findings, 2 = invocation
// or read error. The logic lives here instead of in package main because gremlins
// assigns mutants in cmd/ to the wrong package (see CONTRIBUTING.md).
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
