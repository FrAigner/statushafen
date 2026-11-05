// Formatierte Texte (eingeschränktes Markdown) für Nachrichten und
// Beschreibungen. Gespeichert wird der Markdown-String; hier wird er für die
// Anzeige in bereinigtes HTML gewandelt (renderRichText) bzw. in Klartext für
// Listen und Suche (markdownToPlainText).
//
// Schutz in zwei Schichten, jede für sich ausreichend:
//  1. marked bekommt Tokenizer/Renderer, die Roh-HTML, Bilder, Überschriften,
//     Tabellen und Trennlinien NIE als Markup ausgeben, sondern als sichtbaren,
//     maskierten Text (Altdaten mit `#` oder `|` am Zeilenanfang bleiben
//     lesbar). Links gibt der Renderer nur für https: aus.
//  2. DOMPurify mit FESTER, enger Whitelist (kein USE_PROFILES wie in
//     docs/index.js, das für Repo-eigene Doku bewusst weit ist).
// marked ist exakt gepinnt (package.json "18.1.1", docs.test.js prüft das).
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

export const RICH_TEXT_MAX_LENGTH = 5000

// marked braucht bei pathologischen Eingaben (`*a_` wiederholt,
// `*`..`a`..`*`) quadratische Zeit (5.000 Zeichen rund 0,4 s, 15.000 Zeichen
// bis 9 s; Altdaten dürfen so lang sein). Darüber, bzw. wenn ein einzelner
// Absatz (Block zwischen Leerzeilen) zu lang ist, steht der Text unverändert
// als maskierter Klartext da (nichts wird abgeschnitten).
// Dieselben Grenzen gelten im Backend (richtext.MaxRenderLen und
// MaxBlockLen, Mail): Seite und Mail formatieren denselben Text gleich.
// Beide liegen nicht unter den erlaubten 5.000 Zeichen, erlaubte Texte (auch
// eine lange Liste ohne Leerzeile) werden also immer formatiert. Die Grenzen
// zählen Zeichen (Unicode-Codepoints) wie das Backend (Runen). Der gemeinsame
// Test liest backend/richtext/testdata/parity.json.
export const RICH_TEXT_MAX_RENDER_LENGTH = 8000
export const RICH_TEXT_MAX_BLOCK_LENGTH = RICH_TEXT_MAX_LENGTH
// Zwischenspeicher je Text (Suche rendert sonst bei jedem Tastendruck alle
// Vorfälle neu): LRU mit Obergrenze. Schlüssel sind die Rohtexte; Altdaten
// können mehrere MB lang sein, deshalb werden nur Texte bis
// RICH_TEXT_CACHE_MAX_KEY_LENGTH Zeichen gemerkt.
// Render-Budget: Jeder Text für sich ist begrenzt, doch viele teure Texte auf
// einer Seite (bis zu 200 offene Vorfälle) summieren sich zu Sekunden
// Blockierzeit. Neues Formatieren kostet richTextRenderCost(Text): Zeichen plus
// ein Anteil aus Länge mal Zahl der Formatierzeichen (die quadratische
// Laufzeit von marked entsteht aus vielen offenen `*`, `_`, `>`, `[`); das
// Budget füllt sich mit fester Rate wieder auf. Ist es leer, erscheint der Text unformatiert als maskierter
// Klartext (vollständig, nichts abgeschnitten) und wird nicht gemerkt, sodass
// er später formatiert wird. Normale Vorfälle (einige hundert Zeichen) kosten
// wenig: mehrere hundert passen ins Budget, aber nur wenige Texte mit 5.000
// pathologischen Zeichen (je ca. 0,3 s).
export const RICH_TEXT_RENDER_BUDGET = 60000
export const RICH_TEXT_RENDER_REFILL_PER_SECOND = 6000
export const RICH_TEXT_CACHE_SIZE = 500
export const RICH_TEXT_CACHE_MAX_KEY_LENGTH = 20000

