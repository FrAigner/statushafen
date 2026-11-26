// Pure text functions of the RichTextEditor toolbar.
// The stored value stays the Markdown string; every function gets
// value and selection [start, end) and returns the new value and new selection.
// The selection refers to the same string that is passed in (in the
// editor the normalized value of the <textarea>, i.e. without \r).

export type InlineFormat = 'bold' | 'italic' | 'code'
export type LineFormat = 'list' | 'quote'
export type FormatKind = InlineFormat | LineFormat | 'link'

/** New value and selection after a formatting step. */
export interface FormatResult {
  value: string
  start: number
  end: number
}

const LINE_PREFIX: Record<LineFormat, string> = { list: '- ', quote: '> ' }

export const LINK_URL_PLACEHOLDER = 'https://'

// Length of the run of identical characters directly before `pos` or from `pos` on.
function runBefore(value: string, pos: number, ch: string): number {
  let n = 0
  while (pos - n - 1 >= 0 && value[pos - n - 1] === ch) n++
  return n
}
function runAfter(value: string, pos: number, ch: string): number {
  let n = 0
  while (pos + n < value.length && value[pos + n] === ch) n++
  return n
}

// Takes whitespace at the edges out of the selection (`**ab **` would not be bold).
function trimSelection(value: string, start: number, end: number): { start: number; end: number } {
  let s = start
  let e = end
  while (s < e && /\s/.test(value[s] as string)) s++
  while (e > s && /\s/.test(value[e - 1] as string)) e--
  // Only whitespace selected: treat like an empty selection at the end.
  return s === e ? { start: end, end } : { start: s, end: e }
}

// Stars: `*` italic, `**` bold, `***` both. The number of stars directly around the
// selection decides: bold is present if there are at least 2 on both sides, italic
// if the smaller number is odd.
function toggleStars(value: string, start: number, end: number, count: number, placeholder: string): FormatResult {
  const selected = value.slice(start, end)
  const around = Math.min(runBefore(value, start, '*'), runAfter(value, end, '*'))
  const present = selected && (count === 2 ? around >= 2 : around % 2 === 1)
  if (present) {
    return {
      value: value.slice(0, start - count) + selected + value.slice(end + count),
      start: start - count,
      end: end - count,
    }
  }
  const mark = '*'.repeat(count)
  const inner = selected || placeholder
  return {
    value: value.slice(0, start) + mark + inner + mark + value.slice(end),
    start: start + count,
    end: start + count + inner.length,
  }
}

