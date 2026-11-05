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
// Einladungsablauf für Seitenbenutzer.
//
// Bedrohung: Ein Seitenadmin hängte ein fremdes, vorhandenes Konto
// per E-Mail direkt an seine Seite. Das Konto bekam ungefragt Staff-Mails und
// konnte die SSO-Anmeldung seiner eigentlichen Seite verlieren.
//
// Ablauf jetzt (nur Nicht-Global-Admins; Global Admins ordnen weiter direkt zu):
//  1. POST /api/admin/pages/:page_id/users {email, role_id} legt für jede Adresse,
//     die noch kein Mitglied ist, eine Einladung an (gleiche Antwort und gleiche
//     Liste, ob ein Konto existiert oder nicht). Nur ein vorhandenes, anhängbares
//     Konto bekommt eine Mail; sie geht im Hintergrund raus. Die Kontosuche führt für
// bekannte und unbekannte Adressen dieselben Abfragen aus; eine
//     exakt gleiche Laufzeit wird nicht zugesichert.
//  2. Die Mail enthält einen Einmal-Link mit 128 Bit Zufall; gespeichert ist nur
//     der SHA-256. Gültig pageInvitationTTL.
//  3. Annehmen/Ablehnen (POST /api/invitations/{preview,accept,decline}) nur
//     angemeldet UND mit dem Konto, dessen Adresse eingeladen wurde. Jede
//     Abweichung (falscher Token, anderes Konto) ergibt dieselbe 404.
// 4. Beim Annehmen gelten dieselben Grenzen wie: Global-Admin-Konten,
//     Konten mit SSO-Verknüpfung oder Rolle auf einer anderen Seite und
//     Seiten-SSO-Sitzungen nehmen nicht selbst an (409), das bleibt dem Global
//     Admin. Außerdem muss der Einladende die Rolle noch vergeben dürfen.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// pageInvitationTTL: so lange gilt eine Einladung.
	pageInvitationTTL = 7 * 24 * time.Hour
	// maxPendingPageInvitations begrenzt offene Einladungen je Seite (Schutz vor
	// Mail-Flut über viele Adressen; gezählt werden alle, auch ohne Konto).
	maxPendingPageInvitations = 100
	// Security-Review Eine offene Einladung derselben Adresse, die
	// jünger ist als pageInvitationResendAfter, bleibt unverändert (keine neue
	// Mail, kein neuer Token). Höchstens maxPageInvitationsPerDay neue
	// Einladungen je Seite in 24 Stunden. Beide Regeln gelten für jede Adresse,
	// mit oder ohne Konto (kein Orakel).
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

// pageUserEmailAcceptedMsg ist die einheitliche Antwort beim Hinzufügen per
// E-Mail (Einladung angelegt, Rolle eines Mitglieds geändert oder nichts
// geändert), gleich für bekannte und unbekannte Adressen.
const pageUserEmailAcceptedMsg = "Wenn ein Konto mit dieser E-Mail-Adresse existiert, erhält es eine Einladung; bestehende Mitglieder erhalten die gewählte Rolle"

func hashPageInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// invitePageUser legt (oder erneuert) die Einladung für email auf der Seite an.
// target ist das gefundene, anhängbare Konto (ID 0: keins, dann keine Mail),
// actorPerms die Rechte des Aufrufers auf der Seite. Die Antwort ist in jedem
// Fall dieselbe neutrale 200 (außer bei Formfehlern und den Obergrenzen, die
// ebenfalls nicht vom Konto abhängen).
func (h *Handler) invitePageUser(c *gin.Context, page models.StatusPage, role models.Role, email string, target models.User, actorPerms string) {
	email = strings.ToLower(strings.TrimSpace(email))
	// Nur nackte Adressen: Sie landen im To:-Header und in der Liste. Die
	// Prüfung hängt nicht vom Konto ab (kein Orakel).
	if !isPlainEmailAddress(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email address", "code": "email_invalid"})
		return
	}
	now := time.Now()
	token := rand.Text()
	var inv models.PageInvitation
	var audit models.AuditLog
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Review F3: Einladungen einer Seite laufen nacheinander. Die
		// Seitenzeile wird unter Postgres gesperrt (SQLite lässt die Klausel weg und
		// serialisiert Schreiber ohnehin); erst nach dem Commit der ersten sieht die
		// zweite Sperre und Zähler. Fehler gehen nach oben.
		// Re-Review W1: FOR NO KEY UPDATE statt FOR UPDATE. Der INSERT in
		// user_status_page_roles (Annehmen) braucht wegen des Fremdschlüssels FOR KEY
		// SHARE auf die Seitenzeile; FOR UPDATE kollidiert damit und führte mit einem
		// gleichzeitigen Neu-Einladen derselben Adresse (wartet auf die vom Annehmen
		// gelöschte Einladungszeile) zum Deadlock (40P01). NO KEY UPDATE sperrt
		// Einladende gegeneinander, nicht gegen KEY SHARE.
		if err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Select("id").
			Where("id = ?", page.ID).Take(&models.StatusPage{}).Error; err != nil {
			return err
		}
		// Abgelaufene Einladungen der Seite aufräumen.
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
				// Ein gleichzeitiger Aufruf hat die Adresse zuerst angelegt (Unique-Index
				// auf (Seite, Adresse)): wie "unverändert" behandeln, keine Mail.
				return errInvitationUnchanged
			}
			return err
		}
		// Audit-Eintrag in derselben Transaktion: Die Tagesgrenze zählt diese
		// Einträge, ein späteres Schreiben ließe eine zweite Transaktion zwischen
		// Commit und Audit den alten Zähler sehen. Scheitert er, entsteht nichts.
		// Audit ohne Adresse und ohne Konto-Bezug: Der Eintrag ist für bekannte und
		// unbekannte Adressen gleich (Forensik über Anzahl und Zeitpunkt).
		audit = newAuditEntry(c, auditInvitationCreated, "config", "StatusPage", strconv.Itoa(int(page.ID)), page.Name,
			fmt.Sprintf("invitation_id=%d role_id=%d", inv.ID, role.ID))
		return tx.Create(&audit).Error
	})
	if errors.Is(err, errInvitationUnchanged) {
		// Gleiche Antwort wie beim Anlegen, aber nichts geändert, keine Mail.
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

// replaceablePendingInvitation prüft die Sperre für (Seite, Adresse).
// Review B1: Die 30-Minuten-Sperre hängt an der Adresse, nicht an
// der offenen Zeile. Zurückgezogene und abgelehnte Einladungen bleiben deshalb
// als soft-gelöschte Zeile ("Grabstein") bis zum Ablauf der Sperre stehen und
// zählen hier mit (Unscoped); sonst hebt DELETE + neues POST die Sperre auf und
// erzeugt beliebig viele Mails. Der Schlüssel ist die schon normalisierte
// Adresse (Trim/Lowercase) und immer auf status_page_id begrenzt.
//
// Ergebnis: keine Zeile -> nil; jüngste Zeile jünger als
// pageInvitationResendAfter (offen oder Grabstein) oder offene Einladung mit
// einer Rolle, die der Aufrufer nicht abdeckt -> errInvitationUnchanged; sonst
// werden alle Zeilen der Adresse endgültig gelöscht (alter Link ungültig) und
// nil geliefert. DB-Fehler gehen unverändert nach oben (fail closed).
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
			continue // Grabstein: keine Rolle mehr zu schützen
		}
		var oldRole models.Role
		// Review F4: Nur "Rolle gelöscht" macht die alte Einladung ersetzbar;
		// jeder andere Fehler bricht ab (fail closed), sonst ließe ein DB-Fehler
		// eine stärkere Einladung überschreiben.
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

