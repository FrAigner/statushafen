# What is Statushafen

Statushafen is a platform for status pages. Companies use it to show whether their services are
running, report outages, announce maintenance and notify interested people by email or Google
Chat.

## Building blocks

- **Status pages:** Each status page is its own tenant with its own address, for example
  `acme.example.com` or `/status/acme`. See [Status pages and tenants](/docs/statusseiten).
- **Components:** the services whose state the page shows, such as "Website" or "API".
  See [Components and tags](/docs/komponenten-und-tags).
- **External sources:** status pages of other providers (for example a cloud provider) whose
  state also drives your own components. See [External sources](/docs/externe-quellen).
- **Incidents:** outages with a status history and updates. See [Incidents](/docs/vorfaelle).
- **Maintenance windows:** planned work that starts and ends automatically.
  See [Maintenance windows](/docs/wartungsfenster).
- **Announcements:** general notices on the status page.
  See [Announcements](/docs/ankuendigungen).
- **Subscriptions:** notifications by email (with double opt-in) or Google Chat webhook.
  See [Subscriptions](/docs/abonnements).

## Public page and admin area

The **public status page** shows the overall status, all components, the availability over the
last 90 days, running and upcoming maintenance, and past incidents. If a page is marked as
private, only signed-in users with access can see it. What visitors see and how they search and
filter is described in [The public status page](/docs/oeffentliche-statusseite).

The **admin area** lives under `/admin`. That is where you manage components, external sources,
incidents, maintenance, announcements, templates and subscribers. What a user sees depends on
their role, see [Users, roles and permissions](/docs/benutzer-und-rollen). Layout and operation
of the admin area are described in [Getting started](/docs/erste-schritte#the-admin-area).

## Home page

The Statushafen home page lists all pages that are public and not private under "Current status
pages". If self-service ordering is enabled, it also shows the "Order your own status page"
button, see [Getting started](/docs/erste-schritte).
