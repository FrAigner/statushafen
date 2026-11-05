package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fehlerpfade der Admin-Übersetzungslisten direkt am Handler
// (ohne PageScope-Middleware, die ungültige IDs sonst schon abfängt). Den
// Erfolgsfall samt Rechte- und Mandantenprüfung deckt
// main_translation_admin_list_test.go über den echten Router ab.
func TestAdminTranslationListHandlers_ErrorPaths(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	routes := map[string]gin.HandlerFunc{
		"/incident/:incident_id":   h.GetAllIncidentTranslationsHandler,
		"/component/:component_id": h.GetAllComponentTranslationsHandler,
		"/notice/:notice_id":       h.GetAllGeneralNoticeTranslationsHandler,
	}
	for path, handler := range routes {
		r.GET(path, handler)
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	prefixes := []string{"/incident/", "/component/", "/notice/"}

	for _, p := range prefixes {
		assert.Equal(t, http.StatusBadRequest, get(p+"abc").Code, p)
		w := get(p + "1")
		assert.Equal(t, http.StatusOK, w.Code, p)
		assert.JSONEq(t, `[]`, w.Body.String(), p+": leere Liste statt null")
	}

	sqlDB, err := db.DB()
	require.NoError(t, err)
	_ = sqlDB.Close()
	for _, p := range prefixes {
		assert.Equal(t, http.StatusInternalServerError, get(p+"1").Code, p)
	}
}
