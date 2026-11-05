# Formatted text

Messages and descriptions can be formatted: bold, italic, lists, paragraphs and links. This applies
to incidents and their updates, maintenance windows, notices and the templates for incidents and
maintenance (always the "Message / Details" field, not the title). The formatting is stored and
written as restricted Markdown. The form field has a toolbar, so you do not have to type the
characters yourself (see [Editor](#editor)).

## Editor

Above the field are buttons for bold, italic, bulleted list, quote, code and link (https only).
They add the Markdown characters around the selected text or at the cursor; a second click on bold,
italic or code removes them again. Ctrl+B (Cmd+B on Mac) and Ctrl+I set bold and italic from the
keyboard. After "Link" the address `https://` is selected, so you can type over it.

"Preview" shows the text exactly as the status page will show it, with the same filtering: raw
HTML, images and headings appear as plain text there too. Click "Edit" to go back. Below the field a
counter shows the number of characters against the limit of 5,000; if the text is longer, the
counter turns red and saving is rejected.

## Syntax

| Input | Result |
|---|---|
| `**bold**` | **bold** |
| `*italic*` or `_italic_` | *italic* |
| `- Item` (one line per item) | bullet list |
| `1. Step` (one line per step) | numbered list |
| `> Quote` | indented note |
| `` `code` `` | monospaced text |
| `[Text](https://example.com)` or a bare `https://…` address | link |
| blank line | new paragraph |

A single line break stays a line break.

```text
We are investigating **login errors**.

Affected:
- Login
- Password reset

Details: [Status page](https://status.example.com)
```

## What is not rendered

Headings, images, tables and embedded HTML are not part of it. Such syntax appears unchanged as
text; HTML is never executed. Links work only with `https:`; an `http:`, `javascript:` or `data:`
address does not become a link but is shown as text. Links open in a new tab and carry
`rel="noopener noreferrer nofollow"`.

## Existing texts

Texts written without formatting so far stay readable: they are not changed, and single line
breaks are kept. Characters such as `*` or `_` in the middle of a text or a `#` at the start of a
line still appear as normal text.

## Where the formatting applies

- **Status page** (all layouts): shown formatted, also in the history of an incident.
- **Email**: default emails show the formatted version as HTML. The subject stays plain text.
- **Custom mail templates**: `{{.MessageHTML}}` provides the formatted version, `{{.Message}}`
  still provides the unformatted text, see [Placeholders](/docs/mail-templates#placeholders).
- **Chat and webhook (Google Chat)**: plain text, links as "text (address)", lists with "- ".
- Reports from [external sources](/docs/externe-quellen) stay plain text.

## Length

A message may be at most 5,000 characters long, counting the written text including formatting
characters. Statushafen rejects longer texts with an error message when saving.

Older, longer texts (over 8,000 characters) or single paragraphs over 2,000 characters without a blank
line are shown unchanged as text without formatting, so that the page stays fast.

Formatting many texts on one page also has a time budget: very long texts or texts overloaded with
formatting characters use more of it than short reports. Once the budget is used up, further such texts
appear without formatting for a while (complete, nothing is cut off); after a short time they are
formatted again. Ordinary incident reports are not affected.
