package database

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

const (
	// MinAdminPasswordLength applies as for every other way to set a password.
	MinAdminPasswordLength = 8
	maxAdminEmailLength    = 254
	globalAdminRoleName    = "Global Admin"
)

// ErrAdminExists reports that the address already has an active account. An
// existing account is never overwritten or promoted to admin.
var ErrAdminExists = errors.New("für diese E-Mail-Adresse besteht bereits ein Konto")

// CreateGlobalAdmin creates an account with password and global admin permission. It
// is the way to the first admin in production, where seeding is blocked and
// no SSO is supposed to be needed. All inputs are validated before anything is
// written; on any error the database stays unchanged.
func CreateGlobalAdmin(db *gorm.DB, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := validateAdminEmail(email); err != nil {
		return err
	}
	if utf8.RuneCountInString(password) < MinAdminPasswordLength {
		return fmt.Errorf("das Passwort muss mindestens %d Zeichen lang sein", MinAdminPasswordLength)
	}
	if strings.ContainsRune(password, 0) {
		return errors.New("das Passwort enthält ein ungültiges Zeichen")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&models.User{}).Where("email = ?", email).Count(&existing).Error; err != nil {
			return fmt.Errorf("konto prüfen: %w", err)
		}
		if existing > 0 {
			return ErrAdminExists
		}
		user := models.User{Email: email, PasswordHash: hash, IsGlobalAdmin: true}
		// The role exists only if the database knows it; the admin permission
		// depends on the flag (as with the bootstrap admin via SSO).
		var role models.Role
		if err := tx.Where("name = ?", globalAdminRoleName).Limit(1).Find(&role).Error; err != nil {
			return fmt.Errorf("rolle lesen: %w", err)
		}
		if role.ID != 0 {
			user.RoleID = role.ID
		}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("konto anlegen: %w", err)
		}
		return nil
	})
}

func validateAdminEmail(email string) error {
	if email == "" || len(email) > maxAdminEmailLength {
		return errors.New("ungültige E-Mail-Adresse")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("ungültige E-Mail-Adresse")
	}
	return nil
}
