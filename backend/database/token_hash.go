package database

import (
	"fmt"
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// tokenColumns are the columns that used to hold the token in plain text and
// now carry only its hash (models.HashToken).
var tokenColumns = []struct{ table, column string }{
	{"status_pages", "webhook_token"},
	{"page_orders", "confirm_token"},
	{"page_orders", "set_password_token"},
}

// tokenMigrationBatch limits the rows per pass.
const tokenMigrationBatch = 500

// hashLegacyTokens replaces plain-text tokens in the columns of tokenColumns with
// their hash. Idempotent and safe with concurrent starts: only
// non-empty values without a hash marker (models.TokenHashPrefix) are touched; each row
// is written conditionally on its old value, a second run finds nothing
// anymore. Deleted rows (soft delete) are included, updated_at stays
// unchanged. Links already sent and tokens already shown remain valid,
// because on redemption the presented plain text is hashed and compared.
func hashLegacyTokens(db *gorm.DB) error {
	for _, tc := range tokenColumns {
		for {
			var rows []struct {
				ID    uint
				Token string
			}
			err := db.Table(tc.table).Select("id AS id", tc.column+" AS token").
				Where(tc.column+" IS NOT NULL AND "+tc.column+" <> '' AND "+tc.column+" NOT LIKE ?", models.TokenHashPrefix+"%").
				Order("id").Limit(tokenMigrationBatch).Scan(&rows).Error
			if err != nil {
				return fmt.Errorf("Token-Spalte %s.%s lesen: %w", tc.table, tc.column, err)
			}
			if len(rows) == 0 {
				break
			}
			for _, r := range rows {
				err := db.Table(tc.table).Where("id = ? AND "+tc.column+" = ?", r.ID, r.Token).
					UpdateColumn(tc.column, models.HashToken(r.Token)).Error
				if err != nil {
					return fmt.Errorf("Token-Spalte %s.%s schreiben: %w", tc.table, tc.column, err)
				}
			}
		}
	}
	return nil
}

// migrateLegacyTokens runs hashLegacyTokens at startup. An error is
// logged and does not prevent startup; the next start tries again,
// until then plain-text values count as invalid (TokenMatches).
func migrateLegacyTokens(db *gorm.DB) {
	if err := hashLegacyTokens(db); err != nil {
		slog.Error("Failed to hash legacy tokens", "error", err)
	}
}
