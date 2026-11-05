# Mail templates and placeholders

Mail templates define subject and body of a status page's notifications. They are managed under
**Mail Templates** (permissions `notification_templates:read`, `notification_templates:update` and `notification_templates:delete`). If there is no custom template for an event,
Statushafen sends a built-in default text.

## Structure of a template

A template applies to a combination of **event**, **audience** and **language**:

- **Audience:** "Internal (Staff)" for users of the page or "Subscribers".
- **Language:** one of the languages enabled for the page, see [Languages](/docs/sprachen).
- **Subject** and **Body (HTML)** must not be empty; the subject must not contain a line break.

The preview renders the template with sample data. "Reset to default" removes the custom
template.

## Events

| Event | When |
|---|---|
| Incident created | new incident |
| Incident updated | incident edited or new update |
| Incident resolved | update or change to "Resolved" |
| Maintenance window: Reminder | configured lead time before the start |
| Maintenance window: Started | start reached |
| Maintenance window: Completed | end reached |
| Notice: Created / Updated / Ended | only with "Notify subscribers by email" |
| Subscription confirmation (double opt-in) | new email subscription |

## Placeholders

Placeholders are replaced when the email is sent. Their values are HTML-escaped automatically. The
editor lists them under available placeholders; typing `{` suggests placeholders.

| Placeholder | Content |
|---|---|
| `{{.Title}}` | Title of the incident, maintenance or notice; for the subscription confirmation the status page name |
| `{{.IncidentTitle}}` | same value as `{{.Title}}` (older spelling) |
| `{{.Level}}` | notices only: `info`, `warning` or `critical` |
| `{{.Message}}` | Description or update text as plain text (Markdown source, unformatted) |
| `{{.MessageHTML}}` | the same message formatted as ready-made HTML (bold, lists, links), see [Formatted text](/docs/formatierte-texte); use it in the mail text only, not in attributes and not in the subject (there the HTML would show up as literal text, although it is technically accepted) |
| `{{.Status}}` | Status as a value, e.g. `investigating` |
| `{{.StatusLabel}}` | Status as text, German for language `de`, otherwise English |
| `{{.ComponentNames}}` | Affected components, comma-separated |
| `{{.Tags}}` | Tags of the incident |
| `{{.Action}}` | Kind of event, e.g. `created` or `updated` |
| `{{.Language}}` | Language code of the template |
| `{{.FrontendURL}}` | Link to the status page |
| `{{.UnsubscribeURL}}` | Personal unsubscribe link (subscribers only) |
| `{{.ConfirmURL}}` | Confirmation link (subscription confirmation only) |
| `{{.SourceName}}` | Name of the external source (reports from external sources only) |
| `{{.SourceURL}}` | Link to the original report (reports from external sources only) |
| `{{.FromAddress}}` | Sender of the email (page name and the server's address) |

## Allowed constructs and limits

Templates may contain placeholders and simple conditions only: `{{if …}}…{{else}}…{{end}}` with the
functions `eq`, `ne`, `not`, `and` and `or`, for example `{{if eq .Status "resolved"}}…{{end}}`.
Loops (`range`), sub-templates (`template`, `define`, `block`), variables, `printf` and other
functions are rejected when saving and in the preview. The subject may be at most 500 characters
long, the body at most 64 KB, and the rendered result is size- and time-limited. A previously
saved template that violates these rules is not used; the built-in text is sent instead.

The **subscription confirmation** template must contain `{{.ConfirmURL}}` (not only inside a
condition). Statushafen puts it into a fixed frame with the page name and a note for recipients who
did not sign up; the frame cannot be changed.

## Example

Subject:

```text
[{{.StatusLabel}}] {{.Title}}
```

Body:

```html
<p>{{.Message}}</p>
<p>Affected: {{.ComponentNames}}</p>
<p><a href="{{.FrontendURL}}">Open status page</a></p>
<p><a href="{{.UnsubscribeURL}}">Unsubscribe</a></p>
```

## Reports from external sources

For mirrored reports of [external sources](/docs/externe-quellen#mirrored-reports) there are six
more events (`external_incident_created`, `external_incident_updated`,
`external_incident_resolved` and the same three for maintenance with `external_maintenance_…`),
each for the recipient group "Subscribers" in the page's default language. The editor offers
them as "External incident: …" and "External maintenance: …" and sets the recipient group to
"Subscribers". In addition to the placeholders above, `{{.SourceName}}` (name of the source) and `{{.SourceURL}}` (link to the
original report) are available there. Without a custom template, Statushafen sends a built-in
text. The outage emails of a source always use the built-in text.

## Current limitation

When subscribing, the visitor's language is stored as the email language, provided the page has
enabled it (otherwise the default language of the page applies). All emails to subscribers
(incidents, maintenance, notices, subscription confirmation, reports from external sources) use
the subscriber template in that language, falling back to the **default language of the page**; if
that is missing too, the built-in text is sent (German or English; English for any other
language). A template in a third language is not used as a substitute. Subscribers change the
language in the subscription manager, admins in the subscriber form (or through the API:
`PUT /api/me/subscription`, field `language`).

Internal users get incident emails in their interface language (German or English; English if none
is stored): first the staff template in that language, then the English one, otherwise the built-in
text.

Messages to Google Chat webhooks are fixed texts without a template, in German or English
depending on the subscriber's language (any other language gets English); the global webhook
(`GOOGLE_CHAT_WEBHOOK_URL`) stays English. The global webhook is an operator tool for the whole instance: it only receives incidents of public pages, never those of private pages. The message in them is plain text (see [Formatted text](/docs/formatierte-texte)).
