# Audit log

The **Audit Log** (global admins only) shows who changed what and when. The newest entries are at
the top, 25 per page.

## Columns

| Column | Content |
|---|---|
| Time | when the action took place |
| Actor | email and role of the user; automatic actions appear as "System" |
| Action | what happened as readable text (e.g. "Incident created") with the target's name; new actions that are not translated yet show the technical key (e.g. `incident.created`) |
| Category | area of the action |
| IP Address | address the action came from |

## Filtering

- **Search field** "Filter by actor, target, or IP…": searches the actor's email, the target's
  name and ID, and the IP address.
- **Categories:** One chip each for Authentication, Incident, Configuration, User, Subscriber,
  Notice, Maintenance and System (automatic checks of the self-monitoring). One click filters, a
  second click removes the filter; a chip and the search field can be combined.

## Example actions

The table names the technical keys; the interface shows readable texts for them.

| Area | Actions |
|---|---|
| Sign-in | `auth.login`, `auth.login_failed`, `auth.sso_login`, `auth.sso_linked` |
| Incidents and components | `incident.*`, `postmortem.published`, `component.status_webhook_updated` |
| Status pages | `statuspage.*`, e.g. `webhook_token_generated` |
| Users and roles | `user.*`, `role.*` |
| Subscribers and email | `subscriber.*`, `notification.email_failed`, `email.skipped_disabled` |
| External sources | `external_source.*` (e.g. `fetched`, `outage_email_sent`), `external_link.*`, `external_incident.*` (`mirrored`, `failed`, `pruned`) |
| Languages and texts | `language.*`, `page_languages.updated`, `notification_template.*` |
| System | `system_setting.updated`, `pageorder.*` |

When a user deletes their account, their audit entries are kept without reference to the person.