const ALLOWED_TAGS = ['p', 'br', 'strong', 'em', 'ul', 'ol', 'li', 'a', 'code', 'blockquote']
const ALLOWED_ATTR = ['href', 'start']
// Nur https mit Host; Zugangsdaten in der URL (https://nutzer@host) lehnt
// isHttpsUrl schon vorher ab, hier die zweite Schicht für den Sanitizer.
const ALLOWED_URI_REGEXP = /^https:\/\/[^\s/@]+(?:[/?#]|$)/i

const escapeHtml = (s) => String(s)
  .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;').replace(/'/g, '&#39;')

// Unterstützte Syntax wird ausgewertet, alles andere steht als Text da.
const literal = (raw) => `<p>${escapeHtml(String(raw).trim()).replace(/\n/g, '<br>')}</p>`

// Klartext als HTML (nur maskiert, keine Formatierung) für RichText mit `plain`.
export const plainTextToHtml = (text) => escapeHtml(text ?? '')

export function isHttpsUrl(href) {
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
    // Diese Blöcke gehören nicht zur Teilmenge. undefined schaltet den
    // Tokenizer ab (false hieße: Standard); die Zeilen bleiben dann normaler
    // Text, Altdaten mit `#` oder `|` am Zeilenanfang behalten ihr Aussehen und
    // ihre Zeilenumbrüche. Eingerückter Code (Leerzeichen am Zeilenanfang) ist
    // für Altdaten überraschend.
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
      // Kein https: nie ein Link. Ausgeschriebene Links zeigen "Text (Ziel)",
      // automatisch erkannte Adressen (www., mailto) nur den Text.
      return raw.startsWith('[') ? `${inner} (${escapeHtml(href)})` : escapeHtml(text)
    },
  },
})

