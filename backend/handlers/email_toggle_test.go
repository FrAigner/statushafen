package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// globaler E-Mail-Schalter (Systemeinstellung email.enabled).
// Abgedeckt: GET/PUT mit Teilfeldern samt Fehlerpfaden sowie alle drei
// Versandpfade (Bestell-Bestätigung, Abo-Bestätigung, Incident-/Notice-Mails
// über sendAlertEmail) bei an- und abgeschaltetem Versand.

const skippedAction = "email.skipped_disabled"

// disableEmail legt email.enabled=false an, wie es das Admin-PUT täte.
func disableEmail(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&models.SystemSetting{Key: "email.enabled", Value: "false"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
}

// awaitSkippedAudit wartet auf den Audit-Eintrag für den übersprungenen
// Versand (Incident-/Notice-Mails laufen in einer Goroutine) und liefert ihn.
func awaitSkippedAudit(t *testing.T, db *gorm.DB, targetType string) models.AuditLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var entry models.AuditLog
		err := db.Where("action = ? AND target_type = ?", skippedAction, targetType).First(&entry).Error
		if err == nil {
			return entry
		}
		if time.Now().After(deadline) {
			t.Fatalf("kein %s-Audit-Eintrag für %s", skippedAction, targetType)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func decodeSettings(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	var resp map[string]bool
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("Antwort ist kein JSON: %v (%s)", err, body)
	}
	return resp
}

func TestSettingsHandlers_EmailEnabled(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, true)

	t.Run("GET liefert email_enabled=true, wenn der Key fehlt", func(t *testing.T) {
		w := doStatusPageRequest(r, "GET", "/api/settings", nil)
		requireStatusCode(t, w, http.StatusOK)
		resp := decodeSettings(t, w.Body.Bytes())
		if !resp["email_enabled"] || !resp["orders_enabled"] {
			t.Fatalf("erwartet beide Schalter an, erhalten %v", resp)
		}
	})

	t.Run("PUT nur mit email_enabled lässt orders_enabled unverändert", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"email_enabled": false})
		requireStatusCode(t, w, http.StatusOK)
		resp := decodeSettings(t, w.Body.Bytes())
		if resp["email_enabled"] || !resp["orders_enabled"] {
			t.Fatalf("erwartet email aus, orders an, erhalten %v", resp)
		}
		var ordersRows int64
		db.Model(&models.SystemSetting{}).Where("key = ?", "orders.enabled").Count(&ordersRows)
		if ordersRows != 0 {
			t.Errorf("orders.enabled darf nicht gespeichert werden, Zeilen: %d", ordersRows)
		}
		var audit models.AuditLog
		if err := db.Where("action = ? AND target_id = ?", "system_setting.updated", "email.enabled").First(&audit).Error; err != nil {
			t.Fatalf("Audit-Eintrag für email.enabled fehlt: %v", err)
		}
		if audit.TargetName != "email_enabled" || audit.Details != "false" {
			t.Errorf("Audit-Eintrag unerwartet: %+v", audit)
		}
	})

	t.Run("PUT nur mit orders_enabled lässt email_enabled unverändert", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": false})
		requireStatusCode(t, w, http.StatusOK)
		resp := decodeSettings(t, w.Body.Bytes())
		if resp["email_enabled"] || resp["orders_enabled"] {
			t.Fatalf("erwartet beide aus, erhalten %v", resp)
		}
	})

	t.Run("PUT mit beiden Feldern speichert und auditiert beide", func(t *testing.T) {
		var before int64
		db.Model(&models.AuditLog{}).Where("action = ?", "system_setting.updated").Count(&before)
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": true, "email_enabled": true})
		requireStatusCode(t, w, http.StatusOK)
		resp := decodeSettings(t, w.Body.Bytes())
		if !resp["email_enabled"] || !resp["orders_enabled"] {
			t.Fatalf("erwartet beide an, erhalten %v", resp)
		}
		var after int64
		db.Model(&models.AuditLog{}).Where("action = ?", "system_setting.updated").Count(&after)
		if after-before != 2 {
			t.Errorf("erwartet 2 neue Audit-Einträge, erhalten %d", after-before)
		}
	})

	t.Run("PUT nur mit unbekanntem Feld ergibt 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"unknown": true})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("PUT mit Nicht-Bool-Wert ergibt 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"email_enabled": "nein"})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("PUT mit email_enabled=null ergibt 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"email_enabled": nil})
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

func TestUpdateSettingsHandler_EmailSaveErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, true)
	if err := db.Migrator().DropTable(&models.SystemSetting{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"email_enabled": false})
	requireStatusCode(t, w, http.StatusInternalServerError)
}

