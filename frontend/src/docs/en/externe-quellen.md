# External sources

Many services run on platforms of other providers. With an **external source**, Statushafen reads
such a provider's status page and takes its state into account on your own status page: if a
component fails at the provider that one of your components relies on, your component shows that
too. You manage this under **External sources** in the **Operations** area. You may view the page
with `external_sources:read`; creating needs `external_sources:create`, changing and fetching ("Fetch
now") `external_sources:update`, deleting `external_sources:delete`. "Import" creates your own
components and therefore additionally needs `components:create`.

## Create a source

With "New source" you enter a third-party status page:

| Field | Effect | Example |
|---|---|---|
| Name * | Display name; shown on mirrored reports, in notifications and in the outage email (1 to 200 characters) | `GitHub` |
| Status page address * | Address of the third-party page, `https` only | `https://www.githubstatus.com` |
| Poll interval (seconds) | How often the source is fetched, 60 to 86400, default 120 | `120` |
| Outage after (minutes) | Time without a successful fetch until the outage notice, 10 to 1440, default 60 | `60` |
| Active | Switches regular fetching on or off | on |

The **Summary API v2** (`/api/v2/summary.json`) is supported. Many public status pages offer it,
including pages built on Atlassian Statuspage. Atlassian and Statuspage are trademarks of
Atlassian Pty Ltd; Statushafen is not affiliated with Atlassian. Statushafen
rejects private, local and internal addresses. A newly created source is queried for the first
time at the next tick, or immediately with "Fetch now" in the row menu; the result of the fetch is
shown. For each source only one fetch runs at a time.

The list shows each source's state (Reachable, Fetch failed, Outage, Inactive, Not fetched yet),
the last fetch, and the number of components and links. When you **delete** a source, Statushafen
also removes its components, links and mirrored reports. An inactive source is not fetched; its
links and mirrored reports no longer count then, but the data is kept.

## Adopt components

The fastest way to linked components is the card "Adopt components":

1. Choose the source. Its components and groups appear in a list (with search).
2. Select components or whole groups (at most 200 per run; "Select all visible" helps). Already
   linked components cannot be selected.
3. Under "Target group" choose which of your own groups the new components go under ("Top level"
   = no group) and optionally assign tags.
4. Click "Adopt".

For every selected component an own component is created with its name, status "Operational" and
the link to the external component. A whole external group is created as an own group with its
current components; the group itself is not linked, its status results from the child components.
Everything happens in one step: if something fails, nothing is created. Skipped entries (already
linked, meanwhile vanished, group without components) are listed afterwards; a second click does
not create anything twice.

Good to know: components that the provider adds later are not adopted automatically, name changes
at the provider are not followed, and adopting cannot be undone (delete the components one by
one).

## Link components

If you already have the component, link it by hand on the card "Links": choose your own component,
the source and the external component, then click "Link". The external component can also be a
**whole external group**. "Unlink" removes the link again.

- A component can be linked to several external components. Pure groups cannot be linked, only
  their components.
- For a linked external group, the worst status of its current members counts; new members are
  added automatically, vanished ones do not count.
- If you link a group and one of its components at the same time, the double link has no effect on
  the status; the list warns about it.
- The table shows the current status of each linked component. "Vanished" means the provider no
  longer delivers the component; "Unknown" stands for a status Statushafen does not know.

## Effect on the status

The publicly displayed status of a component is the **worst** of its own status, an open own
incident and the linked external components. If the external status changes, the display changes
at the next fetch; the stored own status stays unchanged. On the public page the component carries
the badge "external" with source and component, see
[The public status page](/docs/oeffentliche-statusseite#external-sources-on-the-page).

## Outage of a source

If a source does not deliver a successful fetch for longer than the configured limit ("Outage
after"), it counts as **down**. A single error triggers nothing. Then the following happens:

- In the admin area a banner "External source unreachable" appears at the top with a link to the
  sources page; the list shows the state "Outage".
- The source's last known status **no longer counts**. A component shows only its own status
  again; publicly, "Data outdated" appears instead of a status.
- Statushafen sends an email to all global admins and all users with `external_sources:update` on the
  page: once at the start, once as a reminder after 24 hours of duration and once when the source
  is back. The language follows the recipient's interface language.

Once the source is reachable again, its status counts again immediately. If email delivery is
switched off in the [System settings](/docs/systemeinstellungen), Statushafen records the skipped
emails in the [audit log](/docs/audit-log).

## Mirrored reports

Besides the status, Statushafen also fetches the source's **incidents and maintenance**. Only
reports that affect at least one linked component are mirrored. The card "Mirrored reports" lists
them with kind, state, affected own components and number of updates; filters by kind (incident,
maintenance), state (open, closed) and source. The texts come from the third-party page and are
neither translated nor changed. For closed reports, the list covers the last 30 days.

- **Publicly**, open reports and those that ended in the last 14 days appear under "Reports from
  external sources", clearly marked as external and with a link to the original page.
- **Notifications:** [subscribers](/docs/abonnements) of the affected components receive a message
  (email or Google Chat) for new, updated and closed reports. During the very first fetch of a
  source everything stays silent, so the provider's history does not cause a flood of emails. The
  language follows the default language of the page (German or English). The texts come from
  built-in defaults; custom templates for these emails exist only via the API, see
  [Mail templates](/docs/mail-templates#reports-from-external-sources).
- **Availability:** mirrored incidents count in the 90-day availability like own incidents, mirrored
  maintenance like maintenance. The provider's impact determines the severity: "minor" counts as
  Degraded Performance, "major" as Partial Outage, "critical" as Major Outage. If the provider
  explicitly reports an incident as without impact ("none"), it does not count. Details under
  [Availability and calculation](/docs/oeffentliche-statusseite#availability-and-calculation).
- **Retention:** Statushafen permanently deletes closed mirrored reports 90 days after they ended
  (hourly clean-up run, entry in the audit log). Open reports always stay. Operators change the
  period with the environment variable `EXTERNAL_INCIDENT_RETENTION_DAYS` (number of days;
  invalid or non-positive values result in 90 again).

## Permissions and audit log

All actions are recorded in the [audit log](/docs/audit-log) (`external_source.*`,
`external_link.*`, `external_incident.*`). A source belongs to exactly one status page; foreign
IDs return an error. The endpoints are in the OpenAPI file, see
[API and webhooks](/docs/api-und-webhooks).
