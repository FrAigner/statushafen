//go:build !race

package handlers_test

// Without the race detector the time limits apply unchanged.
const raceZeitfaktor = 1
