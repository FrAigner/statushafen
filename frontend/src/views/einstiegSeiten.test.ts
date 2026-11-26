import { describe, it, expect } from 'vitest'
import loginSource from './auth/LoginView.vue?raw'
import landingSource from './public/LandingPage.vue?raw'
import orderSource from './public/OrderPage.vue?raw'
import setPasswordSource from './public/OrderSetPasswordPage.vue?raw'
import confirmSource from './public/OrderConfirmPage.vue?raw'
import subscribeConfirmSource from './public/SubscribeConfirmPage.vue?raw'
import invitationSource from './public/InvitationPage.vue?raw'
import unsubscribeSource from './public/Unsubscribe.vue?raw'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'
import { stripHtmlComments } from '../test-support/sourceText'

// Redesign stage 4e "entry": login, order, confirmation and
// unsubscribe pages share assets/entry.css and use only tokens. Previously
// every view carried its own card/form fields/buttons with an indigo gradient (#6366f1,
// #7c3aed), fixed rgba values and `#fff`/`#dc2626` fallbacks. The test
// keeps the pages in that state: no inline style, no colour literals, no
// own <style> block that splits the look off again.
// Under vitest ?raw yields an empty string for .css, so read it from disk.
const entryCss = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../assets/entry.css'), 'utf-8')

const VIEWS = {
  'LoginView.vue': loginSource,
  'LandingPage.vue': landingSource,
  'OrderPage.vue': orderSource,
  'OrderSetPasswordPage.vue': setPasswordSource,
  'OrderConfirmPage.vue': confirmSource,
  'SubscribeConfirmPage.vue': subscribeConfirmSource,
  'InvitationPage.vue': invitationSource,
  'Unsubscribe.vue': unsubscribeSource,
}

const stripComments = (text: string) => stripHtmlComments(text
  .replace(/\/\*[\s\S]*?\*\//g, ''))
  .replace(/(^|\s)\/\/.*$/gm, '$1')

describe('Einstiegsseiten (Stufe 4e)', () => {
  for (const [name, source] of Object.entries(VIEWS)) {
    describe(name, () => {
      const code = stripComments(source)

      it('hat keine Inline-Styles', () => {
        expect(code).not.toMatch(/\sstyle=|:style=/)
      })

      it('enthält keine Farbliterale (Hex, rgb/rgba, Verlauf)', () => {
        expect(code).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
        expect(code).not.toMatch(/rgba?\(/)
      })
    })
  }

  const SHARED = Object.entries(VIEWS).filter(([name]) => name !== 'LandingPage.vue')
  for (const [name, source] of SHARED) {
    it(`${name} bindet die gemeinsame entry.css ein und hat keinen eigenen Style-Block`, () => {
      expect(source).toContain("import '../../assets/entry.css'")
      expect(source).not.toContain('<style')
      expect(source).toContain('entry-card')
    })
  }

  describe('entry.css', () => {
    const css = stripComments(entryCss)

    it('nutzt nur Tokens: keine Farbliterale', () => {
      expect(css).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
      expect(css).not.toMatch(/rgba?\(/)
      expect(css).not.toMatch(/gradient/)
    })

    it('Eingaben, Knöpfe und Umschalter erreichen die Zielgröße (WCAG 2.2: mind. 24 px, hier 32-44)', () => {
      const minHeight = (selector: string) => {
        const block = css.match(new RegExp(`${selector.replace(/[.]/g, '\\.')} \\{([^}]*)\\}`))?.[1] ?? ''
        return Number(block.match(/min-height:\s*(\d+)px/)?.[1])
      }
      expect(minHeight('.entry-field input')).toBeGreaterThanOrEqual(40)
      expect(minHeight('.entry-btn')).toBeGreaterThanOrEqual(44)
      expect(minHeight('.entry-toggle')).toBeGreaterThanOrEqual(32)
    })

    it('Fokus ist sichtbar (Feld, Knopf, Umschalter)', () => {
      for (const sel of ['.entry-field input:focus-visible', '.entry-btn:focus-visible', '.entry-toggle:focus-visible']) {
        expect(css, sel).toContain(sel)
      }
    })

    it('Fehlermeldung hat Rahmen, Fläche und Schrift (nicht nur Farbe)', () => {
      const alert = css.match(/\.entry-alert \{([^}]*)\}/)?.[1] ?? ''
      expect(alert).toContain('border: 1px solid var(--major)')
      expect(alert).toContain('background: var(--major-soft)')
      expect(alert).toContain('color: var(--major-text)')
    })
  })
})
