// Suchsyntax der öffentlichen Statusseite.
// Operatoren (nur als eigenständige, unquotierte Wörter, unabhängig von der
// UI-Sprache und von Groß-/Kleinschreibung): AND/UND, OR/ODER. UND bindet
// stärker als ODER; Wörter ohne Operator gelten als UND. Phrasen in
// Anführungszeichen sind ein Term. Klammern sind bewusst nicht unterstützt.
const AND_WORDS = new Set(['and', 'und'])
const OR_WORDS = new Set(['or', 'oder'])

const tokenize = (text) => {
  const tokens = []
  let i = 0
  while (i < text.length) {
    if (/\s/.test(text[i])) {
      i++
    } else if (text[i] === '"') {
      // Nicht geschlossene Phrase läuft bis zum Ende (indexOf = -1 → slice bis Ende).
      const end = text.indexOf('"', i + 1)
      const stop = end === -1 ? text.length : end
      tokens.push({ quoted: true, value: text.slice(i + 1, stop) })
      i = stop + 1
    } else {
      let j = i
      // Stryker disable next-line EqualityOperator: `<=` liest einmal über das Ende hinaus, slice() kappt – äquivalent
      while (j < text.length && !/\s/.test(text[j]) && text[j] !== '"') j++
      tokens.push({ quoted: false, value: text.slice(i, j) })
      i = j
    }
  }
  return tokens
}

// Ergebnis: Liste von ODER-Gruppen, jede Gruppe eine Liste von UND-Termen
// (klein geschrieben). Leere Liste = keine Einschränkung.
export const parseSearchQuery = (text) => {
  const groups = []
  let current = []
  const flush = () => {
    if (current.length) groups.push(current)
    current = []
  }
  for (const { quoted, value } of tokenize(text || '')) {
    const word = value.toLowerCase()
    if (!quoted && OR_WORDS.has(word)) flush()
    else if (!quoted && AND_WORDS.has(word)) continue
    else if (word) current.push(word)
  }
  flush()
  return groups
}

export const matchesSearchQuery = (groups, fields) => {
  if (!groups.length) return true
  const haystack = fields.filter(Boolean).map(f => f.toLowerCase())
  return groups.some(group => group.every(term => haystack.some(f => f.includes(term))))
}
