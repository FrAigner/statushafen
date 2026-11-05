package handlers

// Drift-Test für die eingebaute App-Doku (frontend/src/docs/{de,en}).
// Die Doku nennt Zahlen, Listen und Namen, die im Code stehen (reservierte Slugs,
// Passwort-Mindestlänge, Fristen externer Quellen, Rechte der Standardrollen,
// Fehlercodes). Ändert sich einer dieser Werte, ohne dass die Doku mitzieht, wird
// dieser Test rot, statt dass die Doku still veraltet (wie es geschehen ist).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/availability"
	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/externalstatus"
)

const docsDir = "../../frontend/src/docs"

func readDoc(t *testing.T, locale, section string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsDir, locale, section+".md"))
	if err != nil {
		t.Fatalf("Doku %s/%s.md nicht lesbar: %v", locale, section, err)
	}
	return string(b)
}

func TestDocs_ReservedSlugsStehenInDerDoku(t *testing.T) {
	for _, locale := range []string{"de", "en"} {
		md := readDoc(t, locale, "statusseiten")
		for slug := range reservedSlugs {
			if !strings.Contains(md, "`"+slug+"`") {
				t.Errorf("%s/statusseiten.md nennt den reservierten Slug %q nicht", locale, slug)
			}
		}
	}
}

func TestDocs_PasswortMindestlaenge(t *testing.T) {
	want := map[string][]string{
		"de": {fmt.Sprintf("mindestens %d Zeichen", minPasswordLength)},
		"en": {fmt.Sprintf("at least %d characters", minPasswordLength)},
	}
	for locale, needles := range want {
		for _, section := range []string{"benutzer-und-rollen", "erste-schritte"} {
			md := readDoc(t, locale, section)
			for _, n := range needles {
				if !strings.Contains(md, n) {
					t.Errorf("%s/%s.md nennt %q nicht", locale, section, n)
				}
			}
		}
	}
}

func TestDocs_ExterneQuellenZahlenStimmenMitDemCodeUeberein(t *testing.T) {
	de := readDoc(t, "de", "externe-quellen")
	en := readDoc(t, "en", "externe-quellen")

	poll := [2]string{
		fmt.Sprintf("%d bis %d, Standard %d", externalstatus.MinPollIntervalSeconds, externalstatus.MaxPollIntervalSeconds, externalstatus.DefaultPollIntervalSeconds),
		fmt.Sprintf("%d to %d, default %d", externalstatus.MinPollIntervalSeconds, externalstatus.MaxPollIntervalSeconds, externalstatus.DefaultPollIntervalSeconds),
	}
	outage := [2]string{
		fmt.Sprintf("%d bis %d, Standard %d", externalstatus.MinOutageAfterMinutes, externalstatus.MaxOutageAfterMinutes, externalstatus.DefaultOutageAfterMinutes),
		fmt.Sprintf("%d to %d, default %d", externalstatus.MinOutageAfterMinutes, externalstatus.MaxOutageAfterMinutes, externalstatus.DefaultOutageAfterMinutes),
	}
	retention := [2]string{
		fmt.Sprintf("%d Tage nach ihrem Ende", externalstatus.DefaultRetentionDays),
		fmt.Sprintf("%d days after they ended", externalstatus.DefaultRetentionDays),
	}
	public := [2]string{
		fmt.Sprintf("in den letzten %d Tagen beendete", publicExternalIncidentDays),
		fmt.Sprintf("ended in the last %d days", publicExternalIncidentDays),
	}
	admin := [2]string{
		fmt.Sprintf("letzten %d Tage", externalIncidentDefaultDays),
		fmt.Sprintf("last %d days", externalIncidentDefaultDays),
	}
	reminder := [2]string{
		fmt.Sprintf("nach %d Stunden", int(externalstatus.OutageReminderAfter.Hours())),
		fmt.Sprintf("after %d hours", int(externalstatus.OutageReminderAfter.Hours())),
	}
	for name, needles := range map[string][2]string{"Abruf-Intervall": poll, "Ausfallfrist": outage, "Aufbewahrung": retention, "öffentliches Fenster": public, "Admin-Fenster": admin, "Erinnerung": reminder} {
		if !strings.Contains(de, needles[0]) {
			t.Errorf("de/externe-quellen.md: %s stimmt nicht mit dem Code überein, erwartet %q", name, needles[0])
		}
		if !strings.Contains(en, needles[1]) {
			t.Errorf("en/externe-quellen.md: %s stimmt nicht mit dem Code überein, erwartet %q", name, needles[1])
		}
	}
	for locale, md := range map[string]string{"de": de, "en": en} {
		if !strings.Contains(md, externalstatus.RetentionDaysEnv) {
			t.Errorf("%s/externe-quellen.md nennt %s nicht", locale, externalstatus.RetentionDaysEnv)
		}
	}
}

