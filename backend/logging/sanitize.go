package logging

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Error texts of HTTP clients (net/url.Error) contain the
// complete URL, Postgres errors the values of the violated column
// ("Key (email)=(a@b.de) already exists"). Neither may ever end up in plain text in the log:
// Google Chat webhook URLs carry key= and token=, Telegram bot URLs
// carry the bot token in the path. The cleanup sits centrally in the log handler
// (redact), SanitizeText/SanitizeErr are additionally available for places
// that pass error texts on outside of slog.

var (
	// urlPattern finds absolute http(s) URLs in free text.
	urlPattern = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	// pgKeyPattern: values in Postgres detail texts "Key (column)=(value)".
	// emailPattern finds email addresses in free text (SMTP servers often repeat
	// the recipient address in the error text).
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	pgKeyPattern = regexp.MustCompile(`Key \(([^)]*)\)=\(.*?\)( already exists| is not present| is still referenced|$)`)
)

// SanitizeText replaces every URL in the text with "scheme://host/[REDACTED]"
// (path, query and userinfo can carry secrets, the host suffices for
// matching) and masks column values in Postgres constraint errors.
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

// SanitizeErr returns the sanitized error text ("" for nil). For a
// *url.Error the URL is replaced before the rest is sanitized.
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

// URLForLog shortens a URL for log lines to scheme, host and path. Query,
// fragment and userinfo are dropped, because access tokens and passwords live there.
// An unreadable URL is replaced by Redacted.
func URLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Redacted
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}
