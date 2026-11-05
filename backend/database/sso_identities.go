package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// removeOrphanedPageSSOIdentities räumt Seiten-SSO-
// Verknüpfungen (user_sso_identities) auf, die vor der Umstellung beim Entfernen
// eines Mitglieds stehen geblieben sind. Sie blockieren sonst weiter die
// SSO-Anmeldung und das Anhängen an andere Seiten (hasSSOIdentityOnOtherPage).
//
// Nicht jede Verknüpfung ohne Rolle ist verwaist: Ein Konto ohne Rolle darf
// sich selbst mit dem SSO einer Seite verknüpfen (ssoLinkUser,
// linkablePageScope), und solche Entscheidungen müssen den Start überleben.
// Gelöscht wird deshalb nur, wenn ein BELEG für das Ende der Mitgliedschaft
// nach der Verknüpfung vorliegt:
//   - keine aktive Zeile in user_status_page_roles für (Konto, Seite) und
//   - eine soft-gelöschte Zeile für (Konto, Seite), gelöscht NACH dem Anlegen
//     der Verknüpfung (das Entfernen auf der Seite löscht weich; die Zeile
//     ist zugleich der Merker gegen erneutes Auto-Join und wird hier nur
//     gelesen, nie verändert).
//
// Wer nach dem Entfernen erneut (ohne Rolle) verknüpft, hat eine jüngere
// Verknüpfung und bleibt unberührt. Hart gelöschte Mitgliedschaften (Seite am
// Konto entzogen, Rolle gelöscht) hinterlassen keinen Beleg; solche Altfälle
// sind von bewussten Verknüpfungen ohne Rolle nicht zu unterscheiden und
// bleiben stehen (lösbar im Profil oder per DELETE durch einen Global Admin).
//
// Idempotent: Nach dem ersten Lauf trifft das Statement nichts mehr. Ein
// DB-Fehler bricht nur die Bereinigung ab (Warnung), der Start läuft weiter;
// dann bleibt der bisherige Zustand, es wird nichts zu viel gelöscht. Geloggt
// wird nur die Anzahl, keine IDs oder E-Mails.
func removeOrphanedPageSSOIdentities(db *gorm.DB) {
	res := db.Exec(`DELETE FROM user_sso_identities
WHERE NOT EXISTS (
  SELECT 1 FROM user_status_page_roles a
  WHERE a.user_id = user_sso_identities.user_id
    AND a.status_page_id = user_sso_identities.status_page_id
    AND a.deleted_at IS NULL)
AND EXISTS (
  SELECT 1 FROM user_status_page_roles e
  WHERE e.user_id = user_sso_identities.user_id
    AND e.status_page_id = user_sso_identities.status_page_id
    AND e.deleted_at IS NOT NULL
    AND e.deleted_at >= user_sso_identities.created_at)`)
	if res.Error != nil {
		slog.Warn("Verwaiste Seiten-SSO-Verknüpfungen konnten nicht bereinigt werden", "error", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		slog.Info("Verwaiste Seiten-SSO-Verknüpfungen entfernt", "count", res.RowsAffected)
	}
}
