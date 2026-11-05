package handlers

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Template-Engine + Platzhalter-System für Benachrichtigungs-Mails.
//
// Die früheren fmt.Sprintf-basierten Subject/Body-Strings werden durch
// html/template ersetzt, sobald für die Kombination
// (StatusPage, Kind, Language) ein NotificationTemplate hinterlegt ist.
// Fehlt ein Custom-Template, greift der bestehende Default-Renderer
// (buildNotificationEmailHTML), damit sich das Verhalten für alle Seiten
// ohne eigene Konfiguration bitgenau nicht ändert.
//
// html/template escaped Platzhalterwerte automatisch (Regressionsschutz). Die Absender-Validierung gegen SMTP-Header-Injection erledigt
// weiterhin sendAlertEmail per hasHeaderInjectionChars auf dem fertig
// gerenderten Subject/Body - ein manipuliertes Template, das CR/LF
// einschleust, wird dort also weiterhin verworfen.

// Notification-Template-Kinds. Bewusst als typisierte Konstanten statt
// Magic-Strings, damit ein Tippfehler beim Nachschlagen des Templates
// gleich zur Compile-Zeit auffällt.
// Incident-Kinds stammen, Maintenance-Kinds sind dazugekommen
// (analog zu den drei DispatchNotifications-Actions in maintenance_scheduler.go:
// MAINTENANCE_REMINDER / MAINTENANCE_STARTED / MAINTENANCE_COMPLETED).
//
// General-Notice-Kinds sind bewusst NICHT enthalten: GeneralNotice-Endpunkte
// versenden aktuell keine Mails (siehe handlers/general_notice.go), das
// die Vorgabe formuliert diesen Teil deshalb explizit als konditional ("falls
// diese per Mail versendet werden"). Wenn Mail-Versand für Notices später
// nachgezogen wird, kommen die zugehörigen Kinds hier hinzu.
const (
	NotificationKindIncidentCreated      = "incident_created"
	NotificationKindIncidentUpdated      = "incident_updated"
	NotificationKindIncidentResolved     = "incident_resolved"
	NotificationKindMaintenanceReminder  = "maintenance_reminder"
	NotificationKindMaintenanceStarted   = "maintenance_started"
	NotificationKindMaintenanceCompleted = "maintenance_completed"
	// Notice-Kinds: kommen zum Tragen, wenn eine GeneralNotice
	// mit NotifySubscribers=true angelegt/aktualisiert/beendet wird und
	// dabei durch DispatchNoticeNotifications geschickt wird.
	NotificationKindNoticeCreated = "notice_created"
	NotificationKindNoticeUpdated = "notice_updated"
	NotificationKindNoticeEnded   = "notice_ended"
	// Bestätigungsmail des Double-Opt-in für E-Mail-Abos
	// (Audience subscriber, Platzhalter {{.ConfirmURL}}).
	NotificationKindSubscriptionConfirm = "subscription_confirm"
	// Teil B2: Meldungen externer Quellen an Abonnenten
	// (Audience subscriber, Platzhalter {{.SourceName}}/{{.SourceURL}}).
	NotificationKindExternalIncidentCreated     = "external_incident_created"
	NotificationKindExternalIncidentUpdated     = "external_incident_updated"
	NotificationKindExternalIncidentResolved    = "external_incident_resolved"
	NotificationKindExternalMaintenanceCreated  = "external_maintenance_created"
	NotificationKindExternalMaintenanceUpdated  = "external_maintenance_updated"
	NotificationKindExternalMaintenanceResolved = "external_maintenance_resolved"
)

// Notification-Audience-Werte (Stage 5 der Mehrsprachigkeit):
// lösen die vorherige Konvention auf, bei der die Zielgruppe implizit über
// Language=en ("staff") bzw. Language=de ("subscriber") codiert war (siehe
// Kommentar an models.NotificationTemplate). NotificationAudienceStaff
// erreicht interne User (Admin/Editor), NotificationAudienceSubscriber die
// öffentlichen Abonnenten.
const (
	NotificationAudienceStaff      = "staff"
	NotificationAudienceSubscriber = "subscriber"
)

// NotificationTemplateData bündelt die Platzhalter, die einem Template zur
// Verfügung stehen. Feldnamen sind bewusst sprechend gehalten, damit
// Templates lesbar bleiben ({{.IncidentTitle}} statt {{.T}}).
//
// Alle Felder sind Rohwerte - html/template escaped sie beim Rendern
// automatisch. Kein Feld darf hier vorab escaped werden, sonst würde
// doppelt escaped und der User sähe &amp;lt; statt <.
type NotificationTemplateData struct {
	// IncidentTitle bleibt für Rückwärts-Kompatibilität mit Templates aus
	// früheren Versionen erhalten. Neu empfohlen: Title (funktioniert für
	// Incidents UND Notices gleichermaßen). Beide Felder halten für
	// Incident-Dispatches denselben Wert.
	IncidentTitle  string
	Title          string
	Status         string
	StatusLabel    string
	Level          string // nur für Notice-Kinds befüllt: info/warning/critical
	ComponentNames string
	Tags           string
	// Message bleibt der Rohtext (Markdown-Quelltext, für Altvorlagen wie
	// bisher). MessageHTML ist die formatierte Fassung als
	// fertiges, bereits bereinigtes Mail-HTML (richtext.ToMailHTML) und
	// wird von html/template unmaskiert ausgegeben.
	Message        string
	MessageHTML    htmltemplate.HTML
	Action         string
	Language       string
	FrontendURL    string
	UnsubscribeURL string
	// ConfirmURL: Bestätigungslink des Double-Opt-in, nur für
	// Kind subscription_confirm befüllt.
	ConfirmURL  string
	FromAddress string
	// SourceName/SourceURL: Name der externen Quelle und Link auf
	// die Original-Meldung, nur für die Kinds external_* befüllt.
	SourceName string
	SourceURL  string
}