function toggleCode(value: string, start: number, end: number, placeholder: string): FormatResult {
  const selected = value.slice(start, end)
  if (!selected.includes('`')) {
    if (selected && value[start - 1] === '`' && value[end] === '`' && runBefore(value, start, '`') === 1 && runAfter(value, end, '`') === 1) {
      return { value: value.slice(0, start - 1) + selected + value.slice(end + 1), start: start - 1, end: end - 1 }
    }
    const inner = selected || placeholder
    return { value: `${value.slice(0, start)}\`${inner}\`${value.slice(end)}`, start: start + 1, end: start + 1 + inner.length }
  }
  // Backtick in the text: fence longer than any backtick run inside, with spaces inside.
  const longest = Math.max(...(selected.match(/`+/g) as RegExpMatchArray).map((r) => r.length))
  const fence = '`'.repeat(longest + 1)
  return {
    value: `${value.slice(0, start)}${fence} ${selected} ${fence}${value.slice(end)}`,
    start: start + fence.length + 1,
    end: start + fence.length + 1 + selected.length,
  }
}

// Does the whole paragraph carry the markup (stars or backtick at start and end)?
function markedWhole(core: string, kind: InlineFormat): boolean {
  if (kind === 'code') return core.length > 2 && core.startsWith('`') && core.endsWith('`')
  const l = runAfter(core, 0, '*')
  const t = runBefore(core, core.length, '*')
  if (l >= core.length) return false
  const around = Math.min(l, t)
  return kind === 'bold' ? around >= 2 : around % 2 === 1
}
function unmark(core: string, kind: InlineFormat): string {
  const n = kind === 'bold' ? 2 : 1
  return core.slice(n, core.length - n)
}

function wrapInline(value: string, start: number, end: number, kind: InlineFormat, placeholder: string): FormatResult {
  const apply = (v: string, s: number, e: number): FormatResult => {
    if (kind === 'code') return toggleCode(v, s, e, placeholder)
    return toggleStars(v, s, e, kind === 'bold' ? 2 : 1, placeholder)
  }
  const t = trimSelection(value, start, end)
  const selected = value.slice(t.start, t.end)
  // Across several paragraphs: wrap each paragraph on its own (`**a\n\nb**` would not be bold).
  if (/\n[ \t]*\n/.test(selected)) {
    const parts = selected.split(/(\n[ \t]*\n\s*)/)
    const isGap = (part: string) => part === '' || /^\n[ \t]*\n\s*$/.test(part)
    // Every paragraph already carries the markup: remove it, otherwise set it everywhere.
    const allMarked = parts.filter((x) => !isGap(x)).every((x) => {
      const c = x.trim()
      return markedWhole(c, kind)
    })
    let out = ''
    for (const part of parts) {
      if (isGap(part)) {
        out += part
        continue
      }
      const lead = part.length - part.trimStart().length
      const core = part.trim()
      const inner = allMarked ? unmark(core, kind) : apply(core, 0, core.length).value
      out += part.slice(0, lead) + inner + part.slice(lead + core.length)
    }
    return { value: value.slice(0, t.start) + out + value.slice(t.end), start: t.start, end: t.start + out.length }
  }
  return apply(value, t.start, t.end)
}

function prefixLines(value: string, start: number, end: number, prefix: string): FormatResult {
  // A selection including a trailing line break (triple click) does not cover the following line.
  const last = end > start && value[end - 1] === '\n' ? end - 1 : end
  const from = value.lastIndexOf('\n', start - 1) + 1
  const nl = value.indexOf('\n', last)
  const to = nl === -1 ? value.length : nl
  const lines = value.slice(from, to).split('\n')
  // Blank lines stay empty; only a single empty line (cursor) gets filled.
  const targets = lines.map((l, i) => (l.trim() ? i : -1)).filter((i) => i >= 0)
  if (targets.length === 0) targets.push(0)
  const allPrefixed = targets.every((i) => (lines[i] as string).startsWith(prefix))
  const next = lines.map((l, i) => {
    if (!targets.includes(i)) return l
    return allPrefixed ? l.slice(prefix.length) : prefix + l
  })
  const block = next.join('\n')
  return { value: value.slice(0, from) + block + value.slice(to), start: from, end: from + block.length }
}

function insertLink(value: string, start: number, end: number, placeholder: string): FormatResult {
  // `]` in the link text would end the link: escape it.
  const raw = value.slice(start, end) || placeholder
  const text = raw.replace(/([\][\\])/g, '\\$1')
  const out = `[${text}](${LINK_URL_PLACEHOLDER})`
  const urlStart = start + text.length + 3
  return {
    value: value.slice(0, start) + out + value.slice(end),
    start: urlStart,
    end: urlStart + LINK_URL_PLACEHOLDER.length,
  }
}

/**
 * @param placeholder text inserted when the selection is empty
 */
export function applyFormat(value: string, start: number, end: number, kind: FormatKind, placeholder = ''): FormatResult {
  if (kind === 'link') return insertLink(value, start, end, placeholder)
  if (kind in LINE_PREFIX) return prefixLines(value, start, end, LINE_PREFIX[kind as LineFormat])
  return wrapInline(value, start, end, kind as InlineFormat, placeholder)
}

/**
 * Smallest contiguous replacement that turns `from` into `to`
 * (for execCommand('insertText'), which keeps the field's undo history).
 */
export function minimalReplace(from: string, to: string): { start: number; end: number; text: string } {
  let p = 0
  const max = Math.min(from.length, to.length)
  while (p < max && from[p] === to[p]) p++
  let s = 0
  while (s < max - p && from[from.length - 1 - s] === to[to.length - 1 - s]) s++
  return { start: p, end: from.length - s, text: to.slice(p, to.length - s) }
}
