package i18n

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type draftRow struct {
	gorm.Model
	Status string
}

func TestScopeConfirmedOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&draftRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Create(&draftRow{Status: StatusDraft})
	db.Create(&draftRow{Status: StatusConfirmed})

	var rows []draftRow
	if err := ScopeConfirmedOnly(db).Find(&rows).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != StatusConfirmed {
		t.Fatalf("expected exactly the confirmed row, got %+v", rows)
	}
}