// checkInvitationLimits: höchstens maxPendingPageInvitations offen und
// maxPageInvitationsPerDay neu angelegt (gezählt im Audit-Log, CreatedAt in
// Unix-Sekunden) je Seite.
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

// deletePageInvitationsOfUser (Security-Review): beim Löschen eines
// Kontos alle Einladungen an seine Adresse oder an das Konto sowie alle von
// ihm ausgesprochenen Einladungen entfernen.
func deletePageInvitationsOfUser(tx *gorm.DB, u models.User) error {
	return tx.Unscoped().Where("email = ? OR invited_user_id = ? OR invited_by_id = ?", strings.ToLower(strings.TrimSpace(u.Email)), u.ID, u.ID).
		Delete(&models.PageInvitation{}).Error
}

// lockAccountForDeletion: Einstieg jeder Konto-Löschung
// (DeleteUserHandler, deleteAccount). Zuerst die Einladungen des Kontos
// löschen, danach die Benutzerzeile mit FOR UPDATE sperren; erst DANACH dürfen
// Rollen, Tags usw. entfernt werden. Gründe:
//   - Die Annahme (AcceptPageInvitationHandler) löscht ihre Einladungszeile und
//     sperrt dann die Benutzerzeile. Wer die Rollen vor dieser Sperre entfernt,
//     löscht nichts, die Annahme committet danach ihre Rolle: Waise am weich
//     gelöschten Konto bzw. FK-Fehler (500) beim harten Löschen in me_delete.
//     Mit der Sperre serialisieren beide Wege; die spätere Rollen-Löschung sieht
//     die committete Rolle (READ COMMITTED).
//   - Reihenfolge Einladung -> Benutzer ist die der Annahme. Umgekehrt
//     (Benutzer zuerst) hielte die Löschung die Benutzersperre und wartete auf
//     die Einladungszeile, die die Annahme hält und mit der sie auf die
//     Benutzersperre wartet: Deadlock. Wartet die Löschung auf die Einladungszeile,
//     hält sie noch keine Benutzersperre.
//
// Fehler (auch Konto nicht mehr da) brechen die Transaktion ab: fail closed.
func lockAccountForDeletion(tx *gorm.DB, u models.User) error {
	if err := deletePageInvitationsOfUser(tx, u); err != nil {
		return err
	}
	return lockLiveUser(tx, u.ID)
}

// lockLiveUser sperrt die Benutzerzeile mit FOR UPDATE und liefert
// gorm.ErrRecordNotFound, wenn das Konto nicht (mehr) existiert, also auch
// nach einer gleichzeitig committeten Löschung (deleted_at-Filter von GORM;
// unter READ COMMITTED prüft Postgres die Zeile nach dem Warten neu).
//
// Wer Rollen an einem Konto anlegt oder ändert (applyUserUpdate,
// AddStatusPageUserHandler), ruft das ZUERST in seiner Transaktion auf. Ohne
// Sperre wartete der Schreiber höchstens auf Rollenzeilen, die die Löschung
// schon entfernt hat, und legte danach trotzdem eine Rolle am gelöschten Konto
// an (Antwort 200). Lock-Reihenfolge: Einladungszeile (nur Annahme/Löschung)
// -> Benutzerzeile -> Rollenzeilen. Diese Schreiber halten vor der
// Benutzersperre nichts (keine Einladungs- oder Rollenzeile), reihen sich also
// vor Annahme und Löschung ein und können keinen Zyklus bilden.
func lockLiveUser(tx *gorm.DB, userID uint) error {
	var locked models.User
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&locked, userID).Error
}

// pruneExpiredInvitations löscht abgelaufene Einladungen aller Seiten sowie
// Grabsteine zurückgezogener/abgelehnter Einladungen, deren Sperre vorbei ist
// (Review B1; idempotent, läuft im Wartungs-Tick).
func pruneExpiredInvitations(db *gorm.DB, now time.Time) (int64, error) {
	res := db.Unscoped().
		Where("expires_at < ? OR (deleted_at IS NOT NULL AND created_at < ?)", now, now.Add(-pageInvitationResendAfter)).
		Delete(&models.PageInvitation{})
	return res.RowsAffected, res.Error
}

