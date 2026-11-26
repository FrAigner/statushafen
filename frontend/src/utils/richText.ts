// Formatted texts (restricted Markdown) for messages and descriptions.
// The Markdown string is what gets stored; here it is turned into sanitized
// HTML for display (renderRichText) or into plain text for lists and search
// (markdownToPlainText).
//
// Protection in two layers, each sufficient on its own:
//  1. marked gets tokenizers/renderers that NEVER output raw HTML, images,
//     headings, tables and horizontal rules as markup, but as visible, escaped
//     text (legacy data with `#` or `|` at the start of a line stays
//     readable). The renderer only outputs links for https:.
//  2. DOMPurify with a FIXED, tight allowlist (no USE_PROFILES as in
//     docs/index.ts, which is deliberately broad for the repo's own docs).
// marked is pinned exactly (package.json "18.1.1", docs.test.js checks that).
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

export const RICH_TEXT_MAX_LENGTH = 5000

// On pathological input (`*a_` repeated, `*`..`a`..`*`) marked needs
// quadratic time (5,000 characters about 0.4 s, 15,000 characters up to
// 9 s; legacy data may be that long). Above the limit, or when a single
// paragraph (block between blank lines) is too long, the text is shown
// unchanged as escaped plain text (nothing is cut off).
// The same limits apply in the backend (richtext.MaxRenderLen and
// MaxBlockLen, mail): page and mail format the same text the same way.
// Neither is below the allowed 5,000 characters, so allowed texts (even
// a long list without a blank line) are always formatted. The limits
// count characters (Unicode code points) like the backend (runes). The shared
// test reads backend/richtext/testdata/parity.json.
export const RICH_TEXT_MAX_RENDER_LENGTH = 8000
export const RICH_TEXT_MAX_BLOCK_LENGTH = RICH_TEXT_MAX_LENGTH
// Cache per text (otherwise the search re-renders all incidents on every
// keystroke): LRU with an upper bound. Keys are the raw texts; legacy data
// can be several MB long, so only texts up to
// RICH_TEXT_CACHE_MAX_KEY_LENGTH characters are remembered.
// Render budget: each text on its own is bounded, but many expensive texts on
// one page (up to 200 open incidents) add up to seconds of blocking time.
// Formatting a new text costs richTextRenderCost(text): characters plus
// a share of length times the number of formatting characters (marked's
// quadratic run time comes from many open `*`, `_`, `>`, `[`); the
// budget refills at a fixed rate. When it is empty, the text appears unformatted as escaped
// plain text (complete, nothing cut off) and is not remembered, so that
// it gets formatted later. Normal incidents (a few hundred characters) cost
// little: several hundred fit into the budget, but only a few texts with 5,000
// pathological characters (about 0.3 s each).
export const RICH_TEXT_RENDER_BUDGET = 60000
export const RICH_TEXT_RENDER_REFILL_PER_SECOND = 6000
export const RICH_TEXT_CACHE_SIZE = 500
export const RICH_TEXT_CACHE_MAX_KEY_LENGTH = 20000

