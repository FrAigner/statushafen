package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// removeEmptyUiOverrides removes UI overrides with an empty or
// whitespace-only value. Until the fix, "Save" on an
// empty line in the editor stored "" as an override and thereby hid the text (in the
// global scope for all visitors); the handler now rejects this
// with 400, here the already stored legacy cases disappear. An empty
// override is never wanted (there is no default text "empty"), so
// no admin change is lost - the bundle default applies again.
// Soft delete as in the DELETE handler; idempotent, because afterwards no row
// matches anymore. An error is only logged: startup is not supposed to fail because of it.
func removeEmptyUiOverrides(db *gorm.DB) {
	if err := db.Where("TRIM(value) = ''").Delete(&models.UiTranslationOverride{}).Error; err != nil {
		slog.Error("Failed to remove empty ui overrides", "error", err)
	}
}
