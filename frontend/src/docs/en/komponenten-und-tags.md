# Components and tags

## Components

Components are the services whose state the status page shows, for example "Website", "API" or
"Payments". They are managed under **Components** (read `components:read`, create
`components:create`, change `components:update`, delete `components:delete`). The list shows groups as collapsible rows and can be searched by name,
description and tag; matches appear together with their groups.

| Field | Effect |
|---|---|
| Name | Required, at most 60 characters, shown on the status page |
| Identifier | Short, stable key (lowercase letters, digits, hyphens, at most 60 characters), unique per status page. Left empty, it is generated from the name when creating (umlauts spelled out, "-2", "-3" ... on duplicates). Cannot be changed after creation so integrations do not break; webhooks still use the component ID |
| Description | Additional text for the component, at most 240 characters; saving it empty clears the previous description |
| Show on the status page | Switch, on by default. Off: the component is missing from the public status page, the availability view, the subscribe wizard and the public overall status (for a group, including its members). In the admin it stays visible with the "Not public" marker; webhook and status changes keep working |
| Group | Group the component belongs to; "No group" = top level |
| Tags | Assigns the component to regions or teams, see below |
| Initial status / Status | State of the component, see table |

Texts that were saved earlier and exceed the limits stay unchanged. The next time the form is
saved, the field reports the excess and the text has to be shortened.

If the status page has more languages, a language switch appears at the top of the form:
"English (default)" and one tab per further language with its state ("empty", "draft",
"done"). In a further language you can translate the name and the description; the description
is optional there as in the default language, a translation only needs the name. Groups only have
a name. With the "Translation is finished and shown on the status page" checkbox it appears there, without it the
translation stays a draft. Everyone who may change components sees the switch; everything is saved
with one click on "Save". Without text in a language, visitors see the default language. The
translations page in the row menu remains available, see [Languages](/docs/sprachen).

A component's ID is shown in the "ID" column. You need it for the
[monitoring webhook](/docs/api-und-webhooks). The row menu "…" gives access to "Edit",
"Translations", "Archive" and "Delete". A deleted component disappears from the status page; incidents
remain but lose their link to it.

### Archiving

Instead of deleting, you can **archive** a component (row menu "Archive", permission `components:update`).
The switch in the card header toggles between **Active** and **Archived** and shows the count of each; with nothing
archived it reads "No archived components". Publicly, an archived component behaves like a hidden one: it is missing
from the status page, the availability, the subscription wizard and the overall status. It is also no longer offered
in the pickers for incidents, maintenance, templates and subscribers. History, availability, incident links and
translations are kept. "Restore" in the row menu of the "Archived" segment makes it active again. A group takes its
members into the archive and brings them back on restore (members archived earlier on their own stay archived); a
member of an archived group can only be restored through the group. The monitoring webhook keeps working for archived
components. Archiving and restoring appear in the [audit log](/docs/audit-log).

### Status values

| Value | Shown as | How to set |
|---|---|---|
| `operational` | Operational | in the form, in the list, via webhook |
| `degraded_performance` | Degraded Performance | in the form, in the list, via webhook |
| `partial_outage` | Partial Outage | in the form, in the list, via webhook |
| `major_outage` | Major Outage | in the form, in the list, via webhook |
| `under_maintenance` | Maintenance | only by a running [maintenance window](/docs/wartungsfenster) or via webhook or API; the form does not offer this value |

The status can be changed on creation ("Initial status"), when editing and directly in the list
(quick status, saved without confirmation). A mere status change does **not** notify subscribers;
create an [incident](/docs/vorfaelle) for that.

The publicly displayed status is the worst of several sources: the stored status, an open incident
(with the status of its severity: "Degraded Performance", "Partial Outage" or "Major Outage";
the stored status stays unchanged) and linked
[external sources](/docs/externe-quellen).

### Groups

Groups bundle components, for example "Core services" with "Web app" and "API". You create them
with the "+ New Group" button; in a component or group you pick the parent group in the "Group"
field. Groups can also be nested; a cycle (a group under itself or its descendants) is rejected.

- A group has **no status of its own**. It results from the worst status of its children (order:
  major outage, partial outage, degraded, maintenance, operational). You cannot set it, not even
  via webhook.
- A group can only be **deleted when it is empty**. First move or delete its components.
- Only groups of the same status page can be chosen as a group.
- Via the API you set the group with `parent_id`, see [API and webhooks](/docs/api-und-webhooks).

### Externally linked components

A component that is linked to a component of another provider's status page carries the badge
"external" on the public page. You manage links under **External sources**, see
[External sources](/docs/externe-quellen).

## Tags and scope

Tags describe the **scope** of components, incidents, maintenance windows and users, for example
regions such as `EU` or `US`. They are managed under **Tags** (global admins only).

- A **global** tag applies to the whole instance, a **page-bound** tag to one status page only.
  The scope cannot be changed later.
- **Publicly**, tags appear on the status page as the "Region:" filter, so visitors can filter
  components and incidents by region.
- **Access:** If both a user and a component or incident have tags, the user may only edit
  entries that share a tag. If either side has no tags, there is no restriction. Global admins
  are never restricted, and the tag `global` acts as a wildcard. The restriction also covers
  the translations of a component or incident (read, create, save, confirm, delete).
  Deleting an **external source** also removes the links of all its linked components, so it
  is only allowed if the user has tag access to every one of these components; otherwise it
  is rejected and nothing is deleted.
- **Assignment:** page roles with write access see the tags of their page in the form and may
  assign **page-bound** tags. **Global** tags (for example regions that also control access)
  can only be assigned by global admins; a global tag already set is kept when editing.
- **Internal emails** about incidents follow the same rule and only reach users with a matching
  tag.

Example: an incident tagged `EU` only reaches internal users tagged `EU`, `global` or without
any tags.
