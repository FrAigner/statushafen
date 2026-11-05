package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// injectDBError lässt die Operation op (query, create, update, delete) auf table
// scheitern, nachdem skip Aufrufe durchgelaufen sind (so bleiben vorbereitende
// Abfragen desselben Handlers unberührt). Die zurückgegebene Funktion entfernt
// den Hook (idempotent, läuft zusätzlich nach dem Test), damit Nachprüfungen den
// Fehler nicht auslösen.
func injectDBError(t *testing.T, db *gorm.DB, op, table string, skip int) (remove func()) {
	t.Helper()
	name := "test:intern:" + op + ":" + table
	seen := 0
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		seen++
		if seen > skip {
			_ = tx.AddError(errors.New("injizierter Fehler"))
		}
	}
	cb := db.Callback()
	var err error
	switch op {
	case "create":
		err = cb.Create().Before("gorm:create").Register(name, hook)
		remove = func() { _ = cb.Create().Remove(name) }
	case "update":
		err = cb.Update().Before("gorm:update").Register(name, hook)
		remove = func() { _ = cb.Update().Remove(name) }
	case "delete":
		err = cb.Delete().Before("gorm:delete").Register(name, hook)
		remove = func() { _ = cb.Delete().Remove(name) }
	case "query":
		err = cb.Query().Before("gorm:query").Register(name, hook)
		remove = func() { _ = cb.Query().Remove(name) }
	default:
		t.Fatalf("unbekannte Operation %q", op)
	}
	require.NoError(t, err)
	t.Cleanup(remove)
	return remove
}
