# Announcements

Announcements (notices) are general messages on the status page, for example about a
new feature or changed support hours. They have no status history and no components. They are
managed under **Notices** (permissions `notices:read`,
`notices:create`, `notices:update` and `notices:delete`).

## Fields

| Field | Effect | Example |
|---|---|---|
| Title * | Heading | `New support hours` |
| Message / Details * | Text with formatting, see [Formatted text](/docs/formatierte-texte) | `From 1 Nov we are available 24/7.` |
| Level * | `info` (Info), `warning` (Warning) or `critical` (Critical) | `info` |
| Active | only active notices are visible | on |
| Show collapsed (saves space) | the notice does not appear directly but behind "Show … more announcement(s)" | off |
| Start / End (optional) | Display period; empty = unlimited | |
| Notify subscribers by email | sends emails to email subscribers | off |

## Visibility

A notice appears publicly in the section "Announcements" (below the components, see
[The public status page](/docs/oeffentliche-statusseite)) when it is active and the current time
lies within its period. Newest first; collapsed notices are grouped behind "Show … more
announcement(s)". The level determines color and icon. Notices can be translated into further
languages, see [Languages](/docs/sprachen).

## Notifications

Only if "Notify subscribers by email" is set do emails go to active email subscribers:

- on creation or activation ("Notice: Created"),
- when an active notice is edited ("Notice: Updated"),
- on deactivation ("Notice: Ended").

Webhook subscribers do not receive notices.
