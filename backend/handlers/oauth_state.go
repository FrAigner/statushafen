package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Der OAuth-Login bei Google und GitHub bindet Beginn und Rückkehr an ein
// signiertes Cookie. Es trägt State, PKCE-Verifier und Ausstellungszeit, dazu
// eine HMAC über den Anbieter und diese Werte. Der Schlüssel stammt aus
// JWT_SECRET (mit eigenem Zweck abgeleitet). Ein Double-Submit-Cookie mit frei
// gewähltem Wert reicht damit nicht mehr: Wer den State nicht vom Server
// bekommen hat, besteht die Prüfung nicht und löst keinen Aufruf beim
// Anbieter aus. Ein State gilt nur einmal und höchstens oauthStateLifetime.
const (
	oauthStateCookie   = "oauth_state"
	oauthStateLifetime = 5 * time.Minute
	// oauthStateMaxUsed begrenzt den Speicher der eingelösten States. Ein Eintrag
	// verfällt nach oauthStateLifetime; ist die Tabelle voll, verdrängt der
	// älteste Eintrag. Ein verdrängter Eintrag ist ohnehin fast abgelaufen.
	oauthStateMaxUsed = 20000
)

// oauthStateUse merkt die bereits eingelösten States, damit ein Cookie nur einen
// Callback freischaltet.
type oauthStateUse struct {
	mu    sync.Mutex
	used  map[string]time.Time
	order []string // Einlösereihenfolge, ältester zuerst
}

// consume vermerkt state als eingelöst. false: war schon eingelöst.
func (u *oauthStateUse) consume(state string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.used == nil {
		u.used = map[string]time.Time{}
	}
	for len(u.order) > 0 {
		oldest := u.order[0]
		if len(u.order) < oauthStateMaxUsed && now.Sub(u.used[oldest]) <= oauthStateLifetime {
			break
		}
		delete(u.used, oldest)
		u.order = u.order[1:]
	}
	if _, seen := u.used[state]; seen {
		return false
	}
	u.used[state] = now
	u.order = append(u.order, state)
	return true
}

func (h *Handler) oauthNow() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// oauthStateMAC berechnet die Signatur des Cookies für den Anbieter.
func oauthStateMAC(provider, payload string) string {
	key := sha256.Sum256([]byte("statushafen/oauth-state/v1\x00" + os.Getenv("JWT_SECRET")))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(provider + "\x00" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// signOAuthState baut den Cookie-Wert "state.verifier.ausgestellt.mac". State und
// Verifier sind base64url ohne Punkt.
func signOAuthState(provider, state, verifier string, issued time.Time) string {
	payload := state + "." + verifier + "." + strconv.FormatInt(issued.Unix(), 10)
	return payload + "." + oauthStateMAC(provider, payload)
}

// verifyOAuthState prüft das Cookie: Aufbau, Signatur für diesen Anbieter,
// Alter. Es liefert State und Verifier. Fehlt eines davon oder stimmt etwas
// nicht, ist ok false.
func verifyOAuthState(provider, value string, now time.Time) (state, verifier string, ok bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	payload := strings.Join(parts[:3], ".")
	if subtle.ConstantTimeCompare([]byte(parts[3]), []byte(oauthStateMAC(provider, payload))) != 1 {
		return "", "", false
	}
	issued, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "", false
	}
	age := now.Sub(time.Unix(issued, 0))
	if age < 0 || age > oauthStateLifetime {
		return "", "", false
	}
	return parts[0], parts[1], true
}
