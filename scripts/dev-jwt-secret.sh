#!/bin/bash
# Wird von dev-backend.sh eingebunden (source). Das Backend startet nur mit einem
# starken JWT_SECRET (mind. 32 Zeichen, kein Platzhalter). Für die lokale
# Entwicklung erzeugt diese Funktion beim ersten Start ein zufälliges Geheimnis,
# legt es in backend/.jwt-secret.local ab (von Git ignoriert, nur für den
# Besitzer lesbar) und exportiert es. Ein bereits gesetztes JWT_SECRET bleibt
# unberührt. Außerhalb der Entwicklungs-APP_ENV-Werte (wie database.IsDevelopmentAppEnv)
# tut die Funktion nichts: Betreiber setzen das Geheimnis selbst.
dev_jwt_secret_sicherstellen() {
  local verzeichnis="$1" env_name datei
  env_name=$(printf '%s' "${APP_ENV:-}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
  case "$env_name" in
    development | dev | test | local) ;;
    *) return 0 ;;
  esac
  [ -n "${JWT_SECRET:-}" ] && return 0
  datei="$verzeichnis/.jwt-secret.local"
  if [ ! -s "$datei" ]; then
    (
      umask 077
      if command -v openssl > /dev/null 2>&1; then
        openssl rand -base64 48 | tr -d '\n' > "$datei"
      else
        head -c 48 /dev/urandom | base64 | tr -d '\n' > "$datei"
      fi
    ) || return 1
  fi
  JWT_SECRET=$(cat "$datei")
  export JWT_SECRET
}
