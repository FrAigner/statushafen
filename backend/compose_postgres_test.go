package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// From PostgreSQL 18 on, the official image keeps its data in a version-specific directory below
// /var/lib/postgresql (PGDATA=/var/lib/postgresql/18/docker) and refuses to start when a volume is
// mounted at the old /var/lib/postgresql/data. The db volume therefore has to follow the image's
// major version.
func TestCompose_PostgresVolumeMatchesImageLayout(t *testing.T) {
	raw, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatalf("docker-compose.yml not readable: %v", err)
	}
	var doc struct {
		Services map[string]struct {
			Image   string   `yaml:"image"`
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("docker-compose.yml not parsable: %v", err)
	}
	db, ok := doc.Services["db"]
	if !ok {
		t.Fatal("no db service")
	}
	m := regexp.MustCompile(`^postgres:(\d+)`).FindStringSubmatch(db.Image)
	if m == nil {
		t.Fatalf("db image %q has no major version tag", db.Image)
	}
	major, _ := strconv.Atoi(m[1])
	want := "/var/lib/postgresql/data"
	if major >= 18 {
		want = "/var/lib/postgresql"
	}
	for _, v := range db.Volumes {
		if parts := strings.Split(v, ":"); len(parts) >= 2 && parts[0] == "postgres_data" {
			if parts[1] != want {
				t.Errorf("postgres:%d needs the data volume at %s, compose mounts it at %s", major, want, parts[1])
			}
			return
		}
	}
	t.Error("db service mounts no postgres_data volume")
}
