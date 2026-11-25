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

	// Named shared-cache in-memory DB: no file in /tmp that remains after the
	// test run (SetupTestDB has no *testing.T for TempDir).
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
		// global system settings (key/value).
		&models.SystemSetting{},
		// external status pages.
		&models.ExternalSource{},
		&models.ExternalComponent{},
		&models.ExternalComponentLink{},
		// mirrored incidents and maintenance.
		&models.ExternalIncident{},
		&models.ExternalIncidentUpdate{},
		&models.ExternalIncidentComponent{},
		// admin message center.
		&models.AdminNotification{},
		&models.NotificationRead{},
		&models.ComponentStatusChange{},
		&models.PageInvitation{},
	)
	if err != nil {
		panic("Migration failed in test setup")
	}

	// the same partial unique indexes as in production
	// (the gorm tags for them no longer exist).
	database.CreateSoftDeleteUniqueIndexes(db)
	database.CreateAdminNotificationIndexes(db)
	database.CreateUserStatusPageRoleUniqueIndex(db)
	database.CreatePageInvitationUniqueIndex(db)

	// Set global DB instance if tests rely on database.DB (e.g., auth middleware)
	database.DB = db

	return db
}

// EnableTranslation switches on automatic translation and the second content
// language (system setting translation.enabled, default off). Tests of
// translations and additional languages call it explicitly.
// Idempotent, so helpers that add a second language can call it repeatedly.
func EnableTranslation(db *gorm.DB) error {
	return db.Where("key = ?", "translation.enabled").Assign(models.SystemSetting{Key: "translation.enabled", Value: "true"}).FirstOrCreate(&models.SystemSetting{}).Error
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

// MustHash returns the bcrypt hash of a test password and aborts on an
// error (test passwords are short, an error would be a test failure).
func MustHash(password string) string {
	hash, err := database.HashPassword(password)
	if err != nil {
		panic("HashPassword failed in test setup: " + err.Error())
	}
	return hash
}
