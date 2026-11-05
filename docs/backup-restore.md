# Datenbank-Backup und Restore

[English](backup-restore.en.md)

Der gesamte Zustand von Statushafen liegt in der PostgreSQL-Datenbank. Sichere sie regelmäßig und
teste das Einspielen, bevor du es brauchst. Konfiguration und Geheimnisse (`backend/.env`, Schlüssel,
SMTP-Zugang) gehören separat in deine Geheimnisverwaltung.

## Backup anlegen

Mit `pg_dump` (Verbindungsdaten wie in `DB_DSN`):

```bash
pg_dump --clean --if-exists --no-owner "postgres://USER:PASSWORT@HOST:5432/statushafen" \
  | gzip > "statushafen-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
```

Läuft die Datenbank im Container aus `docker-compose.yml`:

```bash
docker compose exec -T db pg_dump -U postgres --clean --if-exists --no-owner statushafen \
  | gzip > "statushafen-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
```

Prüfe jede Datei mit `gzip -t <Datei>` und bewahre mehrere Stände auf (zum Beispiel 14 Tage) sowie
mindestens eine Kopie außerhalb des Servers (Offsite). Ein täglicher Cron- oder systemd-Timer genügt.

## Restore-Test (empfohlen)

Spiele ein Backup in eine **leere Testdatenbank** ein, nie zuerst in die produktive:

```bash
createdb -h HOST -U USER statushafen_test
gunzip -c statushafen-<Zeit>.sql.gz | psql -h HOST -U USER -d statushafen_test
```

Starte danach eine Testinstanz mit `DB_NAME=statushafen_test`, prüfe `/healthz` und öffne im
Admin-Bereich einige Seiten.

## Echter Restore

1. Anwendung stoppen, damit nichts schreibt.
2. Zuerst ein frisches Backup des aktuellen Stands anlegen.
3. Backup einspielen:
   `gunzip -c statushafen-<Zeit>.sql.gz | psql -h HOST -U USER -d statushafen`
   (`pg_dump --clean --if-exists` ersetzt bestehende Tabellen.)
4. Anwendung starten und `/healthz` prüfen. Beim Start gleicht Statushafen das Schema an
   (Migration), ältere Backups lassen sich daher in neuere Versionen einspielen.

## Backup-Alter in der Selbstüberwachung (optional)

Die [Selbstüberwachung](../frontend/src/docs/de/selbstueberwachung.md) kann prüfen, ob das letzte
Backup höchstens 36 Stunden alt ist. Dafür schreibt dein Backup-Job nach jedem Lauf eine Datei
`job_<Name>.prom` in den Ordner `SELFMONITOR_METRICS_DIR` (Standard `/job-metrics`, im Container
einbinden), zum Beispiel `job_backup-prod.prom`:

```text
job_last_success_timestamp_seconds{job_name="backup-prod"} 1767225600
job_last_run_timestamp_seconds{job_name="backup-prod"} 1767225600
job_last_exit_code{job_name="backup-prod"} 0
```

Die Werte sind Unix-Zeitstempel in Sekunden. Setze `SELFMONITOR_BACKUP_JOB=backup-prod` (nur
Kleinbuchstaben, Ziffern und Bindestrich). Der Exit-Code `3` bedeutet „nur der Offsite-Upload ist
gescheitert, das lokale Backup liegt vor“ und zählt als frisch.
