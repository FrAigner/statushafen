#!/bin/bash

echo "🛑 Stoppe Statushafen Umgebung..."
source ./scripts/local-translate.sh
local_translate_stop

if [ -f .pids ]; then
    while read p; do
        if kill -0 $p > /dev/null 2>&1; then
            echo "Beende Prozess $p..."
            kill $p
        fi
    done < .pids
    rm .pids
    echo "✅ Umgebung erfolgreich gestoppt."
else
    echo "⚠️ Keine laufenden Prozesse gefunden (Datei .pids fehlt)."
    
    # Fallback: Versuche Prozesse anhand des Namens zu killen
    pkill -f "dev-backend.sh"
    pkill -f "statushafen-dev-backend"
    pkill -f "statushafen-dev-backend"
    pkill -f "go run ."
    pkill -f "statuspag"
    pkill -f "vite"
    echo "🔍 Cleanup durchgeführt."
fi
