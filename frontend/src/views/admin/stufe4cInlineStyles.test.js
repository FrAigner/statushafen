import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Stufe 4c: Die Views zur Statusseitenverwaltung haben keine
// Inline-Styles mehr (Einrückung, Abstände, Farben kommen aus Klassen und
// Tokens). Geprüft wird der Template-Quelltext, weil happy-dom kein CSS rechnet.
const dir = dirname(fileURLToPath(import.meta.url))
const VIEWS = [
  'AdminComponents', 'AdminComponentTranslations', 'AdminSubscribers',
  'AdminIncidentTemplates', 'AdminNotificationTemplates', 'AdminLanguages',
]

describe('Stufe 4c: keine Inline-Styles', () => {
  for (const name of VIEWS) {
    it(`${name}.vue hat kein style="" und kein :style`, () => {
      const source = readFileSync(resolve(dir, `${name}.vue`), 'utf-8')
      const template = source.slice(source.indexOf('<template>')).split('<style')[0]
      expect(template).not.toMatch(/\sstyle="/)
      expect(template).not.toMatch(/:style=/)
    })
  }
})
