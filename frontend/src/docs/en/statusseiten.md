# Status pages and tenants

Each status page is its own tenant: components, incidents, subscribers, templates and roles
always belong to exactly one page. Only global admins may create and list status pages (menu
**Status Pages**, button "+ New status page"). In the interface, the **Status Pages** menu with
all settings of this section is open to global admins only. The `pages:create`, `pages:update` and `pages:delete` permissions (for
example in the "Page Owner" role, see [Users, roles and permissions](/docs/benutzer-und-rollen))
allows the same changes to your own page through the API; in the interface it opens SSO, mail
templates, languages and UI texts.

## Address of a status page

A status page can be reached in several ways:

| Way | Example | Note |
|---|---|---|
| Subdomain | `acme.example.com` | The first label is the slug. |
| Path | `/status/acme` | Always works, regardless of the host. |
| Custom domain | `status.acme.example` | Must be verified via DNS, see below. |

Opening the main address without a slug shows the Statushafen home page.

### Rules for the slug

- Allowed are lowercase letters, digits and single hyphens, not at the start or end (`acme`,
  `acme-cloud`).
- If left empty, Statushafen derives the slug from the name.
- A slug is unique per page. The slug of a deleted page becomes available again.
- **Reserved names** are not allowed because they are already taken as a subdomain or path:
  `www`, `staging`, `api`, `admin`, `app`, `mail`, `login`, `docs`, `status`, `order`, `auth`,
  `subscribe`, `unsubscribe`, `profile`, `healthz`, `static`, `assets`, `fonts` and `system`,
  as well as the typical mailbox names `support`, `security`, `billing`, `postmaster`, `abuse`,
  `noreply`, `no-reply`, `info`, `root`, `hostmaster`, `webmaster` and `edge`.
  The page with the slug `system` is the status page of Statushafen itself: its slug cannot be
  changed and it cannot be deleted.

## Page settings

| Field | Effect | Example |
|---|---|---|
| Status page name | Title of the page, required | `Acme Cloud` |
| Slug (URL identifier) | Part of the address, see above | `acme` |
| Description | Short text about the page | `Official status page of Acme` |
| Logo URL (optional) | Logo in the page header | `https://acme.example/logo.svg` |
| Primary color / Secondary color (optional) | Brand colors of the public page as a hex value such as `#4c6ef5`; empty = default colors | |
| Header design | Style of the page header: Classic (default), Centered or Minimal | |
| Page layout | "Standard" or "List" (overall status, components with 90-day bars, incident timeline) | |
| Reply-to address for notifications (optional) | Reply-To of this page's emails. The sender address is always the server's, the page name is the sender name; empty = no Reply-To | `"Acme Status" <status@acme.example>` |
| Private status page (login required) | Only signed-in users with access see the page; it does not appear on the home page | on/off |
| Default access for all logged-in users | Role for automatic joining, see below | `Viewer` |
| E-mail domains for automatic join | Domains whose accounts join the page automatically | `acme.example` |
| Monitoring webhook | Token for automatic status reports, see [API and webhooks](/docs/api-und-webhooks) | |

The list under **Status Pages** can be searched by name, slug, description and domain. The row
menu offers "Open" (public page) and "Users" (roles on this page). When you delete a page,
Statushafen removes all its data.

### Private and automatic joining

A **private** page is only visible after login, and only to users with a role on the page. Users
get a role by assignment (row menu "Users") or by **automatic joining**. That only applies if both
are set:

- a default role ("Default access for all logged-in users") and
- the account's email domain is in the list "E-mail domains for automatic join".

The domains are comma-separated and must match exactly; subdomains do not count (`acme.example`
does not include `mail.acme.example`). If either field is empty, only explicitly assigned users
get access. The default role must not contain permissions to create, update or delete (`…:create`, `…:update`, `…:delete`) or access to
subscribers; Statushafen checks this when saving and again when someone joins. Someone an admin
has removed from the page does not rejoin automatically.

Without permission (not signed in, no role, not entitled to join) a private page looks no different
from a page that does not exist: all requests answer with 404, and the interface shows "Status page
unavailable" with a link to the login. Joining happens as before on the first request after signing in.

## Custom domain

Enter the desired domain under "Custom Domain", for example `status.acme.example`. Two DNS
records are needed afterwards:

1. A **CNAME** of the domain pointing to the displayed target host (default
   `edge.example.com`).
2. As proof of ownership, a **TXT record** of the form `statushafen-verify=<token>`. The form
   shows the exact value after saving.

Then click "Verify DNS". The state of the domain is shown next to it: "Pending", "Verified"
(waiting for the first request via the domain) and "Active". "Remove domain" undoes the
assignment. Verification is limited to 10 checks per minute per page and per user; beyond that the
interface reports that too many requests were made.

Only valid hostnames in lowercase are accepted (no trailing dot, no wildcard, no IP address;
international names in Punycode). Entering a domain does not reserve it: several pages can enter
the same domain, and the page that **verifies it first via the TXT record** wins. If the domain is
already verified or active on another page, verifying fails with the message "Domain is already in
use".

## Users of a page

In the row menu "Users" you assign users a role on the page or withdraw it (global admins only in
the interface; the API requires `pages:create` or `pages:delete`). "Global Admin" is not a page role; set global
admins under **Users** with the "Global Admin" switch.
Global admins pick the account from the list and assign it directly. Everyone else (for example a
Page Owner, via the API) **invites by email address**: Statushafen creates an invitation and sends
the account with that address a link. The account only becomes a member once its owner opens the
link, signs in with exactly that address and chooses "Accept"; "Decline" deletes the invitation. The
response when inviting is the same for known and unknown addresses, and the "Open invitations" list
shows every address entered; no email is sent without an account. The link is valid for 7 days and
can be used once; only a hash is stored, and only the account that existed when the invitation was
sent can accept it. Inviting the same address again replaces the old link after 30 minutes at the
earliest (before that nothing happens and no new email is sent), and only if your own role covers the
role of the open invitation. Open invitations can be revoked; a page has at most 100 open and 200 new
invitations per day. Statushafen deletes expired invitations automatically, and deleting an account
also deletes all invitations to and from it; the data export contains the open invitations to your
own address. If the inviter loses the right
to assign the role, the invitation lapses. Accounts with the old global SSO, a page SSO link
(OIDC/SAML) or a role on another page, and global admin accounts, get no email and cannot accept an
invitation themselves; only a global admin adds them (a second page would otherwise block the SSO
sign-in of the first). Inviting is limited to 20 requests per minute, as are accepting and
declining. Existing members of the page can also be changed without an email. A
role can only be assigned if it has no more rights than your own role on this page; a member with a
stronger role cannot be changed or removed. The member list shows only email and role.
Each account has exactly one role per page; adding it again changes the existing member's role. Anyone who is not a global admin cannot remove themselves or downgrade to a role without `pages:update` if they are the last member with that right on the page (error "last user with management rights"); a global admin can always repair this. When a member is removed, Statushafen also deletes that account's page SSO link for this page, so the account can later sign in or be added elsewhere. The same applies when a role is deleted or the page is taken away while editing the account.

## Switch tenant

If you have access to several status pages, pick the active page at the top of the header in the
"Tenant" field. All admin views then refer to that page. In the user menu, the role you have on
that page is shown below your email address.
