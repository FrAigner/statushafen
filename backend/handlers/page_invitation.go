package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ─────────────────────────────────────────────────────────────────────────────
// Invitation flow for page users.
//
// Threat: A page admin attached a foreign, existing account
// to their page directly by email. The account received staff mails unasked and
// could lose the SSO login of its actual page.
//
// Flow now (non-global admins only; global admins still assign directly):
//  1. POST /api/admin/pages/:page_id/users {email, role_id} creates an invitation
//     for each address that is not yet a member (same response and same
//     list whether an account exists or not). Only an existing, attachable
//     account gets a mail; it is sent in the background. The account lookup runs
//     the same queries for known and unknown addresses; an
//     exactly equal runtime is not guaranteed.
//  2. The mail contains a one-time link with 128 bits of randomness; only
//     its SHA-256 is stored. Valid for pageInvitationTTL.
//  3. Accepting/declining (POST /api/invitations/{preview,accept,decline}) only
//     logged in AND with the account whose address was invited. Any
//     deviation (wrong token, different account) yields the same 404.
// 4. On accepting, the same limits apply as when attaching directly: global admin accounts,
//     accounts with an SSO link or a role on another page, and
//     page SSO sessions do not accept on their own (409); that stays with the global
//     admin. In addition, the inviter must still be allowed to grant the role.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// pageInvitationTTL: how long an invitation stays valid.
	pageInvitationTTL = 7 * 24 * time.Hour
	// maxPendingPageInvitations limits open invitations per page (protection against
	// mail flooding via many addresses; all are counted, even without an account).
	maxPendingPageInvitations = 100
	// Security review: An open invitation for the same address that is
	// younger than pageInvitationResendAfter stays unchanged (no new
	// mail, no new token). At most maxPageInvitationsPerDay new
	// invitations per page within 24 hours. Both rules apply to every address,
	// with or without an account (no oracle).
	pageInvitationResendAfter = 30 * time.Minute
	maxPageInvitationsPerDay  = 200
	auditInvitationCreated    = "statuspage.invitation_created"

	codeInvitationNotFound        = "invitation_not_found"
	codeInvitationExpired         = "invitation_expired"
	codeInvitationAccountConflict = "invitation_account_conflict"
	codeInvitationAlreadyMember   = "invitation_already_member"
	codeInvitationLimitReached    = "invitation_limit_reached"
	codeInvitationTokenMissing    = "invitation_token_missing"
)

// pageUserEmailAcceptedMsg is the uniform response when adding by
// email (invitation created, a member's role changed, or nothing
// changed), the same for known and unknown addresses.
const pageUserEmailAcceptedMsg = "Wenn ein Konto mit dieser E-Mail-Adresse existiert, erhält es eine Einladung; bestehende Mitglieder erhalten die gewählte Rolle"

func hashPageInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// invitePageUser creates (or renews) the invitation for email on the page.
// target is the account found and attachable (ID 0: none, then no mail),
// actorPerms the caller's permissions on the page. In every case the response is
// the same neutral 200 (except for form errors and the upper limits, which
// also do not depend on the account).
func (h *Handler) invitePageUser(c *gin.Context, page models.StatusPage, role models.Role, email string, target models.User, actorPerms string) {
	email = strings.ToLower(strings.TrimSpace(email))
	// Bare addresses only: They end up in the To: header and in the list. The
	// check does not depend on the account (no oracle).
	if !isPlainEmailAddress(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email address", "code": "email_invalid"})
		return
	}
	now := time.Now()
	token := rand.Text()
	var inv models.PageInvitation
	var audit models.AuditLog
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Review F3: Invitations of a page run one after another. The
		// page row is locked under Postgres (SQLite omits the clause and
		// serializes writers anyway); only after the first one commits does the
		// second see lock and counters. Errors go upward.
		// Re-review W1: FOR NO KEY UPDATE instead of FOR UPDATE. The INSERT into
		// user_status_page_roles (accepting) needs FOR KEY
		// SHARE on the page row because of the foreign key; FOR UPDATE collides with it and, with a
		// concurrent re-invite of the same address (waiting for the invitation row
		// deleted by the accept), led to a deadlock (40P01). NO KEY UPDATE locks
		// inviters against each other, not against KEY SHARE.
		if err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Select("id").
			Where("id = ?", page.ID).Take(&models.StatusPage{}).Error; err != nil {
			return err
		}
		// Clean up expired invitations of the page.
		if err := tx.Unscoped().Where("status_page_id = ? AND expires_at < ?", page.ID, now).
			Delete(&models.PageInvitation{}).Error; err != nil {
			return err
		}
		if err := replaceablePendingInvitation(tx, page.ID, email, actorPerms, now); err != nil {
			return err
		}
		if err := checkInvitationLimits(tx, page.ID, now); err != nil {
			return err
		}
		inv = models.PageInvitation{
			StatusPageID:  page.ID,
			Email:         email,
			RoleID:        role.ID,
			TokenHash:     hashPageInvitationToken(token),
			ExpiresAt:     now.Add(pageInvitationTTL),
			InvitedByID:   getUserID(c),
			InvitedUserID: target.ID,
		}
		if err := tx.Create(&inv).Error; err != nil {
			if isUniqueViolation(err) {
				// A concurrent call created the address first (unique index
				// on (page, address)): treat as "unchanged", no mail.
				return errInvitationUnchanged
			}
			return err
		}
		// Audit entry in the same transaction: The daily limit counts these
		// entries; writing later would let a second transaction between
		// commit and audit see the old counter. If it fails, nothing is created.
		// Audit without address and without account reference: The entry is the same for known and
		// unknown addresses (forensics via count and time).
		audit = newAuditEntry(c, auditInvitationCreated, "config", "StatusPage", strconv.Itoa(int(page.ID)), page.Name,
			fmt.Sprintf("invitation_id=%d role_id=%d", inv.ID, role.ID))
		return tx.Create(&audit).Error
	})
	if errors.Is(err, errInvitationUnchanged) {
		// Same response as when creating, but nothing changed, no mail.
		c.JSON(http.StatusOK, gin.H{"message": pageUserEmailAcceptedMsg})
		return
	}
	if errors.Is(err, errInvitationLimit) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many open invitations for this page", "code": codeInvitationLimitReached})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Einladen", "code": "server_error"})
		return
	}
	logAuditLine(c, audit)
	if target.ID != 0 {
		h.sendPageInvitationEmail(target, page, role, token)
	}
	c.JSON(http.StatusOK, gin.H{"message": pageUserEmailAcceptedMsg})
}

var (
	errInvitationLimit     = errors.New("invitation limit reached")
	errInvitationUnchanged = errors.New("invitation unchanged")
)

// replaceablePendingInvitation checks the lock for (page, address).
// Review B1: The 30-minute lock is tied to the address, not to
// the open row. Revoked and declined invitations therefore remain
// as a soft-deleted row ("tombstone") until the lock expires and
// count here (Unscoped); otherwise DELETE + new POST lifts the lock and
// produces arbitrarily many mails. The key is the already normalized
// address (trim/lowercase), always restricted to status_page_id.
//
// Result: no row -> nil; newest row younger than
// pageInvitationResendAfter (open or tombstone) or open invitation with
// a role the caller does not cover -> errInvitationUnchanged; otherwise
// all rows of the address are permanently deleted (old link invalid) and
// nil is returned. DB errors go upward unchanged (fail closed).
func replaceablePendingInvitation(tx *gorm.DB, pageID uint, email, actorPerms string, now time.Time) error {
	var rows []models.PageInvitation
	if err := tx.Unscoped().Where("status_page_id = ? AND email = ?", pageID, email).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if now.Sub(rows[0].CreatedAt) < pageInvitationResendAfter {
		return errInvitationUnchanged
	}
	for _, old := range rows {
		if old.DeletedAt.Valid {
			continue // Tombstone: no role left to protect
		}
		var oldRole models.Role
		// Review F4: Only "role deleted" makes the old invitation replaceable;
		// every other error aborts (fail closed), otherwise a DB error would
		// let a stronger invitation be overwritten.
		if err := tx.First(&oldRole, old.RoleID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			continue
		}
		if !roleCoveredByActor(actorPerms, oldRole) {
			return errInvitationUnchanged
		}
	}
	return tx.Unscoped().Where("status_page_id = ? AND email = ?", pageID, email).
		Delete(&models.PageInvitation{}).Error
}