const ALLOWED_TAGS = ['p', 'br', 'strong', 'em', 'ul', 'ol', 'li', 'a', 'code', 'blockquote']
const ALLOWED_ATTR = ['href', 'start']
// Only https with a host; credentials in the URL (https://nutzer@host) are
// already rejected by isHttpsUrl, this is the second layer for the sanitizer.
const ALLOWED_URI_REGEXP = /^https:\/\/[^\s/@]+(?:[/?#]|$)/i

const escapeHtml = (s: unknown): string => String(s)
  .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;').replace(/'/g, '&#39;')

// Supported syntax is evaluated, everything else is shown as text.
const literal = (raw: unknown): string => `<p>${escapeHtml(String(raw).trim()).replace(/\n/g, '<br>')}</p>`

// Plain text as HTML (only escaped, no formatting) for RichText with `plain`.
export const plainTextToHtml = (text: unknown): string => escapeHtml(text ?? '')

export function isHttpsUrl(href: string): boolean {
  try {
    const u = new URL(href)
    return u.protocol === 'https:' && u.hostname !== '' && u.username === '' && u.password === ''
  } catch {
    return false
  }
}

const marked = new Marked({
  gfm: true,
  breaks: true,
  async: false,
  tokenizer: {
    // These blocks are not part of the subset. undefined switches the
    // tokenizer off (false would mean: default); the lines then stay normal
    // text, and legacy data with `#` or `|` at the start of a line keeps its look and
    // its line breaks. Indented code (spaces at the start of a line) is
    // surprising for legacy data.
    code() { return undefined },
    heading() { return undefined },
    lheading() { return undefined },
    table() { return undefined },
  },
  renderer: {
    html: ({ text, block }) => (block ? literal(text) : escapeHtml(text)),
    hr: ({ raw }) => literal(raw),
    image: ({ raw }) => escapeHtml(raw),
    del({ tokens }) { return `~~${this.parser.parseInline(tokens)}~~` },
    checkbox: ({ checked }) => (checked ? '[x] ' : '[ ] '),
    code: ({ text }) => `<p><code>${escapeHtml(text).replace(/\n/g, '<br>')}</code></p>`,
    link({ href, text, raw, tokens }) {
      const inner = this.parser.parseInline(tokens)
      if (isHttpsUrl(href)) return `<a href="${escapeHtml(href)}">${inner}</a>`
      // No https: never a link. Written-out links show "text (target)",
      // automatically detected addresses (www., mailto) only the text.
      return raw.startsWith('[') ? `${inner} (${escapeHtml(href)})` : escapeHtml(text)
    },
  },
})

const purify = DOMPurify(window)
// Own instance: the hook applies only here and not to the docs page or the
// mail preview, which use DOMPurify.sanitize with their own configuration.
purify.addHook('afterSanitizeAttributes', (node) => {
  // `start` only on <ol> and only as a number (the list starts e.g. at 3).
  if (node.hasAttribute('start') && !(node.tagName === 'OL' && /^\d{1,9}$/.test(node.getAttribute('start') as string))) {
    node.removeAttribute('start')
  }
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer nofollow')
  }
})

const SANITIZE_CONFIG = {
  ALLOWED_TAGS,
  ALLOWED_ATTR,
  ALLOWED_URI_REGEXP,
  // `start` is not a URI: without this, DOMPurify checks the value against ALLOWED_URI_REGEXP.
  ADD_URI_SAFE_ATTR: ['start'],
  ALLOW_DATA_ATTR: false,
  ALLOW_ARIA_ATTR: false,
}

// More than `max` characters (code points)? Stops after max + 1 characters.
const exceeds = (str: string, max: number): boolean => {
  if (str.length <= max) return false
  const it = str[Symbol.iterator]()
  for (let n = 0; n <= max; n++) if (it.next().done) return false
  return true
}

const tooCostly = (src: string): boolean => {
  const text = src.replace(/\r\n?/g, '\n')
  return exceeds(text, RICH_TEXT_MAX_RENDER_LENGTH)
    || text.split(/\n[^\S\n]*\n/).some(block => exceeds(block, RICH_TEXT_MAX_BLOCK_LENGTH))
}

