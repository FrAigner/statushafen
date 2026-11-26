import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Stage 4c: the views of the status page management no longer have
// inline styles (indentation, spacing, colours come from classes and
// tokens). The template source is checked because happy-dom computes no CSS.
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
