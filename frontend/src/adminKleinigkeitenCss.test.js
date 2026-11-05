import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'fs'
import { dirname, resolve, join } from 'path'
import { fileURLToPath } from 'url'

const src = dirname(fileURLToPath(import.meta.url))
const read = (...p) => readFileSync(resolve(src, ...p), 'utf8')
const stripComments = (s) => s.replace(/\/\*[\s\S]*?\*\//g, '')

// happy-dom rechnet kein CSS aus Dateien. Diese Tests prüfen den Quelltext auf
// die Deklarationen, deren Fehlen sichtbare Fehler verursacht hat. Das echte
// Layout bei 375/390 px prüft e2e/admin-schmal.spec.js (Chromium, gemockte API).

function vueFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap(e =>
    e.isDirectory() ? vueFiles(join(dir, e.name)) : e.name.endsWith('.vue') ? [join(dir, e.name)] : [])
}

describe('Checkbox neben dem Label', () => {
  it('kein Element trägt .form-group und .checkbox-row zugleich (.form-group stapelt per flex-direction: column)', () => {
    const offenders = vueFiles(resolve(src, 'views'))
      .concat(vueFiles(resolve(src, 'components')))
      .filter(f => /class="[^"]*\bform-group\b[^"]*\bcheckbox-row\b|class="[^"]*\bcheckbox-row\b[^"]*\bform-group\b/.test(readFileSync(f, 'utf8')))
    expect(offenders).toEqual([])
  })

  it('AdminSSO: jede Checkbox steht in einer eigenen .checkbox-row mit ihrem Label', () => {
    const vue = read('views/admin/AdminSSO.vue')
    for (const id of ['sso-enabled', 'sso-saml-enabled', 'sso-saml-allow-unspecified', 'sso-test-login']) {
      expect(vue).toMatch(new RegExp(`<div class="checkbox-row">\\s*<input id="${id}"[^>]*type="checkbox" />\\s*<label for="${id}">`))
    }
  })

  it('die globale .checkbox-row ist eine Zeile (flex, nicht column)', () => {
    const css = stripComments(read('style.css'))
    const rule = [...css.matchAll(/(?:^|\})\s*\.checkbox-row\s*\{([^{}]*)\}/g)].map(m => m[1]).join(';')
    expect(rule).toMatch(/display:\s*flex/)
    expect(rule).not.toMatch(/flex-direction:\s*column/)
  })
})

describe('schmale Breiten im Admin', () => {
  it('Seitenkopf und Aktionen umbrechen statt über den Rand zu ragen', () => {
    const css = stripComments(read('components/PageHeader.vue').split('<style')[1])
    expect(css).toMatch(/\.page-head__row\s*\{[^}]*flex-wrap:\s*wrap/)
    expect(css).toMatch(/\.page-head__actions\s*\{[^}]*flex-wrap:\s*wrap/)
    expect(css).toMatch(/\.page-head__titles\s*\{[^}]*min-inline-size:\s*0/)
  })

  it('Domain-Badge der Statusseiten darf umbrechen (Badge ist sonst nowrap und wird in der Karte abgeschnitten)', () => {
    const css = stripComments(read('views/admin/AdminStatusPages.vue').split('<style')[1])
    const rule = css.match(/\.domain-badge\.st-badge\s*\{([^}]*)\}/)
    expect(rule).toBeTruthy()
    expect(rule[1]).toMatch(/white-space:\s*normal/)
    expect(rule[1]).toMatch(/min-inline-size:\s*0/)
  })
})

// Betreiber-Fund 31.10.: Im Formular „Statusseite bearbeiten“ ragte die Farbfläche über die volle Breite
// (Regel für alle Eingaben im FormField: inline-size 100 %) und schob das Hex-Feld aus dem Panel.
describe('Farbwähler im Formular (Statusseiten)', () => {
  it('die allgemeine Feldregel nimmt type=color aus', () => {
    const css = stripComments(read('style.css'))
    const rule = css.match(/\.form-field__control input:not\(\[type="checkbox"\]\)[^{]*\{([^}]*)\}/)
    expect(rule).toBeTruthy()
    expect(rule[0]).toContain(':not([type="color"])')
    expect(rule[1]).toMatch(/inline-size:\s*100%/)
  })

  it('Farbfläche hat feste Breite, das Hex-Feld darf schrumpfen', () => {
    const css = stripComments(read('views/admin/AdminStatusPages.vue').split('<style')[1])
    expect(css).toMatch(/\.color-swatch\s*\{[^}]*inline-size:\s*40px[^}]*flex-shrink:\s*0/)
    expect(css).toMatch(/\.adm-row--nowrap > \.form-input\s*\{[^}]*min-inline-size:\s*0/)
  })
})

// Ein Titel ohne Leerzeichen (etwa eine lange URL) hat keine Umbruchstelle und
// machte die Tabelle zehn Millionen Pixel breit; die Aktionsknöpfe lagen
// außer Sicht. Serverseitig ist ein Titel auf 200 Zeichen begrenzt; die
// Tabellen brechen zusätzlich selbst um, auch für ältere, längere Werte.
describe('überlange Titel in den Admin-Tabellen', () => {
  const css = stripComments(read('style.css'))
  const rule = (selector) => {
    const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    return [...css.matchAll(new RegExp(`(?:^|\\})\\s*${esc}\\s*\\{([^{}]*)\\}`, 'g'))].map(m => m[1]).join(';')
  }

  it('Zellen brechen Wörter ohne Umbruchstelle um (overflow-wrap: anywhere beeinflusst auch die Mindestbreite)', () => {
    expect(rule('.data-table td')).toMatch(/overflow-wrap:\s*anywhere/)
  })

  it('der Titel in der Zelle bricht um und bleibt in der Zelle', () => {
    const r = rule('.data-table .component-name__title')
    expect(r).toMatch(/overflow-wrap:\s*anywhere/)
    expect(r).toMatch(/max-inline-size:\s*100%/)
  })

  it('die Zelle ist in der Breite begrenzt, die Aktionsspalte nicht', () => {
    expect(rule('.data-table td')).toMatch(/max-inline-size:\s*\d+(rem|ch|px)/)
    expect(rule('.data-table td.actions-col')).toMatch(/max-inline-size:\s*none/)
  })

  it('der Tabellenrahmen scrollt waagerecht, statt Inhalt abzuschneiden', () => {
    const r = rule('.data-table-wrapper')
    expect(r).toMatch(/overflow-x:\s*auto/)
    expect(r).not.toMatch(/overflow:\s*hidden/)
  })

  it('die Mehrzeilen-Kurzvorschau (cell-sub) kürzt weiter mit Ellipsis', () => {
    for (const f of ['AdminNotices', 'AdminIncidents', 'AdminMaintenance']) {
      const style = stripComments(read(`views/admin/${f}.vue`).split('<style')[1])
      const r = style.match(/\.cell-sub\s*\{([^}]*)\}/)
      expect(r, f).toBeTruthy()
      expect(r[1], f).toMatch(/text-overflow:\s*ellipsis/)
      expect(r[1], f).toMatch(/max-inline-size/)
    }
  })
})
