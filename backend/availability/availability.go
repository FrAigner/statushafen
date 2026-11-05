// Package availability berechnet Tagesaggregate der Verfügbarkeit.
//
// Component.Status ist nur der aktuelle Wert; Eingabe sind deshalb
// Zeitintervalle aus Vorfällen, Wartungsfenstern, externen Meldungen und (seit
// ) der Statushistorie der Komponenten (FromChanges), jeweils mit
// ihrem Schweregrad (gewichtet). Das
// Paket kennt keine Datenbank und ist rein rechnerisch, damit sich Tagesgrenzen,
// laufende Vorfälle usw. als Tabellentest prüfen lassen.
package availability

import (
	"math"
	"sort"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
)

// NoData kennzeichnet einen Tag, für den es keine Beobachtung gibt (die Seite
// existierte noch nicht). Bewusst kein „operational“: 100 % ohne Daten wäre
// eine Behauptung ohne Grundlage.
const NoData = "no_data"

// Gewichte der Beeinträchtigung je Schweregrad: Verfügbarkeit =
// 1 - gewichtete Ausfallzeit / beobachtete Zeit. Eine Teilstörung gilt damit
// nicht als voller Ausfall. Wartung hat kein Gewicht (kein Ausfall). Die
// Werte sind eine Festlegung des Produkts, kein Messwert; Änderungen wirken
// rückwirkend auf alle 90 Tage und sind per Test festgeschrieben.
const (
	WeightDegradedPerformance = 0.3
	WeightPartialOutage       = 0.6
	WeightMajorOutage         = 1.0
)

// Weight liefert das Gewicht eines Status; 0 für Betrieb, Wartung und alles
// Unbekannte (zählt nicht als Ausfallzeit).
func Weight(status string) float64 {
	switch status {
	case componentstatus.DegradedPerformance:
		return WeightDegradedPerformance
	case componentstatus.PartialOutage:
		return WeightPartialOutage
	case componentstatus.MajorOutage:
		return WeightMajorOutage
	}
	return 0
}

// StatusChange ist ein Eintrag der Statushistorie einer Komponente: ab At gilt
// Status.
type StatusChange struct {
	At     time.Time
	Status string
}

// FromChanges macht aus dem Statusverlauf EINER Komponente Intervalle. changes
// muss nach Zeit aufsteigend sortiert sein (bei gleicher Zeit gilt der spätere
// Eintrag). Jeder Status gilt bis zum nächsten Eintrag, der letzte bis now.
// Nur Beeinträchtigungen und Wartung ergeben ein Intervall; Betrieb und alles
// Unbekannte (z. B. "group") nicht. Einträge in der Zukunft (Uhrenabweichung)
// zählen nicht.
func FromChanges(changes []StatusChange, now time.Time) []Interval {
	var out []Interval
	for i, ch := range changes {
		if ch.Status != componentstatus.UnderMaintenance && Weight(ch.Status) == 0 {
			continue
		}
		start := ch.At.UTC()
		if start.After(now) {
			continue
		}
		end := now.UTC()
		if i+1 < len(changes) && changes[i+1].At.Before(end) {
			end = changes[i+1].At.UTC()
		}
		if !end.After(start) {
			continue // Länge 0 (Wechsel und Gegenwechsel zur selben Zeit) war nie ein Ausfall
		}
		out = append(out, Interval{Start: start, End: end, Status: ch.Status})
	}
	return out
}

// Interval ist ein Zeitraum mit einem Status: degraded_performance,
// partial_outage, major_outage (Beeinträchtigung) oder under_maintenance
// (Wartung). Überlappungen zählen nach dem schlimmsten Status, nicht doppelt.
type Interval struct {
	Start, End time.Time
	Status     string
}

// Impairment ist die Dauer einer Beeinträchtigung je Schweregrad innerhalb
// eines Tages. Überlappende Intervalle zählen nur mit dem schlimmsten Status.
type Impairment struct {
	Status  string
	Seconds int64
}

// Day ist das Ergebnis für einen UTC-Kalendertag.
type Day struct {
	Date string // YYYY-MM-DD (UTC)
	// Status ist der schlechteste Status des Tages, "operational" oder NoData.
	Status string
	// Availability in Prozent (0-100) oder nil bei NoData.
	Availability *float64
	// DowntimeSeconds: gewichtete Ausfallzeit (Summe Dauer x Gewicht, Wartung
	// zählt nicht), auf Sekunden gerundet.
	DowntimeSeconds int64
	// WeightedDownSeconds ist dieselbe Größe ungerundet (für Summary).
	WeightedDownSeconds float64
	// Impairments: ungewichtete Dauer je Schweregrad, schlimmster zuerst; nur
	// Einträge > 0 s.
	Impairments []Impairment
	// ObservedSeconds: beobachteter Teil des Tages (heute nur bis jetzt, am
	// ersten Tag der Daten erst ab Datenbeginn). Bruchteile von
	// Sekunden, sonst ist die Dauer kurz nach 00:00 UTC 0 und das Ergebnis NaN.
	ObservedSeconds float64
}

// Compute liefert days Tage bis einschließlich dem UTC-Tag von now (älteste
// zuerst). dataStart ist der Beginn der Beobachtung (Anlage der Seite, bzw.
// frühester Vorfall); davor gilt NoData. Intervalle dürfen sich überlappen;
// offene Intervalle kappt der Aufrufer auf now.
func Compute(intervals []Interval, dataStart, now time.Time, days int) []Day {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	out := make([]Day, 0, days)
	for i := days - 1; i >= 0; i-- {
		dayStart := today.AddDate(0, 0, -i)
		out = append(out, computeDay(intervals, dayStart, dataStart, now))
	}
	return out
}

