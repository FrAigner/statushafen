#!/usr/bin/env bash
# Prüft dev_jwt_secret_sicherstellen: Entwicklungs-APP_ENV erzeugt ein starkes, persistentes
# Geheimnis (Dateirechte 600), ein gesetztes JWT_SECRET bleibt, jede andere APP_ENV (auch leer)
# erzeugt nichts. Braucht nur bash.
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "FEHLER: $1" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# shellcheck source=scripts/dev-jwt-secret.sh
source scripts/dev-jwt-secret.sh

(
  unset JWT_SECRET
  APP_ENV=development
  dev_jwt_secret_sicherstellen "$tmp"
  [ "${#JWT_SECRET}" -ge 32 ] || fail "Geheimnis kürzer als 32 Zeichen"
  [ "$(cat "$tmp/.jwt-secret.local")" = "$JWT_SECRET" ] || fail "Datei und Variable weichen ab"
  perm=$(stat -c '%a' "$tmp/.jwt-secret.local" 2> /dev/null || stat -f '%Lp' "$tmp/.jwt-secret.local")
  [ "$perm" = "600" ] || fail "Dateirechte $perm statt 600"
  erstes=$JWT_SECRET
  unset JWT_SECRET
  dev_jwt_secret_sicherstellen "$tmp"
  [ "$JWT_SECRET" = "$erstes" ] || fail "Geheimnis ändert sich bei jedem Start"
)

(
  export JWT_SECRET="vorgegebener-wert-vorgegebener-wert-1234"
  APP_ENV=local
  d="$tmp/zwei"; mkdir "$d"
  dev_jwt_secret_sicherstellen "$d"
  [ "$JWT_SECRET" = "vorgegebener-wert-vorgegebener-wert-1234" ] || fail "gesetztes JWT_SECRET überschrieben"
  [ ! -e "$d/.jwt-secret.local" ] || fail "Datei trotz gesetztem JWT_SECRET angelegt"
)

for env_wert in production prod staging ""; do
  (
    unset JWT_SECRET
    APP_ENV="$env_wert"
    d="$tmp/prod-$RANDOM"; mkdir "$d"
    dev_jwt_secret_sicherstellen "$d"
    [ -z "${JWT_SECRET:-}" ] || fail "APP_ENV='$env_wert': Geheimnis erzeugt"
    [ ! -e "$d/.jwt-secret.local" ] || fail "APP_ENV='$env_wert': Datei angelegt"
  )
done
echo "OK: Entwicklungs-JWT_SECRET"
