package database

import (
	"fmt"
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// tokenColumns sind die Spalten, die früher den Token im Klartext hielten und
// jetzt nur noch dessen Hash (models.HashToken) tragen.
var tokenColumns = []struct{ table, column string }{
	{"status_pages", "webhook_token"},
	{"page_orders", "confirm_token"},
	{"page_orders", "set_password_token"},
}

// tokenMigrationBatch begrenzt die Zeilen je Durchgang.
const tokenMigrationBatch = 500

// hashLegacyTokens ersetzt Klartext-Token in den Spalten aus tokenColumns durch
// ihren Hash. Idempotent und sicher bei gleichzeitigen Starts: Angefasst werden
// nur nicht-leere Werte ohne Hash-Marke (models.TokenHashPrefix); jede Zeile
// wird bedingt auf ihren alten Wert geschrieben, ein zweiter Lauf findet nichts
// mehr. Gelöschte Zeilen (Soft Delete) sind mit dabei, updated_at bleibt
// unverändert. Bereits verschickte Links und angezeigte Token bleiben gültig,
// weil beim Einlösen der vorgelegte Klartext gehasht und verglichen wird.
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

// migrateLegacyTokens führt hashLegacyTokens beim Start aus. Ein Fehler wird
// geloggt und verhindert den Start nicht; der nächste Start versucht es erneut,
// bis dahin gelten Klartextwerte als ungültig (TokenMatches).
func migrateLegacyTokens(db *gorm.DB) {
	if err := hashLegacyTokens(db); err != nil {
		slog.Error("Failed to hash legacy tokens", "error", err)
	}
}
