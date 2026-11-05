package handlers

import (
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// RunMaintenanceTickForTest macht den Scheduler-Tick für die externen
// Handler-Tests (package handlers_test) aufrufbar.
func (h *Handler) RunMaintenanceTickForTest() { h.runMaintenanceTick() }

// LockedPageComponentsForTest macht die Baum-Lesung mit Zeilensperre
// für Statement-Tests aufrufbar.
func LockedPageComponentsForTest(tx *gorm.DB, tenantID uint) ([]models.Component, error) {
	return lockedPageComponents(tx, tenantID)
}
