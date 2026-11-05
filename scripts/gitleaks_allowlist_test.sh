#!/usr/bin/env bash
# Prüft mit gitleaks, dass die Allowlist in .gitleaks.toml nur die bekannten Fehlalarme unterdrückt:
# Ein echtes Geheimnis in derselben Zeile wie ein erlaubter Name muss weiter gemeldet werden.
# Aufruf: bash scripts/gitleaks_allowlist_test.sh   (GITLEAKS=<Pfad>, GITLEAKS_CONFIG=<Datei> optional)
# Ohne gitleaks im PATH bricht das Skript ab; die CI installiert es vorher.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
gl=${GITLEAKS:-gitleaks}
conf=${GITLEAKS_CONFIG:-$PWD/.gitleaks.toml}
command -v "$gl" > /dev/null || fail "gitleaks nicht gefunden (GITLEAKS=<Pfad> setzen)"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Erfundene Werte, zur Laufzeit zusammengesetzt, damit dieses Skript selbst nichts Verdächtiges enthält.
key="api""_key"
val="Zx9fQ2mK7pL4vB8nR1tY6uW3aaa"
name="p95_latency""_ms"

# 1) Name allein: Fehlalarm, bleibt unterdrückt.
printf "const columns = [{ key: '%s' }]\n" "$name" > "$tmp/columns.js"
# 2) Echtes Geheimnis in derselben Zeile wie der erlaubte Name: muss gemeldet werden.
printf '%s = "%s"; var %s = 5\n' "$key" "$val" "$name" > "$tmp/leak.go"

report="$tmp/report.json"
"$gl" dir "$tmp" --no-banner -c "$conf" -f json -r "$report" > /dev/null 2>&1 || true
[ -s "$report" ] || fail "kein Bericht von gitleaks"
grep -q 'leak.go' "$report" || fail "echtes Geheimnis neben einem erlaubten Namen wird unterdrückt"
if grep -q 'columns.js' "$report"; then fail "bekannter Fehlalarm wird wieder gemeldet"; fi
echo "OK: gitleaks-Allowlist unterdrückt nur die bekannten Fehlalarme"
