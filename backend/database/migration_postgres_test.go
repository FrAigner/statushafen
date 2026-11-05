package database

// Migrationen in der CI auf einer echten, leeren Postgres-Datenbank
// prüfen. Die übrigen Tests laufen auf SQLite; partielle Unique-Indizes,
// Raw-SQL-Helfer und Backfills verhalten sich dort anders. Deshalb startet
// dieser Test per embedded-postgres ein eigenes Postgres (kein Docker nötig,
// läuft so auch auf GitHub-gehosteten Runnern) und ruft InitDB()
// zweimal auf: Der zweite Lauf darf nichts mehr ändern (Idempotenz, siehe
// Backfill-Muster in CONTRIBUTING.md).
//
// Nur mit MIGRATION_PG_TEST=1, weil er die Postgres-Binaries herunterlädt
// (Maven Central) und Postgres nicht als root startet.

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/stretchr/testify/require"
)

// migrationSnapshot hält fest, was ein zweiter InitDB()-Lauf nicht verändern darf.
// Hashes deckt auch überschriebene Zeilen bei gleicher Anzahl ab (z. B. ein
// Backfill, der Admin-Änderungen zurücksetzt oder updated_at anfasst).
type migrationSnapshot struct {
	Tables  []string
	Indexes []string
	Rows    map[string]int64
	Hashes  map[string]string
}

func takeMigrationSnapshot(t *testing.T) migrationSnapshot {
	t.Helper()
	var s migrationSnapshot
	require.NoError(t, DB.Raw(`SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`).Scan(&s.Tables).Error)
	require.NoError(t, DB.Raw(`SELECT indexname || ': ' || indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY indexname`).Scan(&s.Indexes).Error)
	s.Rows = map[string]int64{}
	s.Hashes = map[string]string{}
	for _, table := range s.Tables {
		var n int64
		require.NoError(t, DB.Table(table).Count(&n).Error, table)
		s.Rows[table] = n
		// Tabellennamen stammen aus pg_tables, werden aber trotzdem gequotet.
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		var hash string
		require.NoError(t, DB.Raw(`SELECT md5(coalesce(string_agg(t::text, '|' ORDER BY t::text), '')) FROM `+quoted+` t`).Scan(&hash).Error, table)
		s.Hashes[table] = hash
	}
	return s
}

// freePort sucht einen freien TCP-Port, damit parallele Läufe nicht kollidieren.
func freePort(t *testing.T) uint32 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return uint32(port)
}

func TestInitDB_PostgresLeereDatenbankZweimal(t *testing.T) {
	if os.Getenv("MIGRATION_PG_TEST") != "1" {
		t.Skip("nur mit MIGRATION_PG_TEST=1 (CI-Schritt „Migrationen (Postgres)“)")
	}

	port := freePort(t)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		Database("statushafen").
		// Wie in Produktion UTF-8. Ohne Vorgabe übernimmt initdb die Locale des
		// Prozesses; in einem Container ohne Locale ist das POSIX → SQL_ASCII, und pgx lehnt
		// dann jede Abfrage ab („client_encoding=UTF8“). Locale C passt zu jedem Encoding.
		Encoding("UTF8").
		Locale("C").
		RuntimePath(t.TempDir()).
		Logger(nil))
	require.NoError(t, pg.Start())
	t.Cleanup(func() { _ = pg.Stop() })

	oldDB := DB
	t.Cleanup(func() { DB = oldDB })
	t.Setenv("DB_DSN", fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", port))
	t.Setenv("SEED_DB", "")

	InitDB()
	erster := takeMigrationSnapshot(t)
	require.NotEmpty(t, erster.Tables, "nach InitDB() muss es Tabellen geben")
	require.EqualValues(t, 1, erster.Rows["status_pages"], "System-Statusseite fehlt")

	var layout string
	require.NoError(t, DB.Raw(`SELECT page_layout FROM status_pages WHERE slug = ?`, SystemStatusPageSlug).Scan(&layout).Error)
	require.Equal(t, "standard", layout, "PageLayout-Default fehlt")

	InitDB()
	require.Equal(t, erster, takeMigrationSnapshot(t), "zweiter InitDB()-Lauf hat Schema oder Daten verändert")

	// Gegenprobe: Der Snapshot muss auch eine geänderte Zeile bei gleicher
	// Zeilenzahl erkennen – sonst fiele ein Backfill, der Admin-Änderungen bei
	// jedem Start überschreibt, nicht auf (Review-Befund 1).
	require.NoError(t, DB.Exec(`UPDATE status_page_languages SET is_default = NOT is_default`).Error)
	require.NotEqual(t, erster, takeMigrationSnapshot(t), "Snapshot erkennt geänderte Zeileninhalte nicht")

	pruefeSoftDeleteUniqueIndexes(t)
	pruefeDatenintegritaet(t)
	// Advisory Lock und Indizes der externen Statusseiten.
	pruefeAdvisoryLock(t)
	pruefeExternalStatusIndexes(t)
	pruefeHelpTooltipsSpalte(t)
	pruefeCompletedStatus(t)
	pruefeAdminNotifications(t)
	pruefeKomponentenKennung(t)
	pruefeRechteCrud(t)
	pruefeSeitenRolleEindeutig(t)
	pruefeEinladungenEindeutig(t)
	pruefeVerwaisteSSOIdentitaeten(t)
	pruefeTokenHashes(t)
}

