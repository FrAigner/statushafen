// Vorschau einer Mail-Vorlage in der Admin-Oberfläche. Die Vorlage stammt von
// einem Seiten-Admin und wird anderen Admins (mit höherer Berechtigung) im
// eingeloggten Kontext angezeigt. Ohne enge Allowlist ließe sich dort eine
// Phishing-Maske einblenden (Formular, Passwortfeld, Overlay per CSS).
//
// Erlaubt ist das, was Mails brauchen: Text-, Listen- und Tabellen-Markup,
// Links (https, http, mailto) und Inline-Style mit Darstellungsangaben.
// Formulare, Eingabefelder, Bilder, Style-Blöcke, Frames, SVG usw. fallen weg,
// ebenso positionierende CSS-Angaben und alles, was Ressourcen nachlädt.
import DOMPurify from 'dompurify'

const ALLOWED_TAGS = [
  'p', 'br', 'hr', 'div', 'span', 'strong', 'b', 'em', 'i', 'u', 's', 'small', 'sub', 'sup', 'code', 'pre',
  'blockquote', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'a',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'td', 'th', 'center',
]
const ALLOWED_ATTR = ['href', 'style', 'colspan', 'rowspan', 'align', 'start']
const ALLOWED_URI_REGEXP = /^(?:https?:|mailto:)/i

// Erlaubte CSS-Eigenschaften (Darstellung innerhalb des eigenen Kastens). Nicht
// dabei: position, top/left/right/bottom/inset, z-index, float, transform,
// Viewport-Größen, background-image, content, behavior u. Ä.
const ALLOWED_CSS = new Set([
  'color', 'background', 'background-color', 'font', 'font-family', 'font-size', 'font-weight', 'font-style',
  'line-height', 'letter-spacing', 'text-align', 'text-decoration', 'text-transform', 'white-space',
  'margin', 'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'padding', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'border', 'border-top', 'border-right', 'border-bottom', 'border-left', 'border-color', 'border-width',
  'border-style', 'border-radius', 'border-collapse', 'width', 'max-width', 'min-width', 'height',
  'display', 'vertical-align', 'overflow-wrap', 'word-break',
])
// Werte, die nachladen, Skript ausführen oder die Größe des Viewports nutzen.
const FORBIDDEN_CSS_VALUE = /url\s*\(|expression|javascript:|@import|\\|\b-?\d*\.?\d+(?:vw|vh|vmin|vmax)\b|!important/i
// Anzeigearten, die den Kasten verlassen oder Interaktion vortäuschen könnten.
const ALLOWED_DISPLAY = new Set(['block', 'inline', 'inline-block', 'table', 'table-cell', 'table-row', 'none'])

export function filterStyle(style) {
  const kept = []
  for (const decl of String(style).split(';')) {
    const idx = decl.indexOf(':')
    if (idx < 1) continue
    const prop = decl.slice(0, idx).trim().toLowerCase()
    const value = decl.slice(idx + 1).trim()
    if (!ALLOWED_CSS.has(prop) || !value || FORBIDDEN_CSS_VALUE.test(value)) continue
    if (prop === 'display' && !ALLOWED_DISPLAY.has(value.toLowerCase())) continue
    // `background:` mit Bild-Funktionen ist oben durch url( ausgeschlossen;
    // Verläufe sind kein Problem, laden aber ebenfalls nichts nach.
    kept.push(`${prop}:${value}`)
  }
  return kept.join(';')
}

// Eigene Instanz: Der Hook gilt nur hier.
const purify = DOMPurify(window)
purify.addHook('afterSanitizeAttributes', (node) => {
  if (node.hasAttribute && node.hasAttribute('style')) {
    const filtered = filterStyle(node.getAttribute('style'))
    if (filtered) node.setAttribute('style', filtered)
    else node.removeAttribute('style')
  }
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer nofollow')
  }
  if (node.hasAttribute && node.hasAttribute('start') && !(node.tagName === 'OL' && /^\d{1,9}$/.test(node.getAttribute('start')))) {
    node.removeAttribute('start')
  }
})

const CONFIG = {
  ALLOWED_TAGS,
  ALLOWED_ATTR,
  ALLOWED_URI_REGEXP,
  ADD_URI_SAFE_ATTR: ['start', 'colspan', 'rowspan', 'align'],
  ALLOW_DATA_ATTR: false,
  ALLOW_ARIA_ATTR: false,
}

// sanitizeMailPreview liefert bereinigtes HTML für v-html in der Vorschau.
export function sanitizeMailPreview(html) {
  if (!html) return ''
  return purify.sanitize(String(html), CONFIG)
}
