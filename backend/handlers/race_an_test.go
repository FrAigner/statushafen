//go:build race

package handlers_test

// Der Race-Detektor verlangsamt CPU-lastige Abfragen um ein
// Vielfaches (Heim-Runner: 19 s statt unter 3 s). Zeitgrenzen in Tests
// werden mit diesem Faktor gedehnt, damit der race-Job nicht an der
// Wanduhr scheitert.
const raceZeitfaktor = 20
