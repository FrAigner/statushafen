//go:build race

package handlers_test

// The race detector slows CPU-heavy queries by a
// multiple (home runner: 19 s instead of under 3 s). Time limits in tests
// are stretched by this factor so that the race job does not fail on the
// wall clock.
const raceZeitfaktor = 20
