package workflowlint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Härtungsregeln für das GITHUB_TOKEN und die Zugangsdaten im Arbeitsverzeichnis:
//
//  3. Jeder Job hat ausdrücklich gesetzte permissions, im Workflow oder im Job.
//     Ohne Angabe gelten die Standardrechte des Repositorys, oft mit
//     Schreibzugriff. "write-all" ist nie erlaubt.
//  4. Jeder actions/checkout-Schritt setzt persist-credentials: false. Sonst
//     bleibt das Token in .git/config, wo jeder spätere Schritt und jeder Code
//     des Repositorys es lesen kann. Wer später pushen muss, übergibt das Token
//     in genau diesem Schritt.

type haertungWorkflow struct {
	Permissions any                    `yaml:"permissions"`
	Jobs        map[string]haertungJob `yaml:"jobs"`
}

type haertungJob struct {
	Permissions any `yaml:"permissions"`
	Steps       []struct {
		Uses string         `yaml:"uses"`
		With map[string]any `yaml:"with"`
	} `yaml:"steps"`
}

// CheckHardening prüft eine Workflow-Datei auf die Härtungsregeln; name erscheint
// in den Befunden.
func CheckHardening(name string, data []byte) ([]Finding, error) {
	var wf haertungWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return pruefeHaertung(name, wf), nil
}

func pruefeHaertung(name string, wf haertungWorkflow) []Finding {
	var findings []Finding
	add := func(format string, args ...any) {
		findings = append(findings, Finding{File: name, Message: fmt.Sprintf(format, args...)})
	}
	if istWriteAll(wf.Permissions) {
		add("permissions: write-all im Workflow ist nicht erlaubt (nur die nötigen Rechte einzeln nennen)")
	}
	ids := make([]string, 0, len(wf.Jobs))
	for id := range wf.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		j := wf.Jobs[id]
		if wf.Permissions == nil && j.Permissions == nil {
			add("Job %q hat keine permissions (weder im Workflow noch im Job): das Token bekäme die Standardrechte des Repositorys", id)
		}
		if istWriteAll(j.Permissions) {
			add("Job %q: permissions: write-all ist nicht erlaubt (nur die nötigen Rechte einzeln nennen)", id)
		}
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Uses, "actions/checkout@") && fmt.Sprint(s.With["persist-credentials"]) != "false" {
				add("Job %q: actions/checkout ohne persist-credentials: false (das Token bliebe in .git/config)", id)
			}
		}
	}
	return findings
}

func istWriteAll(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == "write-all"
}

// CheckHardeningDir prüft alle *.yml/*.yaml in dir auf die Härtungsregeln.
func CheckHardeningDir(dir string) ([]Finding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var findings []Finding
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
		f, err := CheckHardening(path, data)
		if err != nil {
			return nil, err
		}
		findings = append(findings, f...)
	}
	return findings, nil
}
