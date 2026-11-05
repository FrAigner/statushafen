#!/usr/bin/env bash
# Regression gegen eingecheckte Editor-/sed-Artefakte. `sed -i -E` ohne Suffix-Argument legt auf
# macOS eine Kopie `<datei>-E` an (so landete `backend/models/models.go-E` im Repo). Der Test schlägt fehl,
# sobald `git ls-files` eine Datei mit Artefakt-Endung (-E, .orig, .rej, .bak, .swp, ~) oder ein
# Coverage-Profil (coverage*.out) enthält.
# Braucht nur git (kein Netz, kein Docker).
set -euo pipefail
cd "$(dirname "$0")/.."

treffer=$(git ls-files | grep -E -- '-E$|\.orig$|\.rej$|\.bak$|\.swp$|~$|(^|/)coverage[^/]*\.out$' || true)
if [ -n "$treffer" ]; then
  echo "FEHLER: getrackte Artefaktdateien (git rm, nicht einchecken):" >&2
  echo "$treffer" >&2
  exit 1
fi
echo "OK: keine getrackten Artefaktdateien"