func TestSettingsHandlers_EmailNonAdminForbidden(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, false)
	w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"email_enabled": false})
	requireStatusCode(t, w, http.StatusForbidden)
	var rows int64
	db.Model(&models.SystemSetting{}).Count(&rows)
	if rows != 0 {
		t.Errorf("Nicht-Admin darf nichts speichern, Zeilen: %d", rows)
	}
}

// Pfad 1: Bestell-Bestätigung (page_order.go).
func TestCreateOrderHandler_EmailDisabledSkipsMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	disableEmail(t, db)
	r := setupOrderRouter(handlers.NewHandler(db))

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Ohne Mail GmbH",
		"email":        "ohne-mail@example.com",
	})
	// Die Bestellung selbst darf nicht scheitern.
	requireStatusCode(t, w, http.StatusCreated)
	expectNoSMTPMessage(t, srv)

	entry := awaitSkippedAudit(t, db, "PageOrder")
	if entry.TargetName != "ohne-mail@example.com" || entry.Details != "recipient=ohne-mail@example.com" || entry.ActorEmail != "system" {
		t.Errorf("Audit-Eintrag unerwartet: %+v", entry)
	}
	if n := auditCountFor(db, "pageorder.email_failed"); n != 0 {
		t.Errorf("Übersprungener Versand darf nicht als Fehler auditiert werden, Einträge: %d", n)
	}
}

func TestCreateOrderHandler_EmailExplicitlyEnabledSendsMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	if err := db.Create(&models.SystemSetting{Key: "email.enabled", Value: "true"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	r := setupOrderRouter(handlers.NewHandler(db))

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Mit Mail GmbH",
		"email":        "mit-mail@example.com",
	})
	requireStatusCode(t, w, http.StatusCreated)
	awaitSMTPMessage(t, srv)
	if n := auditCountFor(db, skippedAction); n != 0 {
		t.Errorf("bei aktivem Versand kein %s erwartet, Einträge: %d", skippedAction, n)
	}
}

// Pfad 2: Abo-Bestätigung (subscriber_confirm.go).
func TestSubscribe_EmailDisabledSkipsConfirmationMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)
	disableEmail(t, db)

	w := subscribeEmail(r, "kein-versand@example.com", nil)
	requireStatusCode(t, w, http.StatusOK)
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)

	entry := awaitSkippedAudit(t, db, "Subscriber")
	if entry.Category != "subscriber" || entry.TargetName != "kein-versand@example.com" {
		t.Errorf("Audit-Eintrag unerwartet: %+v", entry)
	}
	if n := auditCountFor(db, "subscriber.confirmation_email_failed"); n != 0 {
		t.Errorf("Übersprungener Versand darf nicht als Fehler auditiert werden, Einträge: %d", n)
	}
}

// Pfad 3a: Incident-Mails über sendAlertEmail (incident.go).
func TestDispatchNotifications_EmailDisabledSkipsMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, h, comp, inc := setupDispatchFixtures("Toggle Page", "toggle-page")
	disableEmail(t, db)
	db.Create(&models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "toggle-sub@example.com", SubscribedComponentIDs: "all"})

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	entry := awaitSkippedAudit(t, db, "Incident")
	if entry.Category != "incident" || entry.TargetName != inc.Title {
		t.Errorf("Audit-Eintrag unerwartet: %+v", entry)
	}
	expectNoSMTPMessage(t, srv)
}

// Pfad 3b: Notice-Mails laufen ebenfalls über sendAlertEmail.
func TestDispatchNoticeNotifications_EmailDisabledSkipsMail(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "toggle-notice")
	db := h.DB
	disableEmail(t, db)
	notice := models.GeneralNotice{
		StatusPageID:      page.ID,
		Title:             "Stille Notice",
		Message:           "Geht nicht raus.",
		Level:             "info",
		IsActive:          true,
		NotifySubscribers: true,
	}
	h.DispatchNoticeNotifications(notice, "created")

	entry := awaitSkippedAudit(t, db, "Incident")
	if entry.TargetName != "Stille Notice" || entry.Details != "recipient=notice-sub-toggle-notice@example.com" {
		t.Errorf("Audit-Eintrag unerwartet: %+v", entry)
	}
	expectNoSMTPMessage(t, srv)
}
