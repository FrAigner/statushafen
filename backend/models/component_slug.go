package models

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// Kennung (Slug) einer Komponente bzw. Gruppe. Sie ist je
// Statusseite unter den aktiven Zeilen eindeutig (partieller Unique-Index in
// database/db.go) und nach dem Anlegen unveränderlich, weil Integrationen sie
// als stabilen Schlüssel verwenden sollen.

// MaxComponentSlugLen ist die längste zulässige Kennung (Zeichen, nur ASCII).
const MaxComponentSlugLen = 60

// componentSlugFallback ist die Kennungsbasis, wenn der Name keine
// verwertbaren Zeichen enthält (z. B. nur Sonderzeichen).
const componentSlugFallback = "komponente"

var (
	componentSlugRegex    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	componentSlugSanitize = regexp.MustCompile(`[^a-z0-9]+`)
	componentSlugUmlauts  = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
)

// ValidComponentSlug prüft Format und Länge einer Kennung.
func ValidComponentSlug(s string) bool {
	return len(s) <= MaxComponentSlugLen && componentSlugRegex.MatchString(s)
}

// ComponentSlugBase leitet aus einem Namen die Kennungsbasis ab: Kleinbuchstaben,
// Umlaute transliteriert (ä→ae, ö→oe, ü→ue, ß→ss), jede Folge anderer Zeichen
// wird zu einem Bindestrich. Das Ergebnis ist nie leer und höchstens
// MaxComponentSlugLen lang.
func ComponentSlugBase(name string) string {
	s := componentSlugUmlauts.Replace(strings.ToLower(name))
	s = strings.Trim(componentSlugSanitize.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return componentSlugFallback
	}
	return trimSlug(s, MaxComponentSlugLen)
}

// trimSlug kürzt s auf höchstens n Zeichen, ohne einen Bindestrich am Ende zu
// hinterlassen (s ist reines ASCII).
func trimSlug(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.TrimRight(s, "-")
}

// ComponentSlugWithSuffix liefert die n-te Kollisionsvariante von base: n = 1 ist
// base selbst, ab 2 wird "-n" angehängt (base wird dafür gekürzt, damit die
// Länge eingehalten bleibt).
func ComponentSlugWithSuffix(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := fmt.Sprintf("-%d", n)
	return trimSlug(base, MaxComponentSlugLen-len(suffix)) + suffix
}

// FreeComponentSlug sucht für die Statusseite pageID die erste freie Kennung
// zu name (base, base-2, base-3 …). Nur aktive Zeilen zählen, so wie der
// partielle Unique-Index.
func FreeComponentSlug(db *gorm.DB, pageID uint, name string) (string, error) {
	base := ComponentSlugBase(name)
	for n := 1; ; n++ {
		slug := ComponentSlugWithSuffix(base, n)
		var count int64
		err := db.Session(&gorm.Session{NewDB: true}).Model(&Component{}).
			Where("status_page_id = ? AND slug = ?", pageID, slug).Count(&count).Error
		if err != nil {
			return "", err
		}
		if count == 0 {
			return slug, nil
		}
	}
}

// BeforeCreate vergibt jeder neuen Komponente ohne Kennung automatisch eine.
// Damit bekommen alle Anlegewege (Handler, Seed, Übernahme externer
// Komponenten, Tests) eine eindeutige Kennung, ohne dass jeder Aufrufer daran
// denken muss. Eine gesetzte Kennung bleibt unangetastet; ihre Prüfung (Format,
// Eindeutigkeit) liegt beim Handler.
func (c *Component) BeforeCreate(tx *gorm.DB) error {
	if c.Slug != "" {
		return nil
	}
	slug, err := FreeComponentSlug(tx, c.StatusPageID, c.Name)
	if err != nil {
		return err
	}
	c.Slug = slug
	return nil
}
