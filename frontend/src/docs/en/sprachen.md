# Languages and translations

Statushafen separates three things: the language of the **interface**, the languages of a status
page's **content**, and **custom UI texts**.

## German and English only, second language only with the switch

Content (incidents, maintenance, notices, components, templates and notifications) is available in
**German and English**. Whether a status page offers the second language next to its default
language depends on the switch **Automatic translation** in the
[System settings](/docs/systemeinstellungen) (global admins only). It is **off by default**.

- **Off:** Every status page has its default language only.
  - There are no language tabs in the forms, no translation pages and no "Translate" button; you
    cannot maintain a second language by hand either.
  - The public status page shows all content in the default language, and mails to subscribers and
    internal users go out in the default language as well.
  - The language selector at the top then only switches the interface.
- **On:** German and English as described below, including translation suggestions when a service
  is set up.

Translations already stored are not lost when you switch it off: they reappear as soon as the
switch is on.

**After upgrading:**

- Installations that used translations before start with the switch off. A global admin has to
  switch automatic translation on once in the system settings to show the second language and the
  translations again.
- Languages other than German and English from the former catalog are switched off at startup and
  no longer offered anywhere; their translations stay in the database.
- A status page that had such a language as its default language now uses German.

## Interface language

The interface is available in German and English. Pick the language in the language selector at
the top right. Signed-in users store it in their profile, anonymous visitors in the browser.

If a public status page offers both languages, the language selector also picks the language of the
content. If it has its default language only (for example with automatic translation switched off),
the selector only switches the interface; the content stays in the default language.

## Languages of a status page

Under **Languages** you define which language a status page writes its content in (permission
`languages:update`):

- With automatic translation switched off you only choose the **default language** (German or
  English). A second language enabled earlier stays stored.
- With automatic translation switched on you enable German, English or both; one of them is the
  default language.
- New status pages start with German (default) and English.
- Global admins also see the **language catalog** with German and English: display name and whether
  the language is active. Other languages cannot be added or switched on (message "Content is
  available in German and English only").
- The table shows a national flag before the code and a **Language** column with the name in the
  interface language; **Name** is the native name.

## Delete a language from the catalog

Global admins delete a language with **Delete** in the row menu of the catalog (after a
confirmation). The language is also removed from every status page that had enabled it. It only
works if

- the language is the default language of **no** status page (the message names the pages; set a
  different default language there first) and
- no **email templates** in this language exist any more (delete them under email templates first).

After deleting:

- Translations of incidents, components and announcements that were already saved are kept but
  no longer shown. If you create the same code again later, they are back.
- A deleted language does not come back by itself on restart.
- To merely take it out of the selection, set it to **inactive** instead.

## Translate content

Only with automatic translation switched on: incidents (title and message), their updates (message
only), components (name and description) and notices can be translated into the second language. Each translation starts as a **draft** and has to be **confirmed**; the public API
only returns confirmed translations.
Translations exist only in an additional language the page offers; Statushafen refuses the
default language or a language the page does not offer with a message.

### Display on the status page

The public status page shows the names and descriptions of components and groups as well as the
title and message of incidents and notices, as well as the messages of an incident's updates, in
the language the visitor chose in the language switcher.

- **Missing translation:** If there is no confirmed translation in that language, Statushafen uses
  the translation in the default language of the page, otherwise the original text.
- **Never translated:** Drafts and reports from external sources are never shown translated.
- **Status history:** The history texts have no translation and appear in the original.

### Translation suggestion

You translate by hand or with a suggestion: in incidents, maintenance windows and notices, the tab
of an additional language has the button **Translate from {default language}**. It sends the title
and message of the default language to the configured translation service and fills the fields
with the result.

- **Draft:** The result is a **draft**: the "Translation is done" checkbox is cleared, and nothing
  is published until you have checked it.
- **Requirements:** You need the "Update" permission of the respective tab and a title in the
  default language (for updates: a message, since an update has no title).
- **Limit:** at most 5000 characters per request.
- **No service:** If no service is set up, the service is not reachable or the language is not
  supported (the default service only knows the languages the operator has loaded), a message
  appears below the button and the form stays fully usable. Global admins set up the service under
  [System settings](/docs/systemeinstellungen).
- **Fallback for DeepL:** If DeepL is the selected service and fails (quota used up, access
  rejected, not reachable or language not supported), the operator's default service
  (LibreTranslate) translates instead, provided it is set up and knows the language. A note below
  the button names the fallback service; if it fails too, the DeepL error is shown.
- Title and message go to the service in a single request.

**Translate into all n empty languages** (above the tabs) translates the default language into
every further language that is still empty, one after the other, each as a draft.

- Languages that already have text stay untouched.
- Unsupported languages are skipped and counted in the closing message.
- Any other error (quota, access, service or connection not reachable, session expired) stops the
  run and is shown.

Messages in the admin area (e.g. "saved") stay at the top of the content area and remain visible
while scrolling. Success messages fade out by themselves after a few seconds; errors and warnings
stay until you close them.

The default service (LibreTranslate) supports German and English.

## Customize UI texts

Under **UI Texts** you can override individual interface texts, for example a button label:

- **Global** (global admins only): applies across the whole app, including public pages.
- **For one status page** (permission `ui_translations:update`): applies in that page's admin area.

The most specific value wins: page before global before the default text.

- **Search:** The table can be filtered by area and language and searched (by key or text).
- **Saving:** Changed entries are marked "customized" or "unsaved"; "Save all" saves all open
  changes at once.
- **Reset:** An empty text cannot be saved: to remove a customization, use "Reset" (with a
  confirmation; it only affects the chosen language and scope).