// pruefeTokenHashes: Bestandsdatenbank mit Klartext-Token (Webhook, Bestellung).
// Der Start ersetzt sie durch den Hash, lässt leere Werte und schon gehashte
// Werte unberührt, und ein weiterer Start ändert nichts mehr.
func pruefeTokenHashes(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`INSERT INTO status_pages (name, slug, webhook_token, created_at, updated_at) VALUES ('Tok A', 'tok-hash-a', 'pg-klartext-webhook', now(), now()), ('Tok B', 'tok-hash-b', '', now(), now()), ('Tok C', 'tok-hash-c', ?, now(), now())`, models.HashToken("pg-schon-gehasht")).Error)
	require.NoError(t, DB.Exec(`INSERT INTO page_orders (company_name, email, status, confirm_token, set_password_token, created_at, updated_at) VALUES ('Tok O', 'tok-hash@example.com', 'pending_confirmation', 'pg-klartext-bestaetigung', NULL, now(), now())`).Error)

	InitDB()
	var got []string
	require.NoError(t, DB.Raw(`SELECT webhook_token FROM status_pages WHERE slug LIKE 'tok-hash-%' ORDER BY slug`).Scan(&got).Error)
	require.Equal(t, []string{models.HashToken("pg-klartext-webhook"), "", models.HashToken("pg-schon-gehasht")}, got)
	var order string
	require.NoError(t, DB.Raw(`SELECT confirm_token FROM page_orders WHERE email = 'tok-hash@example.com'`).Scan(&order).Error)
	require.Equal(t, models.HashToken("pg-klartext-bestaetigung"), order)

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "zweiter Lauf hat Schema oder Daten verändert")
}

