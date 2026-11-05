#!/bin/bash
# Lokales Backend, das dem Code folgt: baut und startet neu, sobald sich eine
# Go-Datei, go.mod/go.sum oder der Git-Stand (pull, merge, checkout) ändert.
# Vorher hing lokal hinter Staging, weil `go run .` einmal startet und nie
# nachlädt. Aufruf aus start.sh; Logs landen auf stdout.
SKRIPTE="$(cd "$(dirname "$0")" && pwd)"
cd "$SKRIPTE/../backend" || exit 1
# Seed (SEED_DB=true in backend/.env) läuft nur mit Entwicklungs-APP_ENV; eine gesetzte Variable bleibt.
export APP_ENV="${APP_ENV:-development}"
# Das Backend startet nur mit starkem JWT_SECRET: lokal ein zufälliges erzeugen (backend/.jwt-secret.local).
# shellcheck source=scripts/dev-jwt-secret.sh
source "$SKRIPTE/dev-jwt-secret.sh"
dev_jwt_secret_sicherstellen "$PWD" || { echo "❌ JWT_SECRET konnte nicht erzeugt werden." >&2; exit 1; }
BIN="${TMPDIR:-/tmp}/statushafen-dev-backend-$$"
PID=""

stand() {
  # Prüfsumme über Quelltext und Git-Stand: ändert sich bei jeder Bearbeitung.
  { git rev-parse HEAD 2>/dev/null
    find . -name '*.go' -o -name go.mod -o -name go.sum | xargs stat -f '%N %m' 2>/dev/null | sort
  } | shasum | cut -d' ' -f1
}
stoppen() { [ -n "$PID" ] && kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null; PID=""; }
trap 'stoppen; rm -f "$BIN"; exit 0' INT TERM EXIT

LETZTER=""
while true; do
  AKTUELL=$(stand)
  if [ "$AKTUELL" != "$LETZTER" ]; then
    LETZTER=$AKTUELL
    stoppen
    echo "🔨 Backend wird gebaut ($(git rev-parse --short HEAD 2>/dev/null))..."
    if go build -o "$BIN" .; then
      "$BIN" & PID=$!
      echo "▶️  Backend läuft (PID $PID)"
    else
      echo "❌ Build fehlgeschlagen, warte auf die nächste Änderung."
    fi
  fi
  sleep 3
done
