package translation

import "errors"

// Errors of the translation services. The texts deliberately name neither
// address nor key: they end up in logs and are mapped by the handler to
// error codes. Network errors of the http.Client carry the URL and
// are therefore never passed on, but mapped to ErrUnavailable.
var (
	// ErrUnavailable: service unreachable, timeout, server error
	// or unreadable response.
	ErrUnavailable = errors.New("übersetzungsdienst nicht erreichbar")
	// ErrAuth: the service rejects the key (401/403).
	ErrAuth = errors.New("übersetzungsdienst lehnt den Zugang ab")
	// ErrQuota: quota or call limit of the service reached (429/456).
	ErrQuota = errors.New("kontingent des übersetzungsdienstes erreicht")
	// ErrUnsupportedLanguage: language pair is not offered by the service.
	ErrUnsupportedLanguage = errors.New("sprachpaar nicht unterstützt")
)
