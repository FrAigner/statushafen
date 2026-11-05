package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Bestell- und Einladungsmails laufen über dieselbe begrenzte Warteschlange wie
// die Bestätigungsmails beim Abonnieren: Die Antwort hängt weder vom
// SMTP-Server ab (hängender Server) noch verrät ihre Dauer, ob für die Adresse
// eine Mail rausgeht, und bei ausgefallenem Mailserver stauen sich keine
// unbegrenzt vielen Goroutinen auf.

type orderMailEnv struct {
	db *gorm.DB
	h  *handlers.Handler
	r  *gin.Engine
}

func setupOrderMailEnv(t *testing.T, release func()) *orderMailEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	t.Cleanup(func() {
		if release != nil {
			release()
		}
		h.WaitMails()
	})
	return &orderMailEnv{db: db, h: h, r: setupOrderRouter(h)}
}

func (e *orderMailEnv) order(email string) int {
	return doStatusPageRequest(e.r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Acme", "email": email,
	}).Code
}

func TestCreateOrder_AntwortOhneWartenAufHaengendenSMTP(t *testing.T) {
	e := setupOrderMailEnv(t, startHangingSMTP(t))
	var code int
	d := timed(func() { code = e.order("haengt-bestellung@example.com") })
	assert.Equal(t, http.StatusCreated, code)
	assert.Less(t, d, 2*time.Second, "die Antwort wartet nicht auf den SMTP-Server (Dial 5 s, Gesamtfrist 15 s)")
}

// Gegenprobe zum hängenden Server: Die Bestätigungsmail wird tatsächlich zugestellt.
func TestCreateOrder_BestaetigungsmailWirdImHintergrundZugestellt(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	e := setupOrderMailEnv(t, nil)

	require.Equal(t, http.StatusCreated, e.order("zustellung-bestellung@example.com"))
	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "To: zustellung-bestellung@example.com")
	assert.Contains(t, msg, "/order/confirm?token=")
	e.h.WaitMails()
}

func TestCreateOrder_VolleWarteschlangeBlockiertNichtUndLoggtOhneEingaben(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupOrderMailEnv(t, release)
	e.h.SetMailQueueSize(1)

	for i := 0; i < 3; i++ {
		email := "geheim-" + itoa(uint(i)) + "-bestellung@example.com"
		var code int
		d := timed(func() { code = e.order(email) })
		assert.Equal(t, http.StatusCreated, code)
		assert.Less(t, d, 2*time.Second, "auch bei voller Warteschlange keine Blockade")
	}
	assert.Equal(t, 2, strings.Count(logBuf.String(), "Mail queue full, mail dropped"), "ein Platz belegt, die übrigen zwei verworfen")
	assert.NotContains(t, logBuf.String(), "geheim-")

	release()
	e.h.WaitMails()
	var orders int64
	e.db.Model(&models.PageOrder{}).Count(&orders)
	assert.EqualValues(t, 3, orders, "verworfene Mails lassen die Bestellungen bestehen")
}

type invitationMailEnv struct {
	db     *gorm.DB
	h      *handlers.Handler
	r      *gin.Engine
	path   string
	token  string
	roleID uint
}

func setupInvitationMailEnv(t *testing.T, release func()) *invitationMailEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	t.Cleanup(func() {
		if release != nil {
			release()
		}
		h.WaitMails()
	})
	r := testutils.SetupRouter()
	admin := r.Group("/api/admin/pages/:page_id", middleware.AuthMiddleware())
	admin.POST("/users", middleware.RequirePermission("pages:create"), h.AddStatusPageUserHandler)

	page := models.StatusPage{Name: "Seite Mail", Slug: "seite-mail"}
	require.NoError(t, db.Create(&page).Error)
	ownerRole := models.Role{Name: "Page Owner", Permissions: "pages:create,pages:read,incidents:read"}
	require.NoError(t, db.Create(&ownerRole).Error)
	viewer := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&viewer).Error)
	owner := models.User{Email: "owner@mail.example"}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: owner.ID, StatusPageID: page.ID, RoleID: ownerRole.ID}).Error)
	tok, err := middleware.GenerateToken(owner.ID, owner.Email, "", "", false)
	require.NoError(t, err)
	// Einladungsmails gehen nur an vorhandene Konten.
	for _, addr := range []string{"a", "b", "c"} {
		require.NoError(t, db.Create(&models.User{Email: addr + "@invitee.example"}).Error)
	}
	return &invitationMailEnv{db: db, h: h, r: r, path: "/api/admin/pages/" + itoa(page.ID) + "/users", token: tok, roleID: viewer.ID}
}

func (e *invitationMailEnv) invite(t *testing.T, email string) int {
	t.Helper()
	return idorRequest(t, e.r, "POST", e.path, e.token, map[string]any{"email": email, "role_id": e.roleID}).Code
}

func TestInviteUser_AntwortOhneWartenAufHaengendenSMTP(t *testing.T) {
	e := setupInvitationMailEnv(t, startHangingSMTP(t))
	var code int
	d := timed(func() { code = e.invite(t, "a@invitee.example") })
	assert.Equal(t, http.StatusOK, code)
	assert.Less(t, d, 2*time.Second)
}

func TestInviteUser_EinladungsmailWirdImHintergrundZugestellt(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	e := setupInvitationMailEnv(t, nil)

	require.Equal(t, http.StatusOK, e.invite(t, "a@invitee.example"))
	assert.Contains(t, awaitSMTPMessage(t, srv), "To: a@invitee.example")
	e.h.WaitMails()
}

// Die Einladungen teilen sich die begrenzte Warteschlange: Ein ausgefallener
// Mailserver darf keine unbegrenzt vielen Versand-Goroutinen aufstauen.
func TestInviteUser_VolleWarteschlangeVerwirftMailsUndLoggtOhneEingaben(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupInvitationMailEnv(t, release)
	e.h.SetMailQueueSize(1)

	for _, addr := range []string{"a", "b", "c"} {
		var code int
		d := timed(func() { code = e.invite(t, addr+"@invitee.example") })
		assert.Equal(t, http.StatusOK, code)
		assert.Less(t, d, 2*time.Second)
	}
	assert.Equal(t, 2, strings.Count(logBuf.String(), "Mail queue full, mail dropped"))
	assert.NotContains(t, logBuf.String(), "invitee.example")
}
