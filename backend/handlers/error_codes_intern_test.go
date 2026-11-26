package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Codes that are not reachable via a request.

func TestCodeForStatus(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusBadRequest:          "invalid_request",
		http.StatusUnauthorized:        "unauthorized",
		http.StatusForbidden:           "forbidden",
		http.StatusNotFound:            "not_found",
		http.StatusConflict:            "conflict",
		http.StatusInternalServerError: "server_error",
	} {
		assert.Equal(t, want, codeForStatus(status), "Status %d", status)
	}
}

func TestSubscriberConflictCode(t *testing.T) {
	assert.Equal(t, "subscription_exists_email", subscriberConflictCode("email"))
	assert.Equal(t, "subscription_exists_webhook", subscriberConflictCode("google_chat"))
}

// The verification saves the status; if saving fails,
// 500 server_error is returned (TXT lookup via fake, no network).
func TestVerifyCustomDomain_SpeichernScheitertLiefertCode(t *testing.T) {
	orig := lookupTXT
	defer func() { lookupTXT = orig }()
	lookupTXT = func(context.Context, string) ([]string, error) { return []string{"statushafen-verify=tok-634"}, nil }

	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "V", Slug: "verify-634", CustomDomain: "status.verify634.example", CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-634"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("t634:verify", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("injizierter Fehler"))
	}))

	r := setupCustomDomainRouter(h)
	w := doJSONRequest(r, "POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	assert.NotContains(t, w.Body.String(), "injizierter Fehler")
}

// GetMyPagesHandler with an unusable session (userID has the
// wrong type) answers 500 server_error.
func TestGetMyPages_SitzungMitFalschemTypLiefertCode(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	r := testutils.SetupRouter()
	r.GET("/me/pages", func(c *gin.Context) { c.Set("userID", "kein-uint") }, h.GetMyPagesHandler)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me/pages", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
}
