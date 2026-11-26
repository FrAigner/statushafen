package handlers

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"sync"
)

// Sending confirmation mails in the background.
//
// SMTP sending must not hold up the HTTP request: a hanging server
// would tie it up for up to 15 s, and an address for which a mail is sent
// would then respond noticeably slower than an already known one (oracle for
// existing addresses). The mails therefore run through small, bounded
// queues. If one is full, the mail is dropped and logged; the subscription
// stays pending and can be requested again after the link expires. An
// unbounded backlog when the mail server is down is thereby ruled out.
//
// Two limits prevent a single anonymous source from occupying all slots
// and thereby displacing other people's mails:
//   - Each purpose (subscription confirmation, order, invitation) has its own
//     queue; a flood of subscription requests does not displace invitations.
//   - Within one purpose, a sender key (page, source IP group)
//     may occupy at most a quarter of the slots. A single source thus never
//     fills the queue alone; at least four independent keys are needed. A
//     quarter leaves a page enough room for a spike of sign-ups and
//     keeps the rest free for other pages.
//
// Incident and notice notifications do not run through these queues.

// defaultMailQueueSize: upper bound of simultaneously triggered or running
// background mails per purpose and process. Sufficient for subscription spikes,
// but keeps the number of open SMTP connections and goroutines small.
const defaultMailQueueSize = 32

// mailQueueSourceShare: denominator of the share a key may
// occupy per purpose (1/4 of the slots).
const mailQueueSourceShare = 4

// purposeQueue counts the occupied slots of a purpose, in total and per key.
type purposeQueue struct {
	used  int
	byKey map[string]int
}

// mailQueue is the queue; the zero values suffice (lazily created).
type mailQueue struct {
	mu       sync.Mutex
	size     int
	purposes map[string]*purposeQueue
	wg       sync.WaitGroup
}

// capacity returns the slots per purpose (default if nothing is set).
func (q *mailQueue) capacity() int {
	if q.size <= 0 {
		return defaultMailQueueSize
	}
	return q.size
}

// keyCapacity returns the slots per key: a quarter, at least one.
func (q *mailQueue) keyCapacity() int {
	return max(1, q.capacity()/mailQueueSourceShare)
}

// acquire occupies a slot for the purpose and all keys, or reports false.
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

// release frees the slots again.
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

// SetMailQueueSize sets the upper bound per purpose before the first mail
// is triggered; n <= 0 keeps the default.
func (h *Handler) SetMailQueueSize(n int) {
	h.mailQ.mu.Lock()
	h.mailQ.size = n
	h.mailQ.mu.Unlock()
}

// WaitMails blocks until all mails triggered in the background have been processed
// (tests and orderly shutdown).
func (h *Handler) WaitMails() { h.mailQ.wg.Wait() }

// mailSourceKey forms the share-limit key from a source IP: IPv4
// per address, IPv6 per /64 prefix (like the rate limiter), so that one connection
// does not get more slots by rotating addresses within its own prefix.
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

// mailPageKey is the key of a page for the share limit.
func mailPageKey(pageID uint) string { return "page:" + strconv.FormatUint(uint64(pageID), 10) }

// queueMail runs job in the background. If the purpose's queue or
// a key's share is full, it is dropped; the log contains only the
// fixed message and the purpose, never addresses or keys. A panic in the job
// ends only the job, not the process.
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
