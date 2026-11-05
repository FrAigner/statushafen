# Database backup and restore

[Deutsch](backup-restore.md)

All state of Statushafen lives in the PostgreSQL database. Back it up regularly and test restoring
before you need it. Keep configuration and secrets (`backend/.env`, keys, SMTP credentials) separately
in your secret management.

## Create a backup

With `pg_dump` (connection details as in `DB_DSN`):

```bash
pg_dump --clean --if-exists --no-owner "postgres://USER:PASSWORD@HOST:5432/statushafen" \
  | gzip > "statushafen-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
```

If the database runs in the container from `docker-compose.yml`:

```bash
docker compose exec -T db pg_dump -U postgres --clean --if-exists --no-owner statushafen \
  | gzip > "statushafen-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
```

Check each file with `gzip -t <file>`, keep several generations (for example 14 days) and at least one
copy off the server (offsite). A daily cron job or systemd timer is enough.

## Restore test (recommended)

Restore a backup into an **empty test database**, never into the production one first:

```bash
createdb -h HOST -U USER statushafen_test
gunzip -c statushafen-<time>.sql.gz | psql -h HOST -U USER -d statushafen_test
```

Then start a test instance with `DB_NAME=statushafen_test`, check `/healthz` and open a few pages in
the admin area.

## Real restore

1. Stop the application so nothing writes.
2. First create a fresh backup of the current state.
3. Restore the backup:
   `gunzip -c statushafen-<time>.sql.gz | psql -h HOST -U USER -d statushafen`
   (`pg_dump --clean --if-exists` replaces existing tables.)
4. Start the application and check `/healthz`. On start Statushafen adjusts the schema (migration), so
   older backups can be restored into newer versions.

## Backup age in the self-monitoring (optional)

The self-monitoring can check that the last backup is at most 36 hours old. For that your backup job
writes a file `job_<name>.prom` into the folder `SELFMONITOR_METRICS_DIR` (default `/job-metrics`,
mount it into the container) after each run, for example `job_backup-prod.prom`:

```text
job_last_success_timestamp_seconds{job_name="backup-prod"} 1767225600
job_last_run_timestamp_seconds{job_name="backup-prod"} 1767225600
job_last_exit_code{job_name="backup-prod"} 0
```

The values are Unix timestamps in seconds. Set `SELFMONITOR_BACKUP_JOB=backup-prod` (lowercase
letters, digits and hyphens only). Exit code `3` means "only the offsite upload failed, the local
backup exists" and counts as fresh.
