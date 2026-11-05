package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// removeEmptyUiOverrides entfernt UI-Overrides mit leerem bzw.
// nur aus Leerzeichen bestehendem Wert. Bis zum Fix hat "Speichern" einer
// leeren Zeile im Editor "" als Override abgelegt und damit den Text (im
// globalen Scope für alle Besucher) ausgeblendet; der Handler lehnt das jetzt
// mit 400 ab, hier verschwinden die schon gespeicherten Altfälle. Ein leerer
// Override ist nie gewollt (es gibt keinen Standardtext "leer"), deshalb geht
// dabei keine Admin-Änderung verloren - es gilt wieder der Bundle-Default.
// Soft-Delete wie im DELETE-Handler; idempotent, weil danach keine Zeile mehr
// passt. Ein Fehler wird nur geloggt: Der Start soll daran nicht scheitern.
func removeEmptyUiOverrides(db *gorm.DB) {
	if err := db.Where("TRIM(value) = ''").Delete(&models.UiTranslationOverride{}).Error; err != nil {
		slog.Error("Failed to remove empty ui overrides", "error", err)
	}
}
