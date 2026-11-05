package componentstatus

import (
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// FromIncidentSeverity bildet den Schweregrad eines offenen, zugeordneten
// Vorfalls (Incident.Severity) auf einen Komponentenstatus ab
// Die Schweregrad-Werte sind absichtlich identisch mit den
// Komponentenstatus (degraded_performance/partial_outage/major_outage); die
// Verfügbarkeitsbalken (handlers/availability.go) und die Anzeige nutzen
// DIESE eine Zuordnung, damit sie sich nicht widersprechen. Leere oder
// unbekannte Werte (Altdaten) zählen vorsichtig wie bisher als
// degraded_performance.
func FromIncidentSeverity(severity string) string {
	if models.IsValidIncidentSeverity(severity) {
		return severity
	}
	return DegradedPerformance
}

// FromOpenIncident bildet einen offenen, zugeordneten Vorfall auf einen
// Komponentenstatus ab: incidentSeverity ist der schlimmste
// Schweregrad der offenen Vorfälle der Komponente, leer = kein offener
// Vorfall (-> "operational"). Seit der Umstellung wirkt der Schweregrad statt
// pauschal "degraded_performance". Seit der Umstellung hier statt in
// handlers/component.go, siehe Paketdoku.
func FromOpenIncident(incidentSeverity string) string {
	if incidentSeverity == "" {
		return Operational
	}
	return FromIncidentSeverity(incidentSeverity)
}

// Own kombiniert den manuell gesetzten Status einer Komponente mit dem aus
// offenen, zugeordneten Vorfällen abgeleiteten Status: der
// schlechtere Wert gewinnt (Rangfolge: Severity). So verschluckt ein offener
// Vorfall keinen bereits schlechteren manuellen Status, und ein gelöster
// Vorfall überschreibt umgekehrt nicht den manuellen Status - der bleibt
// unverändert in der DB und wird nur für die Anzeige überlagert.
// incidentSeverity: Wert aus OpenIncidentComponentIDs, leer = kein Vorfall.
func Own(manualStatus, incidentSeverity string) string {
	return Worst(manualStatus, FromOpenIncident(incidentSeverity))
}

// OpenIncidentComponentIDs liefert je Komponente einer Statusseite, die
// mindestens einem offenen (Status nicht resolved, models.IsResolvedIncidentStatus), normalen
// (nicht Wartungsfenster-) Vorfall zugeordnet ist, den SCHLIMMSTEN
// Schweregrad dieser Vorfälle; Komponenten ohne offenen
// Vorfall fehlen in der Map. Wartungs-
// fenster bleiben außen vor, da sie ihren Komponentenstatus bereits über
// eine eigene, destruktive Logik setzen (CreateIncidentHandler/
// maintenance_scheduler.go, "under_maintenance" direkt in die DB
// geschrieben) - diese Ableitung soll das nicht überlagern.
func OpenIncidentComponentIDs(db *gorm.DB, pageID uint) map[uint]string {
	var rows []struct {
		ComponentID uint
		Severity    string
	}
	db.Model(&models.IncidentComponent{}).
		Select("incident_components.component_id AS component_id, incidents.severity AS severity").
		Joins("JOIN incidents ON incidents.id = incident_components.incident_id AND incidents.deleted_at IS NULL").
		Where("incidents.status_page_id = ? AND incidents.is_maintenance = ? AND incidents.status NOT IN ?", pageID, false, models.ResolvedIncidentStatuses).
		Scan(&rows)

	out := make(map[uint]string, len(rows))
	for _, r := range rows {
		st := FromIncidentSeverity(r.Severity)
		if cur, ok := out[r.ComponentID]; !ok || Severity(st) > Severity(cur) {
			out[r.ComponentID] = st
		}
	}
	return out
}
