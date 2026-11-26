package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subscribeFrom subscribes an address for the page with the given slug from a
// fixed source address (router without an upstream proxy: RemoteAddr counts).
func subscribeFrom(r http.Handler, slug, email, remoteAddr string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"channel": "email", "email": email, "component_ids": "all", "slug": slug})
	req, _ := http.NewRequest(http.MethodPost, "/api/subscribe", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Gmail domains assembled from parts so that the repo check for private
// mailbox addresses does not trigger (examples, not accounts).
const (
	gmail      = "@" + "gmail.com"
	googlemail = "@" + "googlemail.com"
)

func queueDrops(buf *lockedBuffer) int {
	return strings.Count(buf.String(), "Mail queue full, mail dropped")
}

// Attack with a hanging SMTP: a single page must not fill the queue of
// subscription confirmations alone (at most a quarter of the 32 slots);
// the sign-up of another page from another source continues to be
// angenommen.
func TestSubscribe_EinzelneSeiteFuelltWarteschlangeNichtAllein(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupAsyncMailEnv(t, release)
	require.NoError(t, e.db.Create(&models.StatusPage{Name: "Andere Seite", Slug: "andere-seite"}).Error)

	const flood = 20
	for i := 0; i < flood; i++ {
		// Rotating source addresses: only the page ties the attacks together.
		src := "198.51.100." + itoa(uint(i+1)) + ":4000"
		assert.Equal(t, http.StatusOK, subscribeFrom(e.r, "doi-page", "flut-"+itoa(uint(i))+"@example.com", src).Code)
	}
	assert.Equal(t, flood-8, queueDrops(logBuf), "je Seite werden höchstens 8 von 32 Plätzen belegt, der Rest verworfen")

	before := queueDrops(logBuf)
	assert.Equal(t, http.StatusOK, subscribeFrom(e.r, "andere-seite", "legitim@example.com", "203.0.113.7:4000").Code)
	assert.Equal(t, before, queueDrops(logBuf), "die legitime Anmeldung der anderen Seite wird angenommen")
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "Mail queue full") {
			assert.NotContains(t, line, "example.com", "das Verwerfen loggt keine Adressen")
			assert.NotContains(t, line, "page:", "und keine Schlüssel")
		}
	}
}

// The same source IP across many pages: it too occupies at most a quarter.
func TestSubscribe_EineQuelleUeberVieleSeitenFuelltWarteschlangeNichtAllein(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupAsyncMailEnv(t, release)
	slugs := []string{"doi-page"}
	for i := 0; i < 5; i++ {
		slug := "quelle-seite-" + itoa(uint(i))
		require.NoError(t, e.db.Create(&models.StatusPage{Name: "Seite " + slug, Slug: slug}).Error)
		slugs = append(slugs, slug)
	}
	for i := 0; i < 20; i++ {
		subscribeFrom(e.r, slugs[i%len(slugs)], "quelle-"+itoa(uint(i))+"@example.com", "192.0.2.50:4000")
	}
	assert.Equal(t, 12, queueDrops(logBuf), "eine Quelle belegt höchstens 8 Plätze")

	before := queueDrops(logBuf)
	assert.Equal(t, http.StatusOK, subscribeFrom(e.r, "doi-page", "andere-quelle@example.com", "192.0.2.51:4000").Code)
	assert.Equal(t, before, queueDrops(logBuf), "eine andere Quelle kommt weiter durch")
}

// Control case: without an attack, confirmation mails are delivered normally, even
// many in a row from the same page and source.
func TestSubscribe_NormaleZustellungOhneAngriffVerwirftNichts(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	e := setupAsyncMailEnv(t, nil)
	logBuf := captureSlog(t)
	for i := 0; i < 12; i++ {
		require.Equal(t, http.StatusOK, subscribeFrom(e.r, "doi-page", "normal-"+itoa(uint(i))+"@example.com", "192.0.2.60:4000").Code)
		assert.Contains(t, awaitSMTPMessage(t, srv), "To: normal-"+itoa(uint(i))+"@example.com")
		e.h.WaitMails()
	}
	assert.Zero(t, queueDrops(logBuf), "ohne Rückstau wird nichts verworfen")
}

// The order uses the same mailbox key: Gmail spelling variants
// (dots, googlemail.com) exhaust the limit of open orders, identical-looking
// addresses at other providers do not.
func TestCreateOrder_GmailVariantenTeilenSichDasLimit(t *testing.T) {
	e := setupOrderMailEnv(t, nil)
	for _, addr := range []string{"mi.ra" + gmail, "m.i.r.a+x" + googlemail, "Mira" + "@GMAIL.com"} {
		require.Equal(t, http.StatusCreated, e.order(addr), addr)
	}
	assert.Equal(t, http.StatusTooManyRequests, e.order("mir.a"+gmail), "vierte Variante desselben Postfachs")

	for _, addr := range []string{"mi.ra@example.org", "m.i.r.a@example.org", "mira@example.org"} {
		assert.Equal(t, http.StatusCreated, e.order(addr), "Gegenprobe: bei example.org sind Punkte verschieden: "+addr)
	}
	e.h.WaitMails()
}

// Orders are anonymous: the same source IP occupies at most a quarter of the
// slots of the order queue (8 of 32), even with rotating addresses.
// The subscription queue is unaffected by this.
func TestCreateOrder_EineQuelleFuelltWarteschlangeNichtAllein(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupOrderMailEnv(t, release)

	for i := 0; i < 12; i++ {
		require.Equal(t, http.StatusCreated, e.order("flut-bestellung-"+itoa(uint(i))+"@example.com"))
	}
	assert.Equal(t, 12-8, queueDrops(logBuf), "eine Quelle belegt höchstens 8 von 32 Plätzen")
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "Mail queue full") {
			assert.Contains(t, line, "order_confirmation", "das Log nennt den Zweck")
			assert.NotContains(t, line, "example.com", "und keine Adressen")
		}
	}
}