// checkInvitationLimits: at most maxPendingPageInvitations open and
// maxPageInvitationsPerDay newly created (counted in the audit log, CreatedAt in
// Unix seconds) per page.
func checkInvitationLimits(tx *gorm.DB, pageID uint, now time.Time) error {
	var open int64
	if err := tx.Model(&models.PageInvitation{}).Where("status_page_id = ?", pageID).Count(&open).Error; err != nil {
		return err
	}
	if open >= maxPendingPageInvitations {
		return errInvitationLimit
	}
	var today int64
	if err := tx.Model(&models.AuditLog{}).
		Where("action = ? AND target_type = ? AND target_id = ? AND created_at > ?", auditInvitationCreated, "StatusPage", strconv.Itoa(int(pageID)), now.Add(-24*time.Hour).Unix()).
		Count(&today).Error; err != nil {
		return err
	}
	if today >= maxPageInvitationsPerDay {
		return errInvitationLimit
	}
	return nil
}

// deletePageInvitationsOfUser (security review): when deleting an
// account, remove all invitations to its address or to the account as well as all
// invitations issued by it.
func deletePageInvitationsOfUser(tx *gorm.DB, u models.User) error {
	return tx.Unscoped().Where("email = ? OR invited_user_id = ? OR invited_by_id = ?", strings.ToLower(strings.TrimSpace(u.Email)), u.ID, u.ID).
		Delete(&models.PageInvitation{}).Error
}

// lockAccountForDeletion: entry point of every account deletion
// (DeleteUserHandler, deleteAccount). First delete the account's invitations,
// then lock the user row with FOR UPDATE; only AFTER that may
// roles, tags, etc. be removed. Reasons:
//   - Accepting (AcceptPageInvitationHandler) deletes its invitation row and
//     then locks the user row. Anyone who removes the roles before this lock
//     deletes nothing, and the accept commits its role afterwards: orphan on the soft-
//     deleted account or FK error (500) on the hard delete in me_delete.
//     With the lock both paths serialize; the later role deletion sees
//     the committed role (READ COMMITTED).
//   - The order invitation -> user is that of accepting. The reverse
//     (user first) would have the deletion hold the user lock and wait for
//     the invitation row, which the accept holds and with which it waits for
//     the user lock: deadlock. If the deletion waits for the invitation row,
//     it does not yet hold a user lock.
//
// Errors (also account no longer there) abort the transaction: fail closed.
func lockAccountForDeletion(tx *gorm.DB, u models.User) error {
	if err := deletePageInvitationsOfUser(tx, u); err != nil {
		return err
	}
	return lockLiveUser(tx, u.ID)
}

// lockLiveUser locks the user row with FOR UPDATE and returns
// gorm.ErrRecordNotFound if the account does not (or no longer) exist, i.e. also
// after a concurrently committed deletion (GORM deleted_at filter;
// under READ COMMITTED Postgres rechecks the row after waiting).
//
// Anyone who creates or changes roles on an account (applyUserUpdate,
// AddStatusPageUserHandler) calls this FIRST in their transaction. Without the
// lock the writer would at most wait for role rows that the deletion
// has already removed, and would still create a role on the deleted account
// afterwards (response 200). Lock order: invitation row (accept/deletion only)
// -> user row -> role rows. These writers hold nothing before the
// user lock (no invitation or role row), so they queue up
// before accept and deletion and cannot form a cycle.
func lockLiveUser(tx *gorm.DB, userID uint) error {
	var locked models.User
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&locked, userID).Error
}

