#!/usr/bin/env bash
# Regression: `./start.sh` startete das Frontend mit der aktiven Node-Version (bei nvm z. B. v23.3.0), obwohl
# nvm eine passende (24.x) installiert hat -> Warnung "außerhalb der engines" und viele npm-EBADENGINE-Zeilen.
# Prüft mit Fake-nvm/Fake-node:
# - passt Node nicht und nvm hat eine passende Version, nutzt das Frontend diese (kein Hinweis, `npm ci`/`npm run dev` laufen damit),
# - passt Node schon, wird nvm nicht angefasst,
# - ohne nvm oder ohne installierte passende Version: wie bisher Hinweis, kein Abbruch, nichts wird installiert.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

[ -f frontend/.nvmrc ] || fail "frontend/.nvmrc fehlt"
[ "$(tr -d '[:space:]' < frontend/.nvmrc)" = 24 ] || fail "frontend/.nvmrc enthält nicht '24'"

mkdir -p "$tmp/w/scripts" "$tmp/w/frontend/node_modules" "$tmp/bin" "$tmp/nvm"
cp start.sh stop.sh "$tmp/w/"
cp frontend/.nvmrc "$tmp/w/frontend/"
cp scripts/local-translate.sh scripts/dev-status.sh "$tmp/w/scripts/"
printf '#!/bin/bash\nexit 0\n' > "$tmp/w/scripts/dev-backend.sh"
chmod +x "$tmp/w/start.sh" "$tmp/w/stop.sh" "$tmp/w/scripts/"*.sh
for c in docker go pkill lsof curl; do printf '#!/bin/bash\nexit 0\n' > "$tmp/bin/$c"; done
# Fake-npm hält fest, mit welcher Node-Version er aufgerufen wurde
# shellcheck disable=SC2016
printf '#!/bin/bash\necho "npm $* mit $(node -v)" >> "%s/calls"\nexit 0\n' "$tmp" > "$tmp/bin/npm"
# Fake-node: gibt $FAKE_NODE aus; `nvm use` setzt es auf $NVM_TARGET
# shellcheck disable=SC2016
printf '#!/bin/bash\necho "$FAKE_NODE"\n' > "$tmp/bin/node"
chmod +x "$tmp/bin/"*
# Fake-nvm: `nvm use` klappt nur, wenn NVM_TARGET gesetzt ist (= passende Version installiert); installiert nie etwas
cat > "$tmp/nvm/nvm.sh" <<'S'
nvm() {
  echo "nvm $*" >> "$NVM_LOG"
  case "$1" in
    use) [ -n "${NVM_TARGET:-}" ] || return 3; FAKE_NODE="$NVM_TARGET"; export FAKE_NODE ;;
    install) echo "nvm install wurde aufgerufen" >> "$NVM_LOG.install" ;;
  esac
}
S

run() { # run <aktive Node-Version> <NVM_DIR> <nvm-Ziel>; setzt rc und out
  rm -f "$tmp/calls" "$tmp/nvm.log" "$tmp/nvm.log.install" "$tmp/w/.pids"; : > "$tmp/calls"
  touch -t 202001010000 "$tmp/w/frontend/node_modules/.package-lock.json"
  touch "$tmp/w/frontend/package-lock.json"
  rc=0
  out=$(cd "$tmp/w" && HOME="$tmp/home" NVM_DIR="$2" NVM_TARGET="$3" NVM_LOG="$tmp/nvm.log" FAKE_NODE="$1" \
    PATH="$tmp/bin:/usr/bin:/bin" LOCAL_TRANSLATE_PORT=59999 ./start.sh 2>&1) || rc=$?
  # `npm run dev` läuft im Hintergrund: kurz warten, bis der Fake-npm sich eingetragen hat
  for _ in $(seq 1 50); do grep -q '^npm run dev' "$tmp/calls" && break; sleep 0.1; done
}

# nvm vorhanden, aktive Version unpassend, 24.x installiert -> wird benutzt
run v23.3.0 "$tmp/nvm" v24.1.0
[ "$rc" = 0 ] || fail "start.sh bricht ab (rc=$rc): $out"
! echo "$out" | grep -q 'außerhalb der engines' || fail "trotz installierter 24.x kommt der engines-Hinweis: $out"
grep -q '^npm ci mit v24.1.0' "$tmp/calls" || fail "npm ci läuft nicht mit der nvm-Version: $(cat "$tmp/calls")"
grep -q '^npm run dev mit v24.1.0' "$tmp/calls" || fail "npm run dev läuft nicht mit der nvm-Version: $(cat "$tmp/calls")"
grep -q '^nvm use' "$tmp/nvm.log" || fail "nvm use wurde nicht aufgerufen"

# nvm vorhanden, aber keine passende Version installiert -> Hinweis wie bisher, kein Abbruch, keine Installation
run v23.3.0 "$tmp/nvm" ""
[ "$rc" = 0 ] || fail "start.sh bricht ohne passende nvm-Version ab (rc=$rc): $out"
echo "$out" | grep -q 'außerhalb der engines' || fail "kein engines-Hinweis ohne passende nvm-Version"
echo "$out" | grep -q 'nvm install 24' || fail "Hinweis nennt 'nvm install 24' nicht"
grep -q '^npm run dev mit v23.3.0' "$tmp/calls" || fail "Frontend startet nicht trotzdem: $(cat "$tmp/calls")"
[ ! -e "$tmp/nvm.log.install" ] || fail "start.sh hat Node per nvm installiert"

# kein nvm -> Hinweis wie bisher, kein Abbruch
run v23.3.0 "$tmp/keinnvm" v24.1.0
[ "$rc" = 0 ] || fail "start.sh bricht ohne nvm ab (rc=$rc): $out"
echo "$out" | grep -q 'außerhalb der engines' || fail "kein engines-Hinweis ohne nvm"
[ ! -e "$tmp/nvm.log" ] || fail "nvm ohne Installation aufgerufen"

# aktive Version passt schon -> nvm wird nicht angefasst
run v22.12.0 "$tmp/nvm" v24.0.0
[ "$rc" = 0 ] || fail "start.sh bricht ab (rc=$rc): $out"
[ ! -e "$tmp/nvm.log" ] || fail "nvm wurde trotz passender Version benutzt: $(cat "$tmp/nvm.log")"
grep -q '^npm run dev mit v22.12.0' "$tmp/calls" || fail "Version wurde geändert: $(cat "$tmp/calls")"
echo "start_nvm_test: ok"