// pruefeEinladungenEindeutig (Review F3): Bestandsdatenbank mit
// doppelten Einladungen je (Seite, Adresse) und ohne Index. Der Start behält die
// jüngste Zeile (höchste ID, auch wenn sie ein Grabstein ist), legt den
// Unique-Index an, der danach auch gleichzeitige Inserts auf genau einen
// reduziert; ein weiterer Start ändert nichts.
func pruefeEinladungenEindeutig(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`DROP INDEX IF EXISTS idx_page_invitation_page_email`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO page_invitations (created_at, updated_at, deleted_at, status_page_id, email, role_id, token_hash, expires_at, invited_by_id, invited_user_id) VALUES
		(now(), now(), NULL, 7799, 'doppel@example.com', 1, 'pg-h1', now() + interval '1 day', 1, 0),
		(now(), now(), now(), 7799, 'doppel@example.com', 1, 'pg-h2', now() + interval '1 day', 1, 0),
		(now(), now(), NULL, 7799, 'einzeln@example.com', 1, 'pg-h3', now() + interval '1 day', 1, 0)`).Error)

	InitDB()
	var hashes []string
	require.NoError(t, DB.Raw(`SELECT token_hash FROM page_invitations WHERE status_page_id = 7799 ORDER BY id`).Scan(&hashes).Error)
	require.Equal(t, []string{"pg-h2", "pg-h3"}, hashes, "behalten wird die jüngste Zeile je (Seite, Adresse)")

	// Acht gleichzeitige Inserts derselben Adresse: genau einer gewinnt.
	var wg sync.WaitGroup
	var mu sync.Mutex
	gewonnen := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := DB.Exec(`INSERT INTO page_invitations (created_at, updated_at, status_page_id, email, role_id, token_hash, expires_at, invited_by_id, invited_user_id) VALUES (now(), now(), 7800, 'parallel@example.com', 1, ?, now() + interval '1 day', 1, 0)`, fmt.Sprintf("pg-p%d", i)).Error
			if err == nil {
				mu.Lock()
				gewonnen++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, gewonnen, "Unique-Index (status_page_id, email) fehlt oder greift nicht")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "zweiter Lauf hat Schema oder Daten verändert")
}

// pruefeKomponentenKennung: Bestandsdatenbank vor der Kennung. Die
// Spalte fehlt, Komponenten haben also keine. Der Start legt die Spalte an
// (leer), vergibt Kennungen deterministisch nach ID (Kollisionen -2, -3, gelöschte
// Zeilen bleiben leer, vorhandene Kennungen bleiben) und legt danach den
// partiellen Unique-Index an; ein weiterer Start ändert nichts.
func pruefeKomponentenKennung(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`ALTER TABLE components DROP COLUMN slug`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO status_pages (name, slug, created_at, updated_at) VALUES ('Kennung577', 'kennung577', now(), now())`).Error)
	var pageID uint
	require.NoError(t, DB.Raw(`SELECT id FROM status_pages WHERE slug = 'kennung577'`).Scan(&pageID).Error)
	for _, name := range []string{"Mail Server", "Mail Server", "Größe Übung", "Mail Server"} {
		require.NoError(t, DB.Exec(`INSERT INTO components (created_at, updated_at, status_page_id, name, status, visible) VALUES (now(), now(), ?, ?, 'operational', true)`, pageID, name).Error)
	}
	require.NoError(t, DB.Exec(`INSERT INTO components (created_at, updated_at, deleted_at, status_page_id, name, status, visible) VALUES (now(), now(), now(), ?, 'Mail Server', 'operational', true)`, pageID).Error)

	InitDB()

	var slugs []string
	require.NoError(t, DB.Raw(`SELECT slug FROM components WHERE status_page_id = ? ORDER BY id`, pageID).Scan(&slugs).Error)
	require.Equal(t, []string{"mail-server", "mail-server-2", "groesse-uebung", "mail-server-3", ""}, slugs)

	var def string
	require.NoError(t, DB.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_components_slug_active'`).Scan(&def).Error)
	require.Contains(t, def, "CREATE UNIQUE INDEX")
	require.Contains(t, def, "deleted_at IS NULL")
	require.Error(t, DB.Exec(`INSERT INTO components (created_at, updated_at, status_page_id, name, slug, status, visible) VALUES (now(), now(), ?, 'X', 'mail-server', 'operational', true)`, pageID).Error, "aktive Kennung muss je Seite eindeutig sein")
	// Der erste Start trug die Statushistorie der vier aktiven
	// Bestandskomponenten nach (die gelöschte bekommt keine).
	var hist int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM component_status_changes h JOIN components c ON c.id = h.component_id WHERE c.status_page_id = ? AND h.source = 'backfill' AND h.old_status = '' AND h.new_status = 'operational'`, pageID).Scan(&hist).Error)
	require.EqualValues(t, 4, hist)
	require.NoError(t, DB.Create(&models.Component{StatusPageID: pageID, Name: "Mail Server"}).Error)
	backfillStatusHistory(DB) // direkt angelegt, nicht über einen Handler: Nachtrag wie beim nächsten Start
	var neu string
	require.NoError(t, DB.Raw(`SELECT slug FROM components WHERE status_page_id = ? ORDER BY id DESC LIMIT 1`, pageID).Scan(&neu).Error)
	require.Equal(t, "mail-server-4", neu)

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "InitDB() nach dem Kennungs-Backfill ist nicht idempotent")
}

// pruefeRechteCrud: Bestandsdatenbank mit Rollen im Altformat
// (read/write, benutzerdefiniert, soft-gelöscht, Global Admin). Der Start
// ersetzt write durch Create/Read/Update/Delete, koppelt die Tabs 1:1, lässt
// updated_at unberührt und legt ein Backup ab; ein weiterer Start ändert nichts.
func pruefeRechteCrud(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`DELETE FROM system_settings WHERE key LIKE 'permissions.crud%'`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO roles (created_at, updated_at, deleted_at, name, permissions) VALUES
		(now(), '2026-09-01 10:00:00+00', NULL, 'Alt-Editor660', 'components:read,incidents:read,incidents:write,notices:read,notices:write'),
		(now(), '2026-09-01 10:00:00+00', NULL, 'Alt-Owner660', 'pages:write,components:read,components:write,admin_dashboard'),
		(now(), '2026-09-01 10:00:00+00', now(), 'Alt-Gelöscht660', 'subscribers:write'),
		(now(), '2026-09-01 10:00:00+00', NULL, 'Global Admin', 'pages:read,pages:write,users:write,roles:write')`).Error)

	InitDB()

	perms := func(name string) string {
		var p string
		require.NoError(t, DB.Raw(`SELECT permissions FROM roles WHERE name = ?`, name).Scan(&p).Error)
		return p
	}
	editor, owner, geloescht, admin := perms("Alt-Editor660"), perms("Alt-Owner660"), perms("Alt-Gelöscht660"), perms("Global Admin")
	for name, p := range map[string]string{"editor": editor, "owner": owner, "gelöscht": geloescht, "admin": admin} {
		require.False(t, permissions.HasLegacyWrite(p), "%s enthält noch write: %s", name, p)
	}
	require.True(t, permissions.Has(editor, "maintenance:delete"))
	require.True(t, permissions.Has(editor, "external_sources:read"))
	require.False(t, permissions.Has(editor, "external_sources:update"))
	require.True(t, permissions.Has(owner, "external_sources:delete"))
	require.True(t, permissions.Has(owner, "sso:update"))
	require.Contains(t, owner, "admin_dashboard")
	require.True(t, permissions.Has(geloescht, "subscribers:delete"))
	require.True(t, permissions.Has(admin, "audit:read"))
	require.True(t, permissions.Has(admin, "system_settings:update"))

	var updated string
	require.NoError(t, DB.Raw(`SELECT to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') FROM roles WHERE name = 'Alt-Editor660'`).Scan(&updated).Error)
	require.Equal(t, "2026-09-01 10:00", updated, "updated_at bleibt")
	var backup int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM system_settings WHERE key = 'permissions.crud_backup' AND value LIKE '%incidents:write%'`).Scan(&backup).Error)
	require.EqualValues(t, 1, backup, "Backup der alten Rechte fehlt")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "InitDB() nach der Rechte-Umstellung ist nicht idempotent")
}

// pruefeCompletedStatus: Bestandsdatenbank mit dem Altwert
// "completed" bei Vorfall, Wartung und Timeline-Update. Der Start stellt ihn
// auf "resolved" um, füllt ResolvedAt nur falls leer, lässt updated_at und
// alle anderen Zeilen unberührt; ein weiterer Start ändert nichts.
func pruefeCompletedStatus(t *testing.T) {
	t.Helper()
	var pageID uint
	require.NoError(t, DB.Raw(`SELECT id FROM status_pages WHERE is_system ORDER BY id LIMIT 1`).Scan(&pageID).Error)
	require.NoError(t, DB.Exec(`INSERT INTO incidents (created_at, updated_at, status_page_id, title, message, status, is_maintenance, resolved_at) VALUES
		(now(), '2026-09-01 10:00:00+00', ?, 'Wartung606', 'm', 'completed', true, NULL),
		(now(), '2026-09-01 10:00:00+00', ?, 'Vorfall606', 'm', 'completed', false, '2026-09-01 09:00:00+00'),
		(now(), '2026-09-01 10:00:00+00', ?, 'Offen606', 'm', 'monitoring', false, NULL)`, pageID, pageID, pageID).Error)
	var wartungID uint
	require.NoError(t, DB.Raw(`SELECT id FROM incidents WHERE title = 'Wartung606'`).Scan(&wartungID).Error)
	require.NoError(t, DB.Exec(`INSERT INTO incident_updates (created_at, incident_id, status, message) VALUES (now(), ?, 'completed', 'fertig')`, wartungID).Error)

	InitDB()

	type row struct {
		Title      string
		Status     string
		ResolvedAt *string
		UpdatedAt  string
	}
	var rows []row
	require.NoError(t, DB.Raw(`SELECT title, status, to_char(resolved_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') AS resolved_at, to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') AS updated_at FROM incidents WHERE title LIKE '%606' ORDER BY title`).Scan(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, "Offen606", rows[0].Title)
	require.Equal(t, "monitoring", rows[0].Status)
	require.Nil(t, rows[0].ResolvedAt)
	require.Equal(t, "Vorfall606", rows[1].Title)
	require.Equal(t, "resolved", rows[1].Status)
	require.Equal(t, "2026-09-01 09:00", *rows[1].ResolvedAt, "vorhandenes ResolvedAt bleibt")
	require.Equal(t, "Wartung606", rows[2].Title)
	require.Equal(t, "resolved", rows[2].Status)
	require.NotNil(t, rows[2].ResolvedAt)
	require.Equal(t, "2026-09-01 10:00", *rows[2].ResolvedAt, "leeres ResolvedAt = letzte Änderung")
	require.Equal(t, "2026-09-01 10:00", rows[2].UpdatedAt, "updated_at bleibt")
	var updStatus string
	require.NoError(t, DB.Raw(`SELECT status FROM incident_updates WHERE incident_id = ?`, wartungID).Scan(&updStatus).Error)
	require.Equal(t, "resolved", updStatus)
	var rest int64
	require.NoError(t, DB.Raw(`SELECT (SELECT count(*) FROM incidents WHERE status = 'completed') + (SELECT count(*) FROM incident_updates WHERE status = 'completed')`).Scan(&rest).Error)
	require.Zero(t, rest, "kein Altwert mehr")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "InitDB() nach der completed-Migration ist nicht idempotent")
}

// pruefeHelpTooltipsSpalte: Eine Bestandsdatenbank ohne die
// Spalte hide_help_tooltips bekommt sie beim Start, bestehende Nutzer haben
// danach die Hilfe-Tooltips an (false), und ein weiterer Start ändert nichts.
func pruefeHelpTooltipsSpalte(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS hide_help_tooltips`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO users (email, created_at, updated_at) VALUES ('bestand472@example.com', now(), now())`).Error)

	InitDB()
	var hide *bool
	require.NoError(t, DB.Raw(`SELECT hide_help_tooltips FROM users WHERE email = 'bestand472@example.com'`).Scan(&hide).Error)
	require.NotNil(t, hide, "hide_help_tooltips fehlt oder ist NULL")
	require.False(t, *hide, "Bestandsnutzer müssen die Hilfe-Tooltips an haben")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "zweiter Start nach dem Hinzufügen von hide_help_tooltips hat etwas verändert")
}

// pruefeSoftDeleteUniqueIndexes prüft auf echtem Postgres: Eine
// Bestandsdatenbank mit den alten, nicht-partiellen Indizes wird beim Start
// umgestellt, gelöschte Tag-Namen und Slugs sind danach wieder frei, und eine
// soft-gelöschte Systemseite bricht den Start nicht mehr ab.
func pruefeSoftDeleteUniqueIndexes(t *testing.T) {
	t.Helper()
	// Stand vor der Umstellung nachbilden (so hat ihn AutoMigrate bzw. der alte Helfer angelegt).
	for _, sql := range []string{
		`DROP INDEX IF EXISTS idx_status_pages_slug_active`,
		`DROP INDEX IF EXISTS idx_tags_global_name_active`,
		`DROP INDEX IF EXISTS idx_tags_scoped_name_active`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_status_pages_slug ON status_pages (slug)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_global_name ON tags (LOWER(name)) WHERE status_page_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_scoped_name ON tags (LOWER(name), status_page_id) WHERE status_page_id IS NOT NULL`,
	} {
		require.NoError(t, DB.Exec(sql).Error, sql)
	}

	InitDB()

	var defs []string
	require.NoError(t, DB.Raw(`SELECT indexname || ': ' || indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname IN ('idx_status_pages_slug', 'idx_tags_global_name', 'idx_tags_scoped_name', 'idx_status_pages_slug_active', 'idx_tags_global_name_active', 'idx_tags_scoped_name_active') ORDER BY indexname`).Scan(&defs).Error)
	require.Len(t, defs, 3, "alte Indizes müssen ersetzt sein: %v", defs)
	for _, def := range defs {
		require.Contains(t, def, "_active: CREATE UNIQUE INDEX", def)
		require.Contains(t, def, "deleted_at IS NULL", def)
	}

	page := models.StatusPage{Name: "Statusseite", Slug: "statusseite"}
	require.NoError(t, DB.Create(&page).Error)
	require.Error(t, DB.Create(&models.StatusPage{Name: "Dup", Slug: "statusseite"}).Error, "aktiver Slug muss eindeutig bleiben")
	require.NoError(t, DB.Delete(&page).Error)
	require.NoError(t, DB.Create(&models.StatusPage{Name: "Neu", Slug: "statusseite"}).Error, "gelöschter Slug muss wieder frei sein")

	for _, scope := range []*uint{nil, &page.ID} {
		tag := models.Tag{Name: "UI-Test", StatusPageID: scope}
		require.NoError(t, DB.Create(&tag).Error)
		require.Error(t, DB.Create(&models.Tag{Name: "ui-test", StatusPageID: scope}).Error, "aktiver Tag-Name muss eindeutig bleiben")
		require.NoError(t, DB.Delete(&tag).Error)
		require.NoError(t, DB.Create(&models.Tag{Name: "UI-Test", StatusPageID: scope}).Error, "gelöschter Tag-Name muss wieder frei sein")
	}

	// Gelöschte Systemseite: Start läuft durch (früher logging.Fatal), die Seite
	// bleibt gelöscht, und ein weiterer Start ändert nichts.
	require.NoError(t, DB.Where("slug = ?", SystemStatusPageSlug).Delete(&models.StatusPage{}).Error)
	InitDB()
	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "InitDB() mit gelöschter Systemseite ist nicht idempotent")
	var aktiv int64
	require.NoError(t, DB.Model(&models.StatusPage{}).Where("slug = ?", SystemStatusPageSlug).Count(&aktiv).Error)
	require.Zero(t, aktiv, "gelöschte Systemseite darf nicht neu entstehen")
}

