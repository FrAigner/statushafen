package logging

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Fehlertexte von HTTP-Clients (net/url.Error) enthalten die
// komplette URL, Postgres-Fehler die Werte der verletzten Spalte
// ("Key (email)=(a@b.de) already exists"). Beides darf nie im Klartext im Log
// landen: Google-Chat-Webhook-URLs tragen key= und token=, Telegram-Bot-URLs
// das Bot-Token im Pfad. Die Bereinigung sitzt zentral im Log-Handler
// (redact), SanitizeText/SanitizeErr stehen zusätzlich für Stellen bereit,
// die Fehlertexte außerhalb von slog weiterreichen.

var (
	// urlPattern findet absolute http(s)-URLs in Freitext.
	urlPattern = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	// pgKeyPattern: Werte in Postgres-Detailtexten "Key (spalte)=(wert)".
	// emailPattern findet E-Mail-Adressen in Freitext (SMTP-Server wiederholen
	// die Empfängeradresse oft im Fehlertext).
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	pgKeyPattern = regexp.MustCompile(`Key \(([^)]*)\)=\(.*?\)( already exists| is not present| is still referenced|$)`)
)

// SanitizeText ersetzt jede URL im Text durch "scheme://host/[REDACTED]"
// (Pfad, Query und Userinfo können Geheimnisse tragen, der Host reicht zum
// Zuordnen) und blendet Spaltenwerte in Postgres-Constraint-Fehlern aus.
func SanitizeText(s string) string {
	if strings.Contains(s, "://") {
		s = urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" {
				return Redacted
			}
			return u.Scheme + "://" + u.Host + "/" + Redacted
		})
	}
	if strings.Contains(s, "Key (") {
		s = pgKeyPattern.ReplaceAllString(s, "Key ($1)=("+Redacted+")$2")
	}
	if strings.Contains(s, "@") {
		s = emailPattern.ReplaceAllStringFunc(s, MaskEmail)
	}
	return s
}

// SanitizeErr liefert den bereinigten Fehlertext ("" bei nil). Bei einem
// *url.Error wird die URL ersetzt, bevor der Rest bereinigt wird.
func SanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return SanitizeText(strings.Replace(err.Error(), ue.URL, "[URL]", 1))
	}
	return SanitizeText(err.Error())
}

// URLForLog kürzt eine URL für Logzeilen auf Schema, Host und Pfad. Query,
// Fragment und Userinfo entfallen, denn dort stehen Zugangstoken und Passwörter.
// Eine nicht lesbare URL wird durch Redacted ersetzt.
func URLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Redacted
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}
