# Users, roles and permissions

## Global admin and page roles

- **Global admin:** may do everything, on all status pages. Only global admins see Overview,
  Status Pages, Audit Log, Users, Roles & Permissions, Tags, Monitoring and System Settings.
- **Role per status page:** all other users need a role on a status page. The role bundles
  permissions. Without a role on the page there is no access.

Global admins manage users and roles under **Users** and **Roles & Permissions**. Users with
`pages:create` may assign a role to users of their page (currently only via the API; in the
interface only the global admin has the row menu "Users" under **Status Pages**). If the user
already has a role on the page, it is replaced.

When you create a user, an email address and a password of **at least 8 characters** are
required; the same rule applies when you change a password or set one during ordering. "Global
admin" is not a page role but a switch on the user (full access to all pages). For self-protection:
you cannot delete your own account under **Users** (use the profile for that), you cannot remove
your own global admin rights, and the last global admin always remains.

## Standard roles for pages

- **Page Owner:** all permissions of "Page Admin" plus the permissions for status pages, mail templates, languages, UI texts and SSO. Whoever is responsible for a
  page including its users, SSO, domain, mail templates and languages gets this role.
- **Page Admin:** maintains content (components, incidents, notices, subscribers), but not the
  page settings and users.
- **Editor:** read components, edit incidents, maintenance, templates and notices.
- **Viewer:** read only (components, incidents, notices).

The "Page Owner" role is created automatically once, at the first startup (if no role of that name exists). Global
admins can adjust, rename or delete it under **Roles & Permissions**; that is kept and it is not created again. Whoever orders a
status page themselves becomes "Page Admin" of that page. If they should also maintain users, SSO,
mail templates and languages of their own page, a global admin assigns them "Page Owner".

Whoever may manage users on a page (`pages:create`/`pages:delete`) can only assign roles whose
rights they hold themselves on that page, and can only invite new accounts by email address: the account becomes a member only once it accepts the invitation (see **Status pages**). Accounts with the old global SSO, a page SSO link or a role on another page, and global admin accounts, can only be added by a global admin. Picking
arbitrary accounts by ID is reserved for global admins.

Each account has exactly one role per page. As a non-global-admin you cannot remove your own role or downgrade it to a role without `pages:update` if you are the last member with that right on the page (message "last user with management rights"). Removing a member, deleting their role or taking the page away from the account also deletes the account's page SSO link for that page.

## Permissions

Every tab of the admin navigation is its own resource with four actions: **Create**, **Read**,
**Update** and **Delete**. A permission is called `<resource>:<action>`. Every API route checks the
action that matches the HTTP method: GET needs read, POST create, PUT and PATCH update, DELETE
delete. Special actions are assigned to one action (see the "Routes" column).

