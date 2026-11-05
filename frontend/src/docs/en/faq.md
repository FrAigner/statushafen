# FAQ

## A menu entry is missing in the admin area.

Many menu entries depend on permissions or are only visible to global admins. The available
permissions are listed under [Users, roles and permissions](/docs/benutzer-und-rollen). Also check
that the right status page is selected as "Tenant" at the top of the header.

## After logging in I see an empty page.

Usually the session has expired. Log out once and log in again.

## I do not receive emails for my subscription.

- Is the subscription confirmed? New email subscriptions need the click in the confirmation link
  (valid for 24 hours), see [Subscriptions](/docs/abonnements#double-opt-in-for-email).
- Does the incident affect one of the subscribed components?
- Is email delivery enabled in the [system settings](/docs/systemeinstellungen)?

## How do I end a subscription?

Via the unsubscribe link in every notification and then "Unsubscribe now". Signed-in users can
also manage their subscription via the user menu.

## Why does a component show a worse status than "Operational" although I changed nothing?

An open incident shows its components at least with the status of its severity ("Degraded
Performance", "Partial Outage" or "Major Outage"; the worst one if there are several). Once the incident is
resolved, the stored status applies again. If the component is linked to an
[external source](/docs/externe-quellen), that source's status counts as well: the worst value
wins.

## "External source unreachable" appears at the top of the admin area.

An external source has not delivered a fetch for longer than the configured limit. Its status does
not count in the meantime. The page **External sources** shows the last error; "Fetch now" checks
the connection, see [Outage of a source](/docs/externe-quellen#outage-of-a-source).

## My outage is missing in the 90-day availability.

The availability is based on incidents, maintenance windows and mirrored reports of external
sources, weighted by severity. A status that was only set via webhook or by hand does not count. See
[The public status page](/docs/oeffentliche-statusseite#availability-over-the-last-90-days).

## I cannot delete a group.

A group can only be deleted when it is empty. First move or delete its components, see
[Groups](/docs/komponenten-und-tags#groups).

## My slug is rejected.

Allowed are lowercase letters, digits and single hyphens; names such as `www`, `login` or `system`
are reserved, see [Status pages](/docs/statusseiten#rules-for-the-slug).

## After maintenance a component shows "Operational" although it is broken.

At the end of a maintenance window Statushafen automatically sets the affected components to
"Operational". Correct the status afterwards by hand or via the
[webhook](/docs/api-und-webhooks) if needed.

## SSO sign-in reports `not_linked`.

The account is not linked to the SSO yet. Sign in normally once and choose "Link with SSO" in
your profile, see [SSO](/docs/sso).

## The monitoring webhook answers with 401.

The token is missing or no longer valid, for example because it was regenerated. Create a new
token and enter it in your monitoring, see [API and webhooks](/docs/api-und-webhooks).

## How do I open the menu on a phone?

In the admin area via the menu icon on the left of the header, in this documentation via "Show sections".
