package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fraigner/statushafen/database"

	"gorm.io/gorm"
)

const cliUsage = "Aufruf: server [create-admin --email ADRESSE --password-stdin]\n" +
	"  create-admin  legt ein Konto mit Global-Admin-Recht an; das Passwort kommt über stdin\n"

// runCLI führt einen Verwaltungsbefehl aus (statt den Server zu starten) und
// liefert den Exit-Code. Ohne Argumente startet main() den Server.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create-admin" {
		sayRaw(stderr, cliUsage)
		return 2
	}
	// InitDB würde mit SEED_DB=true alle Tabellen löschen.
	if strings.TrimSpace(os.Getenv("SEED_DB")) != "" && os.Getenv("SEED_DB") != "false" {
		say(stderr, "create-admin läuft nicht mit gesetztem SEED_DB (das Seeden würde die Tabellen löschen)")
		return 2
	}
	return runCreateAdmin(args[1:], stdin, stdout, stderr, openDatabase())
}

// openDatabase öffnet die Datenbank wie der Serverstart. Als Variable, damit
// ein Test eine Datenbank im Speicher einsetzen kann.
var openDatabase = func() *gorm.DB {
	initLogging()
	database.InitDB()
	return database.DB
}

// exitProcess beendet den Prozess; als Variable, damit main() testbar bleibt.
var exitProcess = os.Exit

// runCreateAdmin legt den Admin an. Das Passwort wird nur von stdin gelesen,
// nie als Argument (Prozessliste, Shell-Verlauf) und nie ausgegeben.
func runCreateAdmin(args []string, stdin io.Reader, stdout, stderr io.Writer, db *gorm.DB) int {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "E-Mail-Adresse des neuen Admins")
	fromStdin := fs.Bool("password-stdin", false, "Passwort aus der ersten Zeile von stdin lesen")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		sayRaw(stderr, cliUsage)
		return 2
	}
	if strings.TrimSpace(*email) == "" || !*fromStdin {
		sayRaw(stderr, cliUsage)
		return 2
	}
	line, err := bufio.NewReader(io.LimitReader(stdin, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		say(stderr, "Passwort konnte nicht gelesen werden")
		return 1
	}
	password := strings.TrimRight(line, "\r\n")
	if err := database.CreateGlobalAdmin(db, *email, password); err != nil {
		if errors.Is(err, database.ErrAdminExists) {
			say(stderr, "Für diese E-Mail-Adresse besteht bereits ein Konto; es wurde nichts geändert.")
		} else {
			say(stderr, "Fehler:", err)
		}
		return 1
	}
	sayf(stdout, "Global-Admin %s wurde angelegt.\n", strings.ToLower(strings.TrimSpace(*email)))
	return 0
}

// cliRequested sagt, ob die Argumente (ohne Programmnamen) einen
// Verwaltungsbefehl nennen. Flags (z. B. des Testlaufs) starten weiter den Server.
func cliRequested(args []string) bool {
	return len(args) > 0 && !strings.HasPrefix(args[0], "-")
}

// Schreibfehler auf stdout/stderr sind für ein Verwaltungskommando nicht behebbar.
func say(w io.Writer, args ...any)            { _, _ = fmt.Fprintln(w, args...) }
func sayf(w io.Writer, f string, args ...any) { _, _ = fmt.Fprintf(w, f, args...) }
func sayRaw(w io.Writer, args ...any)         { _, _ = fmt.Fprint(w, args...) }