| Area | Tab | Resource | Applies to | Permissions | Routes |
|---|---|---|---|---|---|
| Operations | Overview | `overview` | global | `overview:create`, `overview:read`, `overview:update`, `overview:delete` | GET /admin/overview |
| Operations | Components | `components` | page | `components:create`, `components:read`, `components:update`, `components:delete` | POST/PUT/DELETE /pages/{id}/components, component translations |
| Operations | External Sources | `external_sources` | page | `external_sources:create`, `external_sources:read`, `external_sources:update`, `external_sources:delete` | /pages/{id}/external-sources, -links, -incidents, -outages; "Fetch now" = update, "Import" = create (additionally `components:create`) |
| Operations | Incidents | `incidents` | page | `incidents:create`, `incidents:read`, `incidents:update`, `incidents:delete` | /pages/{id}/incidents (without maintenance), timeline entries = create, post-mortem = update, incident translations |
| Operations | Maintenance | `maintenance` | page | `maintenance:create`, `maintenance:read`, `maintenance:update`, `maintenance:delete` | the same routes as incidents for maintenance windows (the kind of record decides) |
| Operations | Notices | `notices` | page | `notices:create`, `notices:read`, `notices:update`, `notices:delete` | /pages/{id}/notices, notice translations |
| Operations | Templates | `incident_templates` | page | `incident_templates:create`, `incident_templates:read`, `incident_templates:update`, `incident_templates:delete` | /pages/{id}/incident-templates |
| Operations | Subscribers | `subscribers` | page | `subscribers:create`, `subscribers:read`, `subscribers:update`, `subscribers:delete` | /pages/{id}/subscribers |
| Content | Mail Templates | `notification_templates` | page | `notification_templates:create`, `notification_templates:read`, `notification_templates:update`, `notification_templates:delete` | /pages/{id}/notification-templates, saving = update, preview = read |
| Content | Languages | `languages` | page | `languages:create`, `languages:read`, `languages:update`, `languages:delete` | PUT /pages/{id}/languages; any page role may read the page languages, the language catalog is reserved for global admins |
| Content | UI Texts | `ui_translations` | page | `ui_translations:create`, `ui_translations:read`, `ui_translations:update`, `ui_translations:delete` | /pages/{id}/ui-overrides; any page role may read, global texts are reserved for global admins |
| Management | Status Pages | `pages` | page | `pages:create`, `pages:read`, `pages:update`, `pages:delete` | PUT/DELETE /admin/pages/{id}, users of the page, webhook token and custom domain = update; only global admins may create a page |
| Management | Users | `users` | global | `users:create`, `users:read`, `users:update`, `users:delete` | /users |
| Management | Roles & Permissions | `roles` | global | `roles:create`, `roles:read`, `roles:update`, `roles:delete` | /roles |
| Management | Tags | `tags` | global | `tags:create`, `tags:read`, `tags:update`, `tags:delete` | /tags |
| Management | Audit Log | `audit` | global | `audit:create`, `audit:read`, `audit:update`, `audit:delete` | GET /admin/audit |
| System | Monitoring | `monitoring` | global | `monitoring:create`, `monitoring:read`, `monitoring:update`, `monitoring:delete` | GET /admin/monitoring |
| System | SSO | `sso` | page | `sso:create`, `sso:read`, `sso:update`, `sso:delete` | /admin/pages/{id}/sso, test login = update |
| System | System Settings | `system_settings` | global | `system_settings:create`, `system_settings:read`, `system_settings:update`, `system_settings:delete` | /settings |

There is also `admin_dashboard`: the "Dashboard" entry in the user menu of the public page.

**Create, update and delete include read:** whoever has `components:update` may also read, without
`components:read` being listed. Update does not include create or delete. This applies to every
resource. Tabs without read permission are hidden, and so are areas without a visible tab; if
someone opens the address of such a page directly, "No permission" appears. The role on the
currently selected status page always counts. Buttons and menu entries appear per action: "New ..."
with create, edit with update, delete with delete.

**Global resources** (Overview, Users, Roles & Permissions, Tags, Audit Log, Monitoring, System
Settings) do not apply to a single page but to the whole platform. They are reserved for global
admins; a page role cannot grant them effectively. The role editor shows them locked in the
matrix. The same applies to creating status pages, the language catalog and the global UI texts.

**Migration from `:write`:** Before this model there was only read and write per area
(`<resource>:write`). At startup a one-time migration replaces `:write` in all roles with create,
read, update and delete and adds the tabs that used to depend on another resource
(`components:write` also grants `external_sources:*`, `incidents:write` also `maintenance:*` and
`incident_templates:*`, `pages:write` also `notification_templates:*`, `languages:*`,
`ui_translations:*` and `sso:*`). Nobody gains or loses permissions. The old values are kept as a
backup in the system settings (`permissions.crud_backup`). Old login tokens containing `:write`
keep working until they expire; the permissions are read fresh from the role on every request
anyway. A remaining `:write` in a role counts as create, read, update and delete when checking, but
is never saved again.

The role editor shows a matrix with one row per tab, grouped by area, and the columns C (create),
R (read), U (update) and D (delete). The "all" switches set a whole row or column; create, update
and delete set read automatically.

## How users get a role

- **Assignment** by a global admin or a user with `pages:create`.
- **Automatic joining:** if the status page has a default role and the account's email domain
  matches the list for automatic joining, the account gets that role. See
  [Status pages](/docs/statusseiten#private-and-automatic-joining).
- **Ordering:** whoever orders a status page becomes its "Page Admin".

## Tags

In addition to the role, tags can narrow access to individual components and incidents, see
[Tags and scope](/docs/komponenten-und-tags#tags-and-scope). Tags on users are always global tags.

## Profile

Under **Profile** (user menu at the top right) every user sets their interface language, links
their account to the SSO of a status page and can delete their account. If you are the only admin
of a page, that page is deleted too after a separate confirmation. The last global admin cannot
delete themselves. Your own data can be downloaded as a JSON file with "Download my data"
(technically `GET /api/me/export`, login required).
