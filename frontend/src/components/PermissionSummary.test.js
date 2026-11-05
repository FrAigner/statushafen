import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import i18n from '../i18n'
import PermissionSummary from './PermissionSummary.vue'
import { RESOURCES } from '../utils/permissions'
import { axe } from '../test-support/axe.js'

// Rechte einer Rolle kompakt in der Liste.
const mountSummary = (props) => mount(PermissionSummary, { props, global: { plugins: [i18n] } })
const chips = (w) => w.findAll('.perm-summary__chip').map(c => c.text())

describe('PermissionSummary', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('zeigt je Ressource die Aktionen als Buchstaben statt vieler Rechte-Pillen', () => {
    const w = mountSummary({ permissions: 'components:create,components:read,components:update,components:delete,incidents:read' })
    expect(chips(w)).toEqual(['Komponenten: CRUD', 'Vorfälle: R'])
  })

  it('kürzt auf max Einträge und nennt den Rest als +n, Tooltip und Screenreader haben alles', () => {
    const w = mountSummary({ permissions: 'components:read,incidents:update,notices:read,subscribers:delete,sso:read', max: 2 })
    expect(chips(w)).toEqual(['Komponenten: R', 'Vorfälle: RU', '+3'])
    const detail = w.find('.perm-summary').attributes('title')
    expect(detail).toBe('Komponenten: Lesen; Vorfälle: Lesen, Ändern; Ankündigungen: Lesen; Abonnenten: Lesen, Löschen; SSO Konfiguration: Lesen')
    expect(w.find('.sr-only').text()).toBe(detail)
    expect(w.find('.perm-summary__chips').attributes('aria-hidden')).toBe('true')
  })

  it('zeigt „Alle Rechte“, wenn jeder Tab vollständig vergeben ist', () => {
    const all = RESOURCES.flatMap(r => ['create', 'read', 'update', 'delete'].map(a => `${r.key}:${a}`)).join(',')
    const w = mountSummary({ permissions: all })
    expect(chips(w)).toEqual(['Alle Rechte'])
    expect(w.find('.perm-summary').attributes('title')).toBe('Alle Rechte')
  })

  it('zeigt Sonderrechte und zählt den Altwert :write als CRUD', () => {
    const w = mountSummary({ permissions: 'notices:write,admin_dashboard' })
    expect(chips(w)).toEqual(['Ankündigungen: CRUD', 'Admin-Dashboard'])
  })

  it('zeigt „Keine Rechte“ ohne Einträge und auf Englisch englisch', () => {
    expect(mountSummary({ permissions: '' }).text()).toBe('Keine Rechte')
    i18n.global.locale.value = 'en'
    const w = mountSummary({ permissions: 'components:update' })
    expect(w.find('.perm-summary').attributes('title')).toBe('Components: Read, Update')
  })

  it('läuft nicht über: Chips brechen um und kürzen zu lange Namen', () => {
    const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'PermissionSummary.vue'), 'utf8').split('<style scoped>')[1]
    const rule = (sel) => css.match(new RegExp(`${sel.replace('.', '\\.')}\\s*\\{([^}]*)\\}`))?.[1] ?? ''
    expect(rule('.perm-summary__chips')).toMatch(/flex-wrap:\s*wrap/)
    expect(rule('.perm-summary__chips')).toMatch(/min-width:\s*0/)
    expect(rule('.perm-summary__chip')).toMatch(/max-width:\s*100%/)
    expect(rule('.perm-summary__chip')).toMatch(/text-overflow:\s*ellipsis/)
  })

  it('hat keine axe-Funde', async () => {
    const w = mountSummary({ permissions: 'components:update,incidents:read,notices:read,sso:read' })
    expect(await axe(w)).toHaveNoViolations()
  })
})
