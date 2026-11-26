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

// runCLI runs an administration command (instead of starting the server) and
// returns the exit code. Without arguments main() starts the server.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create-admin" {
		sayRaw(stderr, cliUsage)
		return 2
	}
	// InitDB would drop all tables with SEED_DB=true.
	if strings.TrimSpace(os.Getenv("SEED_DB")) != "" && os.Getenv("SEED_DB") != "false" {
		say(stderr, "create-admin läuft nicht mit gesetztem SEED_DB (das Seeden würde die Tabellen löschen)")
		return 2
	}
	return runCreateAdmin(args[1:], stdin, stdout, stderr, openDatabase())
}

// openDatabase opens the database like the server start. As a variable so that
// a test can substitute an in-memory database.
var openDatabase = func() *gorm.DB {
	initLogging()
	database.InitDB()
	return database.DB
}

// exitProcess ends the process; as a variable so that main() stays testable.
var exitProcess = os.Exit

// runCreateAdmin creates the admin. The password is read only from stdin,
// never as an argument (process list, shell history) and never printed.
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

// cliRequested reports whether the arguments (without the program name) name an
// administration command. Flags (e.g. of the test run) still start the server.
func cliRequested(args []string) bool {
	return len(args) > 0 && !strings.HasPrefix(args[0], "-")
}

// Write errors on stdout/stderr are not recoverable for an administration command.
func say(w io.Writer, args ...any)            { _, _ = fmt.Fprintln(w, args...) }
func sayf(w io.Writer, f string, args ...any) { _, _ = fmt.Fprintf(w, f, args...) }
func sayRaw(w io.Writer, args ...any)         { _, _ = fmt.Fprint(w, args...) }
