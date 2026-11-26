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

// smtpConfig bundles the SMTP connection data for the
// e-mail sending performed by DispatchNotifications.
//
// production provider: only host/port/
// optional PlainAuth credentials/sender address are needed, compatible with
// SendGrid, Postmark, a self-hosted server, or similar.
type smtpConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	// ReplyTo is the optional reply address of a status page
	// (applyPageSender); empty = no Reply-To header.
	ReplyTo string
}

// defaultSMTPHost/-Port match the local Mailpit SMTP server from
// docker-compose.yml (lines 17-23) and remain the fallback when
// SMTP_HOST/SMTP_PORT are not set, so that local development
// keeps working unchanged.
const (
	defaultSMTPHost = "localhost"
	defaultSMTPPort = "1025"
	defaultSMTPFrom = "alerts@statushafen.local"
)

// Time limits for sending over SMTP. Without them a hanging server would keep every
// triggering request (subscription, order, invitation) open indefinitely. They are variables
// so that tests can shorten them.
var (
	smtpDialTimeout  = 5 * time.Second
	smtpTotalTimeout = 15 * time.Second
)

// loadSMTPConfig liest SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD/SMTP_FROM
// from the environment. If the env vars are missing (local development), it falls back to the
// Mailpit defaults. Extracted into a separate, untested function
// so that the dial target is testable without sending real emails
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

// applyPageSender sets the sender and reply address for mails of a
// status page (see resolveNotificationFromAddress).
func (c *smtpConfig) applyPageSender(page models.StatusPage) {
	c.From = resolveNotificationFromAddress(c.From, page)
	c.ReplyTo = resolveNotificationReplyTo(page)
}

// fromHeaders returns the header lines From (and Reply-To, if set) including the
// trailing line break. Both values have been checked for CR/LF beforehand
// (hasHeaderInjectionChars by the caller or resolveNotificationReplyTo).
func (c smtpConfig) fromHeaders() string {
	h := "From: " + c.From + "\n"
	if c.ReplyTo != "" {
		h += "Reply-To: " + c.ReplyTo + "\n"
	}
	return h
}

// addr returns the "host:port" dial target for smtp.SendMail.
func (c smtpConfig) addr() string {
	return c.Host + ":" + c.Port
}

// envelopeFrom returns the bare sender address for the SMTP MAIL FROM command.
// SMTP_FROM is deliberately in the format "Display Name <address>"
// (for the From: header), but the envelope sender - the from argument of
// smtp.SendMail, which net/smtp turns into "MAIL FROM" - must be the
// bare address according to RFC 5321. Strict providers (e.g. Brevo) otherwise reject the display-name
// format with "501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>";
// Postmark was tolerant here. If From cannot be parsed, it is
// returned unchanged (fallback, does not change existing behavior).
func (c smtpConfig) envelopeFrom() string {
	if addr, err := mail.ParseAddress(c.From); err == nil {
		return addr.Address
	}
	return c.From
}

// mailAuditTarget describes what a mail refers to (order,
// subscriber, incident/notice). deliverMail uses it to write the
// audit entry when sending is globally disabled.
type mailAuditTarget struct {
	Category   string
	TargetType string
	TargetID   string
	TargetName string
}

// deliverMail is the only place that calls smtp.SendMail.
// If email sending is disabled via the system setting email.enabled,
// nothing goes out: the skipped send is logged and written to the audit log as
// email.skipped_disabled, and deliverMail reports
// sent=false without an error. This way the triggering action does not fail (as with
// a send error), and callers do not log a success. The check
// is deliberately here instead of in the three send paths (order and subscription
// confirmation, sendAlertEmail), so that no future path can forget it.
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

// sendMailWithTimeout is equivalent to smtp.SendMail (EHLO, STARTTLS if
// offered, optional login, one recipient), but sets a dial timeout
// and an overall deadline for the whole session, so that a hanging server does not
// block the caller indefinitely.
func sendMailWithTimeout(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := (&net.Dialer{Timeout: smtpDialTimeout}).Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return sendMailSession(conn, host, auth, from, to, msg)
}

// sendMailSession runs the SMTP session on an already established
// connection and sets the overall deadline for it. Separated from sendMailWithTimeout
// so that tests can pass in an already closed connection
// (SetDeadline then fails); the caller closes the connection.
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

// auth returns PlainAuth if SMTP_USER/SMTP_PASSWORD are set, otherwise
// nil (unauthenticated connection, e.g. locally against Mailpit).
func (c smtpConfig) auth() smtp.Auth {
	if c.User == "" || c.Password == "" {
		return nil
	}
	return smtp.PlainAuth("", c.User, c.Password, c.Host)
}