// pageInvitationDTO: offene Einladung in der Seitenverwaltung (ohne Token).
type pageInvitationDTO struct {
	ID        uint      `json:"ID"`
	Email     string    `json:"Email"`
	RoleID    uint      `json:"RoleID"`
	RoleName  string    `json:"RoleName"`
	ExpiresAt time.Time `json:"ExpiresAt"`
	CreatedAt time.Time `json:"CreatedAt"`
}

// GET /api/admin/pages/:page_id/invitations: offene, gültige Einladungen der Seite.
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
		// H5: Ein Fehler beim Laden der Rollennamen ist ein 500, keine
		// Liste mit leeren Namen.
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

// DELETE /api/admin/pages/:page_id/invitations/:id: Einladung zurückziehen.
// Wie beim Entfernen eines Mitglieds: Eine Einladung mit stärkerer Rolle zieht
// nur zurück, wer diese Rolle selbst abdeckt (oder Global Admin ist).
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
		// Review F2: DB-Fehler sind kein "nicht gefunden".
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Zurückziehen", "code": "server_error"})
		return
	}
	if !h.isEffectiveGlobalAdmin(c) {
		// F6: Eine gelöschte Rolle (nicht gefunden) schützt nichts
		// mehr, die Einladung ist ohnehin gegenstandslos: Zurückziehen erlaubt.
		// Jeder andere Lesefehler ist ein 500 (fail closed, nichts geändert).
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
	// Soft-Delete (Review B1): Die Zeile bleibt als Grabstein für die Sperre.
	res := h.DB.Delete(&inv)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Zurückziehen", "code": "server_error"})
		return
	}
	if res.RowsAffected == 0 {
		// O1: Ein gleichzeitiges Annehmen/Ablehnen war schneller; nichts zurückgezogen, kein Audit.
		respondInvitationNotFound(c)
		return
	}
	h.RecordAudit(c, "statuspage.invitation_revoked", "config", "StatusPage", strconv.Itoa(pageID), "", fmt.Sprintf("invitation_id=%d role_id=%d", inv.ID, inv.RoleID))
	c.JSON(http.StatusOK, gin.H{"message": "Zurückgezogen"})
}

type pageInvitationTokenRequest struct {
	Token string `json:"token"`
}

// loadOwnInvitation liest Token und angemeldetes Konto und liefert die
// Einladung, wenn sie zu genau diesem Konto gehört. Sonst ist die Antwort
// schon gesendet (400 ohne Token, 404 für unbekannte Token UND fremde Konten).
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
	// Review F2: Nur "nicht gefunden" ist die neutrale 404; ein anderer DB-Fehler
	// ist ein 500 und verändert nichts.
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

// nonNotFound liefert err, außer es ist "nicht gefunden" (dann nil).
func nonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func respondInvitationNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found", "code": codeInvitationNotFound})
}

