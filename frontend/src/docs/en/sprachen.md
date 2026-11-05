# Languages and translations

Statushafen separates three things: the language of the **interface**, the languages of a status
page's **content**, and **custom UI texts**.

## Interface language

The interface is available in German and English. Pick the language in the language selector at
the top right. Signed-in users store it in their profile, anonymous visitors in the browser.

On a public status page the language selector lists **all languages enabled for that page** (by
their native names), not just German and English. Content appears in the chosen language. If a
language has no interface texts (for example Croatian), labels such as buttons and headings stay in
German; the texts of incidents, components and notices are still translated.

## Languages of a status page

Under **Languages** you define which languages a status page offers content in (permission
`languages:update`):

- At least one language must be active, and one of them is the default language.
- New status pages start with German (default) and English.
- Global admins also maintain the **language catalog**: code, display name and whether the
  language is active. Only catalog languages can be assigned to a page.
  The catalog ships with 30 languages (German, English and many more European languages, shown
  by their native name). Changes or deactivations by the operator are kept. With many languages the
  edit form shows a dropdown instead of tabs.
- The table shows a national flag (where known) before the code and a **Language** column with the
  name in the interface language (e.g. "Bulgarian" or "Bulgarisch"); **Name** is the native name.
- **Format of the code:** 2 to 3 letters, optionally with subtags of 2 to 8 letters or digits
  separated by hyphens (`fr`, `en-us`, `zh-hant-tw`). Uppercase letters become lowercase;
  anything else (`FRA!`, `e`) is rejected. Name and code are required.

## Delete a language from the catalog

Global admins delete a language with **Delete** in the row menu of the catalog (after a
confirmation). The language is also removed from every status page that had enabled it. It only
works if

- the language is the default language of **no** status page (the message names the pages; set a
  different default language there first) and
- no **email templates** in this language exist any more (delete them under email templates first).

Translations of incidents, components and announcements that were already saved are kept but no
longer shown. If you create the same code again later, they are back. A deleted language does not
come back by itself on restart. To merely take it out of the selection, set it to **inactive**
instead.

## Translate content

Incidents (title and message), their updates (message only), components (name and description)
and notices can be translated per language. Each translation starts as a **draft** and has to be **confirmed**; the public API
only returns confirmed translations.

The public status page shows the names and descriptions of components and groups as well as the
title and message of incidents and notices, as well as the messages of an incident's updates, in
the language the visitor chose in the language switcher. If there is no confirmed translation in that language, Statushafen uses the translation
in the default language of the page, otherwise the original text. Drafts and reports from external
sources are never shown translated. The history texts have no translation and appear
in the original.

You translate by hand or with a suggestion: in incidents, maintenance windows and notices, the tab
of an additional language has the button **Translate from {default language}**. It sends the title
and message of the default language to the configured translation service and fills the fields
with the result. The result is a **draft**: the "Translation is done" checkbox is cleared, and
nothing is published until you have checked it. You need the "Update" permission of the respective
tab and a title in the default language (for updates: a message, since an update has no title); at
most 5000 characters per request.

If no service is set up, the service is not reachable or the language is not supported (the default
service only knows the languages the operator has loaded), a message appears below the button and
the form stays fully usable. Global admins set up the service under
[System settings](/docs/systemeinstellungen).

If DeepL is the selected service and fails (quota used up, access rejected, not reachable or language
not supported), the operator's default service (LibreTranslate) translates instead, provided it is set
up and knows the language. A note below the button names the fallback service; if it fails too, the
DeepL error is shown. Title and message go to the service in a single request.

**Translate into all n empty languages** (above the tabs) translates the default language into every
further language that is still empty, one after the other, each as a draft. Languages that already
have text stay untouched. Unsupported languages are skipped and counted in the closing message; any
other error (quota, access, service or connection not reachable, session expired) stops the run and
is shown.

Messages in the admin area (e.g. "saved") stay at the top of the content area and remain visible
while scrolling. Success messages fade out by themselves after a few seconds; errors and warnings
stay until you close them.

The default service (LibreTranslate) supports **23 of the 30** catalog languages: Bulgarian, Czech, Danish, Dutch,
English, Estonian, Finnish, French, German, Greek, Hungarian, Irish, Italian, Latvian, Lithuanian, Polish,
Portuguese, Romanian, Slovak, Slovenian, Spanish, Swedish and Turkish. **Not supported** are Bosnian, Croatian,
Luxembourgish, Macedonian, Maltese, Romansh and Serbian; those can only be translated by hand. (As of version
1.9.6; the production server deliberately loads German and English only.)

## Customize UI texts

Under **UI Texts** you can override individual interface texts, for example a button label:

- **Global** (global admins only): applies across the whole app, including public pages.
- **For one status page** (permission `ui_translations:update`): applies in that page's admin area.

The most specific value wins: page before global before the default text.

The table can be filtered by area and language and searched (by key or text). Changed entries are
marked "customized" or "unsaved"; "Save all" saves all open changes at once. An empty text cannot
be saved: to remove a customization, use "Reset" (with a confirmation; it only affects the chosen
language and scope).
