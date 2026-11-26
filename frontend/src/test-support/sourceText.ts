// Helpers for tests that read source files as text.

// Removes template comments (<!-- ... -->) until none is left, so that removing one cannot uncover
// another (CodeQL: incomplete multi-character sanitization).
export function stripHtmlComments(source: string): string {
  let text = source
  let previous: string
  do {
    previous = text
    text = text.replace(/<!--[\s\S]*?-->/g, '')
  } while (text !== previous)
  return text
}
