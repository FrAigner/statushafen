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

// subscribeFrom meldet eine Adresse für die Seite mit dem Slug von einer
// festen Quelladresse an (Router ohne vorgelagerten Proxy: RemoteAddr zählt).
func subscribeFrom(r http.Handler, slug, email, remoteAddr string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"channel": "email", "email": email, "component_ids": "all", "slug": slug})
	req, _ := http.NewRequest(http.MethodPost, "/api/subscribe", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Gmail-Domains zusammengesetzt, damit die Repo-Prüfung auf private
// Postfachadressen nicht anschlägt (Beispiele, keine Konten).
const (
	gmail      = "@" + "gmail.com"
	googlemail = "@" + "googlemail.com"
)

func queueDrops(buf *lockedBuffer) int {
	return strings.Count(buf.String(), "Mail queue full, mail dropped")
}

// Angriff mit hängendem SMTP: Eine einzelne Seite darf die Warteschlange der
// Abo-Bestätigungen nicht allein füllen (höchstens ein Viertel der 32 Plätze);
// die Anmeldung einer anderen Seite von einer anderen Quelle wird weiter
// angenommen.
func TestSubscribe_EinzelneSeiteFuelltWarteschlangeNichtAllein(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupAsyncMailEnv(t, release)
	require.NoError(t, e.db.Create(&models.StatusPage{Name: "Andere Seite", Slug: "andere-seite"}).Error)

	const flood = 20
	for i := 0; i < flood; i++ {
		// Wechselnde Quelladressen: nur die Seite bindet die Angriffe aneinander.
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

// Dieselbe Quell-IP über viele Seiten hinweg: auch sie belegt höchstens ein Viertel.
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

// Gegenprobe: Ohne Angriff werden Bestätigungsmails normal zugestellt, auch
// viele nacheinander von derselben Seite und Quelle.
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

// Die Bestellung nutzt denselben Postfach-Schlüssel: Gmail-Schreibvarianten
// (Punkte, googlemail.com) erschöpfen das Limit offener Bestellungen, gleich
// aussehende Adressen bei anderen Anbietern nicht.
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

// Bestellungen sind anonym: Dieselbe Quell-IP belegt höchstens ein Viertel der
// Plätze der Bestell-Warteschlange (8 von 32), auch mit wechselnden Adressen.
// Die Abo-Warteschlange bleibt davon unberührt.
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
