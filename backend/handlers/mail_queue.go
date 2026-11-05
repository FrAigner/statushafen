package handlers

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"sync"
)

// Versand von Bestätigungsmails im Hintergrund.
//
// Der SMTP-Versand darf die HTTP-Anfrage nicht aufhalten: Ein hängender Server
// bände sie bis zu 15 s, und eine Adresse, für die eine Mail verschickt wird,
// antwortete dann spürbar langsamer als eine bereits bekannte (Orakel für
// vorhandene Adressen). Die Mails laufen deshalb über kleine, begrenzte
// Warteschlangen. Ist eine voll, wird die Mail verworfen und geloggt; das Abo
// bleibt wartend und lässt sich nach Ablauf des Links erneut anfordern. Ein
// unbegrenzter Rückstau bei ausgefallenem Mailserver ist damit ausgeschlossen.
//
// Zwei Grenzen verhindern, dass eine einzelne anonyme Quelle alle Plätze belegt
// und damit fremde Mails verdrängt:
//   - Je Zweck (Abo-Bestätigung, Bestellung, Einladung) gibt es eine eigene
//     Warteschlange; eine Flut an Abo-Anfragen verdrängt keine Einladungen.
//   - Innerhalb eines Zwecks darf ein Absender-Schlüssel (Seite, Quell-IP-Gruppe)
//     höchstens ein Viertel der Plätze belegen. Eine Quelle füllt die Schlange
//     so nie allein; mindestens vier unabhängige Schlüssel sind nötig. Ein
//     Viertel lässt einer Seite genug Luft für eine Spitze von Anmeldungen und
//     hält den Rest für andere Seiten frei.
//
// Vorfall- und Hinweis-Benachrichtigungen laufen nicht über diese Schlangen.

// defaultMailQueueSize: Obergrenze gleichzeitig angestoßener oder laufender
// Hintergrund-Mails je Zweck und Prozess. Reicht für Spitzen beim Abonnieren,
// hält die Zahl offener SMTP-Verbindungen und Goroutinen aber klein.
const defaultMailQueueSize = 32

// mailQueueSourceShare: Nenner des Anteils, den ein Schlüssel je Zweck
// belegen darf (1/4 der Plätze).
const mailQueueSourceShare = 4

// purposeQueue zählt die belegten Plätze eines Zwecks, gesamt und je Schlüssel.
type purposeQueue struct {
	used  int
	byKey map[string]int
}

// mailQueue ist die Warteschlange; die Nullwerte genügen (träge angelegt).
type mailQueue struct {
	mu       sync.Mutex
	size     int
	purposes map[string]*purposeQueue
	wg       sync.WaitGroup
}

// capacity liefert die Plätze je Zweck (Standard, wenn nichts gesetzt ist).
func (q *mailQueue) capacity() int {
	if q.size <= 0 {
		return defaultMailQueueSize
	}
	return q.size
}

// keyCapacity liefert die Plätze je Schlüssel: ein Viertel, mindestens einer.
func (q *mailQueue) keyCapacity() int {
	return max(1, q.capacity()/mailQueueSourceShare)
}

// acquire belegt einen Platz für den Zweck und alle Schlüssel oder meldet false.
func (q *mailQueue) acquire(purpose string, keys []string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.purposes == nil {
		q.purposes = map[string]*purposeQueue{}
	}
	pq := q.purposes[purpose]
	if pq == nil {
		pq = &purposeQueue{byKey: map[string]int{}}
		q.purposes[purpose] = pq
	}
	if pq.used >= q.capacity() {
		return false
	}
	for _, k := range keys {
		if pq.byKey[k] >= q.keyCapacity() {
			return false
		}
	}
	pq.used++
	for _, k := range keys {
		pq.byKey[k]++
	}
	return true
}

// release gibt die Plätze wieder frei.
func (q *mailQueue) release(purpose string, keys []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	pq := q.purposes[purpose]
	pq.used--
	for _, k := range keys {
		if pq.byKey[k]--; pq.byKey[k] <= 0 {
			delete(pq.byKey, k)
		}
	}
}

// SetMailQueueSize legt die Obergrenze je Zweck fest, bevor die erste Mail
// angestoßen wird; n <= 0 behält den Standard.
func (h *Handler) SetMailQueueSize(n int) {
	h.mailQ.mu.Lock()
	h.mailQ.size = n
	h.mailQ.mu.Unlock()
}

// WaitMails blockiert, bis alle im Hintergrund angestoßenen Mails verarbeitet
// sind (Tests und geordnetes Beenden).
func (h *Handler) WaitMails() { h.mailQ.wg.Wait() }

// mailSourceKey bildet aus einer Quell-IP den Schlüssel der Anteilsgrenze: IPv4
// je Adresse, IPv6 je /64-Präfix (wie der Rate-Limiter), damit ein Anschluss
// nicht durch wechselnde Adressen im eigenen Präfix mehr Plätze bekommt.
func mailSourceKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "ip:" + ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return "ip:" + addr.String()
	}
	prefix, _ := addr.Prefix(64)
	return "ip:" + prefix.String()
}

// mailPageKey ist der Schlüssel einer Seite für die Anteilsgrenze.
func mailPageKey(pageID uint) string { return "page:" + strconv.FormatUint(uint64(pageID), 10) }

// queueMail führt job im Hintergrund aus. Ist die Warteschlange des Zwecks oder
// der Anteil eines Schlüssels voll, wird er verworfen; das Log enthält nur die
// feste Meldung und den Zweck, nie Adressen oder Schlüssel. Ein Panic im Job
// beendet nur den Job, nicht den Prozess.
func (h *Handler) queueMail(purpose string, job func(), keys ...string) bool {
	q := &h.mailQ
	if !q.acquire(purpose, keys) {
		slog.Warn("Mail queue full, mail dropped", "purpose", purpose)
		return false
	}
	q.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Background mail job panicked", "purpose", purpose, "panic_type", fmt.Sprintf("%T", r))
			}
			q.release(purpose, keys)
			q.wg.Done()
		}()
		job()
	}()
	return true
}