func computeDay(intervals []Interval, dayStart, dataStart, now time.Time) Day {
	dayEnd := dayStart.AddDate(0, 0, 1)
	obsStart, obsEnd := dayStart, dayEnd
	if dataStart.After(obsStart) {
		obsStart = dataStart
	}
	if now.Before(obsEnd) {
		obsEnd = now
	}
	d := Day{Date: dayStart.Format("2006-01-02")}
	if !obsEnd.After(obsStart) {
		d.Status = NoData
		return d
	}

	status := componentstatus.Operational
	var clipped []Interval
	for _, iv := range intervals {
		// Länge 0 (sofort geschlossener Vorfall) zählt, wenn der Beginn im Tag liegt.
		overlaps := iv.Start.Before(obsEnd) && iv.End.After(obsStart)
		instant := iv.Start.Equal(iv.End) && !iv.Start.Before(obsStart) && iv.Start.Before(obsEnd)
		if !overlaps && !instant {
			continue
		}
		status = componentstatus.Worst(status, iv.Status)
		if iv.Status == componentstatus.UnderMaintenance {
			continue // Wartung ist kein Ausfall
		}
		clipped = append(clipped, Interval{Start: maxTime(iv.Start, obsStart), End: minTime(iv.End, obsEnd), Status: iv.Status})
	}

	d.Status = status
	d.ObservedSeconds = obsEnd.Sub(obsStart).Seconds()
	byStatus, weighted := sweep(clipped)
	d.Impairments = impairmentList(byStatus)
	d.WeightedDownSeconds = weighted
	d.DowntimeSeconds = int64(math.Round(weighted))
	pct := 100 * (d.ObservedSeconds - weighted) / d.ObservedSeconds
	d.Availability = &pct
	return d
}

// sweep zerlegt die Intervalle an allen Grenzen und weist jedem Abschnitt den
// schlimmsten aktiven Status zu: Überlappungen zählen nicht doppelt, ein
// schwererer Status verdrängt den leichteren. Liefert die Dauer je Status und
// die gewichtete Summe in Sekunden.
func sweep(ivs []Interval) (map[string]time.Duration, float64) {
	// Review: Ereignis-Sweep in O(n log n) statt je Abschnitt alle
	// Intervalle zu prüfen (O(n²), bei Webhook-Flattern mit Zehntausenden
	// Einträgen mehrere Sekunden CPU auf einem öffentlichen Endpunkt).
	type event struct {
		at     time.Time
		status string
		delta  int
	}
	events := make([]event, 0, 2*len(ivs))
	for _, iv := range ivs {
		events = append(events, event{iv.Start, iv.Status, 1}, event{iv.End, iv.Status, -1})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	byStatus := map[string]time.Duration{}
	active := map[string]int{}
	var weighted float64
	for i := 0; i+1 < len(events); i++ {
		active[events[i].status] += events[i].delta
		from, to := events[i].at, events[i+1].at
		if !to.After(from) {
			continue
		}
		worst := ""
		for st, n := range active {
			if n > 0 && componentstatus.Severity(st) > componentstatus.Severity(worst) {
				worst = st
			}
		}
		if Weight(worst) == 0 {
			continue
		}
		byStatus[worst] += to.Sub(from)
		weighted += Weight(worst) * to.Sub(from).Seconds()
	}
	return byStatus, weighted
}

func impairmentList(byStatus map[string]time.Duration) []Impairment {
	var out []Impairment
	for _, st := range []string{componentstatus.MajorOutage, componentstatus.PartialOutage, componentstatus.DegradedPerformance} {
		if sec := int64(byStatus[st] / time.Second); sec > 0 {
			out = append(out, Impairment{Status: st, Seconds: sec})
		}
	}
	return out
}

// Merge bildet die Gesamtseite aus den Komponenten: Status und Dauer je
// Schweregrad stammen aus pageDays (alle Intervalle der sichtbaren
// Komponenten), die Verfügbarkeit ist der Durchschnitt der Komponenten mit
// Daten. Ohne Komponenten bleibt pageDays unverändert.
func Merge(pageDays []Day, comps [][]Day) []Day {
	if len(comps) == 0 {
		return pageDays
	}
	out := make([]Day, len(pageDays))
	for i, pd := range pageDays {
		out[i] = pd
		var sum, down float64
		n := 0
		for _, cd := range comps {
			if cd[i].Availability == nil {
				continue
			}
			sum += *cd[i].Availability
			down += cd[i].WeightedDownSeconds
			n++
		}
		if n == 0 {
			continue
		}
		pct := sum / float64(n)
		out[i].Availability = &pct
		out[i].WeightedDownSeconds = down / float64(n)
		out[i].DowntimeSeconds = int64(math.Round(out[i].WeightedDownSeconds))
	}
	return out
}

// MinObservedForSummary ist die kürzeste Beobachtungsdauer, ab der eine
// Gesamtprozentzahl ausgewiesen wird. Wenige Stunden Beobachtung
// (frisch angelegte Seite mit einem offenen Vorfall) ergäben sonst „0,00 %“ und
// wirken wie ein Totalausfall, obwohl es keine belastbare Datenbasis gibt.
const MinObservedForSummary = 24 * time.Hour

// Summary: Verfügbarkeit über alle Tage mit Daten, gewichtet nach beobachteter
// Dauer; nil, wenn kein Tag Daten hat oder weniger als MinObservedForSummary
// beobachtet wurde.
func Summary(days []Day) *float64 {
	var obs, down float64
	for _, d := range days {
		obs += d.ObservedSeconds
		down += d.WeightedDownSeconds
	}
	if time.Duration(obs*float64(time.Second)) < MinObservedForSummary {
		return nil
	}
	pct := 100 * (obs - down) / obs
	return &pct
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
