# Subscriptions

Visitors can be notified about changes to a status page, by email or via a Google Chat webhook.

## Subscribe

On the status page, "Subscribe now" opens the subscription wizard:

1. Pick a channel: **Email** or **Google Chat** (webhook URL of the chat space, starting with
   `https://`).
2. Pick the components you want to hear about. With no selection, "No restriction: all
   components" applies.
3. Submit.

![First step of the subscription wizard: choosing the channel Email or Google Chat, with the "Cancel" and "Next" buttons](/docs-img/en/subscribe-dialog.webp)


You are notified as soon as an incident or maintenance window affects at least one of the
subscribed components. This includes mirrored reports of
[external sources](/docs/externe-quellen#mirrored-reports) that affect a linked component. Private status pages can only be subscribed to by users with access to
the page.

## Double opt-in for email

A new email subscription starts out **unconfirmed**. Statushafen sends a confirmation email with
a link to `/subscribe/confirm`. Only clicking the button on that page activates the
subscription.

- The link is valid for 24 hours. If it has expired, the unconfirmed subscription is deleted and
  you simply subscribe again.
- Unconfirmed subscriptions receive no notifications. In the admin area they carry the
  "Unconfirmed" badge.
- No confirmation is needed for Google Chat subscriptions and for signed-in users who enter their
  own account address.
- The response when subscribing is the same for new and already subscribed addresses, so nobody
  can query who is subscribed. Only a user signed in with their own account address is told
  "already subscribed".
- Spelling variants of the same mailbox (upper/lower case, `name+tag@...`) count as one address.
  At most three confirmation emails per mailbox are sent in 24 hours, across all pages.

When subscribing, the visitor's language is stored as the email language, provided the page has enabled it (see [mail templates](/docs/mail-templates)).

## Manage a subscription

Signed-in users open their subscription via the user menu ("Manage subscription") and change the
components there or delete the subscription. With no selection, "all components" applies again.

If the page offers more than one language, **Notification language** is shown as well: "page
default" or one of the page's languages. It applies to emails and Google Chat messages.

- If the chosen language is no longer active on the page, messages are sent in the page's default
  language; the choice stays stored until the next save.
- The form shows a language that is no longer active as "page default", and saving then stores
  "page default".
- An explicitly chosen default language is also stored as "page default" and therefore follows a later change of the page's
  default language.

## Unsubscribe

Every email to subscribers contains a personal link to `/unsubscribe?token=…`. It opens a
confirmation page; only clicking "Unsubscribe now" ends the subscription. This way link scanners
of mail gateways cannot end a subscription by accident.

Mail clients with one-click unsubscribe according to RFC 8058 also show their own unsubscribe
button. Statushafen sets the `List-Unsubscribe` and `List-Unsubscribe-Post` headers for this.

## Google Chat webhook

For Google Chat you enter the webhook URL of a chat space. Statushafen posts a text message there
with component, tags, incident, status and details, in German or English depending on the
subscriber's language (any other language gets English). Notices are only sent to email
subscriptions.

### Webhook URL check

The URL is checked when saved:

- **Public subscribing:** only Google Chat webhooks are accepted
  (`https://chat.googleapis.com/v1/spaces/...`, port 443, no credentials); any other address is
  rejected with the same message.
- **Creating in the admin area:** admins may also enter other `https` addresses whose host
  resolves and does not point to an internal address.

### Rate limiting and retries

Google limits chat webhooks (about one message per second per space, plus a per-project limit) and
answers with "too many requests" (HTTP 429) or, under load, 503. Statushafen handles this as
follows:

- At most one message per second to the same URL (short bursts of up to two messages).
- Google's `Retry-After` hint is honoured.
- A notification is retried at most twice, waiting at most 30 seconds in total.
- Errors caused by a wrong URL or key (400, 401, 403, 404) are not retried.
- The server log only contains the status code, the error class and a short identifier of the
  URL, never the URL itself.

**Send test** never waits:

- If Google is throttling, you see "Google is rate limiting right now - please try again in n
  seconds".
- If the target rejects the message, check the URL and key.
- If it is unreachable, try again later.

There are no outgoing webhooks to arbitrary systems of your own. For incoming status reports from
monitoring, see [API and webhooks](/docs/api-und-webhooks).

## Subscribers in the admin area

**Subscribers** lists all subscriptions of the page with channel, contact, components and creator.

- **Permissions:** reading requires `subscribers:read`; creating, changing and deleting require
  `subscribers:create`, `subscribers:update` and `subscribers:delete`.
- **Language:** if the page offers several languages, you also set the **notification language**
  per subscriber there (empty: the page's default language).
- **Creator:** the creating account's email address only appears if that account is itself staff
  of this page (or you created it yourself). If a foreign account subscribed to the page, the list
  only shows "linked to an account" and no further account data.

### Test message from the form

In the **Add subscriber** form, and when editing, **Send test message** immediately sends a short
message to the entered target – the email address or the Google Chat webhook – even before you
save. The result appears right below the button.

- Nothing is saved and no subscription is created.
- The message uses the language currently selected at the top of the admin area (German or
  English), not the subscriber's notification language.
- The button stays disabled until a valid email address or an https URL is entered; the server
  checks the target exactly like when creating a subscriber (no internal addresses).
- It requires `subscribers:create` or `subscribers:update`; at most 5 test messages per minute are
  possible per sender.

Possible errors:

- The webhook target rejects the message or is unreachable.
- The email could not be sent.
- Sending emails is turned off in the system settings.
