package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const (
	// RequestTimeout begrenzt einen einzelnen Aufruf des Dienstes.
	RequestTimeout = 15 * time.Second
	// maxResponseBytes begrenzt die gelesene Antwort (Titel/Meldung sind kurz).
	maxResponseBytes = 1 << 20
)

// postJSON schickt body als JSON und liest höchstens maxResponseBytes zurück.
// Fehler des http.Clients (enthalten die URL) werden zu ErrUnavailable
// verkürzt, damit weder Adresse noch Schlüssel in Fehlertexten stehen.
func postJSON(client *http.Client, target string, header map[string]string, body any) (status int, raw []byte, err error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Statushafen-Translate/1.0")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	return resp.StatusCode, raw, nil
}

// statusError bildet einen HTTP-Status ungleich 200 auf die Fehler oben ab.
func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuth
	case http.StatusTooManyRequests, 456: // 456: DeepL "Kontingent erschöpft"
		return ErrQuota
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrUnsupportedLanguage
	default:
		return ErrUnavailable
	}
}
