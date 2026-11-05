# Self-monitoring

Statushafen monitors itself and records the result on the system page (`system.example.com`).
The components under **Platform** therefore no longer stay "operational" until someone creates an
incident: a failure is detected and shown.

## What is checked

The checks run in the background every minute (email and backup every five minutes).

| Component | Check | Status on failure |
| --- | --- | --- |
| API | The application's own health endpoint responds | Major outage |
| Database | A real query succeeds | Major outage |
| Frontend | The application serves its own home page (internal check: HTML with the page entry point and one linked file from `/assets/`) | Major outage |
| Email delivery | The mail server accepts connections (nothing is sent) | Partial outage |
| Database backup | The last nightly backup is at most 36 hours old | Degraded |

If email delivery is switched off in [System settings](/docs/systemeinstellungen), the mail server
is not checked. The backup check exists only if the operator has set it up (environment variable
`SELFMONITOR_BACKUP_JOB`, e.g. `backup-prod`; lowercase letters, digits and hyphen are allowed). If
the job is set but the metrics directory is missing (`SELFMONITOR_METRICS_DIR`, default
`/job-metrics`, for example because the mount is gone), that counts as a failure: the component
becomes "Degraded" (after three failures at the five-minute interval, i.e. about 15 minutes after
the first one) and if the failure lasts another 15 minutes, an incident is created (about 25 to 30
minutes in total after the first failure). If only the metrics file is missing
in an existing directory (environment without a backup timer), the check is skipped and the
component is not shown as failed.

## How the status comes about

- **Debouncing:** The component only flips after three failures in a row. A single hiccup changes
  nothing.
- **Incident:** If the failure lasts 15 minutes, the system automatically creates an incident
  "Störung: …" (status "Investigating", author "system"). No notifications are sent to subscribers.
- **Recovery:** As soon as the check succeeds again, the component returns to "operational" and
  the incident is closed.
- **Your own changes take precedence:** If a component is not "operational" (for example because
  of maintenance or because an admin set it), monitoring leaves it alone. If an admin has already
  closed the incident, it stays closed.
- **Audit log:** Every change appears in the [audit log](/docs/audit-log) with the actor "system"
  (`selfmonitor.component_failed`, `selfmonitor.incident_opened`, `selfmonitor.component_recovered`,
  `selfmonitor.component_released`). The last one appears when an admin has deleted the monitored
  component: the monitor cleans up and closes its open incident.

## Switching on and off

The **Self-monitoring enabled** switch in [System settings](/docs/systemeinstellungen) turns the
checks off, for example during planned server work. Without an entry it is **off**: it only runs once a global admin explicitly switches it on.
When you switch it off, monitoring releases the failures it set itself: the component returns to
"operational" and the automatic incident is closed with the note "Automatische Verfolgung beendet."
(audit log: "Überwachung abgeschaltet"). The same applies to the email check when email sending is
switched off. Statuses set by an admin are left alone.

If a monitored component is assigned to another status page (e.g. by a database intervention),
monitoring ends its tracking (markers, its own incident; `selfmonitor.component_released` with the
note "verschoben"). If the component still has the failure status that monitoring set, it returns to
"operational"; any other status is left unchanged. Without an open incident the component cannot be
identified exactly: monitoring then only ends tracking (note "nicht mehr auf der Systemseite") and
touches no component on other pages.

Publicly hidden components are not evaluated (archived or invisible, also when only the parent
group is archived or hidden; the same rule as on the status page): monitoring sets no status and
opens no public "Störung: …" incident for a component that does not exist publicly. If a component is
archived or hidden during an ongoing failure, monitoring releases it (status back to
"operational", incident closed with a note that tracking ended and the component is no longer
shown, audit log "Komponente archiviert oder ausgeblendet"). Since the incident stays in the public
history, its title and message are replaced by neutral texts without the check name or error
detail ("Störung: automatische Überwachung" and "Die automatische Selbstüberwachung meldete eine
Störung.") and their translations are deleted. Only texts written by monitoring and left unchanged
are replaced; texts changed or extended by an admin and updates by an admin stay. The same applies if the
component is deleted or moved to another page during a failure. Incidents that are already closed
and whose component is now hidden, deleted or moved are cleaned up the same way at application
start and every 10 minutes after that (also while monitoring is switched off). After restoring, monitoring resumes
normally with a fresh debounce.

## Limits

Monitoring runs inside the application. If the whole application or the server goes down, it can
no longer record that; monitoring from the outside is still needed for that case.

The frontend check calls the application itself internally (`127.0.0.1`), not the public address.
It therefore does not cover Cloudflare, Caddy (access lock, bot protection) or the certificate;
that also needs monitoring from the outside. `FRONTEND_URL` only indicates that a frontend is
served and is never the check target. If the application itself answers 401 or 403, that still
counts as a failure.
