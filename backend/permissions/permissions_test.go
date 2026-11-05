package permissions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// create/update/delete schließen read ein, aber nicht umgekehrt
// und nicht über Ressourcengrenzen hinweg.
func TestHas_Implikation(t *testing.T) {
	tests := []struct {
		name, list, required string
		want                 bool
	}{
		{"exaktes Recht", "components:create", "components:create", true},
		{"create impliziert read", "components:create", "components:read", true},
		{"update impliziert read", "components:update", "components:read", true},
		{"delete impliziert read", "components:delete", "components:read", true},
		{"read impliziert kein update", "components:read", "components:update", false},
		{"create impliziert kein update", "components:create", "components:update", false},
		{"update impliziert kein delete", "components:update", "components:delete", false},
		{"andere Ressource", "components:update", "incidents:read", false},
		{"Leerzeichen", " components:read , incidents:read", "incidents:read", true},
		{"leere Liste", "", "components:read", false},
		{"verlangtes Recht ohne Aktion", "components:read", "components", false},
		{"Sonderrecht", "admin_dashboard", AdminDashboard, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Has(tc.list, tc.required))
		})
	}
}

// Kompatibilitätsschicht: ein Alt-Eintrag "res:write" zählt als
// create+read+update+delete derselben Ressource, aber nur dort.
func TestHas_AltwertWriteGiltAlsCrud(t *testing.T) {
	for _, act := range Actions {
		assert.True(t, Has("components:write", Perm("components", act)), act)
		assert.False(t, Has("components:write", Perm("incidents", act)), act)
	}
	assert.True(t, Has("components:write", "components:write"))
}

func TestHasAny(t *testing.T) {
	assert.True(t, HasAny("incidents:update", []string{"components:read", "incidents:read"}))
	assert.False(t, HasAny("notices:read", []string{"components:read", "incidents:read"}))
	assert.False(t, HasAny("notices:read", nil))
}

func TestCanChange(t *testing.T) {
	assert.False(t, CanChange("components:read,incidents:read,admin_dashboard"))
	assert.True(t, CanChange("components:read,notices:create"))
	assert.True(t, CanChange("notices:update"))
	assert.True(t, CanChange("notices:delete"))
	assert.True(t, CanChange("notices:write"), "Altwert gilt als verändernd")
	assert.False(t, CanChange(""))
}

func TestNormalize(t *testing.T) {
	assert.Equal(t, "notices:create,notices:read,notices:update,notices:delete",
		Normalize("notices:write"))
	assert.Equal(t, "notices:create,notices:read", Normalize("notices:create"))
	assert.Equal(t, "admin_dashboard,unbekannt", Normalize("admin_dashboard, unbekannt"))
	assert.Equal(t, "notices:read", Normalize("notices:read,notices:read"))
	assert.Equal(t, Normalize(Normalize("a:write,b:update")), Normalize("a:write,b:update"), "idempotent")
}

func TestMigrateLegacy_KopplungEinsZuEins(t *testing.T) {
	has := func(list, p string) bool {
		for _, e := range strings.Split(list, ",") {
			if e == p {
				return true
			}
		}
		return false
	}
	t.Run("components:write bringt external_sources:* wie bisher", func(t *testing.T) {
		out := MigrateLegacy("components:write")
		for _, res := range []string{"components", "external_sources"} {
			for _, act := range Actions {
				assert.True(t, has(out, Perm(res, act)), res+":"+act)
			}
		}
		assert.False(t, has(out, "incidents:read"))
		assert.False(t, HasLegacyWrite(out))
	})
	t.Run("components:read bringt external_sources:read, nicht mehr", func(t *testing.T) {
		out := MigrateLegacy("components:read")
		assert.True(t, has(out, "external_sources:read"))
		assert.False(t, Has(out, "external_sources:update"))
		assert.False(t, Has(out, "components:update"))
	})
	t.Run("incidents:write bringt Wartung und Vorlagen", func(t *testing.T) {
		out := MigrateLegacy("incidents:write")
		for _, res := range []string{"incidents", "maintenance", "incident_templates"} {
			for _, act := range Actions {
				assert.True(t, Has(out, Perm(res, act)), res+":"+act)
			}
		}
	})
	t.Run("incidents:read liest Wartung, aber keine Vorlagen", func(t *testing.T) {
		out := MigrateLegacy("incidents:read")
		assert.True(t, Has(out, "maintenance:read"))
		assert.False(t, Has(out, "incident_templates:read"), "Vorlagen verlangten schon fürs Lesen write")
	})
	t.Run("pages:write bringt Inhalte-Tabs und SSO", func(t *testing.T) {
		out := MigrateLegacy("pages:write")
		for _, res := range []string{"pages", "notification_templates", "languages", "ui_translations", "sso"} {
			for _, act := range Actions {
				assert.True(t, has(out, Perm(res, act)), res+":"+act)
			}
		}
	})
	t.Run("pages:read bleibt nur pages:read", func(t *testing.T) {
		assert.Equal(t, "pages:read", MigrateLegacy("pages:read"))
	})
	t.Run("Sonderrecht und Unbekanntes bleiben", func(t *testing.T) {
		out := MigrateLegacy("admin_dashboard,foo:bar,notices:write")
		assert.True(t, has(out, "admin_dashboard"))
		assert.True(t, has(out, "foo:bar"))
		assert.True(t, has(out, "notices:delete"))
	})
	t.Run("idempotent", func(t *testing.T) {
		for _, in := range []string{"", "components:write,incidents:read", "pages:write,subscribers:read", "x:write"} {
			once := MigrateLegacy(in)
			assert.ElementsMatch(t, strings.Split(once, ","), strings.Split(MigrateLegacy(once), ","), in)
		}
	})
}