// pruefeDatenintegritaet und die Systemseiten-Kennzeichnung auf echtem
// Postgres ab: Bestandsdatenbank mit den alten Unique-Indizes, verwaisten
// Seitenrollen und einer Systemseite ohne Kennzeichen wird beim Start
// umgestellt, bereinigt und gekennzeichnet; ein weiterer Start ändert nichts.
func pruefeDatenintegritaet(t *testing.T) {
	t.Helper()
	// Stand vor den Fixes nachbilden (alte Indizes wie von AutoMigrate angelegt).
	for _, sql := range []string{
		`DROP INDEX IF EXISTS idx_roles_name_active`,
		`DROP INDEX IF EXISTS idx_users_email_active`,
		`DROP INDEX IF EXISTS idx_notification_template_active`,
		`DROP INDEX IF EXISTS idx_incident_translation_active`,
		`DROP INDEX IF EXISTS idx_component_translation_active`,
		`DROP INDEX IF EXISTS idx_notice_translation_active`,
		`DROP INDEX IF EXISTS idx_status_pages_system_active`,
		`CREATE UNIQUE INDEX idx_roles_name ON roles (name)`,
		`CREATE UNIQUE INDEX idx_users_email ON users (email)`,
		`CREATE UNIQUE INDEX idx_notification_template ON notification_templates (status_page_id, kind, audience, language)`,
		`CREATE UNIQUE INDEX idx_incident_translation ON incident_translations (incident_id, language_code)`,
		`CREATE UNIQUE INDEX idx_component_translation ON component_translations (component_id, language_code)`,
		`CREATE UNIQUE INDEX idx_notice_translation ON general_notice_translations (general_notice_id, language_code)`,
		// Systemseite ohne Kennzeichen, aktiv, mit dem reservierten Slug.
		`UPDATE status_pages SET is_system = false, deleted_at = NULL, slug = 'system' WHERE slug = 'system' OR is_system`,
	} {
		require.NoError(t, DB.Exec(sql).Error, sql)
	}
	// verwaiste Zuordnungen (Benutzer und Rolle soft-gelöscht) und eine intakte.
	var pageID uint
	require.NoError(t, DB.Raw(`SELECT id FROM status_pages WHERE slug = 'system'`).Scan(&pageID).Error)
	rolle := models.Role{Name: "Bestand513", Permissions: "x"}
	nutzer := models.User{Email: "bestand513@example.com"}
	geloescht := models.User{Email: "geloescht514@example.com"}
	require.NoError(t, DB.Create(&rolle).Error)
	require.NoError(t, DB.Create(&nutzer).Error)
	require.NoError(t, DB.Create(&geloescht).Error)
	require.NoError(t, DB.Create(&models.UserStatusPageRole{UserID: nutzer.ID, StatusPageID: pageID, RoleID: rolle.ID}).Error)
	require.NoError(t, DB.Create(&models.UserStatusPageRole{UserID: geloescht.ID, StatusPageID: pageID, RoleID: rolle.ID}).Error)
	require.NoError(t, DB.Delete(&geloescht).Error)

	InitDB()

	var defs []string
	require.NoError(t, DB.Raw(`SELECT indexname || ': ' || indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname IN ('idx_roles_name', 'idx_users_email', 'idx_notification_template', 'idx_incident_translation', 'idx_component_translation', 'idx_notice_translation', 'idx_roles_name_active', 'idx_users_email_active', 'idx_notification_template_active', 'idx_incident_translation_active', 'idx_component_translation_active', 'idx_notice_translation_active', 'idx_status_pages_system_active') ORDER BY indexname`).Scan(&defs).Error)
	require.Len(t, defs, 7, "alte Indizes müssen ersetzt sein: %v", defs)
	for _, def := range defs {
		require.Contains(t, def, "_active: CREATE UNIQUE INDEX", def)
		require.Contains(t, def, "deleted_at IS NULL", def)
	}

	// Namen/E-Mails gelöschter Zeilen sind wieder frei, aktive bleiben eindeutig.
	require.Error(t, DB.Create(&models.Role{Name: "Bestand513", Permissions: "y"}).Error)
	require.NoError(t, DB.Delete(&rolle).Error)
	require.NoError(t, DB.Create(&models.Role{Name: "Bestand513", Permissions: "y"}).Error, "gelöschter Rollenname muss frei sein")
	require.Error(t, DB.Create(&models.User{Email: "bestand513@example.com"}).Error)
	require.NoError(t, DB.Create(&models.User{Email: "geloescht514@example.com"}).Error, "gelöschte E-Mail muss frei sein")

	// verwaiste Zeilen sind bereinigt (Nutzer und Rolle teils gelöscht).
	var verwaist int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM user_status_page_roles WHERE user_id = ?`, geloescht.ID).Scan(&verwaist).Error)
	require.Zero(t, verwaist, "verwaiste user_status_page_roles müssen entfernt sein")

	// Bestands-Systemseite gekennzeichnet, nichts verdoppelt, Slug-Änderung
	// erzeugt beim nächsten Start keine zweite Seite.
	var systemSeiten int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM status_pages WHERE is_system`).Scan(&systemSeiten).Error)
	require.EqualValues(t, 1, systemSeiten)
	var seitenGesamt int64
	require.NoError(t, DB.Unscoped().Model(&models.StatusPage{}).Count(&seitenGesamt).Error)
	require.NoError(t, DB.Exec(`UPDATE status_pages SET slug = 'umbenannt515' WHERE is_system`).Error)
	InitDB()
	var seitenDanach int64
	require.NoError(t, DB.Unscoped().Model(&models.StatusPage{}).Count(&seitenDanach).Error)
	require.Equal(t, seitenGesamt, seitenDanach, "Slug-Änderung der Systemseite darf keine zweite Seite erzeugen")
	require.Error(t, DB.Exec(`INSERT INTO status_pages (name, slug, is_system, created_at, updated_at) VALUES ('zweite', 'zweite515', true, now(), now())`).Error, "höchstens eine aktive Systemseite")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "InitDB() nach der Datenintegritäts-Migration ist nicht idempotent")
}

