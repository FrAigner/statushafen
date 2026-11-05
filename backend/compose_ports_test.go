package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Die Dienste der Entwicklungsumgebung (Datenbank mit Standardpasswort, Mailfänger
// ohne Anmeldung) dürfen nur vom eigenen Rechner erreichbar sein. Eine
// Port-Zuordnung ohne Host-Adresse ("5432:5432") bindet an alle Schnittstellen,
// also auch ins lokale Netz.
func TestCompose_PortsAreBoundToLoopback(t *testing.T) {
	raw, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatalf("docker-compose.yml nicht lesbar: %v", err)
	}
	var doc struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("docker-compose.yml nicht lesbar: %v", err)
	}
	if len(doc.Services) == 0 {
		t.Fatal("keine Dienste gefunden")
	}
	total := 0
	for name, svc := range doc.Services {
		for _, p := range svc.Ports {
			total++
			if !strings.HasPrefix(p, "127.0.0.1:") && !strings.HasPrefix(p, "[::1]:") {
				t.Errorf("Dienst %s: Port %q ohne Bindung an 127.0.0.1", name, p)
			}
		}
	}
	for _, name := range []string{"db", "mailpit"} {
		if len(doc.Services[name].Ports) == 0 {
			t.Errorf("Dienst %s hat keine Ports mehr (Test prüft sonst nichts)", name)
		}
	}
	if total == 0 {
		t.Fatal("keine Port-Zuordnungen gefunden")
	}
}