// pruneExpiredInvitations deletes expired invitations of all pages as well as
// tombstones of revoked/declined invitations whose lock has ended
// (review B1; idempotent, runs in the maintenance tick).
func pruneExpiredInvitations(db *gorm.DB, now time.Time) (int64, error) {
	res := db.Unscoped().
		Where("expires_at < ? OR (deleted_at IS NOT NULL AND created_at < ?)", now, now.Add(-pageInvitationResendAfter)).
		Delete(&models.PageInvitation{})
	return res.RowsAffected, res.Error
}

// pageInvitationDTO: open invitation in page administration (without token).
type pageInvitationDTO struct {
	ID        uint      `json:"ID"`
	Email     string    `json:"Email"`
	RoleID    uint      `json:"RoleID"`
	RoleName  string    `json:"RoleName"`
	ExpiresAt time.Time `json:"ExpiresAt"`
	CreatedAt time.Time `json:"CreatedAt"`
}

// GET /api/admin/pages/:page_id/invitations: open, valid invitations of the page.
func (h *Handler) GetPageInvitationsHandler(c *gin.Context) {
	pageID, err := strconv.Atoi(c.Param("page_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	var invs []models.PageInvitation
	if err := h.DB.Where("status_page_id = ? AND expires_at > ?", pageID, time.Now()).Order("created_at desc").Find(&invs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Laden", "code": "server_error"})
		return
	}
	roleNames := map[uint]string{}
	var roleIDs []uint
	for _, inv := range invs {
		roleIDs = append(roleIDs, inv.RoleID)
	}
	if len(roleIDs) > 0 {
		var roles []models.Role
		// H5: An error while loading the role names is a 500, not a
		// list with empty names.
		if err := h.DB.Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Laden", "code": "server_error"})
			return
		}
		for _, r := range roles {
			roleNames[r.ID] = r.Name
		}
	}
	out := make([]pageInvitationDTO, 0, len(invs))
	for _, inv := range invs {
		out = append(out, pageInvitationDTO{ID: inv.ID, Email: inv.Email, RoleID: inv.RoleID, RoleName: roleNames[inv.RoleID], ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt})
	}
	c.JSON(http.StatusOK, out)
}

// DELETE /api/admin/pages/:page_id/invitations/:id: revoke an invitation.
// As when removing a member: An invitation with a stronger role is revoked
// only by someone who covers that role themselves (or is a global admin).
func (h *Handler) RevokePageInvitationHandler(c *gin.Context) {
	pageID, err1 := strconv.Atoi(c.Param("page_id"))
	invID, err2 := strconv.Atoi(c.Param("id"))
	if err1 != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id", "code": "invalid_id"})
		return
	}
	var inv models.PageInvitation
	if err := h.DB.Where("id = ? AND status_page_id = ?", invID, pageID).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondInvitationNotFound(c)
			return
		}
		// Review F2: DB errors are not "not found".
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Zurückziehen", "code": "server_error"})
		return
	}
	if !h.isEffectiveGlobalAdmin(c) {
		// F6: A deleted role (not found) protects nothing
		// anymore, the invitation is moot anyway: revoking allowed.
		// Any other read error is a 500 (fail closed, nothing changed).
		var role models.Role
		if err := h.DB.First(&role, inv.RoleID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Zurückziehen", "code": "server_error"})
				return
			}
		} else if !roleCoveredByActor(h.actorPagePermissions(c, uint(pageID)), role) {
			abortRoleExceedsOwn(c)
			return
		}
	}
	// Soft delete (review B1): The row remains as a tombstone for the lock.
	res := h.DB.Delete(&inv)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Zurückziehen", "code": "server_error"})
		return
	}
	if res.RowsAffected == 0 {
		// O1: A concurrent accept/decline was faster; nothing revoked, no audit.
		respondInvitationNotFound(c)
		return
	}
	h.RecordAudit(c, "statuspage.invitation_revoked", "config", "StatusPage", strconv.Itoa(pageID), "", fmt.Sprintf("invitation_id=%d role_id=%d", inv.ID, inv.RoleID))
	c.JSON(http.StatusOK, gin.H{"message": "Zurückgezogen"})
}

type pageInvitationTokenRequest struct {
	Token string `json:"token"`
}

