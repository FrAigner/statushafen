package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fraigner/statushafen/database"

	"github.com/gin-gonic/gin"
)

// The frontend checked required fields, minimum password length,
// language code format and slugs, while the backend accepted everything (direct API calls
// bypassed the check). The rules now live here in one place; every
// violation yields 400 with a `code` that the frontend translates via i18n
// (pattern like group_not_empty / system_page_slug_locked).
const (
	// minPasswordLength applies to every way of setting a password (creating/
	// changing a user, set-password of the order).
	minPasswordLength = 8

	// maxEmailLength is the upper bound of an email address (RFC 5321).
	// Longer inputs are rejected or truncated before lookup, counters and audit log.
	maxEmailLength = 254

	// Upper bounds (in bytes) for texts that anonymous or self-registered
	// users send. Without them, values up to the body limit (1 MiB) were
	// stored, and every later send read them in again.
	maxWebhookURLLength = 2048
	maxLanguageLength   = 16
	maxSlugInputLength  = 100
	// maxComponentIDsLength: list "1,2,3" or "all"; enough for a few
	// thousand components.
	maxComponentIDsLength = 8192

	codeInputTooLong           = "input_too_long"
	codeInputInvalidCharacters = "input_invalid_characters"

	codeNameRequired        = "name_required"
	codePasswordTooShort    = "password_too_short"
	codePasswordTooLong     = "password_too_long"
	codeLanguageCodeInvalid = "language_code_invalid"
	codeSlugReserved        = "slug_reserved"
	codeOrderNameTooLong    = "order_name_too_long"
	codeSlugTooLong         = "slug_too_long"
	codeEmailTooLong        = "email_too_long"
	codeInvalidCharacters   = "invalid_characters"
)

// languageCodeRegex: BCP47 light - 2-3 letters of the main language, optional
// subtags (region, script) of 2-8 characters, e.g. de, en-us, zh-hant-tw.
// The code is lowercased before the check (CreateLanguageHandler).
var languageCodeRegex = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

// reservedSlugs are names that are already taken or could be taken as a subdomain (<slug>.<base domain>) or
// path: platform hosts (www, staging,
// api, admin, app, mail, edge), frontend routes (login, docs, status, order, auth,
// subscribe, unsubscribe, profile) and backend/static paths (healthz, static,
// assets, fonts). "system" is the fixed slug of the system status page.
var reservedSlugs = map[string]struct{}{
	"www": {}, "staging": {}, "api": {}, "admin": {}, "app": {}, "mail": {},
	"login": {}, "docs": {}, "status": {}, "order": {}, "auth": {},
	"subscribe": {}, "unsubscribe": {}, "profile": {},
	"healthz": {}, "static": {}, "assets": {}, "fonts": {}, "system": {},
	// Typical mailboxes and hosts: a slug appears in sender and
	// subdomain names and must not fake an operator address.
	"support": {}, "security": {}, "billing": {}, "postmaster": {}, "abuse": {},
	"noreply": {}, "no-reply": {}, "info": {}, "root": {}, "hostmaster": {},
	"webmaster": {}, "edge": {},
}

func isReservedSlug(slug string) bool {
	_, ok := reservedSlugs[slug]
	return ok
}

// containsInvalidText reports NUL bytes and invalid UTF-8: PostgreSQL rejects
// both in text columns, so saving otherwise failed with 500.
func containsInvalidText(values ...string) bool {
	for _, v := range values {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return true
		}
	}
	return false
}

func abortInvalidInput(c *gin.Context, code, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": code})
}

// passwordTooShort counts characters (runes), not bytes, so that umlauts do not
// count double.
func passwordTooShort(pw string) bool {
	return len([]rune(pw)) < minPasswordLength
}

// passwordTooLong counts bytes because bcrypt stops at 72 bytes.
func passwordTooLong(pw string) bool {
	return len(pw) > database.MaxPasswordBytes
}

// hashPassword is database.HashPassword; a variable so that a test can trigger the
// error path (hashing fails).
var hashPassword = database.HashPassword

// rejectInvalidPassword answers a password that is too short or too long with
// 400 and an error code and returns true if the request is thereby finished.
func rejectInvalidPassword(c *gin.Context, pw string) bool {
	switch {
	case passwordTooShort(pw):
		abortInvalidInput(c, codePasswordTooShort, fmt.Sprintf("Passwort muss mindestens %d Zeichen lang sein", minPasswordLength))
	case passwordTooLong(pw):
		abortInvalidInput(c, codePasswordTooLong, fmt.Sprintf("Passwort darf höchstens %d Byte lang sein", database.MaxPasswordBytes))
	default:
		return false
	}
	return true
}

// textField is an input value with its upper bound in bytes for checkInputTexts.
type textField struct {
	value string
	max   int
}

// checkInputTexts is the central check for single-line texts from
// public and self-registered inputs: too long (input_too_long) or
// containing control characters or invalid UTF-8 (input_invalid_characters). A
// NUL byte is only rejected by PostgreSQL on write; without this check it turned
// into a 500 instead of a 400. Length first (cheap), then characters.
func checkInputTexts(fields ...textField) error {
	for _, f := range fields {
		if len(f.value) > f.max {
			return newCodedError(codeInputTooLong, "Eine Eingabe ist zu lang.")
		}
	}
	for _, f := range fields {
		if !utf8.ValidString(f.value) || strings.IndexFunc(f.value, unicode.IsControl) >= 0 {
			return newCodedError(codeInputInvalidCharacters, "Eine Eingabe enthält unzulässige Zeichen.")
		}
	}
	return nil
}

// checkSubscriberTexts checks the texts of a subscription (email, webhook URL,
// component list, language). The check applies to all paths that create
// or change a subscription.
func checkSubscriberTexts(email, webhookURL, componentIDs, language string) error {
	return checkInputTexts(
		textField{email, maxEmailLength},
		textField{webhookURL, maxWebhookURLLength},
		textField{componentIDs, maxComponentIDsLength},
		textField{language, maxLanguageLength},
	)
}

// limitEmail truncates a user input that is logged as an email address
// to maxEmailLength characters.
func limitEmail(s string) string {
	if utf8.RuneCountInString(s) <= maxEmailLength {
		return s
	}
	return string([]rune(s)[:maxEmailLength])
}
