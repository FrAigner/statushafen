package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// GitHub Actions minutes are limited. Draft pull requests therefore run no CI at all (CI runs once a
// PR is marked ready, and again on every later push), superseded PR runs are cancelled, and the
// nightly SSO end-to-end test only runs when main changed since the previous night.

type workflow struct {
	On          map[string]any `yaml:"on"`
	Concurrency map[string]any `yaml:"concurrency"`
	Jobs        map[string]struct {
		If    string `yaml:"if"`
		Needs any    `yaml:"needs"`
	} `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	raw, err := os.ReadFile("../.github/workflows/" + name)
	if err != nil {
		t.Fatalf("%s not readable: %v", name, err)
	}
	var wf workflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("%s not parsable: %v", name, err)
	}
	return wf
}

func TestWorkflows_SkipDraftPullRequests(t *testing.T) {
	for _, name := range []string{"ci.yml", "code-quality.yml"} {
		wf := readWorkflow(t, name)
		pr, ok := wf.On["pull_request"].(map[string]any)
		if !ok {
			t.Errorf("%s: pull_request trigger needs explicit types", name)
			continue
		}
		types, _ := pr["types"].([]any)
		var got []string
		for _, ty := range types {
			got = append(got, ty.(string))
		}
		if !strings.Contains(strings.Join(got, ","), "ready_for_review") {
			t.Errorf("%s: pull_request types %v lack ready_for_review (a draft marked ready must run CI)", name, got)
		}
		for job, j := range wf.Jobs {
			if !strings.Contains(j.If, "github.event.pull_request.draft") {
				t.Errorf("%s: job %s runs on draft pull requests (if: %q)", name, job, j.If)
			}
		}
		if wf.Concurrency["cancel-in-progress"] == nil {
			t.Errorf("%s: superseded pull request runs are not cancelled", name)
		}
	}
}

func TestE2ESSO_RunsOnlyAfterChangesOnMain(t *testing.T) {
	wf := readWorkflow(t, "e2e-sso-nachts.yml")
	sso, ok := wf.Jobs["sso"]
	if !ok {
		t.Fatal("job sso missing")
	}
	if sso.Needs != "changes" || !strings.Contains(sso.If, "needs.changes.outputs.changed") {
		t.Errorf("job sso must depend on the changes job (needs: %v, if: %q)", sso.Needs, sso.If)
	}
	if _, ok := wf.Jobs["changes"]; !ok {
		t.Error("job changes missing")
	}
}
