# System settings

Under **System Settings** global admins set switches for the whole platform. A switch that has
never been set counts as enabled (exception: self-monitoring is then off). Every change is recorded in the [audit log](/docs/audit-log)
(`system_setting.updated`).

## Self-service ordering enabled

Controls whether new status pages can be ordered via `/order`, see
[Getting started](/docs/erste-schritte).

- **On:** the home page shows "Order your own status page".
- **Off:** the button disappears, the form reports that ordering is currently unavailable and new
  orders are rejected.

## Email delivery enabled

Controls whether Statushafen sends any email at all.

- **Off:** no email goes out, neither order and subscription confirmations nor notifications
  about incidents, maintenance and notices. Every skipped email is recorded in the audit log as
  `email.skipped_disabled` with its recipient.
- Google Chat webhooks keep working.

The switch is useful, for example, when the mail provider's sending quota is used up or a test
instance should not send email.

## Self-monitoring

Turns the automatic self-monitoring of the system page on or off (see
[Self-monitoring](/docs/selbstueberwachung)). Unlike the other switches it is **off** without an entry and only runs once explicitly switched on.

- **On:** Statushafen checks API, database, frontend, email delivery and backup and sets the
  status of the components on the system page.
- **Off:** No checks are evaluated; failures that are already set remain.

## Automatic translation

Sets which service handles the **Translate** button in the forms (see
[Languages](/docs/sprachen)). The suggestion always lands as a draft.

- **Operator default (LibreTranslate):** The operator provides the service (environment variable
  `LIBRETRANSLATE_URL`). The texts do not leave the operator's infrastructure. Only the languages
  loaded there are available; for others the button reports "language not supported".
- **Own LibreTranslate server:** address (`https://` only, port 443, publicly reachable; internal
  addresses are rejected) and an optional API key.
- **DeepL (own key):** The texts are sent to DeepL for translation, mind data protection. A free
  personal key includes about 500,000 characters per month; keys for the free plan end in `:fx`.

The key is only stored and never shown or returned again: the field stays empty, "Remove the stored
key" deletes it. The audit log only says "set" or "removed". **Test connection** translates a sample
text with the saved configuration.

## Overview and monitoring

Two more pages are open to global admins only:

- **Overview** (Operations area) is the tenant overview: key figures on active status pages, open
  incidents, subscribers and overall status, plus a searchable table of all pages with status,
  components, subscribers and open incidents.
- **Monitoring** (System area) shows the server's operating figures from the built-in measurement:
  requests, error rate (only responses with status 5xx; 4xx such as 404 or 409 are not counted; marked yellow from 5 %, red from 10 %), latency (mean, p50, p95, maximum), requests per minute and process uptime,
  plus saturation (goroutines, heap, GC cycles, database pool) and a table per endpoint. A link
  opens the operator's Grafana dashboard.
