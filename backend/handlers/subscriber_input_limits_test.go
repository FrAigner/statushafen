package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
)

// Öffentliche Abo-Eingaben haben feste Obergrenzen und enthalten keine
// Steuerzeichen. Ein NUL-Byte erreichte sonst die Datenbank, die es ablehnt
// (500 statt 400); überlange Werte wurden bis zum Body-Limit gespeichert.
func TestSubscribe_EingabenBegrenztUndOhneSteuerzeichen(t *testing.T) {
	db, r, _ := setupDOIRouter(t)
	valid := func() map[string]string {
		return map[string]string{"channel": "email", "email": "leser@example.com", "component_ids": "all", "slug": "doi-page"}
	}
	long := func(n int) string { return strings.Repeat("a", n) }

	cases := []struct {
		name   string
		mutate func(m map[string]string)
		code   string
	}{
		{"component_ids mit NUL-Byte", func(m map[string]string) { m["component_ids"] = "1,2\x003" }, "input_invalid_characters"},
		{"component_ids mit Zeilenumbruch", func(m map[string]string) { m["component_ids"] = "1,2\n3" }, "input_invalid_characters"},
		{"component_ids zu lang", func(m map[string]string) { m["component_ids"] = strings.Repeat("1,", 5000) }, "input_too_long"},
		{"language mit NUL-Byte", func(m map[string]string) { m["language"] = "de\x00" }, "input_invalid_characters"},
		{"language zu lang", func(m map[string]string) { m["language"] = long(17) }, "input_too_long"},
		{"E-Mail mit NUL-Byte", func(m map[string]string) { m["email"] = "leser\x00@example.com" }, "input_invalid_characters"},
		{"E-Mail zu lang", func(m map[string]string) { m["email"] = long(250) + "@example.com" }, "input_too_long"},
		{"Slug mit NUL-Byte", func(m map[string]string) { m["slug"] = "doi-page\x00" }, "input_invalid_characters"},
		{"Slug zu lang", func(m map[string]string) { m["slug"] = long(300) }, "input_too_long"},
		{"webhook_url zu lang", func(m map[string]string) {
			m["channel"], m["webhook_url"] = "google_chat", "https://chat.googleapis.com/v1/spaces/"+long(2100)
		}, "input_too_long"},
		{"webhook_url mit NUL-Byte", func(m map[string]string) {
			m["channel"], m["webhook_url"] = "google_chat", "https://chat.googleapis.com/v1/spaces/A\x00B"
		}, "input_invalid_characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := valid()
			tc.mutate(body)
			w := postJSON(r, "/api/subscribe", body, nil)
			requireStatusCode(t, w, http.StatusBadRequest)
			assert.Equal(t, tc.code, responseField(t, w, "code"))
		})
	}
	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	assert.Zero(t, n, "abgewiesene Eingaben legen nichts an")

	t.Run("Gegenprobe: Werte an der Grenze und normale Eingaben gehen durch", func(t *testing.T) {
		body := valid()
		body["component_ids"] = "1,2,3"
		body["language"] = "de"
		requireStatusCode(t, postJSON(r, "/api/subscribe", body, nil), http.StatusOK)

		edge := map[string]string{
			"channel": "email", "email": long(64) + "@" + strings.Repeat("b", 63) + ".example.com",
			"component_ids": strings.Repeat("1,", 1000) + "1", "slug": "doi-page", "language": long(16),
		}
		requireStatusCode(t, postJSON(r, "/api/subscribe", edge, nil), http.StatusOK)
	})
}

func TestSubscriberAdmin_EingabenBegrenztUndOhneSteuerzeichen(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"

	for name, payload := range map[string]map[string]string{
		"SubscribedComponentIDs mit NUL": {"Channel": "email", "Email": "a@example.com", "SubscribedComponentIDs": "1\x002"},
		"Language zu lang":               {"Channel": "email", "Email": "a@example.com", "SubscribedComponentIDs": "all", "Language": strings.Repeat("x", 17)},
		"WebhookURL zu lang":             {"Channel": "google_chat", "WebhookURL": "https://chat.example.com/" + strings.Repeat("a", 2100)},
	} {
		t.Run("Anlegen: "+name, func(t *testing.T) {
			w := postJSON(r, base, payload, nil)
			requireStatusCode(t, w, http.StatusBadRequest)
			code, _ := responseField(t, w, "code").(string)
			assert.Contains(t, []string{"input_too_long", "input_invalid_characters"}, code)
		})
	}

	sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "b@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}
	require.NoError(t, db.Create(&sub).Error)
	t.Run("Ändern: Komponentenliste mit NUL", func(t *testing.T) {
		w := putJSON(r, http.MethodPut, base+"/"+strconv.Itoa(int(sub.ID)), map[string]string{"Channel": "email", "Email": "b@example.com", "SubscribedComponentIDs": "1\x00"})
		requireStatusCode(t, w, http.StatusBadRequest)
		assert.Equal(t, "input_invalid_characters", responseField(t, w, "code"))
	})
	t.Run("Gegenprobe: normale Änderung", func(t *testing.T) {
		w := putJSON(r, http.MethodPut, base+"/"+strconv.Itoa(int(sub.ID)), map[string]string{"Channel": "email", "Email": "b@example.com", "SubscribedComponentIDs": "1,2"})
		requireStatusCode(t, w, http.StatusOK)
	})
}

func TestUpdateMySubscription_EingabenBegrenzt(t *testing.T) {
	h, r, _, viewerTok, page, _ := newCodeEnvRouter(t)
	require.NoError(t, h.DB.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "v613@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)
	url := "/api/me/subscription?slug=" + page.Slug

	for name, payload := range map[string]map[string]string{
		"NUL in component_ids": {"component_ids": "1\x00"},
		"language zu lang":     {"component_ids": "all", "language": strings.Repeat("x", 40)},
		"component_ids lang":   {"component_ids": strings.Repeat("1,", 5000)},
	} {
		t.Run(name, func(t *testing.T) {
			status, out := doJSON(t, r, http.MethodPut, url, viewerTok, payload)
			assert.Equal(t, http.StatusBadRequest, status, "%v", out)
			assert.Contains(t, []any{"input_too_long", "input_invalid_characters"}, out["code"])
		})
	}
	status, out := doJSON(t, r, http.MethodPut, url, viewerTok, map[string]string{"component_ids": "1,2", "language": "de"})
	assert.Equal(t, http.StatusOK, status, "%v", out)
}

func TestTestWebhook_EingabenBegrenzt(t *testing.T) {
	_, r, _, viewerTok, _, _ := newCodeEnvRouter(t)
	for name, tc := range map[string]struct{ url, code string }{
		"zu lang":  {"https://chat.googleapis.com/v1/spaces/" + strings.Repeat("a", 2100), "input_too_long"},
		"mit NUL":  {"https://chat.googleapis.com/v1/spaces/A\x00B", "input_invalid_characters"},
		"mit CRLF": {"https://chat.googleapis.com/v1/spaces/A\r\nB", "input_invalid_characters"},
	} {
		t.Run(name, func(t *testing.T) {
			status, out := doJSON(t, r, http.MethodPost, "/api/subscribe/test-webhook", viewerTok, map[string]string{"webhook_url": tc.url})
			assert.Equal(t, http.StatusBadRequest, status, "%v", out)
			assert.Equal(t, tc.code, out["code"])
		})
	}
}