// Keine Rechte gewonnen: Für jede Altkombination gewährt die migrierte Liste
// jede Aktion auf jeder Ressource genau dann, wenn die alte Prüfung sie
// gewährt hätte (alte Prüfung: je Tab das Recht aus der Kopplung).
func TestMigrateLegacy_WirkungGleich(t *testing.T) {
	type tab struct{ res, oldRead, oldWrite string }
	tabs := []tab{
		{"components", "components:read", "components:write"},
		{"external_sources", "components:read", "components:write"},
		{"incidents", "incidents:read", "incidents:write"},
		{"maintenance", "incidents:read", "incidents:write"},
		{"incident_templates", "incidents:write", "incidents:write"},
		{"notices", "notices:read", "notices:write"},
		{"subscribers", "subscribers:read", "subscribers:write"},
		{"notification_templates", "pages:write", "pages:write"},
		{"languages", "pages:write", "pages:write"},
		{"ui_translations", "pages:write", "pages:write"},
		{"sso", "pages:write", "pages:write"},
	}
	oldHas := func(list, required string) bool {
		for _, e := range strings.Split(list, ",") {
			if e == required || (strings.HasSuffix(required, ":read") && e == strings.TrimSuffix(required, ":read")+":write") {
				return true
			}
		}
		return false
	}
	bases := []string{"components", "incidents", "notices", "subscribers", "pages"}
	// Alle Teilmengen aus read/write je Basisressource.
	for mask := 0; mask < 1<<(2*len(bases)); mask++ {
		var parts []string
		for i, b := range bases {
			if mask&(1<<(2*i)) != 0 {
				parts = append(parts, b+":read")
			}
			if mask&(1<<(2*i+1)) != 0 {
				parts = append(parts, b+":write")
			}
		}
		old := strings.Join(parts, ",")
		migrated := MigrateLegacy(old)
		for _, tb := range tabs {
			assert.Equal(t, oldHas(old, tb.oldRead), Has(migrated, Perm(tb.res, Read)), "read %s bei %q", tb.res, old)
			for _, act := range []string{Create, Update, Delete} {
				assert.Equal(t, oldHas(old, tb.oldWrite), Has(migrated, Perm(tb.res, act)), "%s:%s bei %q", tb.res, act, old)
			}
		}
	}
}

func TestFullAccessUndUnion(t *testing.T) {
	full := FullAccess()
	for _, r := range Resources {
		for _, act := range Actions {
			assert.True(t, Has(full, Perm(r.Key, act)), r.Key+":"+act)
		}
	}
	assert.Equal(t, "a:read,b:read,c:read", Union("a:read,b:read", "b:read,c:read"))
	assert.Equal(t, "x:create,x:read", List("x:create"))
}

func TestResources_Eindeutig(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Resources {
		assert.False(t, seen[r.Key], r.Key)
		seen[r.Key] = true
		assert.True(t, IsResource(r.Key))
	}
	assert.False(t, IsResource("nope"))
}

