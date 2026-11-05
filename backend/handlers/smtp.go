package handlers

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"time"

	"github.com/fraigner/statushafen/models"
)

// smtpConfig bündelt die SMTP-Verbindungsdaten für den in
// DispatchNotifications versendeten E-Mail-Versand.
//
// Produktions-Anbieter unabhängig gehalten: es werden nur Host/Port/
// optionale PlainAuth-Credentials/Absenderadresse benötigt, kompatibel mit
// SendGrid, Postmark, einem eigenen Server o.ä.
type smtpConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	// ReplyTo ist die optionale Antwortadresse einer Statusseite
	// (applyPageSender); leer = kein Reply-To-Header.
	ReplyTo string
}

// defaultSMTPHost/-Port entsprechen dem lokalen Mailpit-SMTP-Server aus
// docker-compose.yml (Zeilen 17-23) und bleiben der Fallback, wenn
// SMTP_HOST/SMTP_PORT nicht gesetzt sind, damit lokale Entwicklung
// unverändert funktioniert.
const (
	defaultSMTPHost = "localhost"
	defaultSMTPPort = "1025"
	defaultSMTPFrom = "alerts@statushafen.local"
)

// Zeitgrenzen für den SMTP-Versand. Ohne sie hielte ein hängender Server jede
// auslösende Anfrage (Abo, Bestellung, Einladung) dauerhaft offen. Variablen,
// damit Tests sie verkürzen können.
var (
	smtpDialTimeout  = 5 * time.Second
	smtpTotalTimeout = 15 * time.Second
)

// loadSMTPConfig liest SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD/SMTP_FROM
// aus der Umgebung. Fehlen die Env-Vars (lokale Entwicklung), wird auf die
// Mailpit-Defaults zurückgefallen. In eine eigene, ungetestete Funktion
// ausgelagert, damit der Dial-Target testbar ist, ohne real E-Mails zu
// versenden.
func loadSMTPConfig() smtpConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = defaultSMTPHost
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = defaultSMTPPort
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = defaultSMTPFrom
	}
	return smtpConfig{
		Host:     host,
		Port:     port,
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     from,
	}
}

// applyPageSender setzt Absender und Antwortadresse für Mails einer
// Statusseite (siehe resolveNotificationFromAddress).
func (c *smtpConfig) applyPageSender(page models.StatusPage) {
	c.From = resolveNotificationFromAddress(c.From, page)
	c.ReplyTo = resolveNotificationReplyTo(page)
}

// fromHeaders liefert die Kopfzeilen From (und Reply-To, falls gesetzt) samt
// abschließendem Zeilenumbruch. Beide Werte sind vorher auf CR/LF geprüft
// (hasHeaderInjectionChars beim Aufrufer bzw. resolveNotificationReplyTo).
func (c smtpConfig) fromHeaders() string {
	h := "From: " + c.From + "\n"
	if c.ReplyTo != "" {
		h += "Reply-To: " + c.ReplyTo + "\n"
	}
	return h
}

// addr liefert das "host:port"-Dial-Target für smtp.SendMail.
func (c smtpConfig) addr() string {
	return c.Host + ":" + c.Port
}

// envelopeFrom liefert die nackte Absenderadresse für den SMTP-MAIL-FROM-Befehl
// SMTP_FROM liegt bewusst im Format "Anzeigename <adresse>" vor
// (für den From:-Header), aber der Envelope-Absender - das from-Argument von
// smtp.SendMail, das net/smtp zu "MAIL FROM" macht - muss laut RFC 5321 die
// nackte Adresse sein. Strikte Anbieter (z.B. Brevo) lehnen das Anzeigename-
// Format sonst mit "501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>"
// ab; Postmark war hier tolerant. Lässt sich From nicht parsen, wird es
// unverändert zurückgegeben (Fallback, ändert bisheriges Verhalten nicht).
func (c smtpConfig) envelopeFrom() string {
	if addr, err := mail.ParseAddress(c.From); err == nil {
		return addr.Address
	}
	return c.From
}

// mailAuditTarget beschreibt, worauf sich eine Mail bezieht (Bestellung,
// Subscriber, Incident/Notice). deliverMail schreibt damit den
// Audit-Eintrag, wenn der Versand global abgeschaltet ist.
type mailAuditTarget struct {
	Category   string
	TargetType string
	TargetID   string
	TargetName string
}

// deliverMail ist die einzige Stelle, die smtp.SendMail aufruft.
// Ist der E-Mail-Versand per Systemeinstellung email.enabled abgeschaltet,
// geht nichts raus: Der übersprungene Versand wird geloggt und als
// email.skipped_disabled ins Audit-Log geschrieben, und deliverMail meldet
// sent=false ohne Fehler. So scheitert die auslösende Aktion nicht (wie bei
// einem Versandfehler), und die Aufrufer loggen keinen Erfolg. Die Prüfung
// steht bewusst hier statt in den drei Versandpfaden (Bestell-, Abo-
// Bestätigung, sendAlertEmail), damit kein künftiger Pfad sie vergessen kann.
func (h *Handler) deliverMail(cfg smtpConfig, recipient string, msg []byte, target mailAuditTarget) (bool, error) {
	if !h.getSystemSettingBool(settingEmailEnabled, true) {
		slog.Warn("E-Mail-Versand global deaktiviert, Mail nicht gesendet",
			"target_type", target.TargetType, "target_id", target.TargetID)
		h.RecordSystemAudit("email.skipped_disabled", target.Category, target.TargetType, target.TargetID, target.TargetName,
			"recipient="+recipient)
		return false, nil
	}
	return true, sendMailWithTimeout(cfg.addr(), cfg.Host, cfg.auth(), cfg.envelopeFrom(), recipient, msg)
}

// sendMailWithTimeout entspricht smtp.SendMail (EHLO, STARTTLS wenn
// angeboten, optionale Anmeldung, ein Empfänger), setzt aber ein Dial-Timeout
// und eine Gesamtfrist für die ganze Sitzung, damit ein hängender Server den
// Aufrufer nicht dauerhaft blockiert.
func sendMailWithTimeout(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := (&net.Dialer{Timeout: smtpDialTimeout}).Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return sendMailSession(conn, host, auth, from, to, msg)
}

// sendMailSession führt die SMTP-Sitzung auf einer bereits aufgebauten
// Verbindung aus und setzt dafür die Gesamtfrist. Von sendMailWithTimeout
// getrennt, damit Tests eine bereits geschlossene Verbindung übergeben können
// (SetDeadline schlägt dann fehl); die Verbindung schließt der Aufrufer.
func sendMailSession(conn net.Conn, host string, auth smtp.Auth, from, to string, msg []byte) error {
	if err := conn.SetDeadline(time.Now().Add(smtpTotalTimeout)); err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// auth liefert PlainAuth, wenn SMTP_USER/SMTP_PASSWORD gesetzt sind, sonst
// nil (unauthentifizierte Verbindung, z.B. lokal gegen Mailpit).
func (c smtpConfig) auth() smtp.Auth {
	if c.User == "" || c.Password == "" {
		return nil
	}
	return smtp.PlainAuth("", c.User, c.Password, c.Host)
}
