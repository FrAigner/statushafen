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

// Redesign Stufe 4e „Einstieg“: Login, Bestellung, Bestätigungs- und
// Abmeldeseiten teilen sich assets/entry.css und nutzen nur Tokens. Vorher trug
// jede View eigene Karte/Formularfelder/Knöpfe mit Indigo-Verlauf (#6366f1,
// #7c3aed), festen rgba-Werten und `#fff`/`#dc2626`-Rückfallwerten. Der Test
// hält die Seiten auf dem Stand: kein Inline-Style, keine Farbliterale, kein
// eigener <style>-Block, der das Aussehen wieder abspaltet.
// ?raw liefert für .css unter vitest einen leeren String, deshalb von der Platte lesen.
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

const stripComments = (text) => text
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/<!--[\s\S]*?-->/g, '')
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
      const minHeight = (selector) => {
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
