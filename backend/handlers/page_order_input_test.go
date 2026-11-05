package handlers_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postOrder(t *testing.T, r http.Handler, body map[string]any) (int, []byte) {
	t.Helper()
	w := doStatusPageRequest(r, "POST", "/api/public/orders", body)
	return w.Code, w.Body.Bytes()
}

// Das Limit offener Bestellungen zählt je Postfach: Schreibvarianten derselben
// Adresse (Groß-/Kleinschreibung, "+tag") teilen sich die drei Plätze.
func TestOrderCreate_PendingLimitCountsPerMailbox(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for _, email := range []string{"opfer@example.com", "Opfer@Example.com", "OPFER+a@example.com"} {
		code, body := postOrder(t, r, map[string]any{"company_name": "Firma", "email": email})
		require.Equal(t, http.StatusCreated, code, "%s: %s", email, body)
	}
	for _, email := range []string{"opfer+b@example.com", "OPFER@EXAMPLE.COM", "opfer+x+y@example.com"} {
		code, body := postOrder(t, r, map[string]any{"company_name": "Firma", "email": email})
		require.Equal(t, http.StatusTooManyRequests, code, "%s: %s", email, body)
		requireErrorCode(t, body, "too_many_orders")
	}
	assert.EqualValues(t, 3, count(db, &models.PageOrder{}, "lower(email) LIKE ?", "opfer%@example.com"))

	// Gegenprobe: ein anderes Postfach und eine andere Domain sind unberührt.
	for _, email := range []string{"anderer@example.com", "opfer@example.org", "opfer-x@example.com"} {
		code, body := postOrder(t, r, map[string]any{"company_name": "Firma", "email": email})
		assert.Equal(t, http.StatusCreated, code, "%s: %s", email, body)
	}
}

// "_" und "%" gelten in der Postfach-Suche als Zeichen, nicht als Platzhalter:
// Wer "a_b@example.com" bestellt, nutzt nicht die Plätze von "axb@example.com".
func TestOrderCreate_PendingLimitTreatsWildcardCharactersLiterally(t *testing.T) {
	_, r := newInputValidationRouter(t)
	for _, email := range []string{"axb+1@example.com", "axb+2@example.com", "axb+3@example.com", "100abc+1@example.com", "100abc+2@example.com", "100abc+3@example.com"} {
		code, body := postOrder(t, r, map[string]any{"company_name": "Firma", "email": email})
		require.Equal(t, http.StatusCreated, code, "%s: %s", email, body)
	}
	for _, email := range []string{"a_b@example.com", "100%@example.com"} {
		code, body := postOrder(t, r, map[string]any{"company_name": "Firma", "email": email})
		assert.Equal(t, http.StatusCreated, code, "%s: %s", email, body)
	}
}

// Die Felder der Bestellung sind begrenzt, bevor etwas gespeichert oder per Mail
// verschickt wird.
func TestOrderCreate_FieldLengthLimits(t *testing.T) {
	db, r := newInputValidationRouter(t)
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name     string
		body     map[string]any
		wantCode int
		wantErr  string
	}{
		{"Name 120 Zeichen ok", map[string]any{"company_name": strings.Repeat("ä", 120), "email": "n1@example.com"}, http.StatusCreated, ""},
		{"Name 121 Zeichen", map[string]any{"company_name": strings.Repeat("ä", 121), "email": "n2@example.com"}, http.StatusBadRequest, "order_name_too_long"},
		{"Name sehr lang", map[string]any{"company_name": long(900_000), "email": "n3@example.com"}, http.StatusBadRequest, "order_name_too_long"},
		{"Slug 60 Zeichen ok", map[string]any{"company_name": "F", "email": "s1@example.com", "slug": long(60)}, http.StatusCreated, ""},
		{"Slug 61 Zeichen", map[string]any{"company_name": "F", "email": "s2@example.com", "slug": long(61)}, http.StatusBadRequest, "slug_too_long"},
		{"E-Mail 254 Zeichen ok", map[string]any{"company_name": "F", "email": long(242) + "@example.com"}, http.StatusCreated, ""},
		{"E-Mail 255 Zeichen", map[string]any{"company_name": "F", "email": long(243) + "@example.com"}, http.StatusBadRequest, "email_too_long"},
		{"E-Mail sehr lang", map[string]any{"company_name": "F", "email": long(900_000) + "@example.com"}, http.StatusBadRequest, "email_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postOrder(t, r, tc.body)
			require.Equal(t, tc.wantCode, code, string(body[:min(len(body), 300)]))
			if tc.wantErr != "" {
				requireErrorCode(t, body, tc.wantErr)
			}
		})
	}
	assert.EqualValues(t, 3, count(db, &models.PageOrder{}, "1 = 1"), "abgelehnte Bestellungen werden nicht gespeichert")
}

// Ein NUL-Byte lässt PostgreSQL das Speichern ablehnen (vorher 500). Es ist eine
// ungültige Eingabe und wird mit 400 beantwortet.
func TestOrderCreate_NulByteIsBadRequest(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for name, body := range map[string]map[string]any{
		"Name":   {"company_name": "Fi\x00rma", "email": "nul1@example.com"},
		"Slug":   {"company_name": "Firma", "email": "nul2@example.com", "slug": "fi\x00rma"},
		"E-Mail": {"company_name": "Firma", "email": "nul\x00@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			code, resp := postOrder(t, r, body)
			assert.Equal(t, http.StatusBadRequest, code, string(resp))
		})
	}
	code, resp := postOrder(t, r, map[string]any{"company_name": "Fi\x00rma", "email": "nul3@example.com"})
	require.Equal(t, http.StatusBadRequest, code)
	requireErrorCode(t, resp, "invalid_characters")
	assert.Zero(t, count(db, &models.PageOrder{}, "1 = 1"))
}

// Die neuen Fehlercodes stehen als Konstanten, nicht als Literal in gin.H; die
// Übersetzungsprüfung der Handler findet sie daher nicht. Sie werden hier einzeln geprüft.
func TestOrderCreate_NewErrorCodesAreTranslated(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "locales", lang+".json"))
		require.NoError(t, err)
		var doc struct {
			API struct {
				Errors map[string]string `json:"errors"`
			} `json:"api"`
		}
		require.NoError(t, json.Unmarshal(raw, &doc))
		for _, code := range []string{"order_name_too_long", "slug_too_long", "email_too_long", "invalid_characters"} {
			assert.NotEmpty(t, doc.API.Errors[code], "%s: Übersetzung für %s fehlt", lang, code)
		}
	}
}