// pruefeSeitenRolleEindeutig: Bestandsdatenbank mit doppelten
// Seitenrollen. Der Start behält je (Konto, Seite) nur die bisher wirksame Zeile
// (kleinste ID; die andere ist soft-gelöscht), legt den partiellen Unique-Index an und
// ein weiterer Start ändert nichts mehr.
func pruefeSeitenRolleEindeutig(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`DROP INDEX IF EXISTS idx_user_status_page_role_active`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO users (email, created_at, updated_at) VALUES ('doppel800@example.com', now(), now())`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO status_pages (name, slug, created_at, updated_at) VALUES ('Doppel800', 'doppel800', now(), now())`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO roles (created_at, updated_at, name, permissions) VALUES (now(), now(), 'Doppel800-A', 'components:read'), (now(), now(), 'Doppel800-B', 'components:read')`).Error)
	var uid, pid, ra, rb uint
	require.NoError(t, DB.Raw(`SELECT id FROM users WHERE email = 'doppel800@example.com'`).Scan(&uid).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM status_pages WHERE slug = 'doppel800'`).Scan(&pid).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM roles WHERE name = 'Doppel800-A'`).Scan(&ra).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM roles WHERE name = 'Doppel800-B'`).Scan(&rb).Error)
	require.NoError(t, DB.Exec(`INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES
		('2026-09-01 10:00:00+00', '2026-09-01 10:00:00+00', ?, ?, ?),
		('2026-09-02 10:00:00+00', '2026-10-01 10:00:00+00', ?, ?, ?)`, uid, pid, ra, uid, pid, rb).Error)

	InitDB()
	aktiv := func() []uint {
		var roleIDs []uint
		require.NoError(t, DB.Raw(`SELECT role_id FROM user_status_page_roles WHERE user_id = ? AND status_page_id = ? AND deleted_at IS NULL`, uid, pid).Scan(&roleIDs).Error)
		return roleIDs
	}
	require.Equal(t, []uint{ra}, aktiv(), "behalten wird die bisher wirksame Zeile (kleinste ID), auch wenn die andere jünger ist")
	err := DB.Exec(`INSERT INTO user_status_page_roles (created_at, updated_at, user_id, status_page_id, role_id) VALUES (now(), now(), ?, ?, ?)`, uid, pid, rb).Error
	require.Error(t, err, "Unique-Index fehlt")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "zweiter Lauf hat Schema oder Daten verändert")
}