const purify = DOMPurify(window)
// Eigene Instanz: Der Hook gilt nur hier und nicht für die Doku-Seite oder die
// Mail-Vorschau, die DOMPurify.sanitize mit eigener Konfiguration nutzen.
purify.addHook('afterSanitizeAttributes', (node) => {
  // `start` nur auf <ol> und nur als Zahl (Liste beginnt z. B. bei 3).
  if (node.hasAttribute('start') && !(node.tagName === 'OL' && /^\d{1,9}$/.test(node.getAttribute('start')))) {
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
  // `start` ist kein URI: ohne diese Angabe prüft DOMPurify den Wert gegen ALLOWED_URI_REGEXP.
  ADD_URI_SAFE_ATTR: ['start'],
  ALLOW_DATA_ATTR: false,
  ALLOW_ARIA_ATTR: false,
}

// Mehr als `max` Zeichen (Codepoints)? Bricht nach max + 1 Zeichen ab.
const exceeds = (str, max) => {
  if (str.length <= max) return false
  const it = str[Symbol.iterator]()
  for (let n = 0; n <= max; n++) if (it.next().done) return false
  return true
}

const tooCostly = (src) => {
  const text = src.replace(/\r\n?/g, '\n')
  return exceeds(text, RICH_TEXT_MAX_RENDER_LENGTH)
    || text.split(/\n[^\S\n]*\n/).some(block => exceeds(block, RICH_TEXT_MAX_BLOCK_LENGTH))
}

const FORMAT_CHARS = /[*_`>~[\]]/g
export const richTextRenderCost = (text) => text.length + ((text.match(FORMAT_CHARS)?.length ?? 0) * text.length) / 1500

let renderBudget = RICH_TEXT_RENDER_BUDGET
let renderBudgetAt = performance.now()

// Füllt das Budget nach der verstrichenen Zeit auf (höchstens bis zum Maximum).
const refillRenderBudget = () => {
  const now = performance.now()
  const elapsed = Math.max(0, now - renderBudgetAt)
  renderBudgetAt = now
  renderBudget = Math.min(RICH_TEXT_RENDER_BUDGET, renderBudget + (elapsed / 1000) * RICH_TEXT_RENDER_REFILL_PER_SECOND)
}

export function resetRichTextBudget() {
  renderBudget = RICH_TEXT_RENDER_BUDGET
  renderBudgetAt = performance.now()
}

// Verbraucht das Budget für einen neuen Text; false, wenn es nicht reicht.
const spendRenderBudget = (text) => {
  refillRenderBudget()
  const cost = richTextRenderCost(text)
  if (cost > renderBudget) return false
  renderBudget -= cost
  return true
}

export function memoize(fn) {
  const cache = new Map()
  const memoized = (key) => {
    if (key.length > RICH_TEXT_CACHE_MAX_KEY_LENGTH) return fn(key)
    if (cache.has(key)) {
      // LRU: Treffer wandert ans Ende, verdrängt wird der am längsten ungenutzte.
      const hit = cache.get(key)
      cache.delete(key)
      cache.set(key, hit)
      return hit
    }
    const value = fn(key)
    if (cache.size >= RICH_TEXT_CACHE_SIZE) cache.delete(cache.keys().next().value)
    cache.set(key, value)
    return value
  }
  memoized.has = (key) => cache.has(key)
  return memoized
}

// Rendert Markdown zu bereinigtem HTML. Nur für v-html in RichText.vue.
// Das Ergebnis enthält keine Zeilenumbrüche (Absätze und <br> trennen schon),
// damit white-space: pre-wrap im umgebenden Layout keine Leerzeilen erzeugt.
export const renderRichText = (markdown) => {
  const src = String(markdown ?? '')
  // Gemerkte Texte kosten nichts; neue brauchen Budget, sonst Klartext (nicht gemerkt).
  if (src.trim() === '' || renderCached.has(src)) return renderCached(src)
  return spendRenderBudget(src) ? renderCached(src) : literal(src)
}

const renderCached = memoize((src) => {
  if (src.trim() === '') return ''
  // Fail-closed: Wo DOMPurify kein DOM findet (isSupported false), liefert
  // sanitize() seine Eingabe unverändert zurück; dann lieber Klartext.
  if (!purify.isSupported || tooCostly(src)) return literal(src)
  // Tief verschachtelte Zitate (`>>>>...`) lassen marked mit RangeError (Aufrufstapel)
  // scheitern; ein Fehler im Render würde sonst die ganze öffentliche Seite leeren.
  // Dann lieber der Text als maskierter Klartext.
  try {
    return purify.sanitize(marked.parse(src), SANITIZE_CONFIG).replace(/\n/g, '')
  } catch {
    return literal(src)
  }
})

// Klartext für Listen und Suche: ohne Markdown-Zeichen, Listen als "- " bzw.
// "1. ", Links als "Text (Adresse)" (wie das Backend, richtext.ToPlainText).
export const markdownToPlainText = (markdown) => plainCached(String(markdown ?? ''))

const plainCached = memoize((src) => {
  const html = renderRichText(src)
  if (html === '') return ''
  const body = new DOMParser().parseFromString(html, 'text/html').body
  return blocksToText(body).trim()
})

function inlineToText(node) {
  let out = ''
  for (const child of node.childNodes) {
    if (child.nodeType === 3) {
      out += child.textContent
    } else if (child.nodeName === 'BR') {
      out += '\n'
    } else if (child.nodeName === 'A') {
      const text = inlineToText(child)
      const href = child.getAttribute('href')
      out += text === href ? href : `${text} (${href})`
    } else {
      out += (child.nodeName === 'P' && out !== '' ? '\n' : '') + inlineToText(child)
    }
  }
  return out
}

function blocksToText(root) {
  const parts = []
  // childNodes statt children: Mit einem unvollständigen DOM (happy-dom in Tests)
  // fehlen die <p>, dann stehen Textknoten direkt im body.
  for (const el of root.childNodes) {
    if (el.nodeType === 3) {
      parts.push(el.textContent.trim())
    } else if (el.nodeName === 'UL' || el.nodeName === 'OL') {
      const first = Number(el.getAttribute('start') ?? 1) // der Sanitizer lässt nur Ziffern durch
      const lines = [...el.children].map((li, i) =>
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
