#!/usr/bin/env bash
# Hält die Sprachliste des lokalen LibreTranslate (scripts/local-translate.sh) und den
# Standardkatalog (backend/database/languages.go, defaultLanguages) gleich und prüft die Grenzen
# des Containers: festes Image, nur 127.0.0.1, Volume, Abschalter.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
script=scripts/local-translate.sh

shell=$(sed -n 's/^LT_CODES="\(.*\)"/\1/p' "$script" | tr ',' '\n' | sort)
go=$(sed -n '/^var defaultLanguages/,/^}/p' backend/database/languages.go | sed -n 's/.*{Code: "\([a-z-]*\)".*/\1/p' | sort)
[ -n "$shell" ] && [ -n "$go" ] || fail "Codes nicht gefunden"
[ "$shell" = "$go" ] || fail "Sprachcodes weichen ab: $(diff <(echo "$shell") <(echo "$go") | tr '\n' ' ')"
[ "$(echo "$shell" | wc -l | tr -d ' ')" = 30 ] || fail "erwartet 30 Sprachen"

# Nicht unterstützte Sprachen müssen aus dem Katalog stammen und dürfen nicht in LT_LOAD_ONLY landen.
unsup=$(sed -n 's/^LT_UNSUPPORTED="\(.*\)"/\1/p' "$script" | tr ',' '\n' | sort)
[ -n "$unsup" ] && [ -z "$(comm -13 <(echo "$shell") <(echo "$unsup"))" ] || fail "LT_UNSUPPORTED enthält Codes außerhalb des Katalogs"
grep -q 'load_only=' "$script" || fail "LT_LOAD_ONLY wird nicht gefiltert"
grep -q 'LT_LOAD_ONLY="\$load_only"' "$script" || fail "LT_LOAD_ONLY nutzt nicht die gefilterte Liste"
grep -q 'LT_IMAGE="libretranslate/libretranslate:v[0-9]' "$script" || fail "Image nicht auf eine Version festgelegt"
grep -q '127.0.0.1:\$port:5000' "$script" || fail "Port muss an 127.0.0.1 gebunden sein"
# Host-Port-Default: 5000 belegt auf macOS der AirPlay-Empfänger (Kontrollzentrum), start.sh startete den Container dann nie.
grep -q 'LOCAL_TRANSLATE_PORT:-5001}' "$script" || fail "Default-Port muss 5001 sein"
grep -q 'LOCAL_TRANSLATE_PORT:-5000}' "$script" && fail "Default-Port darf nicht 5000 sein (macOS AirPlay-Empfänger)"
grep -q 'LT_DISABLE_WEB_UI=true' "$script" || fail "Web-Oberfläche nicht abgeschaltet"
grep -q -- '-v "\$LT_VOLUME:' "$script" || fail "Modell-Volume fehlt"
grep -q 'LOCAL_TRANSLATE:-1}" = "0"' "$script" || fail "Schalter LOCAL_TRANSLATE fehlt"
grep -q 'local_translate_start' start.sh || fail "start.sh startet den Dienst nicht"
grep -q 'local_translate_stop' stop.sh || fail "stop.sh stoppt den Dienst nicht"

# Verhalten ohne Docker bzw. mit Schalter: nichts startet, keine URL wird gesetzt.
out=$(env -i PATH=/usr/bin:/bin bash -c "source $script; LOCAL_TRANSLATE=0 local_translate_start; echo URL=\${LIBRETRANSLATE_URL:-leer}")
echo "$out" | grep -q 'URL=leer' || fail "LOCAL_TRANSLATE=0 darf keine URL setzen"
out=$(env -i PATH=/usr/bin:/bin bash -c "source $script; export LIBRETRANSLATE_URL=http://x:1; local_translate_start; echo URL=\$LIBRETRANSLATE_URL")
echo "$out" | grep -q 'URL=http://x:1' || fail "gesetzte URL muss bleiben"
out=$(env -i PATH=/nonexistent /bin/bash -c "source $script; local_translate_start; echo URL=\${LIBRETRANSLATE_URL:-leer}")
echo "$out" | grep -q 'URL=leer' || fail "ohne Docker darf keine URL gesetzt werden"
echo "local_translate_test: ok"