// pruefeVerwaisteSSOIdentitaeten: Bestandsdatenbank mit einer
// Seiten-SSO-Verknüpfung, deren Mitgliedschaft danach weich entfernt wurde
// (Altfall vor der Umstellung), und einer bewussten Verknüpfung ohne Rolle. Der Start
// löscht nur die verwaiste, ein weiterer Start ändert nichts mehr.
func pruefeVerwaisteSSOIdentitaeten(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec(`INSERT INTO users (email, created_at, updated_at) VALUES ('waise821@example.com', now(), now()), ('ohnerolle821@example.com', now(), now())`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO status_pages (name, slug, created_at, updated_at) VALUES ('Waise821', 'waise821', now(), now())`).Error)
	require.NoError(t, DB.Exec(`INSERT INTO roles (created_at, updated_at, name, permissions) VALUES (now(), now(), 'Waise821', 'components:read')`).Error)
	var waise, ohne, pid, rid uint
	require.NoError(t, DB.Raw(`SELECT id FROM users WHERE email = 'waise821@example.com'`).Scan(&waise).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM users WHERE email = 'ohnerolle821@example.com'`).Scan(&ohne).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM status_pages WHERE slug = 'waise821'`).Scan(&pid).Error)
	require.NoError(t, DB.Raw(`SELECT id FROM roles WHERE name = 'Waise821'`).Scan(&rid).Error)
	require.NoError(t, DB.Exec(`INSERT INTO user_status_page_roles (created_at, updated_at, deleted_at, user_id, status_page_id, role_id) VALUES
		('2026-09-01 10:00:00+00', '2026-09-01 10:00:00+00', '2026-10-01 10:00:00+00', ?, ?, ?)`, waise, pid, rid).Error)
	require.NoError(t, DB.Exec(`INSERT INTO user_sso_identities (user_id, status_page_id, protocol, issuer, subject, created_at) VALUES
		(?, ?, 'oidc', 'iss821', 'waise', '2026-09-02 10:00:00+00'),
		(?, ?, 'oidc', 'iss821', 'ohne', '2026-09-02 10:00:00+00')`, waise, pid, ohne, pid).Error)

	InitDB()
	var subjects []string
	require.NoError(t, DB.Raw(`SELECT subject FROM user_sso_identities WHERE issuer = 'iss821' ORDER BY subject`).Scan(&subjects).Error)
	require.Equal(t, []string{"ohne"}, subjects, "nur die verwaiste Verknüpfung wird gelöscht")
	var marker int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM user_status_page_roles WHERE user_id = ? AND deleted_at IS NOT NULL`, waise).Scan(&marker).Error)
	require.EqualValues(t, 1, marker, "die soft-gelöschte Mitgliedszeile (Merker) bleibt")

	vorher := takeMigrationSnapshot(t)
	InitDB()
	require.Equal(t, vorher, takeMigrationSnapshot(t), "zweiter Lauf hat Schema oder Daten verändert")
}
