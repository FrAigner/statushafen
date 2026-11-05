//go:build !race

package handlers_test

// Ohne Race-Detektor gelten die Zeitgrenzen unverändert.
const raceZeitfaktor = 1