const FORMAT_CHARS = /[*_`>~[\]]/g
export const richTextRenderCost = (text: string): number => text.length + ((text.match(FORMAT_CHARS)?.length ?? 0) * text.length) / 1500

let renderBudget = RICH_TEXT_RENDER_BUDGET
let renderBudgetAt = performance.now()

// Refills the budget according to the elapsed time (at most up to the maximum).
const refillRenderBudget = () => {
  const now = performance.now()
  const elapsed = Math.max(0, now - renderBudgetAt)
  renderBudgetAt = now
  renderBudget = Math.min(RICH_TEXT_RENDER_BUDGET, renderBudget + (elapsed / 1000) * RICH_TEXT_RENDER_REFILL_PER_SECOND)
}

export function resetRichTextBudget(): void {
  renderBudget = RICH_TEXT_RENDER_BUDGET
  renderBudgetAt = performance.now()
}

// Spends the budget for a new text; false if it is not enough.
const spendRenderBudget = (text: string): boolean => {
  refillRenderBudget()
  const cost = richTextRenderCost(text)
  if (cost > renderBudget) return false
  renderBudget -= cost
  return true
}

export type Memoized<R> = ((key: string) => R) & { has: (key: string) => boolean }

export function memoize<R>(fn: (key: string) => R): Memoized<R> {
  const cache = new Map<string, R>()
  const memoized = (key: string): R => {
    if (key.length > RICH_TEXT_CACHE_MAX_KEY_LENGTH) return fn(key)
    if (cache.has(key)) {
      // LRU: a hit moves to the end; the least recently used one is evicted.
      const hit = cache.get(key) as R
      cache.delete(key)
      cache.set(key, hit)
      return hit
    }
    const value = fn(key)
    if (cache.size >= RICH_TEXT_CACHE_SIZE) cache.delete(cache.keys().next().value as string)
    cache.set(key, value)
    return value
  }
  memoized.has = (key: string) => cache.has(key)
  return memoized
}

// Renders Markdown to sanitized HTML. Only for v-html in RichText.vue.
// The result contains no line breaks (paragraphs and <br> separate already),
// so that white-space: pre-wrap in the surrounding layout creates no blank lines.
export const renderRichText = (markdown: unknown): string => {
  const src = String(markdown ?? '')
  // Remembered texts cost nothing; new ones need budget, otherwise plain text (not remembered).
  if (src.trim() === '' || renderCached.has(src)) return renderCached(src)
  return spendRenderBudget(src) ? renderCached(src) : literal(src)
}

const renderCached = memoize((src: string): string => {
  if (src.trim() === '') return ''
  // Fail-closed: where DOMPurify finds no DOM (isSupported false), sanitize()
  // returns its input unchanged; plain text is preferable then.
  if (!purify.isSupported || tooCostly(src)) return literal(src)
  // Deeply nested quotes (`>>>>...`) make marked fail with a RangeError (call stack);
  // an error in rendering would otherwise empty the whole public page.
  // The text as escaped plain text is preferable then.
  try {
    // async: false above: parse returns a string.
    return purify.sanitize(marked.parse(src) as string, SANITIZE_CONFIG).replace(/\n/g, '')
  } catch {
    return literal(src)
  }
})

// Plain text for lists and search: without Markdown characters, lists as "- " or
// "1. ", links as "text (address)" (like the backend, richtext.ToPlainText).
export const markdownToPlainText = (markdown: unknown): string => plainCached(String(markdown ?? ''))

const plainCached = memoize((src: string): string => {
  const html = renderRichText(src)
  if (html === '') return ''
  const body = new DOMParser().parseFromString(html, 'text/html').body
  return blocksToText(body).trim()
})

function inlineToText(node: Node): string {
  let out = ''
  for (const child of node.childNodes) {
    if (child.nodeType === 3) {
      out += child.textContent
    } else if (child.nodeName === 'BR') {
      out += '\n'
    } else if (child.nodeName === 'A') {
      const text = inlineToText(child)
      const href = (child as Element).getAttribute('href')
      out += text === href ? href : `${text} (${href})`
    } else {
      out += (child.nodeName === 'P' && out !== '' ? '\n' : '') + inlineToText(child)
    }
  }
  return out
}

function blocksToText(root: Node): string {
  const parts: string[] = []
  // childNodes instead of children: with an incomplete DOM (happy-dom in tests)
  // the <p> are missing, and text nodes then sit directly in the body.
  for (const el of root.childNodes) {
    if (el.nodeType === 3) {
      parts.push((el.textContent as string).trim())
    } else if (el.nodeName === 'UL' || el.nodeName === 'OL') {
      const list = el as Element
      const first = Number(list.getAttribute('start') ?? 1) // the sanitizer only lets digits through
      const lines = [...list.children].map((li, i) =>
        `${el.nodeName === 'OL' ? `${first + i}. ` : '- '}${inlineToText(li).trim().replace(/\n/g, '\n  ')}`)
      parts.push(lines.join('\n'))
    } else if (el.nodeName === 'BLOCKQUOTE') {
      parts.push(`> ${inlineToText(el).trim().replace(/\n/g, '\n> ')}`)
    } else {
      parts.push(inlineToText(el).trim())
    }
  }
  return parts.filter(part => part !== '').join('\n\n')
}
