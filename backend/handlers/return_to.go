package handlers

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxReturnToLength bounds the return target of a login (bytes).
const maxReturnToLength = 2048

// sanitizeReturnTo validates the frontend path a login started from
// (query parameter return_to of the login start; gin has decoded it once).
// It returns the path unchanged when it is safe, otherwise "" (the login
// goes on, the frontend then picks its default). Safe means:
//
//   - at most maxReturnToLength bytes of valid UTF-8,
//   - starts with exactly one "/" (no "//" and no "/\", which browsers treat
//     as protocol-relative), also after decoding it once more,
//   - no control characters, whitespace or backslash anywhere,
//   - no control characters after decoding (CR/LF, NUL, ...),
//   - does not start with "/api" (any case), before or after decoding: the
//     target is a frontend route, never a backend endpoint.
//
// The value is only handed back to the frontend through sso-exchange (JSON),
// never used in a server-side redirect.
func sanitizeReturnTo(raw string) string {
	if raw == "" || len(raw) > maxReturnToLength || !utf8.ValidString(raw) || !safeReturnToPath(raw) {
		return ""
	}
	if strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' }) >= 0 {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) || !safeReturnToPath(decoded) || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return ""
	}
	return raw
}

// safeReturnToPath checks the start of a return target: one slash, not
// followed by a slash or backslash, and not the backend prefix /api.
func safeReturnToPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return false
	}
	return !strings.HasPrefix(strings.ToLower(p), "/api")
}
