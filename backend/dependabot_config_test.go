package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Dependabot soll jedes Paketmanifest des Repos aktuell halten, nicht nur
// Pakete mit bekannter Schwachstelle. Der Test findet die Manifeste selbst; ein
// neues Manifest ohne Eintrag in .github/dependabot.yml macht ihn rot.
type dependabotUpdate struct {
	Ecosystem string `yaml:"package-ecosystem"`
	Directory string `yaml:"directory"`
	Schedule  struct {
		Interval string `yaml:"interval"`
	} `yaml:"schedule"`
	Limit  int            `yaml:"open-pull-requests-limit"`
	Groups map[string]any `yaml:"groups"`
}

type dependabotConfig struct {
	Version int                `yaml:"version"`
	Updates []dependabotUpdate `yaml:"updates"`
}

func loadDependabotConfig(t *testing.T) dependabotConfig {
	t.Helper()
	raw, err := os.ReadFile("../.github/dependabot.yml")
	require.NoError(t, err, ".github/dependabot.yml fehlt")
	var cfg dependabotConfig
	require.NoError(t, yaml.Unmarshal(raw, &cfg))
	return cfg
}

// manifestEcosystems ordnet Dateinamen dem Dependabot-Ökosystem zu.
func manifestEcosystem(name string) string {
	switch {
	case name == "go.mod":
		return "gomod"
	case name == "package.json":
		return "npm"
	case name == "Dockerfile":
		return "docker"
	case strings.HasPrefix(name, "docker-compose") && strings.HasSuffix(name, ".yml"):
		return "docker-compose"
	}
	return ""
}

func TestDependabot_EveryManifestHasAnUpdateEntry(t *testing.T) {
	cfg := loadDependabotConfig(t)
	require.Equal(t, 2, cfg.Version)

	covered := map[string]bool{} // "ökosystem|verzeichnis"
	for _, u := range cfg.Updates {
		covered[u.Ecosystem+"|"+u.Directory] = true
	}

	found := 0
	require.NoError(t, filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || strings.HasPrefix(d.Name(), "security-") || d.Name() == ".claude") {
			return filepath.SkipDir
		}
		eco := manifestEcosystem(d.Name())
		if d.IsDir() || eco == "" {
			return nil
		}
		found++
		rel, relErr := filepath.Rel("..", filepath.Dir(p))
		require.NoError(t, relErr)
		dir := "/" + path.Clean(filepath.ToSlash(rel))
		if dir == "/." {
			dir = "/"
		}
		assert.True(t, covered[eco+"|"+dir], "%s (%s): kein Eintrag in dependabot.yml", p, eco)
		return nil
	}))
	assert.GreaterOrEqual(t, found, 4, "go.mod, package.json, Dockerfile und docker-compose.yml erwartet")

	// GitHub Actions liegen unter .github/workflows und brauchen "/".
	assert.True(t, covered["github-actions|/"], "github-actions fehlt")
}

func TestDependabot_EntriesAreWeeklyGroupedAndPointToExistingDirectories(t *testing.T) {
	cfg := loadDependabotConfig(t)
	require.NotEmpty(t, cfg.Updates)
	seen := map[string]bool{}
	for _, u := range cfg.Updates {
		key := u.Ecosystem + "|" + u.Directory
		assert.False(t, seen[key], "doppelter Eintrag %s", key)
		seen[key] = true

		assert.Equal(t, "weekly", u.Schedule.Interval, "%s: wöchentlich, damit keine täglichen PRs entstehen", key)
		assert.True(t, u.Limit > 0 && u.Limit <= 10, "%s: open-pull-requests-limit zwischen 1 und 10", key)
		assert.NotEmpty(t, u.Groups, "%s: kleine Updates gruppieren", key)

		info, err := os.Stat(filepath.Join("..", filepath.FromSlash(u.Directory)))
		if assert.NoError(t, err, "%s: Verzeichnis fehlt", key) {
			assert.True(t, info.IsDir(), key)
		}
	}
	ecosystems := []string{"gomod", "npm", "docker", "docker-compose", "github-actions"}
	for _, e := range ecosystems {
		assert.True(t, slices.ContainsFunc(cfg.Updates, func(u dependabotUpdate) bool {
			return u.Ecosystem == e
		}), "Ökosystem %s fehlt", e)
	}
}
