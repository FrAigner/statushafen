# The public status page

The public status page is what visitors and customers see. You reach it at the page's address, see
[Status pages and tenants](/docs/statusseiten). The page loads its data when opened and every
minute after that; "Last updated" shows the current state.

## Overall status and layout

At the top is the **overall status** ("All systems running normally", "Degraded performance", "Partial
outage", "Major outage" or "Maintenance in progress") with the number of components that run
normally. Below follow, as far as there is anything to show:

1. **Search and filters**.
2. **Current incidents**, including running maintenance.
3. **Scheduled maintenance**, see [Maintenance windows](/docs/wartungsfenster).
4. **Reports from external sources**, see below.
5. **Availability over the last 90 days**.
6. **Components** as a tree with their groups. With the page layout "List", cards with 90-day
   bars and an incident timeline are shown instead.
7. **Announcements** of the page, see [Announcements](/docs/ankuendigungen).
8. **Past incidents**: resolved incidents, newest first.
9. **Get notified** with the "Subscribe now" button, see [Subscriptions](/docs/abonnements).

## Searching and filtering

- **Search field:** searches components (name and description) and incidents (title and message).
  Several words count as AND. `AND`/`UND` and `OR`/`ODER` combine terms, with AND binding
  stronger than OR. A phrase goes in quotes. Examples: `Frankfurt AND database`,
  `London OR Paris`, `"Frankfurt South" OR Hamburg`. The question mark next to the field shows
  this help.
- **Region:** chips with the page's [tags](/docs/komponenten-und-tags#tags-and-scope). You can
  pick several regions. With many tags the page first shows only part of them and a "Show more"
  button.
- **Status:** chips for Operational, Degraded Performance, Partial Outage and Major Outage. They
  refer to the status of the components; an incident matches if one of its affected components
  has the chosen status.
- The result counter shows components and incidents; "Clear filters" resets everything.

## Incidents and their history

Under "Current incidents" every open incident is shown with title, status, affected components and
start. The three most recent incidents show their latest three updates right below; the newest
carries the label **Latest update**. A click on "View full history" opens all updates, newest
first. At the very bottom of the history is the **initial report**, i.e. the original description
of the incident. It is kept even when later updates change the status. Once an incident is
resolved, it moves to "Past incidents". The page first shows all open and the newest 50 resolved
incidents; "Load older incidents" adds 50 more each time. How admins maintain incidents is described
under [Incidents](/docs/vorfaelle).

## Availability over the last 90 days

The section shows a bar for each of the last 90 days (UTC, including today) and the availability
in percent above it. With the page layout "List" there are bars per component as well. The values
come from the incidents and maintenance windows of the page; the server caches them for up to 60
seconds.

- The colour of a day shows the worst status of that day: Degraded Performance, Partial Outage
  or Major Outage. The levels also differ in shape without colour perception (diagonal hatching,
  cross-hatching, solid). A tooltip gives the availability in percent and the duration per
  severity. How the percentage is derived is described under
  [Availability and calculation](#availability-and-calculation).
- Maintenance is shown as maintenance and does **not** count as downtime.
- Mirrored reports of [external sources](/docs/externe-quellen) count like own incidents and
  maintenance.
- Days before the page was created and days without observation are shown neutrally as "No
  data". Statushafen does not invent 100 % or 0 % for them. The overall percentage only appears
  once the page has been observed for at least 24 hours; before that the page shows "no data
  yet".
- **Status history of the components:** every status change of a component (via webhook, by hand,
  by a maintenance window or by self-monitoring) is stored with time, old and new status and
  source, and counts in the availability with its status, even without an incident. If an incident
  is running at the same time, the worst status applies. The history starts with the update that
  introduced it: the status current at that time applies from then on; anything set before is
  unknown to Statushafen (an [incident](/docs/vorfaelle) is still needed for that). Entries are
  kept for 365 days; the last state before that cut-off is kept as the starting status.
  If a monitor changes the status by webhook several times within 30 seconds (flapping), Statushafen
  condenses the history: the status applies immediately, but at most two entries remain per
  30-second window: one with the worst status of the window and one with the final state. An
  outage is never lost. Availability is pessimistic here: the whole window counts as the worst
  status, never as better. At most
  the newest 10,000 changes of the window per component are evaluated; if that cuts data, the
  availability response contains `history_truncated: true`.
- Percentages are truncated, not rounded, so 99.996 % does not appear as 100.00 %.

### Availability and calculation

Not every disruption is a full outage. Statushafen weights the time of impairment by severity:

| Severity | Weight |
| --- | --- |
| Degraded Performance | 0.3 |
| Partial Outage | 0.6 |
| Major Outage | 1.0 |
| Maintenance | 0 (does not count) |

**Availability = 1 − weighted downtime ÷ observed time**, per day and component. Six hours of
"Degraded Performance" make 1.8 hours of weighted downtime, so 92.5 % for that day (before: 75 %,
every disruption counted as a full outage).

- **Overlaps** do not count twice: at any moment only the worst status applies. An incident that
  spans several days or midnight is split across the days involved.
- **Whole page:** the page value is the average over all visible components, the day colour is the
  worst status of any component. An incident without a component affects all visible components.
- **Where the severity comes from:** own incidents carry a severity (Degraded Performance
  0.3, Partial Outage 0.6, Major Outage 1.0); incidents without one, including older ones, count
  as "Degraded Performance". A change also applies retroactively (within a minute at most, because
  of caching). Mirrored reports of [external sources](/docs/externe-quellen) bring
  their impact: "minor" counts as Degraded Performance, "major" as Partial Outage, "critical" as
  Major Outage, "none" not at all. The status from the components' status history counts with its own level
  (Degraded Performance, Partial Outage, Major Outage; maintenance does not count).
- Below 24 hours of observation the page still shows "No data" instead of a percentage.

The raw data comes from `GET /api/public/pages/<slug>/availability`, see
[API and webhooks](/docs/api-und-webhooks#further-api).

## External sources on the page

Components that are linked to an [external source](/docs/externe-quellen) carry the **external**
badge. The badge is a link to the source page (opens in a new tab). Below it, the text says which
component of the source provides the status; for a linked group, the worst component of the
group. If the source is down, "Data outdated" appears instead of a status.

The section **Reports from external sources** shows the incidents and maintenance of the
providers that affect linked components: open reports and those that ended in the last 14 days.
Each is marked "external", names the affected components and links to the original page ("View
original at …"); the original page is always authoritative. Search and filters also apply to
these reports, via the affected components.

## Private pages

A private page only shows content to signed-in users with access; everyone else sees the notice
"Status page unavailable" with a link to the login (and, if the page uses SSO, the button "Sign in
with SSO"). The notice deliberately says "does not exist or is private": for visitors without
permission a private page answers technically exactly like a page that does not exist (HTTP 404), so
that nobody can tell from the outside which addresses belong to private pages. The API does not
reveal it either. Private pages do not appear on the Statushafen home page.
