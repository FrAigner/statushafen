#!/usr/bin/env bash
# Regression `./start.sh` schrieb frontend/package-lock.json um, weil es `npm install` aufrief
# (andere npm-Version -> libc-Felder weg, esbuild als optional). Prüft mit Fake-Befehlen:
# - bei neuerer Lock-Datei läuft `npm ci`, nie `npm install`,
# - eine Node-Version außerhalb von engines gibt einen Hinweis aus (ohne Abbruch), eine passende nicht.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/w/scripts" "$tmp/w/frontend/node_modules" "$tmp/bin"
cp start.sh stop.sh "$tmp/w/"
cp scripts/local-translate.sh scripts/dev-status.sh "$tmp/w/scripts/"
printf '#!/bin/bash\nexit 0\n' > "$tmp/w/scripts/dev-backend.sh"
chmod +x "$tmp/w/start.sh" "$tmp/w/stop.sh" "$tmp/w/scripts/"*.sh
for c in docker go pkill lsof curl; do printf '#!/bin/bash\nexit 0\n' > "$tmp/bin/$c"; done
printf '#!/bin/bash\necho "npm $*" >> "%s/calls"\nexit 0\n' "$tmp" > "$tmp/bin/npm"
# shellcheck disable=SC2016
printf '#!/bin/bash\necho "$FAKE_NODE"\n' > "$tmp/bin/node"
chmod +x "$tmp/bin/"*

run() { # run <Node-Version>; setzt rc und out
  rm -f "$tmp/calls" "$tmp/w/.pids"; : > "$tmp/calls"
  # Lock-Datei neuer als node_modules/.package-lock.json -> Installation wird ausgelöst
  touch -t 202001010000 "$tmp/w/frontend/node_modules/.package-lock.json"
  touch "$tmp/w/frontend/package-lock.json"
  rc=0
  out=$(cd "$tmp/w" && FAKE_NODE="$1" PATH="$tmp/bin:/usr/bin:/bin" LOCAL_TRANSLATE_PORT=59999 ./start.sh 2>&1) || rc=$?
}

run v23.3.0
[ "$rc" = 0 ] || fail "start.sh bricht bei unpassender Node-Version ab (rc=$rc): $out"
grep -q '^npm ci' "$tmp/calls" || fail "bei neuerer Lock-Datei wird nicht 'npm ci' aufgerufen: $(cat "$tmp/calls")"
! grep -Eq '^npm install' "$tmp/calls" || fail "start.sh ruft 'npm install' auf (ändert package-lock.json): $(cat "$tmp/calls")"
echo "$out" | grep -q 'außerhalb der engines' || fail "Node v23.3.0 gibt keinen engines-Hinweis aus"

for v in v20.18.0 v21.7.0 v22.11.0 v18.20.0; do
  run "$v"
  echo "$out" | grep -q 'außerhalb der engines' || fail "$v gibt keinen engines-Hinweis aus"
done
for v in v20.19.0 v22.12.0 v22.20.1 v24.0.0 v25.1.0; do
  run "$v"
  ! echo "$out" | grep -q 'außerhalb der engines' || fail "$v ist gültig, gibt aber einen engines-Hinweis aus"
done

# Ohne neuere Lock-Datei und mit vorhandenen node_modules wird gar nichts installiert
rm -f "$tmp/calls"; : > "$tmp/calls"; rm -f "$tmp/w/.pids"
touch "$tmp/w/frontend/node_modules/.package-lock.json"
(cd "$tmp/w" && FAKE_NODE=v22.12.0 PATH="$tmp/bin:/usr/bin:/bin" LOCAL_TRANSLATE_PORT=59999 ./start.sh > /dev/null 2>&1)
! grep -Eq '^npm (ci|install)' "$tmp/calls" || fail "installiert trotz aktuellem node_modules: $(cat "$tmp/calls")"
echo "start_npm_test: ok"
