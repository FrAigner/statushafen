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
	// MinAdminPasswordLength gilt wie bei jedem anderen Weg, ein Passwort zu setzen.
	MinAdminPasswordLength = 8
	maxAdminEmailLength    = 254
	globalAdminRoleName    = "Global Admin"
)

// ErrAdminExists meldet, dass die Adresse schon ein aktives Konto hat. Ein
// bestehendes Konto wird nie überschrieben oder zum Admin befördert.
var ErrAdminExists = errors.New("für diese E-Mail-Adresse besteht bereits ein Konto")

// CreateGlobalAdmin legt ein Konto mit Passwort und Global-Admin-Recht an. Es
// ist der Weg zum ersten Admin in Produktion, wo das Seeden gesperrt ist und
// kein SSO nötig sein soll. Alle Eingaben werden geprüft, bevor etwas
// geschrieben wird; bei jedem Fehler bleibt die Datenbank unverändert.
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
		// Die Rolle gibt es nur, wenn die Datenbank sie kennt; das Admin-Recht
		// hängt am Flag (wie beim Bootstrap-Admin über SSO).
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
