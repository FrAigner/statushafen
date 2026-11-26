package handlers

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// sqlStateUnique is the SQLSTATE code of a unique violation in Postgres.
const sqlStateUnique = "23505"

// isUniqueViolation detects a unique violation across databases.
// The pre-check (SELECT) and the creation (INSERT) are not atomic: if
// two requests create the same slug/name at the same time, only the
// (partial) unique index kicks in, and the handler answered with 500 instead of 409. The
// handlers therefore translate the error centrally via this helper into the same
// response as the pre-check.
//
// Detected: gorm.ErrDuplicatedKey (if TranslateError is active),
// Postgres errors with SQLSTATE 23505 (pgx/pgconn and lib/pq both return
// SQLState()) and SQLite ("UNIQUE constraint failed", test database).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var st interface{ SQLState() string }
	if errors.As(err, &st) && st.SQLState() == sqlStateUnique {
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
