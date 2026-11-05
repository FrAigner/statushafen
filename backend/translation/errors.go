package translation

import "errors"

// Fehler der Übersetzungsdienste. Die Texte nennen bewusst weder
// Adresse noch Schlüssel: Sie landen in Logs und werden vom Handler auf
// Fehlercodes abgebildet. Netzwerkfehler des http.Clients tragen die URL und
// werden deshalb nie weitergereicht, sondern auf ErrUnavailable abgebildet.
var (
	// ErrUnavailable: Dienst nicht erreichbar, Zeitüberschreitung, Serverfehler
	// oder unlesbare Antwort.
	ErrUnavailable = errors.New("übersetzungsdienst nicht erreichbar")
	// ErrAuth: der Dienst lehnt den Schlüssel ab (401/403).
	ErrAuth = errors.New("übersetzungsdienst lehnt den Zugang ab")
	// ErrQuota: Kontingent oder Aufruf-Limit des Dienstes erreicht (429/456).
	ErrQuota = errors.New("kontingent des übersetzungsdienstes erreicht")
	// ErrUnsupportedLanguage: Sprachpaar wird vom Dienst nicht angeboten.
	ErrUnsupportedLanguage = errors.New("sprachpaar nicht unterstützt")
)
