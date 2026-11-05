# Templates

Templates speed up recurring incidents and maintenance windows. They are managed under
**Templates** and require the `incident_templates:read`, `incident_templates:create`, `incident_templates:update` and `incident_templates:delete` permissions.

## Fields

| Field | Effect | Example |
|---|---|---|
| Internal template name * | only visible in the admin area | `DB outage` |
| Type * | Incident or maintenance window | Incident |
| Title * | Suggested title | `Database unreachable` |
| Message / Details * | Suggested text | `We are investigating the problem.` |
| Default status | pre-selected status | `Investigating` |
| Preselected component(s) | pre-selected components | `API` |

## Usage

When **creating** an incident or maintenance window, "Use template" appears as soon as matching
templates exist. The template fills title, message, status and components once; after that
everything can be changed freely.

## Multiple languages

If the status page has further languages, a language switch appears at the top of the form:
"English (default)" and one entry per further language with its state ("empty", "draft", "done").
In a further language you can translate the title and message of the template; the default
language is required, further languages are optional. Everyone with the "update" permission for
templates sees the switch; everything is saved with one click on "Save". When you "Use template",
the form also takes over these texts into the languages of the new incident or maintenance window
(same state as in the template); afterwards they can be changed freely. See also
[Languages](/docs/sprachen).

An incident does not remember which template it came from. Changing or deleting a template does
not change existing incidents.

Templates are not the same as [mail templates](/docs/mail-templates): templates fill the form,
mail templates define the notification texts.
