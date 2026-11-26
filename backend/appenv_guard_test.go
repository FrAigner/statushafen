package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Structural guard against the fail-open pattern for development exceptions:
// comparisons such as APP_ENV != "production" open the exception for every
// other value ("prod", empty, typo). Development exceptions are queried
// therefore exclusively via database.IsDevelopmentAppEnv (allowlist).
// Otherwise only the choice of log format may read APP_ENV (no
// Sicherheitsmerkmal).
var appEnvReaders = map[string]bool{
	"database/db.go":     true, // IsDevelopmentAppEnv
	"logging/logging.go": true, // log format
}

var productionCompare = regexp.MustCompile(`(?:!=|==)\s*"production"|"production"\s*(?:!=|==)`)

func nonTestGoFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && path != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			files[filepath.ToSlash(path)] = string(raw)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

func TestAppEnv_OnlyReadInAllowedPlaces(t *testing.T) {
	for path, src := range nonTestGoFiles(t) {
		if strings.Contains(src, `"APP_ENV"`) && !appEnvReaders[path] {
			t.Errorf("%s liest APP_ENV: Entwicklungsausnahmen über database.IsDevelopmentAppEnv() prüfen", path)
		}
	}
}

func TestAppEnv_NoComparisonWithProduction(t *testing.T) {
	for path, src := range nonTestGoFiles(t) {
		if path == "logging/logging.go" {
			continue // Log format: JSON in production, not a security feature
		}
		if loc := productionCompare.FindStringIndex(src); loc != nil {
			t.Errorf("%s vergleicht mit \"production\" (%q): fail-open bei jedem anderen Wert, stattdessen Allowlist der Entwicklungsumgebungen", path, src[loc[0]:loc[1]])
		}
	}
}

// Control check: the search pattern recognizes both spellings of the pattern.
func TestAppEnv_ProductionCompareMatchesBothForms(t *testing.T) {
	for _, code := range []string{`os.Getenv("APP_ENV") != "production"`, `os.Getenv("APP_ENV") == "production"`, `"production" == env`} {
		assert.True(t, productionCompare.MatchString(code), code)
	}
	assert.False(t, productionCompare.MatchString(`env := "production"`))
}
