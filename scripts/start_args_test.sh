#!/usr/bin/env bash
# Regression: `./start.sh status` war ein kompletter Neustart, weil start.sh Argumente ignorierte.
# Prüft mit Fake-Befehlen im PATH, dass status und unbekannte Argumente NICHTS starten oder stoppen,
# und dass status den Zustand per Exit-Code meldet (0 = alles läuft, sonst 1).
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Arbeitskopie mit Stubs für alles, was etwas startet
mkdir -p "$tmp/w/scripts" "$tmp/w/frontend/node_modules" "$tmp/bin"
cp start.sh stop.sh "$tmp/w/"
cp scripts/local-translate.sh scripts/dev-status.sh "$tmp/w/scripts/"
touch "$tmp/w/frontend/package-lock.json" "$tmp/w/frontend/node_modules/.package-lock.json"
printf '#!/bin/bash\necho gestartet >> "%s/dev-backend-marker"\n' "$tmp" > "$tmp/w/scripts/dev-backend.sh"
chmod +x "$tmp/w/start.sh" "$tmp/w/stop.sh" "$tmp/w/scripts/"*.sh
# Fake-Befehle protokollieren jeden Aufruf; docker ps / curl liefern je nach FAKE_UP
for c in docker npm go pkill lsof; do
  printf '#!/bin/bash\necho "%s $*" >> "%s/calls"\n[ "%s" = docker ] && [ "$1" = ps ] && [ "${FAKE_UP:-0}" = 1 ] && echo "Up 1 hour"\nexit 0\n' "$c" "$tmp" "$c" > "$tmp/bin/$c"
done
printf '#!/bin/bash\necho "curl $*" >> "%s/calls"\nif [ "${FAKE_UP:-0}" = 1 ]; then printf 200; else printf 000; exit 7; fi\n' "$tmp" > "$tmp/bin/curl"
chmod +x "$tmp/bin/"*

run() { # run <Argumente...>; setzt rc und out, räumt Spuren vorher auf
  rm -f "$tmp/calls" "$tmp/dev-backend-marker" "$tmp/w/.pids"; : > "$tmp/calls"
  if [ -n "${FAKE_PIDS:-}" ]; then printf '%s\n%s\n' "$FAKE_PIDS" "$FAKE_PIDS" > "$tmp/w/.pids"; fi
  rc=0
  out=$(cd "$tmp/w" && PATH="$tmp/bin:/usr/bin:/bin" LOCAL_TRANSLATE_PORT=59999 ./start.sh "$@" 2>&1) || rc=$?
}
nichts_gestartet() { # $1 = Beschreibung
  [ ! -e "$tmp/dev-backend-marker" ] || fail "$1: Backend wurde gestartet"
  if [ -n "${FAKE_PIDS:-}" ]; then
    [ "$(sed -n 1p "$tmp/w/.pids")" = "$FAKE_PIDS" ] || fail "$1: .pids wurde verändert"
  else
    [ ! -e "$tmp/w/.pids" ] || fail "$1: .pids wurde angelegt"
  fi
  ! grep -Eq '^docker (run|stop|rm|start|compose)|^npm|^go |^pkill' "$tmp/calls" || fail "$1: startet/stoppt etwas: $(grep -E '^(docker (run|stop|rm|start|compose)|npm|go |pkill)' "$tmp/calls" | tr '\n' ';')"
}

# status ohne laufende Dienste: Exit 1, nichts gestartet
FAKE_UP=0 run status
nichts_gestartet "status (alles aus)"
[ "$rc" = 1 ] || fail "status ohne laufende Dienste muss Exit 1 liefern (war $rc)"
echo "$out" | grep -q '❌ Backend' || fail "status zeigt Backend nicht als ❌"

# status mit allem oben: Exit 0
sleep 30 &
sleeper=$!
disown
FAKE_PIDS=$sleeper
FAKE_UP=1 run status
nichts_gestartet "status (alles an)"
FAKE_PIDS=
kill "$sleeper" 2> /dev/null || true
[ "$rc" = 0 ] || fail "status mit laufenden Diensten muss Exit 0 liefern (war $rc): $out"
echo "$out" | grep -q '✅ Backend' && echo "$out" | grep -q '✅ LibreTranslate' || fail "status zeigt laufende Dienste nicht als ✅"
# Keine maschinenspezifischen Reste (externe Platte) im Status
! echo "$out" | grep -qi 'platte' || fail "status nennt noch eine externe Platte: $out"

# unbekanntes Argument: Hilfe, Exit 2, nichts gestartet
for a in bogus --help -h statuss; do
  FAKE_UP=0 run "$a"
  nichts_gestartet "Argument $a"
  [ "$rc" = 2 ] || fail "Argument $a muss Exit 2 liefern (war $rc)"
  echo "$out" | grep -q 'Aufruf: ./start.sh' || fail "Argument $a gibt keine Hilfe aus"
done

# Gegenprobe der Gegenprobe: ohne Argument startet es tatsächlich (sonst prüfen die Fakes nichts)
FAKE_UP=0 run
for _ in 1 2 3 4 5 6 7 8 9 10; do [ -e "$tmp/dev-backend-marker" ] && break; sleep 0.3; done
[ -e "$tmp/dev-backend-marker" ] || fail "start.sh ohne Argument startet das Backend nicht (Test-Aufbau defekt)"
# start.sh darf keine fehlende Hilfsdatei laden (source ohne Datei meldet nur, bricht aber nicht ab)
! echo "$out" | grep -Eiq 'no such file|nicht gefunden|not found' || fail "start.sh meldet eine fehlende Datei: $out"
echo "start_args_test: ok"
