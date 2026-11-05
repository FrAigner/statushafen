package handlers_test

// Der Set-Password-Link darf auch bei gleichzeitiger Einlösung nur einmal
// gelten. SQLite serialisiert Schreiber und zeigt das nicht; deshalb ein
// echtes Postgres (READ COMMITTED), nur mit MIGRATION_PG_TEST=1 (siehe
// startInvitationPostgres).

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetOrderPassword_TenParallelRedemptionsYieldExactlyOneSuccess_Postgres(t *testing.T) {
	db := startInvitationPostgres(t)
	require.NoError(t, db.AutoMigrate(&models.PageOrder{}))
	warmUpPool(t, db, 12)
	r := setupOrderRouter(handlers.NewHandler(db))
	order, user := confirmedOrderWithFreshUser(t, db, "parallel@example.com", "tok-parallel-1")

	const n = 10
	codes := make([]int, n)
	bodies := make([]string, n)
	passwords := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		passwords[i] = fmt.Sprintf("parallel-passwort-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, body := setPasswordRequest(r, "tok-parallel-1", passwords[i])
			codes[i], bodies[i] = code, string(body)
		}()
	}
	close(start)
	wg.Wait()

	ok, rejected := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusBadRequest, http.StatusConflict:
			rejected++
		}
	}
	assert.Equal(t, 1, ok, "genau eine Einlösung gelingt: %v %v", codes, bodies)
	assert.Equal(t, n-1, rejected, "alle anderen werden abgewiesen: %v", codes)

	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	matches := 0
	for _, pw := range passwords {
		if database.CheckPasswordHash(pw, reloaded.PasswordHash) {
			matches++
		}
	}
	assert.Equal(t, 1, matches, "das Konto hat das Passwort der einen erfolgreichen Einlösung")
	var reloadedOrder models.PageOrder
	require.NoError(t, db.First(&reloadedOrder, order.ID).Error)
	assert.Equal(t, "active", reloadedOrder.Status)
	assert.Empty(t, reloadedOrder.SetPasswordTokenHash)
}
