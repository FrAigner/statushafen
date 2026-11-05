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

// Das Frontend prüfte Pflichtfelder, Passwort-Mindestlänge,
// Sprachcode-Format und Slugs, das Backend nahm alles an (direkte API-Aufrufe
// umgingen die Prüfung). Die Regeln stehen jetzt hier an einer Stelle; jede
// Verletzung liefert 400 mit einem `code`, den das Frontend per i18n übersetzt
// (Muster wie group_not_empty / system_page_slug_locked).
const (
	// minPasswordLength gilt für jeden Weg, ein Passwort zu setzen (Benutzer
	// anlegen/ändern, Set-Password der Bestellung).
	minPasswordLength = 8

	// maxEmailLength ist die Obergrenze einer E-Mail-Adresse (RFC 5321).
	// Längere Eingaben werden vor Suche, Zähler und Audit-Log abgewiesen bzw. gekürzt.
	maxEmailLength = 254

	// Obergrenzen (in Byte) für Texte, die anonyme oder selbst angemeldete
	// Nutzer senden. Ohne sie wurden Werte bis zum Body-Limit (1 MiB)
	// gespeichert, und jeder spätere Versand las sie wieder ein.
	maxWebhookURLLength = 2048
	maxLanguageLength   = 16
	maxSlugInputLength  = 100
	// maxComponentIDsLength: Liste "1,2,3" oder "all"; reicht für einige
	// Tausend Komponenten.
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

// languageCodeRegex: BCP47 light - 2-3 Buchstaben Hauptsprache, optional
// Untertags (Region, Schrift) mit 2-8 Zeichen, z. B. de, en-us, zh-hant-tw.
// Der Code wird vor der Prüfung klein geschrieben (CreateLanguageHandler).
var languageCodeRegex = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

// reservedSlugs sind Namen, die als Subdomain (<slug>.<Basisdomain>) oder
// Pfad bereits belegt sind oder sein könnten: Plattform-Hosts (www, staging,
// api, admin, app, mail, edge), Frontend-Routen (login, docs, status, order, auth,
// subscribe, unsubscribe, profile) und Backend-/Static-Pfade (healthz, static,
// assets, fonts). "system" ist der feste Slug der Systemstatusseite.
var reservedSlugs = map[string]struct{}{
	"www": {}, "staging": {}, "api": {}, "admin": {}, "app": {}, "mail": {},
	"login": {}, "docs": {}, "status": {}, "order": {}, "auth": {},
	"subscribe": {}, "unsubscribe": {}, "profile": {},
	"healthz": {}, "static": {}, "assets": {}, "fonts": {}, "system": {},
	// Typische Mail-Postfächer und Hosts: ein Slug taucht in Absender- und
	// Subdomain-Namen auf und darf keine Betreiberadresse vortäuschen.
	"support": {}, "security": {}, "billing": {}, "postmaster": {}, "abuse": {},
	"noreply": {}, "no-reply": {}, "info": {}, "root": {}, "hostmaster": {},
	"webmaster": {}, "edge": {},
}

func isReservedSlug(slug string) bool {
	_, ok := reservedSlugs[slug]
	return ok
}

// containsInvalidText meldet NUL-Bytes und ungültiges UTF-8: PostgreSQL lehnt
// beides in Textspalten ab, das Speichern scheiterte sonst mit 500.
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

// passwordTooShort zählt Zeichen (Runen), nicht Bytes, damit Umlaute nicht
// doppelt zählen.
func passwordTooShort(pw string) bool {
	return len([]rune(pw)) < minPasswordLength
}

// passwordTooLong zählt Bytes, weil bcrypt bei 72 Byte endet.
func passwordTooLong(pw string) bool {
	return len(pw) > database.MaxPasswordBytes
}

// hashPassword ist database.HashPassword; als Variable, damit ein Test den
// Fehlerpfad (Hashen scheitert) auslösen kann.
var hashPassword = database.HashPassword

// rejectInvalidPassword beantwortet ein zu kurzes oder zu langes Passwort mit
// 400 und Fehlercode und meldet true, wenn die Anfrage damit beendet ist.
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

// textField ist ein Eingabewert samt Obergrenze in Byte für checkInputTexts.
type textField struct {
	value string
	max   int
}

// checkInputTexts ist die zentrale Prüfung für einzeilige Texte aus
// öffentlichen und selbst angemeldeten Eingaben: überlang (input_too_long) oder
// mit Steuerzeichen bzw. ungültigem UTF-8 (input_invalid_characters). Ein
// NUL-Byte lehnt PostgreSQL erst beim Schreiben ab; ohne diese Prüfung wurde
// daraus ein 500 statt eines 400. Zuerst die Länge (billig), dann die Zeichen.
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

// checkSubscriberTexts prüft die Texte eines Abos (E-Mail, Webhook-URL,
// Komponentenliste, Sprache). Die Prüfung gilt für alle Wege, die ein Abo
// anlegen oder ändern.
func checkSubscriberTexts(email, webhookURL, componentIDs, language string) error {
	return checkInputTexts(
		textField{email, maxEmailLength},
		textField{webhookURL, maxWebhookURLLength},
		textField{componentIDs, maxComponentIDsLength},
		textField{language, maxLanguageLength},
	)
}

// limitEmail kürzt eine Nutzereingabe, die als E-Mail-Adresse protokolliert
// wird, auf maxEmailLength Zeichen.
func limitEmail(s string) string {
	if utf8.RuneCountInString(s) <= maxEmailLength {
		return s
	}
	return string([]rune(s)[:maxEmailLength])
}
