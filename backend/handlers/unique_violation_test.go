package handlers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

type fakeSQLStateErr struct{ state string }

func (e fakeSQLStateErr) Error() string    { return "fehler " + e.state }
func (e fakeSQLStateErr) SQLState() string { return e.state }

// isUniqueViolation erkennt Postgres (SQLSTATE 23505, auch
// eingewickelt), SQLite und gorm.ErrDuplicatedKey, aber keine anderen Fehler.
func TestIsUniqueViolation(t *testing.T) {
	assert.False(t, isUniqueViolation(nil))
	assert.False(t, isUniqueViolation(errors.New("connection refused")))
	assert.False(t, isUniqueViolation(fakeSQLStateErr{state: "23503"}), "FK-Verletzung ist keine Unique-Verletzung")
	assert.True(t, isUniqueViolation(fakeSQLStateErr{state: "23505"}))
	assert.True(t, isUniqueViolation(fmt.Errorf("insert: %w", fakeSQLStateErr{state: "23505"})))
	assert.True(t, isUniqueViolation(errors.New("constraint failed: UNIQUE constraint failed: status_pages.slug (2067)")))
	assert.True(t, isUniqueViolation(gorm.ErrDuplicatedKey))
}
