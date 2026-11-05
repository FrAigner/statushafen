package handlers

import (
	"errors"
	"net/http"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const codeLastPageOwner = "last_page_owner"

// deleteSSOIdentitiesOnPage entfernt die Seiten-SSO-Verknüpfungen
// (OIDC/SAML) eines Kontos auf GENAU dieser Seite. Endet die Mitgliedschaft,
// bliebe die Verknüpfung sonst verwaist stehen: Sie blockiert später die
// SSO-Anmeldung und das Anhängen an andere Seiten (hasSSOIdentityOnOtherPage,
// errOIDCOtherPageRole). UserSSOIdentity hat kein Soft-Delete, das Löschen ist
// endgültig. Identitäten auf anderen Seiten bleiben unberührt. Läuft in der
// Transaktion des Aufrufers. liefert die Anzahl gelöschter
// Verknüpfungen für den Audit-Eintrag.
func deleteSSOIdentitiesOnPage(tx *gorm.DB, userID, pageID uint) (int64, error) {
	res := tx.Where("user_id = ? AND status_page_id = ?", userID, pageID).
		Delete(&models.UserSSOIdentity{})
	return res.RowsAffected, res.Error
}

// deleteSSOIdentitiesForRoles entfernt die Seiten-SSO-Verknüpfungen zu den
// übergebenen Mitgliedschaften. Je (Konto, Seite) gibt es dank
// Unique-Index höchstens eine aktive Zeile.
func deleteSSOIdentitiesForRoles(tx *gorm.DB, rows []models.UserStatusPageRole) error {
	for _, r := range rows {
		if _, err := deleteSSOIdentitiesOnPage(tx, r.UserID, r.StatusPageID); err != nil {
			return err
		}
	}
	return nil
}

// dedupePageRoleInputs behält je Seite nur den letzten Eintrag:
// Der partielle Unique-Index (user_id, status_page_id) erlaubt je Seite genau
// eine Rolle; doppelte Seiten im Request würden sonst mit 500 scheitern.
// Einträge ohne Rolle (RoleID 0) bleiben unverändert erhalten.
func dedupePageRoleInputs(in []PageRoleInput) []PageRoleInput {
	last := map[uint]int{}
	for i, pr := range in {
		if pr.RoleID != 0 {
			last[pr.StatusPageID] = i
		}
	}
	out := make([]PageRoleInput, 0, len(in))
	for i, pr := range in {
		if pr.RoleID == 0 || last[pr.StatusPageID] == i {
			out = append(out, pr)
		}
	}
	return out
}

// errLastPageOwner: die Änderung ließe die Seite ohne Mitglied mit pages:update.
var errLastPageOwner = errors.New("last page owner")

// ensureOtherPageOwner liefert errLastPageOwner, wenn neben userID
// kein weiteres aktives Mitglied der Seite pages:update hat. Konten, die
// soft-gelöscht sind, zählen nicht (wie soleAdminPages in me_delete.go). Läuft
// in der Transaktion des Aufrufers; die Mitgliedszeilen der Seite werden unter
// Postgres mit FOR UPDATE gesperrt, damit zwei Owner nicht gleichzeitig beide
// "den anderen" sehen (SQLite serialisiert Schreiber ohnehin).
func ensureOtherPageOwner(tx *gorm.DB, pageID, userID uint) error {
	var rows []models.UserStatusPageRole
	// SQLite kennt FOR UPDATE nicht, GORMs Dialekt lässt die Klausel dort weg.
	q := tx.Preload("Role").
		Joins("JOIN users ON users.id = user_status_page_roles.user_id AND users.deleted_at IS NULL").
		Where("user_status_page_roles.status_page_id = ?", pageID).
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "user_status_page_roles"}})
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	upd := permissions.Perm("pages", permissions.Update)
	for _, r := range rows {
		if r.UserID != userID && r.Role.ID != 0 && permissions.Has(r.Role.Permissions, upd) {
			return nil
		}
	}
	return errLastPageOwner
}

// lockOutApplies: Betrifft die Änderung die Sperrregel? Das gilt
// für einen Nicht-Global-Admin, der sich selbst entfernt (newRole == nil) oder
// auf eine Rolle ohne pages:update herabstuft, obwohl er jetzt pages:update hat.
// Global Admins sind nie betroffen (sie können jederzeit reparieren).
func (h *Handler) lockOutApplies(c *gin.Context, targetID uint, current models.Role, newRole *models.Role) bool {
	if targetID != getUserID(c) || h.isEffectiveGlobalAdmin(c) {
		return false
	}
	upd := permissions.Perm("pages", permissions.Update)
	if !permissions.Has(current.Permissions, upd) {
		return false // war schon kein Owner, nichts geht verloren
	}
	return newRole == nil || !permissions.Has(newRole.Permissions, upd)
}

func respondLastPageOwner(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": "You are the last user with page management rights on this page", "code": codeLastPageOwner})
}