// loadOwnInvitation reads the token and logged-in account and returns the
// invitation if it belongs to exactly this account. Otherwise the response is
// already sent (400 without token, 404 for unknown tokens AND foreign accounts).
func (h *Handler) loadOwnInvitation(c *gin.Context) (models.PageInvitation, models.User, bool) {
	var in pageInvitationTokenRequest
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation token missing", "code": codeInvitationTokenMissing})
		return models.PageInvitation{}, models.User{}, false
	}
	var inv models.PageInvitation
	var u models.User
	invErr := h.DB.Where("token_hash = ?", hashPageInvitationToken(strings.TrimSpace(in.Token))).First(&inv).Error
	userErr := h.DB.First(&u, getUserID(c)).Error
	// Review F2: Only "not found" is the neutral 404; any other DB error
	// is a 500 and changes nothing.
	if nonNotFound(invErr) != nil || nonNotFound(userErr) != nil {
		respondInvitationServerError(c)
		return models.PageInvitation{}, models.User{}, false
	}
	if invErr != nil || userErr != nil || inv.InvitedUserID != u.ID || !strings.EqualFold(strings.TrimSpace(u.Email), inv.Email) {
		respondInvitationNotFound(c)
		return models.PageInvitation{}, models.User{}, false
	}
	return inv, u, true
}

func respondInvitationServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Laden der Einladung", "code": "server_error"})
}

// nonNotFound returns err unless it is "not found" (then nil).
func nonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func respondInvitationNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found", "code": codeInvitationNotFound})
}

// invitationContext checks an own invitation for expiry, page and role.
// Expired or moot invitations are deleted in the process.
func (h *Handler) invitationContext(c *gin.Context, inv models.PageInvitation) (models.StatusPage, models.Role, bool) {
	var page models.StatusPage
	var role models.Role
	if inv.ExpiresAt.Before(time.Now()) {
		h.DB.Unscoped().Delete(&inv) //nolint:errcheck // Aufräumen ist Best Effort, die Antwort bleibt 410.
		c.JSON(http.StatusGone, gin.H{"error": "Invitation expired", "code": codeInvitationExpired})
		return page, role, false
	}
	// Review F2: Only "not found" (page/role/inviter gone or rights
	// lost) makes the invitation moot. Any other DB error
	// does not consume it: 500, no write access, no audit.
	gone, err := h.invitationSubjectGone(h.DB, &page, &role, inv)
	if err != nil {
		respondInvitationServerError(c)
		return page, role, false
	}
	if gone {
		h.DB.Delete(&inv) //nolint:errcheck // gegenstandslose Einladung (Grabstein, Review B1), Antwort bleibt 404.
		respondInvitationNotFound(c)
		return page, role, false
	}
	return page, role, true
}

// invitationSubjectGone loads page and role and reports whether the invitation
// is moot (true) or whether a DB error occurred (error). db is
// h.DB for the pre-check and the transaction when accepting (F5:
// role and inviter's rights are re-read there).
func (h *Handler) invitationSubjectGone(db *gorm.DB, page *models.StatusPage, role *models.Role, inv models.PageInvitation) (bool, error) {
	if err := db.First(page, inv.StatusPageID).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), nonNotFound(err)
	}
	if err := db.First(role, inv.RoleID).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), nonNotFound(err)
	}
	globalOnly, err := h.globalOnlyRoleIDs(db, []uint{role.ID})
	if err != nil {
		return false, err // O2: fail closed
	}
	if globalOnly[role.ID] {
		return true, nil
	}
	may, err := h.inviterMayStillAssign(db, inv, *role)
	return !may, err
}

