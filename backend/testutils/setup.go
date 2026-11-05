package testutils

import (
	"os"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SetupTestDB initializes an in-memory SQLite database and AutoMigrates the models.
func SetupTestDB() *gorm.DB {
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	}

	// Benannte Shared-Cache-In-Memory-DB: keine Datei in /tmp, die nach dem
	// Testlauf liegen bleibt (SetupTestDB hat kein *testing.T für TempDir).
	dbName := "file:statuspage_test_" + uuid.New().String() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		panic("Failed to connect to in-memory test database")
	}

	err = db.AutoMigrate(
		&models.StatusPage{},
		&models.Role{},
		&models.User{},
		&models.UserStatusPageRole{},
		&models.StatusPageSSOConfig{},
		&models.Component{},
		&models.ComponentTranslation{},
		&models.Incident{},
		&models.IncidentComponent{},
		&models.IncidentTranslation{},
		&models.GeneralNotice{},
		&models.GeneralNoticeTranslation{},
		&models.Subscriber{},
		&models.AuditLog{},
		&models.IncidentUpdate{},
		&models.IncidentUpdateTranslation{},
		&models.PostMortem{},
		&models.ActionItem{},
		&models.Tag{},
		&models.ComponentTag{},
		&models.IncidentTag{},
		&models.UserTag{},
		&models.GeneralNotice{},
		&models.IncidentTemplate{},
		&models.IncidentTemplateTranslation{},
		&models.NotificationTemplate{},
		&models.Language{},
		&models.StatusPageLanguage{},
		&models.PageOrder{},
		&models.UiTranslationOverride{},
		&models.OIDCLoginState{},
		&models.UserSSOIdentity{},
		// SAML-Login je Statusseite.
		&models.SAMLLoginState{},
		&models.SAMLAssertionUse{},
		// globale Systemeinstellungen (Key/Value).
		&models.SystemSetting{},
		// externe Statusseiten.
		&models.ExternalSource{},
		&models.ExternalComponent{},
		&models.ExternalComponentLink{},
		// gespiegelte Vorfälle und Wartungen.
		&models.ExternalIncident{},
		&models.ExternalIncidentUpdate{},
		&models.ExternalIncidentComponent{},
		// Admin-Nachrichtencenter.
		&models.AdminNotification{},
		&models.NotificationRead{},
		&models.ComponentStatusChange{},
		&models.PageInvitation{},
	)
	if err != nil {
		panic("Migration failed in test setup")
	}

	// dieselben partiellen Unique-Indizes wie in der Produktion
	// (die gorm-Tags dafür gibt es nicht mehr).
	database.CreateSoftDeleteUniqueIndexes(db)
	database.CreateAdminNotificationIndexes(db)
	database.CreateUserStatusPageRoleUniqueIndex(db)
	database.CreatePageInvitationUniqueIndex(db)

	// Set global DB instance if tests rely on database.DB (e.g., auth middleware)
	database.DB = db

	return db
}

// SetupRouter creates a new gin Engine for testing
func SetupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()

	// Ensure DB is in context if expected (though our middleware uses database.DB now)
	r.Use(func(c *gin.Context) {
		c.Set("db", database.DB)
		c.Next()
	})

	return r
}

// MustHash liefert den bcrypt-Hash eines Testpassworts und bricht bei einem
// Fehler ab (Testpasswörter sind kurz, ein Fehler wäre ein Testfehler).
func MustHash(password string) string {
	hash, err := database.HashPassword(password)
	if err != nil {
		panic("HashPassword failed in test setup: " + err.Error())
	}
	return hash
}
