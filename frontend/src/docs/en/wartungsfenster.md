# Maintenance windows

Maintenance windows announce planned work. They are managed under **Maintenance windows**
(permissions `maintenance:read`, `maintenance:create`, `maintenance:update`, `maintenance:delete`) and appear on the status page under "Scheduled Maintenance".

## Fields

| Field | Effect | Example |
|---|---|---|
| Title * | Heading | `Database update` |
| Message / Details * | Description for visitors, with formatting ([Formatted text](/docs/formatierte-texte)) | `Short interruptions possible.` |
| Affected component(s) * | Components affected during the maintenance | `API` |
| Tags (scope) | As for [incidents](/docs/vorfaelle) | `EU` |
| Start * / End * | Planned period; the end must be after the start | `02:00` to `03:00` |
| Reminder before | Lead time for a reminder email in minutes, hours or days; `0` = none | `1 hour` |

## Automatic flow

Statushafen checks all maintenance windows every minute and sets the status itself:

| Value | Shown as | When |
|---|---|---|
| `scheduled` | Scheduled | before the start |
| `in_progress` | In Progress | from the start; affected components are set to "Maintenance" |
| `resolved` | Completed | from the end; the components go back to "Operational" |

On the status page, a window appears under "Scheduled Maintenance" before its start and under
"Current incidents" (with a clock icon) afterwards. In the 90-day availability, maintenance does
not count as downtime, see
[The public status page](/docs/oeffentliche-statusseite#availability-over-the-last-90-days).

When you create a window, its status is derived from the times. If the start is already in the
past, the maintenance runs immediately.

> Note: after the end the components are "Operational" again, even if they had a different
> outage before the maintenance. Check their status afterwards if needed.

## Notifications

- **Reminder:** once before the start, if a lead time is set.
- **Started** and **Completed:** on every automatic transition, each with a history entry.

The texts are in the [mail templates](/docs/mail-templates) under "Maintenance window:
Reminder/Started/Completed".