// inviterMayStillAssign: The inviter is still a global admin or has on the
// page a role that may add accounts and covers the invitation role.
// Whoever lost their rights has their open invitations lapse.
//
// Review F2: (false, nil) means "rights lost or account/role not
// found"; any other DB error is returned as an error (fail closed).
func (h *Handler) inviterMayStillAssign(db *gorm.DB, inv models.PageInvitation, role models.Role) (bool, error) {
	var inviter models.User
	if err := db.First(&inviter, inv.InvitedByID).Error; err != nil {
		return false, nonNotFound(err)
	}
	if inviter.IsGlobalAdmin {
		return true, nil
	}
	var ur models.UserStatusPageRole
	if err := db.Preload("Role").Where(whereUserIDAndStatusPageID, inviter.ID, inv.StatusPageID).First(&ur).Error; err != nil {
		return false, nonNotFound(err)
	}
	return permissions.Covers(ur.Role.Permissions, "pages:create") && roleCoveredByActor(ur.Role.Permissions, role), nil
}

// accountMayJoinByInvitation: the same limits as for direct attaching
// by non-global admins. Membership on a second
// page would otherwise block the SSO login of the first (errOIDCOtherPageRole).
func (h *Handler) accountMayJoinByInvitation(c *gin.Context, u models.User, pageID uint) bool {
	return middleware.SSOPageID(c) == 0 && !u.IsGlobalAdmin && u.SSOProvider == "" && u.SSOSubject == "" &&
		!h.hasRoleOnOtherPage(u.ID, pageID) && !h.hasSSOIdentityOnOtherPage(u.ID, pageID)
}

// POST /api/invitations/preview {token}: page and role of the own invitation.
// Does not consume the token (mail gateways with link prefetching).
func (h *Handler) PreviewPageInvitationHandler(c *gin.Context) {
	inv, _, ok := h.loadOwnInvitation(c)
	if !ok {
		return
	}
	page, role, ok := h.invitationContext(c, inv)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"page_name": page.Name, "role_name": role.Name, "expires_at": inv.ExpiresAt})
}

