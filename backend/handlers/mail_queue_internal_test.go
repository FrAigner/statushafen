package handlers

import (
	"bytes"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ein Panic im Hintergrundjob beendet nur den Job: Der Prozess läuft weiter,
// der Platz in der Warteschlange wird frei, und das Log nennt nur den Typ des
// Panics, nie dessen Inhalt.
func TestQueueMail_PanicGibtPlatzFreiUndLoggtKeinenInhalt(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := &Handler{}
	h.SetMailQueueSize(1)
	assert.True(t, h.queueMail("test", func() { panic("geheimer-inhalt") }))
	h.WaitMails()

	var ran atomic.Bool
	assert.True(t, h.queueMail("test", func() { ran.Store(true) }), "der Platz ist nach dem Panic wieder frei")
	h.WaitMails()
	assert.True(t, ran.Load())
	assert.Contains(t, buf.String(), "panic_type=string")
	assert.NotContains(t, buf.String(), "geheimer-inhalt")
}

func TestQueueMail_StandardgroesseUndVollerZustand(t *testing.T) {
	h := &Handler{}
	h.SetMailQueueSize(0)
	assert.Equal(t, defaultMailQueueSize, h.mailQ.capacity())
	assert.Equal(t, defaultMailQueueSize/4, h.mailQ.keyCapacity())

	block := make(chan struct{})
	full := &Handler{}
	full.SetMailQueueSize(1)
	assert.True(t, full.queueMail("test", func() { <-block }))
	assert.False(t, full.queueMail("test", func() { t.Error("darf nicht laufen") }), "voll: verworfen")
	close(block)
	full.WaitMails()
}

// Die Warteschlangen sind nach Zweck getrennt: Sind alle Plätze der
// Abo-Bestätigungen belegt (hier über vier Schlüssel mit je einem Viertel),
// werden Bestellungen und Einladungen weiter angenommen.
func TestQueueMail_VolleAboWarteschlangeVerdraengtAndereZweckeNicht(t *testing.T) {
	h := &Handler{}
	h.SetMailQueueSize(8)
	block := make(chan struct{})
	for _, key := range []string{"page:1", "page:2", "page:3", "page:4"} {
		for i := 0; i < 2; i++ {
			assert.True(t, h.queueMail("subscription_confirmation", func() { <-block }, key))
		}
	}
	assert.False(t, h.queueMail("subscription_confirmation", func() { t.Error("darf nicht laufen") }, "page:5"), "Abo-Queue voll")
	assert.True(t, h.queueMail("order_confirmation", func() { <-block }, "ip:192.0.2.1"), "Bestellung unberührt")
	assert.True(t, h.queueMail("page_invitation", func() { <-block }, "page:1"), "Einladung unberührt")
	close(block)
	h.WaitMails()
}

// Ein Schlüssel belegt höchstens ein Viertel; fertige Mails geben den Anteil
// frei, und Mails ohne Schlüssel unterliegen nur der Gesamtgrenze.
func TestQueueMail_AnteilJeSchluesselWirdFreigegeben(t *testing.T) {
	h := &Handler{}
	h.SetMailQueueSize(8)
	block := make(chan struct{})
	assert.True(t, h.queueMail("subscription_confirmation", func() { <-block }, "page:1", "ip:192.0.2.1"))
	assert.True(t, h.queueMail("subscription_confirmation", func() { <-block }, "page:1", "ip:192.0.2.2"))
	assert.False(t, h.queueMail("subscription_confirmation", func() { t.Error("darf nicht laufen") }, "page:1", "ip:192.0.2.3"), "Seite hat ihren Anteil")
	assert.True(t, h.queueMail("subscription_confirmation", func() { <-block }, "page:2", "ip:192.0.2.9"), "andere Seite und Quelle")
	close(block)
	h.WaitMails()
	assert.True(t, h.queueMail("subscription_confirmation", func() {}, "page:1", "ip:192.0.2.1"), "Anteil nach Abschluss wieder frei")
	h.WaitMails()
	assert.Empty(t, h.mailQ.purposes["subscription_confirmation"].byKey, "keine Reste in der Schlüsselzählung")
}

func TestMailSourceKey_FasstIPv6ZumPraefixZusammen(t *testing.T) {
	assert.Equal(t, "ip:192.0.2.1", mailSourceKey("192.0.2.1"))
	assert.Equal(t, "ip:192.0.2.1", mailSourceKey("::ffff:192.0.2.1"))
	assert.Equal(t, mailSourceKey("2001:db8:1:2::1"), mailSourceKey("2001:db8:1:2:ffff::9"))
	assert.NotEqual(t, mailSourceKey("2001:db8:1:2::1"), mailSourceKey("2001:db8:1:3::1"))
	assert.Equal(t, "ip:kein-ip", mailSourceKey("kein-ip"))
}
