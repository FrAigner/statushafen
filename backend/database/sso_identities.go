package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// removeOrphanedPageSSOIdentities cleans up page SSO
// links (user_sso_identities) that were left behind before the changeover when removing
// a member. They otherwise continue to block
// SSO login and attaching to other pages (hasSSOIdentityOnOtherPage).
//
// Not every link without a role is orphaned: an account without a role may
// link itself with the SSO of a page (ssoLinkUser,
// linkablePageScope), and such decisions must survive startup.
// Therefore deletion happens only if there is EVIDENCE that the membership ended
// after the link was created:
//   - no active row in user_status_page_roles for (account, page) and
//   - a soft-deleted row for (account, page), deleted AFTER the creation
//     of the link (removal on the page deletes softly; the row
//     is at the same time the marker against re-auto-join and is only read here,
//     never changed).
//
// Anyone who links again (without a role) after removal has a more recent
// link and stays untouched. Hard-deleted memberships (page withdrawn from the
// account, role deleted) leave no evidence; such legacy cases
// cannot be distinguished from deliberate links without a role and
// stay (solvable in the profile or via DELETE by a global admin).
//
// Idempotent: after the first run the statement matches nothing anymore. A
// DB error only aborts the cleanup (warning), startup continues;
// the previous state then remains, nothing too much is deleted. Only the
// count is logged, no IDs or emails.
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