// invitationContext prüft eine eigene Einladung auf Ablauf, Seite und Rolle.
// Abgelaufene oder gegenstandslose Einladungen werden dabei gelöscht.
func (h *Handler) invitationContext(c *gin.Context, inv models.PageInvitation) (models.StatusPage, models.Role, bool) {
	var page models.StatusPage
	var role models.Role
	if inv.ExpiresAt.Before(time.Now()) {
		h.DB.Unscoped().Delete(&inv) //nolint:errcheck // Aufräumen ist Best Effort, die Antwort bleibt 410.
		c.JSON(http.StatusGone, gin.H{"error": "Invitation expired", "code": codeInvitationExpired})
		return page, role, false
	}
	// Review F2: Nur "nicht gefunden" (Seite/Rolle/Einladender weg oder Rechte
	// verloren) macht die Einladung gegenstandslos. Ein anderer DB-Fehler
	// verbraucht sie nicht: 500, kein Schreibzugriff, kein Audit.
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

// invitationSubjectGone lädt Seite und Rolle und meldet, ob die Einladung
// gegenstandslos ist (true) oder ob ein DB-Fehler vorliegt (Fehler). db ist
// h.DB für die Vorprüfung und die Transaktion beim Annehmen (F5:
// Rolle und Rechte des Einladenden werden dort erneut gelesen).
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

// inviterMayStillAssign: Der Einladende ist noch Global Admin oder hat auf der
// Seite eine Rolle, die Konten hinzufügen darf und die Einladungsrolle abdeckt.
// Wer seine Rechte verloren hat, dessen offene Einladungen verfallen.
//
// Review F2: (false, nil) heißt "Rechte verloren oder Konto/Rolle nicht
// gefunden"; jeder andere DB-Fehler kommt als Fehler zurück (fail closed).
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

// accountMayJoinByInvitation: dieselben Grenzen wie beim direkten Anhängen
// durch Nicht-Global-Admins. Die Mitgliedschaft auf einer zweiten
// Seite sperrte sonst die SSO-Anmeldung der ersten (errOIDCOtherPageRole).
func (h *Handler) accountMayJoinByInvitation(c *gin.Context, u models.User, pageID uint) bool {
	return middleware.SSOPageID(c) == 0 && !u.IsGlobalAdmin && u.SSOProvider == "" && u.SSOSubject == "" &&
		!h.hasRoleOnOtherPage(u.ID, pageID) && !h.hasSSOIdentityOnOtherPage(u.ID, pageID)
}

// POST /api/invitations/preview {token}: Seite und Rolle der eigenen Einladung.
// Verbraucht den Token nicht (Mail-Gateways mit Link-Prefetching).
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

// POST /api/invitations/accept {token}: Einladung annehmen (einmalig).
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
		// Einmalig: Nur wer die Zeile tatsächlich löscht, legt die Mitgliedschaft an.
		// Review F1: nur die offene Zeile. Ohne "deleted_at IS NULL" träfe das
		// Unscoped-DELETE auch den Grabstein einer gleichzeitig zurückgezogenen
		// oder abgelehnten Einladung (RowsAffected = 1) und die Annahme ginge durch.
		res := tx.Unscoped().Where("id = ? AND deleted_at IS NULL", inv.ID).Delete(&models.PageInvitation{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		// H3, Review-Fund 1: ZUERST die Benutzerzeile mit FOR UPDATE
		// lesen (samt Global-Admin/SSO-Prüfung). Jeder INSERT in
		// user_status_page_roles nimmt wegen des Fremdschlüssels FOR KEY SHARE auf
		// die Benutzerzeile; das kollidiert mit FOR UPDATE und macht diese Sperre zum
		// echten Serialisierungspunkt je Konto. Ein FOR UPDATE auf die Rollenzeilen
		// allein sperrt bei einem Konto ohne Rolle nichts: Zwei gleichzeitig
		// angenommene Einladungen (Seite A und B) sähen beide "keine Rolle". Erst
		// nach der Sperre wird die Ein-Seiten-Grenze geprüft.
		var fresh models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, u.ID).Error; err != nil {
			return err
		}
		if fresh.IsGlobalAdmin || fresh.SSOProvider != "" || fresh.SSOSubject != "" {
			return errInvitationOtherPage
		}
		// Security-Review Ein-Seiten-Grenze in der Transaktion erneut
		// prüfen (nach der Benutzersperre; die Rollenzeilen werden unter Postgres
		// zusätzlich gesperrt), damit eine gleichzeitig vergebene Rolle auf einer
		// anderen Seite nicht durchrutscht.
		if err := ensureNoRoleOnOtherPage(tx, u.ID, page.ID); err != nil {
			return err
		}
		// F5: Seite, Rolle und Rechte des Einladenden gelten nur, wenn
		// sie auch jetzt noch bestehen (vorher nur Vorprüfung, kleines Zeitfenster).
		var txPage models.StatusPage
		var txRole models.Role
		gone, err := h.invitationSubjectGone(tx, &txPage, &txRole, inv)
		if err != nil {
			return err
		}
		if gone {
			return gorm.ErrRecordNotFound
		}
		var n int64
		if err := tx.Model(&models.UserStatusPageRole{}).Where(whereUserIDAndStatusPageID, u.ID, page.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errInvitationAlreadyMember
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
		// Die Einladung ist verbraucht; die bestehende Rolle bleibt unverändert.
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

// ensureNoRoleOnOtherPage: Rollen bzw. Seiten-SSO-Verknüpfungen des Kontos auf
// anderen Seiten in der laufenden Transaktion; die Rollenzeilen werden unter
// Postgres gesperrt (SQLite lässt FOR UPDATE weg und serialisiert Schreiber).
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
	if err := tx.Model(&models.UserSSOIdentity{}).Where("user_id = ? AND status_page_id <> ?", userID, pageID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errInvitationOtherPage
	}
	return nil
}

// POST /api/invitations/decline {token}: Einladung ablehnen (löscht sie, auch
// wenn sie abgelaufen ist).
func (h *Handler) DeclinePageInvitationHandler(c *gin.Context) {
	inv, u, ok := h.loadOwnInvitation(c)
	if !ok {
		return
	}
	// Soft-Delete (Review B1): Ablehnen hebt die Sperre ebenfalls nicht auf.
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

// Einzeilig wie pageOrderConfirmEmailTemplate (CR/LF-Guard).
var pageInvitationEmailTemplate = strings.NewReplacer("\n", "", "\t", "").Replace(rawPageInvitationEmailTemplate)

// buildPageInvitationMail liefert Betreff und HTML-Body in der Sprache des
// Kontos (en, sonst de). Seiten- und Rollenname sind maskiert.
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

// sendPageInvitationEmail baut die Mail synchron und verschickt sie über die
// begrenzte Hintergrund-Warteschlange (mail_queue.go), damit die Antwort für
// Adressen mit und ohne Konto gleich lange dauert und ein ausgefallener
// Mailserver keine unbegrenzt vielen Versand-Goroutinen aufstaut. Weder Token
// noch Link landen im Log.
func (h *Handler) sendPageInvitationEmail(u models.User, page models.StatusPage, role models.Role, token string) {
	// Die Adresse entspricht (bis auf Groß-/Kleinschreibung) der geprüften
	// Eingabe aus invitePageUser; der CR/LF-Guard unten deckt sie zusätzlich ab.
	recipient := strings.TrimSpace(u.Email)
	smtpCfg := loadSMTPConfig()
	smtpCfg.applyPageSender(page)
	// Der Token steht im URL-FRAGMENT, nicht in der Query. Ein
	// Fragment geht nie an den Server (keine Caddy-/Cloudflare-/Proxy-Logs) und
	// nie im Referer hinaus; in der Query läge er in Logs und Browser-Verlauf.
	// Das Frontend liest weiter auch die alte Query (schon verschickte Mails).
	link := getFrontendURL() + "/invitation#token=" + url.QueryEscape(token)
	subject, body := buildPageInvitationMail(u.PreferredLanguage, page.Name, role.Name, link)
	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(body) || hasHeaderInjectionChars(smtpCfg.From) || hasHeaderInjectionChars(recipient) {
		slog.Error("Skipping page invitation mail: subject/body/sender contain invalid CR/LF characters", "page_id", page.ID, "user_id", u.ID)
		return
	}
	// Der Seitenname steht im Betreff; Umlaute brauchen die
	// RFC-2047-Kodierung (der CR/LF-Guard oben prüft den Klartext vorher).
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