// incidentTemplateDataOpts bündelt die Eingaben für newIncidentTemplateData,
// damit die Parameterliste die SonarCloud-Obergrenze von 7 nicht überschreitet.
type incidentTemplateDataOpts struct {
	Title       string
	StatusRaw   string
	CompNames   string
	TagNames    string
	Message     string
	MessageHTML string
	Action      string
	Language    string
	FrontendURL string
	FromAddress string
}

// newIncidentTemplateData baut die Platzhalter aus einem Incident +
// begleitender Metainfo (Sprache, URLs, Absender). Bewusst als Helper
// statt zweier fast-identischer Struct-Literale in DispatchNotifications -
// so kann der Datensatz pro Sprache mit einem Aufruf erzeugt werden und
// per-Empfänger-Felder (UnsubscribeURL/FrontendURL) werden anschließend
// nur noch am Rückgabewert gesetzt.
func newIncidentTemplateData(opts incidentTemplateDataOpts) NotificationTemplateData {
	return NotificationTemplateData{
		IncidentTitle:  opts.Title,
		Title:          opts.Title,
		Status:         opts.StatusRaw,
		StatusLabel:    statusLabelFor(opts.StatusRaw, opts.Language),
		ComponentNames: opts.CompNames,
		Tags:           opts.TagNames,
		Message:        opts.Message,
		MessageHTML:    htmltemplate.HTML(opts.MessageHTML), // #nosec G203 -- Ergebnis von richtext.ToMailHTML, siehe dort
		Action:         opts.Action,
		Language:       opts.Language,
		FrontendURL:    opts.FrontendURL,
		FromAddress:    opts.FromAddress,
	}
}

// statusLabelFor liefert das lokalisierte Label zum Status ("Investigating"/
// "Untersuchung"/...) mit Fallback auf den Rohwert für unbekannte Stati.
// Wird als vorgerechneter Platzhalter {{.StatusLabel}} an Templates
// gereicht, damit Autoren nicht selbst die Sprach-Umschaltung nachbauen
// müssen.
func statusLabelFor(status, lang string) string {
	meta, ok := incidentStatusMeta[status]
	if !ok {
		return status
	}
	if lang == "de" {
		return meta.labelDE
	}
	return meta.labelEN
}

// resolveIncidentKind mappt die DispatchNotifications-Actions
// ("CREATED"/"UPDATED"/...) auf einen NotificationTemplate-Kind. Ein
// UPDATED-Event mit finalem Status "resolved" wird zum eigenständigen
// "incident_resolved"-Kind, damit Admins die Auflösungs-Mail getrennt
// vom regulären Status-Update anpassen können.
func resolveIncidentKind(action, status string) string {
	switch action {
	case "CREATED":
		return NotificationKindIncidentCreated
	case "UPDATED":
		if models.IsResolvedIncidentStatus(status) {
			return NotificationKindIncidentResolved
		}
		return NotificationKindIncidentUpdated
	case "MAINTENANCE_REMINDER":
		return NotificationKindMaintenanceReminder
	case "MAINTENANCE_STARTED":
		return NotificationKindMaintenanceStarted
	case "MAINTENANCE_COMPLETED":
		return NotificationKindMaintenanceCompleted
	}
	// Unbekannte Action: lookupNotificationTemplate überspringt die
	// DB-Abfrage bei leerem Kind (siehe dort) - der Aufrufer bekommt dann
	// den hartkodierten Default-Renderer.
	return ""
}

// lookupNotificationTemplate lädt das gespeicherte Custom-Template für
// (statusPageID, kind, audience, language), sofern vorhanden. Nil-Rückgabe =
// keins, dann fällt der Renderer auf den Default zurück.
func (h *Handler) lookupNotificationTemplate(statusPageID uint, kind, audience, language string) *models.NotificationTemplate {
	if kind == "" {
		return nil
	}
	var tmpl models.NotificationTemplate
	err := h.DB.Where("status_page_id = ? AND kind = ? AND audience = ? AND language = ?", statusPageID, kind, audience, language).First(&tmpl).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("Failed to load notification template", "status_page_id", statusPageID, "kind", kind, "audience", audience, "language", language, "error", err)
		}
		return nil
	}
	return &tmpl
}

