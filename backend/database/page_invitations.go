package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// CreatePageInvitationUniqueIndex legt (Security-Review,
// Fund F3) den Unique-Index (status_page_id, email) über ALLE Zeilen an, auch
// über weich gelöschte "Grabsteine". Ohne ihn umgehen zwei gleichzeitige
// Einladungen derselben Adresse (Postgres, READ COMMITTED) die 30-Minuten-Sperre,
// weil beide vorher "keine Zeile" lesen. Bewusst KEIN partieller Index
// (deleted_at IS NULL): Die Sperre hängt gerade an den Grabsteinen, und der
// Ersatzweg (replaceablePendingInvitation) löscht vorher alle Zeilen der
// Adresse endgültig, passt also zum vollen Index. Die Adresse wird beim
// Einladen normalisiert (Trim/Lowercase) gespeichert.
//
// Bestandsdaten: Doppelte Zeilen je (Seite, Adresse) lassen die Index-Anlage
// scheitern. Behalten wird die jüngste (höchste ID): Sie trägt den zuletzt
// verschickten Link und die aktuelle Sperre; ältere Zeilen sind durch sie
// ersetzt (eine neue Einladung löscht ohnehin alle älteren der Adresse). Das
// sind kurzlebige Einladungsdaten, keine Admin-Konfiguration. Idempotent
// (zweiter Lauf: nichts zu löschen, IF NOT EXISTS); Raw-SQL wie die übrigen
// Index-Helfer, weil der Schlüssel über zwei Spalten mit Namen gewünscht ist.
func CreatePageInvitationUniqueIndex(db *gorm.DB) {
	res := db.Exec(`DELETE FROM page_invitations WHERE EXISTS (
  SELECT 1 FROM page_invitations o
  WHERE o.status_page_id = page_invitations.status_page_id
    AND o.email = page_invitations.email
    AND o.id > page_invitations.id)`)
	if res.Error != nil {
		logging.Fatal("Failed to dedupe page_invitations", "error", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("Doppelte Einladungen bereinigt", "count", res.RowsAffected)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_page_invitation_page_email ON page_invitations (status_page_id, email)`).Error; err != nil {
		logging.Fatal("Failed to create idx_page_invitation_page_email", "error", err)
	}
}
