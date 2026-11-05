// Package componentstatus enthält die Rangfolge der Komponentenstatus.
//
// Vorher lag sie als statusSeverity/worstStatus nur in
// handlers/component.go. Der Abfrage-Dienst für externe Statusseiten
// (Paket externalstatus) braucht dieselbe Rangfolge, um Statuswechsel zu
// erkennen, darf aber handlers nicht importieren (handlers importiert
// externalstatus). Deshalb ein eigenes Paket (nur models/gorm als
// Abhängigkeit); handlers delegiert dorthin, damit es nur eine Rangfolge und
// nur eine Vorfall-Überlagerung gibt.
package componentstatus

// Statuswerte einer Komponente (Component.Status) plus "unknown" für
// externe Komponenten mit einem Status, den wir nicht kennen.
const (
	Operational         = "operational"
	DegradedPerformance = "degraded_performance"
	PartialOutage       = "partial_outage"
	MajorOutage         = "major_outage"
	UnderMaintenance    = "under_maintenance"
	Group               = "group"
	Unknown             = "unknown"
)

// Severity gibt jedem Status einen numerischen Schweregrad (höher = schlimmer).
//
// Wartung liegt zwischen "operational" und den echten Störungen:
// geplant, aber für Nutzer spürbar. Das gilt auch für gekoppelte externe
// Komponenten: eine Wartung beim Anbieter färbt die eigene
// Komponente auf "under_maintenance", jede echte Störung (eigene oder
// externe) hat Vorrang. "group" und "unknown" haben Rang 0 und verschlechtern
// nie etwas: Ein unbekannter externer Status soll keinen Fehlalarm auslösen.
func Severity(s string) int {
	switch s {
	case MajorOutage:
		return 5
	case PartialOutage:
		return 4
	case DegradedPerformance:
		return 3
	case UnderMaintenance:
		return 2
	case Operational:
		return 1
	default:
		return 0
	}
}

// Worst gibt den schlimmsten Status aus statuses zurück, mindestens "operational".
func Worst(statuses ...string) string {
	worst := Operational
	for _, s := range statuses {
		if Severity(s) > Severity(worst) {
			worst = s
		}
	}
	return worst
}