// renderNotificationTemplate parst und rendert Subject und Body eines
// Custom-Templates. Fehlerfall (Parse-/Execute-Fehler): leere Strings +
// error - der Aufrufer entscheidet dann per Fallback auf den Default.
//
// html/template escaped Platzhalterwerte automatisch, das ist der
// eigentliche Grund für html/template statt text/template hier.
func renderNotificationTemplate(tmpl *models.NotificationTemplate, data NotificationTemplateData) (subject, body string, err error) {
	subject, err = executeStringTemplate("subject", tmpl.Subject, data)
	if err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}
	body, err = executeStringTemplate("body", tmpl.BodyHTML, data)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return subject, body, nil
}

// executeStringTemplate parst, prüft (Länge und erlaubte Konstrukte) und
// rendert eine Vorlage mit begrenzter Ausgabegröße und Laufzeit. Die Prüfung
// läuft bei jeder Ausführung, weil auch bereits gespeicherte Vorlagen aus der
// Zeit vor den Grenzen nichts Unzulässiges mehr ausführen dürfen.
func executeStringTemplate(name, raw string, data NotificationTemplateData) (string, error) {
	maxIn, maxOut := MaxTemplateBodyLen, maxTemplateBodyOutput
	if name == "subject" {
		maxIn, maxOut = MaxTemplateSubjectLen, maxTemplateSubjectOutput
	}
	t, err := parseAllowedTemplate(name, raw, maxIn)
	if err != nil {
		return "", err
	}
	return runWithTimeout(templateExecTimeout, func() (string, error) {
		w := &limitedWriter{max: maxOut}
		if err := t.Execute(w, data); err != nil {
			if w.exceeded {
				return "", errTemplateOutputTooLarge()
			}
			return "", err
		}
		return w.buf.String(), nil
	})
}

// ValidateNotificationTemplate prüft ein Template vor dem Speichern:
// Parse-Fehler und CR/LF im Subject (SMTP-Header-Injection-Sink) werden
// abgelehnt. Der Body darf CR/LF enthalten - sendAlertEmail faltet ihn vor
// dem Versand zu einer Zeile.
//
// Der explizite CR/LF-Guard hier hat einen Grund: sendAlertEmail lehnt
// zwar dieselben Zeichen zur Sendezeit ab (Defense-in-Depth bleibt), aber
// dann würde jede einzelne Mail dieser Statusseite still verworfen. Die
// Prüfung beim Speichern verhindert das schon im Admin-UI mit klarer
// Fehlermeldung (Vorbereitung).
func ValidateNotificationTemplate(subject, body string) error {
	return ValidateNotificationTemplateForKind("", subject, body)
}

// ValidateNotificationTemplateForKind prüft zusätzlich die Pflichtangaben der
// Art: Die Bestätigungsmail des Double-Opt-in muss den Bestätigungslink
// ({{.ConfirmURL}}) unbedingt ausgeben, sonst ließe sich die Vorlage für
// beliebigen Inhalt an Dritte missbrauchen und könnte kein Abo bestätigen.
func ValidateNotificationTemplateForKind(kind, subject, body string) error {
	if strings.TrimSpace(subject) == "" {
		return newCodedError("template_subject_empty", "subject darf nicht leer sein")
	}
	if strings.TrimSpace(body) == "" {
		return newCodedError("template_body_empty", "body darf nicht leer sein")
	}
	if hasHeaderInjectionChars(subject) {
		return newCodedError("template_subject_crlf", "subject darf keine CR/LF-Zeichen enthalten (SMTP-Header-Injection)")
	}
	if _, err := parseAllowedTemplate("subject", subject, MaxTemplateSubjectLen); err != nil {
		return err
	}
	_, used, err := parseAllowedTemplateFields("body", body, MaxTemplateBodyLen)
	if err != nil {
		return err
	}
	if kind == NotificationKindSubscriptionConfirm && !used["ConfirmURL"] {
		return errConfirmURLMissing()
	}
	return nil
}

func errConfirmURLMissing() error {
	return newCodedError("template_confirm_url_missing", "Die Vorlage muss den Platzhalter {{.ConfirmURL}} enthalten")
}

// applyNotificationTemplate rendert das übergebene Custom-Template ODER gibt
// die Defaults zurück, wenn kein Template gesetzt ist oder das Rendern
// fehlschlägt (letzteres wird geloggt - eine defekte Vorlage darf den
// Mailversand nicht komplett unterbinden). Der Lookup wird bewusst NICHT
// hier gemacht, damit DispatchNotifications ihn einmal pro Sprache statt
// pro Empfänger machen kann.
func applyNotificationTemplate(
	tmpl *models.NotificationTemplate,
	data NotificationTemplateData,
	defaultSubject string,
	defaultBody string,
) (subject, body string) {
	if tmpl == nil {
		return defaultSubject, defaultBody
	}
	subject, body, err := renderNotificationTemplate(tmpl, data)
	if err != nil {
		slog.Error("Notification template render failed, falling back to default", "template_id", tmpl.ID, "status_page_id", tmpl.StatusPageID, "kind", tmpl.Kind, "audience", tmpl.Audience, "language", tmpl.Language, "error_code", TemplateErrorCode(err))
		return defaultSubject, defaultBody
	}
	return subject, body
}
