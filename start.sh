#!/bin/bash

# Unterbefehle: status zeigt NUR den Zustand (startet und stoppt nichts); ohne Argument wird gestartet.
case "${1:-}" in
    "" | start | restart) ;; # restart = start: eine laufende Instanz (.pids) wird unten zuerst gestoppt
    status) exec ./scripts/dev-status.sh ;;
    stop) exec ./stop.sh ;;
    *)
        echo "Aufruf: ./start.sh [start|status|stop|restart]" >&2
        echo "  (ohne Argument)  Umgebung starten (laufende Instanz wird vorher gestoppt)" >&2
        echo "  status           nur Zustand anzeigen, nichts starten oder stoppen (Exit 0 = alles läuft)" >&2
        echo "  stop             Umgebung stoppen (wie ./stop.sh)" >&2
        echo "  restart          wie start" >&2
        exit 2
        ;;
esac

echo "🚀 Starte Statushafen Umgebung..."

# Stoppe eventuell laufende Instanzen
if [ -f .pids ]; then
    ./stop.sh
fi

# 0. Lokales LibreTranslate (30 Sprachen) für den Übersetzen-Knopf; setzt LIBRETRANSLATE_URL fürs Backend
source ./scripts/local-translate.sh
local_translate_start

# 1. Backend starten
echo "⚙️ Starte Backend (Port 8080)..."
# Watcher statt `go run .`: startet bei Code-/Git-Änderungen neu (scripts/dev-backend.sh)
./scripts/dev-backend.sh > backend.log 2>&1 &
BACKEND_PID=$!

# 2. Frontend starten
echo "🎨 Starte Frontend (Port 5173)..."
cd frontend || exit 1
# Node-Version gegen die engines der Abhängigkeiten (Vite 7 u. a.) prüfen: nur Hinweis, kein Abbruch
# node_engines_ok: Exit 0, wenn Node passt (oder nicht ermittelbar), sonst 1.
node_engines_ok() {
    local v major minor
    v=$(node -v 2> /dev/null) || return 0
    v=${v#v}
    major=${v%%.*}
    minor=${v#*.}
    minor=${minor%%.*}
    case "$major$minor" in *[!0-9]* | "") return 0 ;; esac
    if [ "$major" -ge 24 ]; then return 0
    elif [ "$major" -eq 22 ] && [ "$minor" -ge 12 ]; then return 0
    elif [ "$major" -eq 20 ] && [ "$minor" -ge 19 ]; then return 0
    fi
    return 1
}
# Passt die aktive Node-Version nicht und gibt es nvm, wird die in frontend/.nvmrc genannte, bereits
# installierte Version verwendet (nur für den Frontend-Teil dieses Skripts). Nichts wird installiert,
# bei Misserfolg bleibt alles beim Alten (Hinweis unten).
node_nvm_waehlen() {
    node_engines_ok && return 0
    local nvm_sh="${NVM_DIR:-$HOME/.nvm}/nvm.sh"
    [ -s "$nvm_sh" ] && [ -f .nvmrc ] || return 0
    # shellcheck disable=SC1090
    source "$nvm_sh" > /dev/null 2>&1 || return 0
    nvm use --silent > /dev/null 2>&1 || true
    return 0
}
node_engines_hinweis() {
    node_engines_ok && return 0
    local v
    v=$(node -v 2> /dev/null)
    v=${v#v}
    echo "⚠️  Node v$v liegt außerhalb der engines (^20.19.0 || ^22.12.0 || >=24.0.0) der Frontend-Abhängigkeiten." >&2
    echo "   Bitte Node 22.12+ oder 24+ verwenden (z. B. 'nvm install 24'; frontend/.nvmrc wählt sie dann automatisch); es geht weiter, kann aber EBADENGINE-Warnungen erzeugen." >&2
    return 0
}
node_nvm_waehlen
node_engines_hinweis
# Abhängigkeiten nachziehen, wenn sich package-lock.json geändert hat. `npm ci` statt `npm install`:
# ci installiert exakt den Lock-Stand und schreibt package-lock.json nie um (npm install tat das mit
# abweichender npm-Version: libc-Felder weg, esbuild als optional markiert).
if [ ! -d node_modules ] || [ package-lock.json -nt node_modules/.package-lock.json ]; then
    npm ci || echo "⚠️  npm ci ist fehlgeschlagen (package.json und package-lock.json nicht synchron?). Lock-Datei bewusst nicht automatisch ändern: im frontend/ manuell 'npm install' ausführen und das Ergebnis prüfen." >&2
fi
npm run dev > ../frontend.log 2>&1 &
FRONTEND_PID=$!
cd ..

# PIDs speichern, damit wir sie später stoppen können
echo "$BACKEND_PID" > .pids
echo "$FRONTEND_PID" >> .pids

echo "✅ Umgebung läuft!"
echo "➡️  Frontend: http://localhost:5173"
echo "➡️  Backend-Logs: tail -f backend.log"
echo "➡️  Frontend-Logs: tail -f frontend.log"
echo "💡 Zum Beenden einfach './stop.sh' ausführen."
