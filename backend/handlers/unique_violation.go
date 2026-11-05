package handlers

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// sqlStateUnique ist der SQLSTATE-Code einer Unique-Verletzung in Postgres.
const sqlStateUnique = "23505"

// isUniqueViolation erkennt eine Unique-Verletzung DB-übergreifend.
// Die Vorabprüfung (SELECT) und das Anlegen (INSERT) sind nicht atomar: legen
// zwei Anfragen gleichzeitig denselben Slug/Namen an, greift erst der
// (partielle) Unique-Index, und der Handler antwortete mit 500 statt 409. Die
// Handler übersetzen den Fehler deshalb zentral über diesen Helfer in dieselbe
// Antwort wie die Vorabprüfung.
//
// Erkannt werden: gorm.ErrDuplicatedKey (falls TranslateError aktiv ist),
// Postgres-Fehler mit SQLSTATE 23505 (pgx/pgconn und lib/pq liefern beide
// SQLState()) und SQLite ("UNIQUE constraint failed", Testdatenbank).
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
