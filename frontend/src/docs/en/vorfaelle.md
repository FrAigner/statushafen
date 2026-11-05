# Incidents

An incident reports an outage. Incidents are managed under **Incidents** (permissions
`incidents:read`, `incidents:create`, `incidents:update`, `incidents:delete`) and appear on the status page under "Current Incidents".

## Create an incident

| Field | Effect | Example |
|---|---|---|
| Title * | Heading on the status page | `Login not possible` |
| Message / Details * | Description for visitors, with formatting ([Formatted text](/docs/formatierte-texte)) | `We are investigating login errors.` |
| Status * | Current state, see below | `Investigating` |
| Affected component(s) * | Components of the page that are affected | `Website`, `API` |
| Tags (scope) | Narrows visibility and internal emails, see [tags](/docs/komponenten-und-tags#tags-and-scope) | `EU` |
| Severity | Degraded Performance (default), Partial Outage or Major Outage; weights the [availability](/docs/oeffentliche-statusseite#availability-and-calculation) (0.3 / 0.6 / 1.0), can be changed later. Maintenance does not count as downtime | `Partial Outage` |
| Start / End | Times of the incident; the end only with "Resolved" | |

"Use template" pre-fills title, message, status and components from a
[template](/docs/vorlagen).

## Status flow

| Value | Shown as | Meaning |
|---|---|---|
| `investigating` | Investigating | Outage known, cause still open |
| `identified` | Identified | Cause found |
| `monitoring` | Monitoring | Fix deployed, situation is being watched |
| `resolved` | Resolved | Incident closed |

The usual order is top to bottom, but Statushafen does not enforce it: you can pick any of the
four statuses directly and also reopen an incident; Statushafen rejects other values. When you set
an incident to "Resolved", Statushafen records the time as its end (also via an update). Setting
it to any other status clears its end time. The end must not be before the start. Resolved
incidents appear on the status page under "Past Incidents".

As long as an incident is open (`investigating`, `identified`, `monitoring`), the status page
shows its components at least with the status of its severity: "Degraded Performance", "Partial
Outage" or "Major Outage". This is the same mapping as in the availability bars, so the display and
the bars never contradict each other. If a component belongs to several open incidents, the worst
severity applies. A worse, manually set status stays in place.

## Updates

Clicking an incident opens the detail view with its history. There you add a new entry with the
update form (new status and update message). The update copies its status into the
incident and appears publicly in the history. The incident's original description stays as the
**initial report** and is shown at the very bottom of the history. On the status page, the incident
shows its latest updates; the newest carries the label "Latest update", see
[The public status page](/docs/oeffentliche-statusseite#incidents-and-their-history).

If the status page has several languages and you may change the incident, a language switcher
appears above the update message. One message per additional language (no title) is saved together
with the update. You add translations to updates that are already posted with **Translations** on
the entry in the history. Visitors see the message in their language once the translation is marked
as done; otherwise the default language of the page or the original.

Example history:

1. `Investigating`: "Login fails for some users."
2. `Identified`: "A certificate has expired."
3. `Monitoring`: "A new certificate is in place."
4. `Resolved`: "No errors for 30 minutes."

## Notifications

When you create or edit an incident and with every update, Statushafen sends notifications:

- to internal users (global admins and users with a role on the page, filtered by tags),
- to [subscribers](/docs/abonnements) who subscribed to at least one affected component.

The texts come from the [mail templates](/docs/mail-templates). An update with status "Resolved"
uses the "Incident resolved" template.

So that a slip or a misused account cannot trigger thousands of mails, limits apply per page and per
user: at most 30 new incidents, maintenance windows and announcements per hour per page (60 per user)
and 120 changes and updates per hour. Beyond that the interface answers "Too many requests"; after the
time given in `Retry-After` you can continue. Normal operation, even a long incident with many
updates, stays far below.

## Post-mortem

After the incident you can write a post-mortem in the detail view: summary, root cause, impact,
resolution, detection and follow-up actions (open, in progress, done). It stays a draft until you
publish it.

## Translations

Title and message of each incident can be translated into further languages, and so can the
message of each update (see above), see
[Languages and translations](/docs/sprachen).