// POST /api/invitations/accept {token}: accept an invitation (one-time).
func (h *Handler) AcceptPageInvitationHandler(c *gin.Context) {
	inv, u, ok := h.loadOwnInvitation(c)
	if !ok {
		return
	}
	page, role, ok := h.invitationContext(c, inv)
	if !ok {
		return
	}
	if !h.accountMayJoinByInvitation(c, u, page.ID) {
		respondInvitationAccountConflict(c)
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// One-time: Only whoever actually deletes the row creates the membership.
		// Review F1: only the open row. Without "deleted_at IS NULL" the
		// Unscoped DELETE would also hit the tombstone of a concurrently revoked
		// or declined invitation (RowsAffected = 1) and the accept would go through.
		res := tx.Unscoped().Where("id = ? AND deleted_at IS NULL", inv.ID).Delete(&models.PageInvitation{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		// H3, review finding 1: FIRST read the user row with FOR UPDATE
		// (including the global admin/SSO check). Every INSERT into
		// user_status_page_roles takes FOR KEY SHARE on
		// the user row because of the foreign key; that collides with FOR UPDATE and makes this lock the
		// real serialization point per account. A FOR UPDATE on the role rows
		// alone locks nothing for an account without a role: two concurrently
		// accepted invitations (page A and B) would both see "no role". Only
		// after the lock is the one-page limit checked.
		var fresh models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, u.ID).Error; err != nil {
			return err
		}
		if fresh.IsGlobalAdmin || fresh.SSOProvider != "" || fresh.SSOSubject != "" {
			return errInvitationOtherPage
		}
		// Security review: Re-check the one-page limit in the transaction
		// (after the user lock; the role rows are additionally locked under
		// Postgres) so that a concurrently granted role on
		// another page does not slip through.
		if err := ensureNoRoleOnOtherPage(tx, u.ID, page.ID); err != nil {
			return err
		}
		// F5: Page, role and the inviter's rights count only if
		// they still exist now (before that only a pre-check, small time window).
		var txPage models.StatusPage
		var txRole models.Role
		gone, err := h.invitationSubjectGone(tx, &txPage, &txRole, inv)
		if err != nil {
			return err
		}
		if gone {
			return gorm.ErrRecordNotFound
		}
		var existing models.UserStatusPageRole
		err = tx.Where(whereUserIDAndStatusPageID, u.ID, page.ID).First(&existing).Error
		switch {
		case err == nil && existing.JoinedVia == models.JoinedViaAutoJoin:
			// Accepting is the consent an auto-join lacks: the membership becomes
			// an assigned one with the invited role (custom domain login).
			return tx.Model(&existing).Updates(map[string]any{"role_id": role.ID, "joined_via": models.JoinedViaAssigned}).Error
		case err == nil:
			return errInvitationAlreadyMember
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		return tx.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error
	})
	switch {
	case err == nil:
	case errors.Is(err, gorm.ErrRecordNotFound):
		respondInvitationNotFound(c)
		return
	case errors.Is(err, errInvitationOtherPage):
		respondInvitationAccountConflict(c)
		return
	case errors.Is(err, errInvitationAlreadyMember) || isUniqueViolation(err):
		// The invitation is consumed; the existing role remains unchanged.
		h.DB.Unscoped().Where("id = ?", inv.ID).Delete(&models.PageInvitation{}) //nolint:errcheck // Best Effort
		c.JSON(http.StatusConflict, gin.H{"error": "Already a member of this page", "code": codeInvitationAlreadyMember})
		return
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Annehmen", "code": "server_error"})
		return
	}
	pid := strconv.Itoa(int(page.ID))
	h.RecordAudit(c, "statuspage.invitation_accepted", "config", "StatusPage", pid, page.Name, fmt.Sprintf("invitation_id=%d user_id=%d role_id=%d", inv.ID, u.ID, role.ID))
	h.RecordAudit(c, "statuspage.user_role_assigned", "config", "StatusPage", pid, "", fmt.Sprintf("user_id=%d role_id=%d", u.ID, role.ID))
	c.JSON(http.StatusOK, gin.H{"page_name": page.Name, "role_name": role.Name})
}

var (
	errInvitationAlreadyMember = errors.New("already member")
	errInvitationOtherPage     = errors.New("role or sso identity on other page")
)

func respondInvitationAccountConflict(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": "This account cannot join the page by invitation; ask a global admin", "code": codeInvitationAccountConflict})
}

// ensureNoRoleOnOtherPage: roles or page SSO links of the account on
// other pages in the running transaction; the role rows are locked under
// Postgres (SQLite omits FOR UPDATE and serializes writers).
func ensureNoRoleOnOtherPage(tx *gorm.DB, userID, pageID uint) error {
	var roles []models.UserStatusPageRole
	if err := tx.Where("user_id = ?", userID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Find(&roles).Error; err != nil {
		return err
	}
	for _, r := range roles {
		if r.StatusPageID != pageID {
			return errInvitationOtherPage
		}
	}
	var n int64
	if err := pageSSOIdentitiesElsewhere(tx, userID, pageID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errInvitationOtherPage
	}
	return nil
}

// POST /api/invitations/decline {token}: decline an invitation (deletes it, even
// if it has expired).
func (h *Handler) DeclinePageInvitationHandler(c *gin.Context) {
	inv, u, ok := h.loadOwnInvitation(c)
	if !ok {
		return
	}
	// Soft delete (review B1): Declining does not lift the lock either.
	if err := h.DB.Delete(&inv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Ablehnen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "statuspage.invitation_declined", "config", "StatusPage", strconv.Itoa(int(inv.StatusPageID)), "", fmt.Sprintf("invitation_id=%d user_id=%d", inv.ID, u.ID))
	c.JSON(http.StatusOK, gin.H{"message": "Abgelehnt"})
}

// ── Mail ─────────────────────────────────────────────────────────────────────

const (
	pageInvitationSubjectDE = "Einladung zur Statusseite "
	pageInvitationSubjectEN = "Invitation to the status page "
)

const rawPageInvitationEmailTemplate = `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">
	<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">
		<div style="padding:24px;">
			<h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">%s</h2>
			<p style="margin:0 0 16px;color:#57616f;font-size:14px;line-height:1.5;">%s</p>
			<a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">%s</a>
			<p style="margin:20px 0 0;color:#8b95a3;font-size:12px;">%s</p>
		</div>
	</div>
</div>`

// Single-line like pageOrderConfirmEmailTemplate (CR/LF guard).
var pageInvitationEmailTemplate = strings.NewReplacer("\n", "", "\t", "").Replace(rawPageInvitationEmailTemplate)

// buildPageInvitationMail returns subject and HTML body in the language of the
// account (en, otherwise de). Page and role names are escaped.
func buildPageInvitationMail(lang, pageName, roleName, link string) (string, string) {
	p, r := html.EscapeString(pageName), html.EscapeString(roleName)
	if strings.HasPrefix(strings.ToLower(lang), "en") {
		return pageInvitationSubjectEN + pageName, fmt.Sprintf(pageInvitationEmailTemplate,
			"You have been invited",
			fmt.Sprintf("You have been invited to the status page &quot;%s&quot; with the role &quot;%s&quot;. Sign in with this email address and accept or decline the invitation.", p, r),
			link, "Review invitation",
			"The link is valid for 7 days and can be used once. If you do not expect this invitation, simply decline it or ignore this email.")
	}
	return pageInvitationSubjectDE + pageName, fmt.Sprintf(pageInvitationEmailTemplate,
		"Du wurdest eingeladen",
		fmt.Sprintf("Du wurdest zur Statusseite &quot;%s&quot; mit der Rolle &quot;%s&quot; eingeladen. Melde dich mit dieser E-Mail-Adresse an und nimm die Einladung an oder lehne sie ab.", p, r),
		link, "Einladung ansehen",
		"Der Link ist 7 Tage gültig und nur einmal verwendbar. Wenn du diese Einladung nicht erwartest, lehne sie ab oder ignoriere diese E-Mail.")
}

// sendPageInvitationEmail builds the mail synchronously and sends it via the
// bounded background queue (mail_queue.go), so that the response takes equally long for
// addresses with and without an account and a failed
// mail server does not pile up unboundedly many sending goroutines. Neither token
// nor link end up in the log.
func (h *Handler) sendPageInvitationEmail(u models.User, page models.StatusPage, role models.Role, token string) {
	// The address matches (apart from upper/lower case) the checked
	// input from invitePageUser; the CR/LF guard below additionally covers it.
	recipient := strings.TrimSpace(u.Email)
	smtpCfg := loadSMTPConfig()
	smtpCfg.applyPageSender(page)
	// The token is in the URL FRAGMENT, not in the query. A
	// fragment never goes to the server (no Caddy/Cloudflare/proxy logs) and
	// never leaves in the Referer; in the query it would be in logs and browser history.
	// The frontend still also reads the old query (mails already sent).
	link := getFrontendURL() + "/invitation#token=" + url.QueryEscape(token)
	subject, body := buildPageInvitationMail(u.PreferredLanguage, page.Name, role.Name, link)
	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(body) || hasHeaderInjectionChars(smtpCfg.From) || hasHeaderInjectionChars(recipient) {
		slog.Error("Skipping page invitation mail: subject/body/sender contain invalid CR/LF characters", "page_id", page.ID, "user_id", u.ID)
		return
	}
	// The page name is in the subject; umlauts need
	// RFC 2047 encoding (the CR/LF guard above checks the plain text beforehand).
	msg := smtpCfg.fromHeaders() +
		"To: " + recipient + "\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\n" +
		"MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n" + body
	pageID := strconv.FormatUint(uint64(page.ID), 10)
	h.queueMail("page_invitation", func() {
		sent, err := h.deliverMail(smtpCfg, recipient, []byte(msg), mailAuditTarget{Category: "config", TargetType: "StatusPage", TargetID: pageID, TargetName: page.Name})
		if err != nil {
			slog.Error("Failed to send page invitation mail", "page_id", page.ID, "user_id", u.ID, "smtp_addr", smtpCfg.addr(), "error", err)
			h.RecordSystemAudit("statuspage.invitation_email_failed", "config", "StatusPage", pageID, page.Name,
				fmt.Sprintf("user_id=%d smtp_addr=%s", u.ID, smtpCfg.addr()))
			return
		}
		if sent {
			slog.Info("Page invitation mail sent", "page_id", page.ID, "user_id", u.ID)
		}
	}, mailPageKey(page.ID))
}
