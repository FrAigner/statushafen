// Search syntax of the public status page.
// Operators (only as standalone, unquoted words, independent of the UI
// language and of case): AND/UND, OR/ODER. AND binds tighter than OR; words
// without an operator count as AND. Phrases in quotes are one term.
// Parentheses are deliberately not supported.
const AND_WORDS = new Set(['and', 'und'])
const OR_WORDS = new Set(['or', 'oder'])

interface SearchToken {
  quoted: boolean
  value: string
}

/** OR groups, each a list of AND terms (lower case). */
export type SearchQuery = string[][]

const tokenize = (text: string): SearchToken[] => {
  const tokens: SearchToken[] = []
  let i = 0
  while (i < text.length) {
    if (/\s/.test(text[i]!)) {
      i++
    } else if (text[i] === '"') {
      // An unclosed phrase runs to the end (indexOf = -1 -> slice to the end).
      const end = text.indexOf('"', i + 1)
      const stop = end === -1 ? text.length : end
      tokens.push({ quoted: true, value: text.slice(i + 1, stop) })
      i = stop + 1
    } else {
      let j = i
      // Stryker disable next-line EqualityOperator: `<=` reads once past the end, slice() truncates - equivalent
      while (j < text.length && !/\s/.test(text[j]!) && text[j] !== '"') j++
      tokens.push({ quoted: false, value: text.slice(i, j) })
      i = j
    }
  }
  return tokens
}

// Result: list of OR groups, each group a list of AND terms (lower case).
// Empty list = no restriction.
export const parseSearchQuery = (text: string | null | undefined): SearchQuery => {
  const groups: SearchQuery = []
  let current: string[] = []
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

export const matchesSearchQuery = (groups: SearchQuery, fields: (string | null | undefined)[]): boolean => {
  if (!groups.length) return true
  const haystack = fields.filter((f): f is string => Boolean(f)).map(f => f.toLowerCase())
  return groups.some(group => group.every(term => haystack.some(f => f.includes(term))))
}