func TestHasLegacyWrite(t *testing.T) {
	assert.True(t, HasLegacyWrite("a:read, b:write"))
	assert.False(t, HasLegacyWrite("a:read,b:update"))
	assert.False(t, HasLegacyWrite(""))
}

// Alt-"key:write" gilt in Has auch für die Tabs, die vor der Umstellung
// an die Schlüsselressource gekoppelt waren (sonst Rechteverlust bei
// fehlgeschlagener Migration oder nach einem Rollback).
func TestHas_AltwertWriteMitKopplung(t *testing.T) {
	kopplung := map[string][]string{
		"components": {"external_sources"},
		"incidents":  {"maintenance", "incident_templates"},
		"pages":      {"notification_templates", "languages", "ui_translations", "sso"},
	}
	gekoppelt := map[string]bool{}
	for key, tabs := range kopplung {
		for _, tab := range tabs {
			gekoppelt[tab] = true
			for _, act := range Actions {
				assert.True(t, Has(key+":write", Perm(tab, act)), "%s:write -> %s:%s", key, tab, act)
			}
		}
	}
	// Keine Rechte-Eskalation: nur die Kopplung, sonst nichts (auch nicht andere
	// Schlüssel und nicht die Gegenrichtung).
	for key := range kopplung {
		for _, r := range Resources {
			erlaubt := r.Key == key
			for _, tab := range kopplung[key] {
				erlaubt = erlaubt || r.Key == tab
			}
			for _, act := range Actions {
				assert.Equal(t, erlaubt, Has(key+":write", Perm(r.Key, act)), "%s:write -> %s:%s", key, r.Key, act)
			}
		}
	}
	assert.False(t, Has("incidents:write", "users:read"))
	assert.False(t, Has("incidents:write", "pages:read"))
	assert.False(t, Has("maintenance:write", "incidents:read"), "Kopplung gilt nur Schlüssel -> Tab")
	assert.False(t, Has("external_sources:write", "components:read"))
	assert.False(t, Has("components:read", "external_sources:read"), "Leseerbschaft gilt nicht in Has")
	assert.False(t, Has("pages:update", "languages:update"), "Neu-Recht koppelt nicht")
}

// Normalize löst Alt-"key:write" samt Kopplung auf, wie die Migration.
func TestNormalize_AltwertMitKopplung(t *testing.T) {
	assert.Equal(t,
		"components:create,components:read,components:update,components:delete,"+
			"external_sources:create,external_sources:read,external_sources:update,external_sources:delete",
		Normalize("components:write"))
	for key := range writeCoupling {
		assert.Equal(t, MigrateLegacy(key+":write"), Normalize(key+":write"), key)
	}
}

// Covers entscheidet, ob eine Rolle von jemandem vergeben werden darf.
func TestCovers(t *testing.T) {
	assert.True(t, Covers("pages:create,pages:read", "pages:read"))
	assert.True(t, Covers("pages:create,pages:read", ""))
	assert.False(t, Covers("pages:create,pages:read", "pages:create,roles:read"))
	assert.True(t, Covers("pages:write", "pages:create,pages:delete"))
	assert.False(t, Covers("", "pages:read"))
	assert.False(t, Covers("incidents:read", "incidents:write"))
	// Altwert in want wird normalisiert: pages:write gegen migrierte CRUD-Liste samt gekoppelten Tabs.
	assert.True(t, Covers(MigrateLegacy("pages:write"), "pages:write"))
	assert.False(t, Covers("pages:create,pages:read,pages:update,pages:delete", "pages:write"))
	assert.False(t, Covers(MigrateLegacy("pages:write"), "pages:write,sso:delete,roles:read"))
	assert.True(t, Covers(MigrateLegacy("pages:write,roles:read"), "pages:write,roles:read"))
	assert.False(t, Covers("pages:create,pages:read", "pages:create,pages:read,pages:update")) // nur teilweise
	assert.False(t, Covers("pages:read", "unbekannt:xyz"))                                     // unbekanntes Recht
	assert.False(t, Covers("pages:read", "foo"))                                               // Eintrag ohne Doppelpunkt
	assert.True(t, Covers(AdminDashboard, AdminDashboard))
	// Sonderrecht ohne Doppelpunkt: exakter Treffer (Review).
	assert.True(t, Covers("pages:read,"+AdminDashboard, "pages:read,"+AdminDashboard))
	assert.False(t, Covers("pages:read", "pages:read,"+AdminDashboard))
	assert.False(t, Covers("pages:read,admin_dashboardx", AdminDashboard))
}
