package models

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// Identifier (slug) of a component or group. It is unique per
// status page among active rows (partial unique index in
// database/db.go) and immutable after creation, because integrations are meant to use it
// as a stable key.

// MaxComponentSlugLen is the longest permitted identifier (characters, ASCII only).
const MaxComponentSlugLen = 60

// componentSlugFallback is the identifier base when the name contains no
// usable characters (e.g. only special characters).
const componentSlugFallback = "komponente"

var (
	componentSlugRegex    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	componentSlugSanitize = regexp.MustCompile(`[^a-z0-9]+`)
	componentSlugUmlauts  = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
)

// ValidComponentSlug checks the format and length of an identifier.
func ValidComponentSlug(s string) bool {
	return len(s) <= MaxComponentSlugLen && componentSlugRegex.MatchString(s)
}

// ComponentSlugBase derives the identifier base from a name: lowercase,
// umlauts transliterated (ä→ae, ö→oe, ü→ue, ß→ss), every run of other characters
// becomes a hyphen. The result is never empty and at most
// MaxComponentSlugLen long.
func ComponentSlugBase(name string) string {
	s := componentSlugUmlauts.Replace(strings.ToLower(name))
	s = strings.Trim(componentSlugSanitize.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return componentSlugFallback
	}
	return trimSlug(s, MaxComponentSlugLen)
}

// trimSlug shortens s to at most n characters without leaving a hyphen at the end
// (s is pure ASCII).
func trimSlug(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.TrimRight(s, "-")
}

// ComponentSlugWithSuffix returns the n-th collision variant of base: n = 1 is
// base itself, from 2 on "-n" is appended (base is shortened for that so that the
// length is kept).
func ComponentSlugWithSuffix(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := fmt.Sprintf("-%d", n)
	return trimSlug(base, MaxComponentSlugLen-len(suffix)) + suffix
}

// FreeComponentSlug finds, for the status page pageID, the first free identifier
// for name (base, base-2, base-3 …). Only active rows count, just like the
// partial unique index.
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

// BeforeCreate automatically assigns an identifier to every new component without one.
// This way all creation paths (handler, seed, adoption of external
// components, tests) get a unique identifier without every caller having to
// think of it. A set identifier is left untouched; its validation (format,
// uniqueness) is up to the handler.
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
