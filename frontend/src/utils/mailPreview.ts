// Preview of a mail template in the admin UI. The template comes from a page
// admin and is shown to other admins (with higher permissions) in the
// logged-in context. Without a tight allowlist, a phishing mask could be
// overlaid there (form, password field, overlay via CSS).
//
// Allowed is what mails need: text, list and table markup, links (https,
// http, mailto) and inline style with presentational declarations.
// Forms, input fields, images, style blocks, frames, SVG etc. are dropped,
// as are positioning CSS declarations and anything that loads resources.
import DOMPurify from 'dompurify'

const ALLOWED_TAGS = [
  'p', 'br', 'hr', 'div', 'span', 'strong', 'b', 'em', 'i', 'u', 's', 'small', 'sub', 'sup', 'code', 'pre',
  'blockquote', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'a',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'td', 'th', 'center',
]
const ALLOWED_ATTR = ['href', 'style', 'colspan', 'rowspan', 'align', 'start']
const ALLOWED_URI_REGEXP = /^(?:https?:|mailto:)/i

// Allowed CSS properties (presentation within the template's own box). Not
// included: position, top/left/right/bottom/inset, z-index, float, transform,
// viewport sizes, background-image, content, behavior and the like.
const ALLOWED_CSS = new Set([
  'color', 'background', 'background-color', 'font', 'font-family', 'font-size', 'font-weight', 'font-style',
  'line-height', 'letter-spacing', 'text-align', 'text-decoration', 'text-transform', 'white-space',
  'margin', 'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'padding', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'border', 'border-top', 'border-right', 'border-bottom', 'border-left', 'border-color', 'border-width',
  'border-style', 'border-radius', 'border-collapse', 'width', 'max-width', 'min-width', 'height',
  'display', 'vertical-align', 'overflow-wrap', 'word-break',
])
// Values that load resources, run script or use the viewport size.
const FORBIDDEN_CSS_VALUE = /url\s*\(|expression|javascript:|@import|\\|\b-?\d*\.?\d+(?:vw|vh|vmin|vmax)\b|!important/i
// Display types that could leave the box or fake interaction.
const ALLOWED_DISPLAY = new Set(['block', 'inline', 'inline-block', 'table', 'table-cell', 'table-row', 'none'])

export function filterStyle(style: unknown): string {
  const kept: string[] = []
  for (const decl of String(style).split(';')) {
    const idx = decl.indexOf(':')
    if (idx < 1) continue
    const prop = decl.slice(0, idx).trim().toLowerCase()
    const value = decl.slice(idx + 1).trim()
    if (!ALLOWED_CSS.has(prop) || !value || FORBIDDEN_CSS_VALUE.test(value)) continue
    if (prop === 'display' && !ALLOWED_DISPLAY.has(value.toLowerCase())) continue
    // `background:` with image functions is excluded above via url(;
    // gradients are no problem and do not load anything either.
    kept.push(`${prop}:${value}`)
  }
  return kept.join(';')
}

// Own instance: the hook applies only here.
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
  if (node.hasAttribute && node.hasAttribute('start') && !(node.tagName === 'OL' && /^\d{1,9}$/.test(node.getAttribute('start') as string))) {
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

// sanitizeMailPreview returns sanitized HTML for v-html in the preview.
export function sanitizeMailPreview(html: unknown): string {
  if (!html) return ''
  return purify.sanitize(String(html), CONFIG)
}
