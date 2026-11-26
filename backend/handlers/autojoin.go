package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"
)

// autoJoinDomainRegex: a hostname of at least two labels (letters,
// digits, hyphens; no hyphen at the label edge).
var autoJoinDomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// normalizeAutoJoinDomains turns the admin input into a
// canonical, comma-separated list: lowercase, without leading "@",
// without duplicates and empty entries, stably sorted by first occurrence.
// Invalid entries yield an error message.
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

// emailMatchesAutoJoin checks whether the domain of the email is exactly in the
// normalized allowlist. Subdomains deliberately do not count, an
// empty list means "no auto-join".
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

// roleSafeAsDefault: a default role (auto-join) must not write anything
// and must not read subscriber data.
func roleSafeAsDefault(list string) bool {
	if permissions.CanChange(list) { // create/update/delete (and the legacy value write)
		return false
	}
	for _, p := range strings.Split(list, ",") {
		if strings.HasPrefix(strings.TrimSpace(p), "subscribers:") {
			return false
		}
	}
	return true
}

// validateAutoJoinSettings normalizes AutoJoinEmailDomains and
// validates DefaultRoleID server-side: the role must exist and must contain neither
// "*:write" nor "subscribers:*".
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
