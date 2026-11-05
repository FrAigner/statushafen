#!/bin/bash
# Zeigt NUR den Zustand der lokalen Entwicklungsumgebung (./start.sh status); startet und stoppt nichts.
# Exit 0, wenn Backend, Frontend, Postgres, Mailpit und (falls aktiv) LibreTranslate laufen, sonst 1.
cd "$(dirname "$0")/.." || exit 1
source ./scripts/local-translate.sh

fehler=0
ok()   { echo "✅ $1"; }
warn() { echo "⚠️  $1"; fehler=1; }
down() { echo "❌ $1"; fehler=1; }

http_code() { curl -s -o /dev/null -m 3 -w '%{http_code}' "$1" 2> /dev/null || true; }
container_status() { docker ps --filter "name=^$1\$" --format '{{.Status}}' 2> /dev/null | head -n 1; }
port_belegt() { (exec 3<> "/dev/tcp/127.0.0.1/$1") 2> /dev/null; }

echo "Statushafen Umgebung:"

# Backend: PID (Zeile 1 in .pids) und HTTP-Antwort
bpid=""; [ -f .pids ] && bpid=$(sed -n 1p .pids)
code=$(http_code http://localhost:8080/healthz)
if [ -n "$bpid" ] && kill -0 "$bpid" 2> /dev/null; then pid_txt="PID $bpid"; else pid_txt="kein Prozess aus .pids"; fi
if [ "${code:-000}" = "200" ]; then ok "Backend: läuft (Port 8080, HTTP 200, $pid_txt)"
elif [ "${code:-000}" != "000" ]; then warn "Backend: antwortet mit HTTP $code (Port 8080, $pid_txt)"
elif [ -n "$bpid" ] && kill -0 "$bpid" 2> /dev/null; then warn "Backend: Prozess $bpid läuft, Port 8080 antwortet nicht (startet noch? backend.log)"
else down "Backend: läuft nicht (./start.sh)"; fi

# Frontend (Vite)
fpid=""; [ -f .pids ] && fpid=$(sed -n 2p .pids)
code=$(http_code http://localhost:5173/)
if [ "${code:-000}" = "200" ]; then ok "Frontend: läuft (Port 5173, HTTP 200${fpid:+, PID $fpid})"
elif [ "${code:-000}" != "000" ]; then warn "Frontend: antwortet mit HTTP $code (Port 5173)"
else down "Frontend: läuft nicht (./start.sh)"; fi

# Container
if command -v docker > /dev/null 2>&1 && docker info > /dev/null 2>&1; then
  for spec in "statushafen_db:Postgres" "statushafen_mailpit:Mailpit"; do
    st=$(container_status "${spec%%:*}")
    if [ -n "$st" ]; then ok "${spec##*:}: Container ${spec%%:*} läuft ($st)"
    else down "${spec##*:}: Container ${spec%%:*} läuft nicht (docker compose up -d)"; fi
  done
  # LibreTranslate (optional: aus bei LOCAL_TRANSLATE=0 oder gesetzter LIBRETRANSLATE_URL)
  port="${LOCAL_TRANSLATE_PORT:-5001}"
  if [ "${LOCAL_TRANSLATE:-1}" = "0" ]; then echo "ℹ️  LibreTranslate: aus (LOCAL_TRANSLATE=0)"
  elif [ -n "${LIBRETRANSLATE_URL:-}" ]; then echo "ℹ️  LibreTranslate: extern ($LIBRETRANSLATE_URL), kein lokaler Container"
  else
    st=$(container_status "$LT_CONTAINER")
    if [ -n "$st" ]; then
      code=$(http_code "http://127.0.0.1:$port/languages")
      if [ "${code:-000}" = "200" ]; then ok "LibreTranslate: Container läuft, erreichbar auf 127.0.0.1:$port ($st)"
      else warn "LibreTranslate: Container läuft ($st), aber 127.0.0.1:$port antwortet nicht (lädt noch Modelle? docker logs $LT_CONTAINER)"; fi
    elif port_belegt "$port"; then
      wer=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2> /dev/null | awk 'NR==2{print $1}')
      warn "LibreTranslate: kein Container, Port $port ist von ${wer:-etwas anderem} belegt (auf macOS Port 5000 = Kontrollzentrum/AirPlay; LOCAL_TRANSLATE_PORT ändern)"
    else down "LibreTranslate: Container $LT_CONTAINER läuft nicht (./start.sh)"; fi
  fi
else
  down "Docker nicht verfügbar: Postgres, Mailpit und LibreTranslate nicht prüfbar"
fi

if [ "$fehler" = 0 ]; then echo "✅ Alles läuft."; else echo "⚠️  Nicht alles läuft (Exit 1)."; fi
exit "$fehler"
