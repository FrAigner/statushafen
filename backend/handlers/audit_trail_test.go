package handlers_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Audit-Trail-Lücken und Request-ID-Korrelation.

func captureAuditLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.New(&buf, func(k string) string {
		return map[string]string{"LOG_FORMAT": "json"}[k]
	}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func auditLogLine(t *testing.T, buf *bytes.Buffer, action string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "audit" && m["action"] == action {
			return m
		}
	}
	t.Fatalf("keine audit-Logzeile für %q in %s", action, buf.String())
	return nil
}

func auditActions(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var actions []string
	require.NoError(t, db.Model(&models.AuditLog{}).Order("id").Pluck("action", &actions).Error)
	return actions
}

// RecordAudit schreibt zusätzlich eine Logzeile mit der Request-ID, damit ein
// Audit-Eintrag (Model ohne Request-ID-Feld) mit dem Access-Log korreliert
// werden kann. Die E-Mail aus TargetName gehört nicht ins Log.
func TestRecordAudit_LogsRequestID(t *testing.T) {
	buf := captureAuditLog(t)
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.GET("/x", func(c *gin.Context) {
		c.Set("userID", uint(9))
		h.RecordAudit(c, "user.updated", "user", "User", "9", "geheim@example.com", "")
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-42")
	r.ServeHTTP(httptest.NewRecorder(), req)

	line := auditLogLine(t, buf, "user.updated")
	assert.Equal(t, "req-42", line["request_id"])
	assert.Equal(t, "user", line["category"])
	assert.Equal(t, "User", line["target_type"])
	assert.Equal(t, "9", line["target_id"])
	assert.EqualValues(t, 9, line["actor_id"])
	assert.NotContains(t, buf.String(), "geheim@example.com")
}

func TestRecordAudit_LogsWithoutActor(t *testing.T) {
	buf := captureAuditLog(t)
	h := handlers.NewHandler(testutils.SetupTestDB())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	h.RecordAudit(c, "auth.login_failed", "auth", "User", "", "x@example.com", "")
	line := auditLogLine(t, buf, "auth.login_failed")
	assert.NotContains(t, line, "actor_id")
}

func postLogin(r *gin.Engine, email, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Erfolgreiche Passwort-Logins fehlten bisher im Audit-Log (nur
// auth.login_failed und SSO-Logins wurden erfasst).
func TestLoginHandler_SuccessIsAudited(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	user := models.User{Email: "audit-login@example.com", PasswordHash: testutils.MustHash("pw-123456")}
	require.NoError(t, db.Create(&user).Error)

	require.Equal(t, http.StatusOK, postLogin(r, user.Email, "pw-123456").Code)

	var entry models.AuditLog
	require.NoError(t, db.Where("action = ?", "auth.login").First(&entry).Error)
	assert.Equal(t, "auth", entry.Category)
	assert.Equal(t, strconv.FormatUint(uint64(user.ID), 10), entry.TargetID)
	assert.Equal(t, user.Email, entry.TargetName)
	require.NotNil(t, entry.ActorID)
	assert.Equal(t, user.ID, *entry.ActorID)
	assert.Equal(t, user.Email, entry.ActorEmail)
}

// Passwortänderung und Global-Admin-Wechsel sind sicherheitsrelevant und
// müssen als eigene Aktionen im Audit-Log erkennbar sein, nicht nur als
// unspezifisches user.updated.
func TestUpdateUserHandler_AuditsPasswordAndAdminChanges(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/users/:id", h.UpdateUserHandler)
	user := models.User{Email: "audit-update@example.com"}
	require.NoError(t, db.Create(&user).Error)
	// Ohne weiteren Global Admin wäre das Herabstufen unten 409
	// (letzter Global Admin).
	require.NoError(t, db.Create(&models.User{Email: "audit-other-admin@example.com", IsGlobalAdmin: true}).Error)
	path := "/api/users/" + strconv.FormatUint(uint64(user.ID), 10)

	put := func(payload map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	// Ohne Änderung an Passwort/Admin-Flag: nur user.updated.
	put(map[string]any{"email": user.Email})
	assert.Equal(t, []string{"user.updated"}, auditActions(t, db))

	put(map[string]any{"email": user.Email, "password": "neu-123456", "is_global_admin": true})
	assert.Equal(t, []string{"user.updated", "user.updated", "user.password_changed", "user.global_admin_changed"}, auditActions(t, db))

	var entry models.AuditLog
	require.NoError(t, db.Where("action = ?", "user.global_admin_changed").First(&entry).Error)
	assert.Equal(t, "is_global_admin=true", entry.Details)

	put(map[string]any{"email": user.Email, "is_global_admin": false})
	var last models.AuditLog
	require.NoError(t, db.Where("action = ?", "user.global_admin_changed").Last(&last).Error)
	assert.Equal(t, "is_global_admin=false", last.Details)
}
