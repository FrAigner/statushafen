package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"
)

// autoJoinDomainRegex: ein Hostname aus mindestens zwei Labels (Buchstaben,
// Ziffern, Bindestriche; kein Bindestrich am Label-Rand).
var autoJoinDomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// normalizeAutoJoinDomains macht aus der Admin-Eingabe eine
// kanonische, kommagetrennte Liste: Kleinschreibung, ohne führendes "@",
// ohne Duplikate und Leereinträge, stabil sortiert nach erstem Auftreten.
// Ungültige Einträge liefern eine Fehlermeldung.
func normalizeAutoJoinDomains(raw string) (string, string, bool) {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(part)), "@")
		if d == "" || seen[d] {
			continue
		}
		if !autoJoinDomainRegex.MatchString(d) {
			return "", "AutoJoinEmailDomains: \"" + d + "\" ist keine gültige Domain (z.B. acme.example)", false
		}
		seen[d] = true
		out = append(out, d)
	}
	return strings.Join(out, ","), "", true
}

// emailMatchesAutoJoin prüft, ob die Domain der E-Mail exakt in der
// normalisierten Allowlist steht. Subdomains zählen bewusst nicht mit, eine
// leere Liste heißt "kein Auto-Join".
func emailMatchesAutoJoin(email, domains string) bool {
	if domains == "" {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range strings.Split(domains, ",") {
		if d == domain {
			return true
		}
	}
	return false
}

// roleSafeAsDefault: eine Default-Rolle (Auto-Join) darf nichts schreiben
// und keine Abonnentendaten lesen.
func roleSafeAsDefault(list string) bool {
	if permissions.CanChange(list) { // create/update/delete (und der Altwert write)
		return false
	}
	for _, p := range strings.Split(list, ",") {
		if strings.HasPrefix(strings.TrimSpace(p), "subscribers:") {
			return false
		}
	}
	return true
}

// validateAutoJoinSettings normalisiert AutoJoinEmailDomains und
// prüft DefaultRoleID serverseitig: Die Rolle muss existieren und darf weder
// "*:write" noch "subscribers:*" enthalten.
func (h *Handler) validateAutoJoinSettings(page *models.StatusPage) (int, string, bool) {
	domains, msg, ok := normalizeAutoJoinDomains(page.AutoJoinEmailDomains)
	if !ok {
		return http.StatusBadRequest, msg, false
	}
	page.AutoJoinEmailDomains = domains
	if page.DefaultRoleID == nil {
		return 0, "", true
	}
	var role models.Role
	if err := h.DB.First(&role, *page.DefaultRoleID).Error; err != nil {
		return http.StatusBadRequest, "DefaultRoleID: Rolle existiert nicht", false
	}
	if !roleSafeAsDefault(role.Permissions) {
		return http.StatusBadRequest, "DefaultRoleID: Die Rolle darf keine Schreibrechte (*:write) und keinen Zugriff auf Abonnenten (subscribers:*) enthalten", false
	}
	return 0, "", true
}
