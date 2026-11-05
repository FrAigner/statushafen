package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ableitung und Prüfung der Komponenten-Kennung.
func TestComponentSlugBase(t *testing.T) {
	cases := map[string]string{
		"Mail Server":         "mail-server",
		"  API  Gateway (DE)": "api-gateway-de",
		"Größe Übung Ärger":   "groesse-uebung-aerger",
		"Öl & Gas":            "oel-gas",
		"---":                 "komponente",
		"":                    "komponente",
		"日本語":                 "komponente",
		"a_b.c":               "a-b-c",
	}
	for in, want := range cases {
		assert.Equal(t, want, ComponentSlugBase(in), in)
	}
	long := ComponentSlugBase(strings.Repeat("a", 59) + " bbb")
	assert.Equal(t, strings.Repeat("a", 59), long, "Kürzen darf keinen Bindestrich am Ende lassen")
}

func TestComponentSlugWithSuffix(t *testing.T) {
	assert.Equal(t, "x", ComponentSlugWithSuffix("x", 0))
	assert.Equal(t, "x", ComponentSlugWithSuffix("x", 1))
	assert.Equal(t, "x-2", ComponentSlugWithSuffix("x", 2))
	assert.Equal(t, "x-10", ComponentSlugWithSuffix("x", 10))
	full := strings.Repeat("a", 60)
	got := ComponentSlugWithSuffix(full, 12)
	assert.Equal(t, strings.Repeat("a", 57)+"-12", got)
	assert.Len(t, got, 60)
}

func TestValidComponentSlug(t *testing.T) {
	for _, ok := range []string{"a", "a1", "mail-server", "a-b-c", strings.Repeat("a", 60)} {
		assert.True(t, ValidComponentSlug(ok), ok)
	}
	for _, bad := range []string{"", "A", "a b", "-a", "a-", "a--b", "ä", "a_b", strings.Repeat("a", 61)} {
		assert.False(t, ValidComponentSlug(bad), bad)
	}
}