func TestDocs_VerfuegbarkeitNenntDasFensterDesCodes(t *testing.T) {
	hours := int(availability.MinObservedForSummary.Hours())
	for locale, needles := range map[string][]string{
		"de": {fmt.Sprintf("letzten %d Tage", availabilityDays), fmt.Sprintf("mindestens %d Stunden beobachtet", hours)},
		"en": {fmt.Sprintf("last %d days", availabilityDays), fmt.Sprintf("at least %d hours", hours)},
	} {
		md := readDoc(t, locale, "oeffentliche-statusseite")
		for _, needle := range needles {
			if !strings.Contains(md, needle) {
				t.Errorf("%s/oeffentliche-statusseite.md nennt %q nicht", locale, needle)
			}
		}
	}
}

// Die Rechte der Standardrolle "Page Owner" sind in der Doku-Tabelle der Rechte
// aufgeführt; eine neue Berechtigung im Seed ohne Doku fällt hier auf.
func TestDocs_RechteDerStandardrollenStehenInDerDoku(t *testing.T) {
	for _, locale := range []string{"de", "en"} {
		md := readDoc(t, locale, "benutzer-und-rollen")
		for _, perm := range strings.Split(database.PageOwnerPermissions, ",") {
			if !strings.Contains(md, "`"+perm+"`") {
				t.Errorf("%s/benutzer-und-rollen.md nennt das Recht %q nicht", locale, perm)
			}
		}
	}
}

// Jeder Fehlercode, den die API-Doku nennt, muss im Backend vorkommen (als
// Stringliteral in einem Handler); sonst beschreibt die Doku einen Code, den es
// nicht gibt.
func TestDocs_GenannteFehlercodesGibtEsImBackend(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	codeRow := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| \\d{3} \\|")
	for _, locale := range []string{"de", "en"} {
		rows := codeRow.FindAllStringSubmatch(readDoc(t, locale, "api-und-webhooks"), -1)
		if len(rows) < 8 {
			t.Fatalf("%s/api-und-webhooks.md: Tabelle der Fehlercodes nicht gefunden (%d Zeilen)", locale, len(rows))
		}
		for _, r := range rows {
			if !strings.Contains(src.String(), `"`+r[1]+`"`) {
				t.Errorf("%s/api-und-webhooks.md nennt den Fehlercode %q, der im Backend nicht vorkommt", locale, r[1])
			}
		}
	}
}

// Die Ereignisse für gespiegelte Meldungen und ihre Platzhalter sind nur per API
// anlegbar; die Doku muss genau diese Namen nennen.
func TestDocs_MailTemplatesNennenExterneEreignisseUndPlatzhalter(t *testing.T) {
	for _, locale := range []string{"de", "en"} {
		md := readDoc(t, locale, "mail-templates")
		for _, needle := range []string{
			"`" + NotificationKindExternalIncidentCreated + "`",
			"`" + NotificationKindExternalIncidentUpdated + "`",
			"`" + NotificationKindExternalIncidentResolved + "`",
			"`{{.SourceName}}`", "`{{.SourceURL}}`",
		} {
			if !strings.Contains(md, needle) {
				t.Errorf("%s/mail-templates.md nennt %s nicht", locale, needle)
			}
		}
		if !strings.Contains(md, "external_maintenance_") {
			t.Errorf("%s/mail-templates.md nennt die external_maintenance_*-Ereignisse nicht", locale)
		}
	}
}
