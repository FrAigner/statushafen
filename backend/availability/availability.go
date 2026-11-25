// Package availability computes daily aggregates of availability.
//
// Component.Status is only the current value; the inputs are therefore
// time intervals from incidents, maintenance windows, external messages and (since
// ) the status history of the components (FromChanges), each with
// its severity (weighted). The
// package knows no database and is purely computational, so that day boundaries,
// ongoing incidents etc. can be checked as table tests.
package availability

import (
	"math"
	"sort"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
)

// NoData marks a day for which there is no observation (the page
// did not exist yet). Deliberately not "operational": 100 % without data would be
// a claim without basis.
const NoData = "no_data"

// Impairment weights per severity: availability =
// 1 - weighted downtime / observed time. A partial disruption thus
// does not count as a full outage. Maintenance has no weight (no outage). The
// values are a product decision, not a measurement; changes apply
// retroactively to all 90 days and are pinned down by a test.
const (
	WeightDegradedPerformance = 0.3
	WeightPartialOutage       = 0.6
	WeightMajorOutage         = 1.0
)

// Weight returns the weight of a status; 0 for operational, maintenance and anything
// unknown (does not count as downtime).
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

// StatusChange is an entry of a component's status history: from At on,
// Status applies.
type StatusChange struct {
	At     time.Time
	Status string
}

// FromChanges turns the status history of ONE component into intervals. changes
// must be sorted ascending by time (for equal times the later
// entry wins). Each status applies until the next entry, the last until now.
// Only impairments and maintenance yield an interval; operational and anything
// unknown (e.g. "group") do not. Entries in the future (clock skew)
// do not count.
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
			continue // length 0 (a change and its reverse at the same time) was never an outage
		}
		out = append(out, Interval{Start: start, End: end, Status: ch.Status})
	}
	return out
}

// Interval is a period with a status: degraded_performance,
// partial_outage, major_outage (impairment) or under_maintenance
// (maintenance). Overlaps count by the worst status, not twice.
type Interval struct {
	Start, End time.Time
	Status     string
}

// Impairment is the duration of an impairment per severity within
// a day. Overlapping intervals count only with the worst status.
type Impairment struct {
	Status  string
	Seconds int64
}

// Day is the result for one UTC calendar day.
type Day struct {
	Date string // YYYY-MM-DD (UTC)
	// Status is the worst status of the day, "operational" or NoData.
	Status string
	// Availability in percent (0-100) or nil for NoData.
	Availability *float64
	// DowntimeSeconds: weighted downtime (sum of duration x weight, maintenance
	// does not count), rounded to seconds.
	DowntimeSeconds int64
	// WeightedDownSeconds is the same quantity unrounded (for summary).
	WeightedDownSeconds float64
	// Impairments: unweighted duration per severity, worst first; only
	// entries > 0 s.
	Impairments []Impairment
	// ObservedSeconds: observed part of the day (today only until now, on the
	// first day of data only from the start of data). Fractions of
	// seconds, otherwise the duration just after 00:00 UTC is 0 and the result NaN.
	ObservedSeconds float64
}

// Compute returns days days up to and including the UTC day of now (oldest
// first). dataStart is the start of observation (page creation, or
// earliest incident); before that NoData applies. Intervals may overlap;
// open intervals are capped to now by the caller.
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
		// length 0 (an immediately closed incident) counts if the start lies within the day.
		overlaps := iv.Start.Before(obsEnd) && iv.End.After(obsStart)
		instant := iv.Start.Equal(iv.End) && !iv.Start.Before(obsStart) && iv.Start.Before(obsEnd)
		if !overlaps && !instant {
			continue
		}
		status = componentstatus.Worst(status, iv.Status)
		if iv.Status == componentstatus.UnderMaintenance {
			continue // maintenance is not an outage
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

// sweep splits the intervals at all boundaries and assigns each section the
// worst active status: overlaps do not count twice, a
// more severe status displaces the lighter one. Returns the duration per status and
// the weighted sum in seconds.
func sweep(ivs []Interval) (map[string]time.Duration, float64) {
	// Review: event sweep in O(n log n) instead of checking all
	// intervals per section (O(n²), with webhook flapping and tens of thousands of
	// entries several seconds of CPU on a public endpoint).
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

// Merge builds the whole page from the components: status and duration per
// severity come from pageDays (all intervals of the visible
// components), availability is the average of the components with
// data. Without components pageDays stays unchanged.
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

// MinObservedForSummary is the shortest observation duration from which an
// overall percentage is reported. A few hours of observation
// (freshly created page with an open incident) would otherwise yield "0.00 %" and
// look like a total outage, although there is no reliable data basis.
const MinObservedForSummary = 24 * time.Hour

// Summary: availability over all days with data, weighted by observed
// duration; nil if no day has data or less than MinObservedForSummary
// was observed.
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
