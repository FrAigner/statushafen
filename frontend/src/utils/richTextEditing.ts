// Reine Textfunktionen der Werkzeugleiste des RichTextEditors.
// Der gespeicherte Wert bleibt der Markdown-String; jede Funktion bekommt
// Wert und Auswahl [start, end) und liefert neuen Wert und neue Auswahl.
// Die Auswahl bezieht sich auf denselben String, der übergeben wird (im
// Editor der normalisierte Wert des <textarea>, also ohne \r).

const LINE_PREFIX = { list: '- ', quote: '> ' }

export const LINK_URL_PLACEHOLDER = 'https://'

// Länge der Reihe gleicher Zeichen direkt vor `pos` bzw. ab `pos`.
function runBefore(value, pos, ch) {
  let n = 0
  while (pos - n - 1 >= 0 && value[pos - n - 1] === ch) n++
  return n
}
function runAfter(value, pos, ch) {
  let n = 0
  while (pos + n < value.length && value[pos + n] === ch) n++
  return n
}

// Nimmt Leerraum an den Rändern aus der Auswahl heraus (`**ab **` wäre kein Fettdruck).
function trimSelection(value, start, end) {
  let s = start
  let e = end
  while (s < e && /\s/.test(value[s])) s++
  while (e > s && /\s/.test(value[e - 1])) e--
  // Nur Leerraum markiert: wie leere Auswahl am Ende behandeln.
  return s === e ? { start: end, end } : { start: s, end: e }
}

// Sterne: `*` kursiv, `**` fett, `***` beides. Die Zahl der Sterne direkt um die
// Auswahl entscheidet: Fett ist da, wenn beidseitig mindestens 2 stehen, Kursiv,
// wenn die kleinere Zahl ungerade ist.
function toggleStars(value, start, end, count, placeholder) {
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

function toggleCode(value, start, end, placeholder) {
  const selected = value.slice(start, end)
  if (!selected.includes('`')) {
    if (selected && value[start - 1] === '`' && value[end] === '`' && runBefore(value, start, '`') === 1 && runAfter(value, end, '`') === 1) {
      return { value: value.slice(0, start - 1) + selected + value.slice(end + 1), start: start - 1, end: end - 1 }
    }
    const inner = selected || placeholder
    return { value: `${value.slice(0, start)}\`${inner}\`${value.slice(end)}`, start: start + 1, end: start + 1 + inner.length }
  }
  // Backtick im Text: Zaun länger als jede Backtick-Reihe darin, mit Leerzeichen innen.
  const longest = Math.max(...selected.match(/`+/g).map((r) => r.length))
  const fence = '`'.repeat(longest + 1)
  return {
    value: `${value.slice(0, start)}${fence} ${selected} ${fence}${value.slice(end)}`,
    start: start + fence.length + 1,
    end: start + fence.length + 1 + selected.length,
  }
}

// Trägt der ganze Absatz die Auszeichnung (Sterne bzw. Backtick am Anfang und Ende)?
function markedWhole(core, kind) {
  if (kind === 'code') return core.length > 2 && core.startsWith('`') && core.endsWith('`')
  const l = runAfter(core, 0, '*')
  const t = runBefore(core, core.length, '*')
  if (l >= core.length) return false
  const around = Math.min(l, t)
  return kind === 'bold' ? around >= 2 : around % 2 === 1
}
function unmark(core, kind) {
  const n = kind === 'bold' ? 2 : 1
  return core.slice(n, core.length - n)
}

function wrapInline(value, start, end, kind, placeholder) {
  const apply = (v, s, e) => {
    if (kind === 'code') return toggleCode(v, s, e, placeholder)
    return toggleStars(v, s, e, kind === 'bold' ? 2 : 1, placeholder)
  }
  const t = trimSelection(value, start, end)
  const selected = value.slice(t.start, t.end)
  // Über mehrere Absätze: jeden Absatz für sich umschließen (`**a\n\nb**` wäre kein Fettdruck).
  if (/\n[ \t]*\n/.test(selected)) {
    const parts = selected.split(/(\n[ \t]*\n\s*)/)
    const isGap = (part) => part === '' || /^\n[ \t]*\n\s*$/.test(part)
    // Jeder Absatz trägt die Auszeichnung schon: zurücknehmen, sonst überall setzen.
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

function prefixLines(value, start, end, prefix) {
  // Eine Auswahl samt abschließendem Zeilenumbruch (Dreifachklick) umfasst die Folgezeile nicht.
  const last = end > start && value[end - 1] === '\n' ? end - 1 : end
  const from = value.lastIndexOf('\n', start - 1) + 1
  const nl = value.indexOf('\n', last)
  const to = nl === -1 ? value.length : nl
  const lines = value.slice(from, to).split('\n')
  // Leerzeilen bleiben leer; nur bei einer einzelnen leeren Zeile (Cursor) wird sie befüllt.
  const targets = lines.map((l, i) => (l.trim() ? i : -1)).filter((i) => i >= 0)
  if (targets.length === 0) targets.push(0)
  const allPrefixed = targets.every((i) => lines[i].startsWith(prefix))
  const next = lines.map((l, i) => {
    if (!targets.includes(i)) return l
    return allPrefixed ? l.slice(prefix.length) : prefix + l
  })
  const block = next.join('\n')
  return { value: value.slice(0, from) + block + value.slice(to), start: from, end: from + block.length }
}

function insertLink(value, start, end, placeholder) {
  // `]` im Linktext würde den Link beenden: maskieren.
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
 * @param {string} value
 * @param {number} start
 * @param {number} end
 * @param {'bold'|'italic'|'code'|'list'|'quote'|'link'} kind
 * @param {string} placeholder Text, der bei leerer Auswahl eingefügt wird
 */
export function applyFormat(value, start, end, kind, placeholder = '') {
  if (kind === 'link') return insertLink(value, start, end, placeholder)
  if (kind in LINE_PREFIX) return prefixLines(value, start, end, LINE_PREFIX[kind])
  return wrapInline(value, start, end, kind, placeholder)
}

/**
 * Kleinster zusammenhängender Austausch, der `from` in `to` überführt
 * (für execCommand('insertText'), das die Undo-Historie des Feldes behält).
 */
export function minimalReplace(from, to) {
  let p = 0
  const max = Math.min(from.length, to.length)
  while (p < max && from[p] === to[p]) p++
  let s = 0
  while (s < max - p && from[from.length - 1 - s] === to[to.length - 1 - s]) s++
  return { start: p, end: from.length - s, text: to.slice(p, to.length - s) }
}
