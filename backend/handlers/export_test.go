package handlers

import (
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// RunMaintenanceTickForTest makes the scheduler tick accessible to the external
// handler tests (package handlers_test).
func (h *Handler) RunMaintenanceTickForTest() { h.runMaintenanceTick() }

// LockedPageComponentsForTest makes the tree read with row lock
// callable for statement tests.
func LockedPageComponentsForTest(tx *gorm.DB, tenantID uint) ([]models.Component, error) {
	return lockedPageComponents(tx, tenantID)
}
