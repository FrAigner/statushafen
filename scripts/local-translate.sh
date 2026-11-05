#!/bin/bash
# Lokales LibreTranslate für den Übersetzen-Knopf (Betreiber-Wunsch 31.10.2026).
# Wird von start.sh/stop.sh eingebunden (source). Ohne Docker, bei LOCAL_TRANSLATE=0 oder bei
# bereits gesetzter LIBRETRANSLATE_URL passiert nichts.
#
# Die Codes sind die 30 Standardsprachen des Katalogs (backend/database/languages.go,
# defaultLanguages); scripts/local_translate_test.sh hält beide Listen gleich.
# Das erste Laden der Modelle dauert lange und braucht mehrere GB RAM; sie liegen in einem
# benannten Volume und werden danach nicht neu geladen.
LT_IMAGE="libretranslate/libretranslate:v1.9.6"
LT_CONTAINER="statushafen_libretranslate"
LT_VOLUME="statushafen_libretranslate_models"
LT_CODES="bs,bg,da,de,en,et,fi,fr,el,ga,it,hr,lv,lt,lb,mt,mk,nl,pl,pt,rm,ro,sv,sr,sk,sl,es,cs,tr,hu"
# Diese Katalogsprachen kennt LibreTranslate v1.9.6 nicht (Modellindex, geprüft 31.10.2026). Steht ein
# unbekannter Code in LT_LOAD_ONLY, startet der Dienst gar nicht ("Unavailable language codes"),
# deshalb werden sie beim Start ausgelassen; der Übersetzen-Knopf meldet für sie "nicht unterstützt".
LT_UNSUPPORTED="bs,hr,lb,mk,mt,rm,sr"

local_translate_start() {
  if [ "${LOCAL_TRANSLATE:-1}" = "0" ]; then
    echo "ℹ️  Lokale Übersetzung aus (LOCAL_TRANSLATE=0)."
    return 0
  fi
  if [ -n "${LIBRETRANSLATE_URL:-}" ]; then
    echo "ℹ️  LIBRETRANSLATE_URL ist gesetzt ($LIBRETRANSLATE_URL), kein lokaler Container."
    return 0
  fi
  if ! command -v docker > /dev/null 2>&1 || ! docker info > /dev/null 2>&1; then
    echo "ℹ️  Docker nicht verfügbar, kein lokales LibreTranslate (Übersetzen-Knopf meldet dann 'nicht eingerichtet')."
    return 0
  fi
  local port="${LOCAL_TRANSLATE_PORT:-5001}"
  local load_only="" code
  for code in ${LT_CODES//,/ }; do
    case ",$LT_UNSUPPORTED," in *",$code,"*) continue ;; esac
    load_only="${load_only:+$load_only,}$code"
  done
  if docker ps --filter "name=^$LT_CONTAINER$" --filter status=running --format '{{.Names}}' | grep -qx "$LT_CONTAINER"; then
    echo "ℹ️  LibreTranslate-Container läuft bereits."
  elif (exec 3<> "/dev/tcp/127.0.0.1/$port") 2> /dev/null; then
    echo "ℹ️  Auf Port $port läuft schon etwas, kein LibreTranslate-Container gestartet (LOCAL_TRANSLATE_PORT ändert den Port)."
    return 0
  else
    echo "🌐 Starte LibreTranslate mit 23 von 30 Katalogsprachen auf 127.0.0.1:$port (erstes Laden der Modelle dauert lange, mehrere GB RAM; LOCAL_TRANSLATE=0 schaltet ab)..."
    # Alter, gestoppter oder abgestürzter Container gleichen Namens (restart-Schleife) weicht dem neuen.
    docker stop "$LT_CONTAINER" > /dev/null 2>&1
    docker rm "$LT_CONTAINER" > /dev/null 2>&1
    docker run -d --name "$LT_CONTAINER" --restart unless-stopped \
      -p "127.0.0.1:$port:5000" \
      -e LT_LOAD_ONLY="$load_only" -e LT_DISABLE_WEB_UI=true \
      -v "$LT_VOLUME:/home/libretranslate/.local" \
      "$LT_IMAGE" > /dev/null || { echo "⚠️  LibreTranslate konnte nicht gestartet werden."; return 0; }
  fi
  export LIBRETRANSLATE_URL="http://localhost:$port"
}

local_translate_stop() {
  if command -v docker > /dev/null 2>&1 && docker ps --format '{{.Names}}' 2> /dev/null | grep -qx "$LT_CONTAINER"; then
    echo "Stoppe LibreTranslate (Modelle bleiben im Volume $LT_VOLUME)..."
    docker stop "$LT_CONTAINER" > /dev/null
  fi
}
