import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, h } from 'vue'
import { readFileSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import i18n from '../i18n'
import PermissionMatrix from './PermissionMatrix.vue'
import { RESOURCES } from '../utils/permissions'
import { axe } from '../test-support/axe.js'

// Berechtigungs-Matrix mit einer Zeile je Tab, gruppiert nach Bereich.
let wrapper = null

function mountMatrix(initial = [], standalone = ['admin_dashboard']) {
  const model = ref([...initial])
  wrapper = mount({
    render: () => h(PermissionMatrix, {
      standalone,
      modelValue: model.value,
      'onUpdate:modelValue': (v) => { model.value = v },
    }),
  }, { global: { plugins: [i18n] }, attachTo: document.body })
  return { wrapper, model }
}

const cell = (perm) => wrapper.find(`input[value="${perm}"]`)
const column = (action) => wrapper.find(`input[data-column="${action}"]`)
const rowAll = (area) => wrapper.find(`tr[data-area="${area}"] input[data-row-all]`)

describe('PermissionMatrix', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    i18n.global.locale.value = 'de'
  })

  it('baut eine Zeile je Tab, gruppiert nach Bereich, mit den Spalten C R U D', () => {
    mountMatrix()

    expect(wrapper.findAll('tbody[data-group]').map(g => g.attributes('data-group')))
      .toEqual(['operations', 'content', 'management', 'system'])
    expect(wrapper.findAll('tbody th[scope="rowgroup"]').map(th => th.text()))
      .toEqual(['Betrieb', 'Inhalte', 'Verwaltung', 'System'])
    const areas = wrapper.findAll('tbody tr[data-area]').map(r => r.attributes('data-area'))
    expect(areas).toEqual(RESOURCES.map(r => r.key))
    expect(wrapper.findAll('thead th')).toHaveLength(5)
    expect(wrapper.findAll('thead .perm-abbr').map(e => e.text())).toEqual(['C', 'R', 'U', 'D'])
    expect(wrapper.findAll('thead .perm-action-text').map(e => e.text())).toEqual(['Anlegen', 'Lesen', 'Ändern', 'Löschen'])
    // Jede Zeile hat vier Zellen mit Checkbox.
    expect(wrapper.findAll('tr[data-area="components"] td input')).toHaveLength(4)
  })

  it('beschriftet Zeilen mit dem Tab-Namen und Zellen mit vollem aria-label', () => {
    mountMatrix()

    expect(wrapper.find('tr[data-area="components"] .perm-label').text()).toBe('Komponenten')
    expect(wrapper.find('tr[data-area="external_sources"] .perm-label').text()).toBe('Externe Quellen')
    expect(cell('components:update').attributes('aria-label')).toBe('Komponenten – Ändern')
    expect(cell('components:update').attributes('title')).toBe('components:update')
    expect(column('delete').attributes('aria-label')).toBe('Alle Tabs – Löschen')
    expect(rowAll('maintenance').attributes('aria-label')).toBe('Wartungsfenster – alles')
  })

  it('zeigt Beschriftungen auf Englisch', async () => {
    mountMatrix()
    i18n.global.locale.value = 'en'
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('thead .perm-action-text').map(e => e.text())).toEqual(['Create', 'Read', 'Update', 'Delete'])
    expect(cell('incidents:create').attributes('aria-label')).toBe('Incidents – Create')
    expect(wrapper.findAll('tbody th[scope="rowgroup"]').map(th => th.text())).toEqual(['Operations', 'Content', 'Management', 'System'])
  })

  it('setzt Lesen mit, wenn Anlegen, Ändern oder Löschen gewählt wird', async () => {
    const { model } = mountMatrix()
    for (const act of ['create', 'update', 'delete']) {
      model.value = []
      await wrapper.vm.$nextTick()
      await cell(`notices:${act}`).setValue(true)
      expect(model.value).toEqual([`notices:${act}`, 'notices:read'])
    }
  })

  it('entzieht mit Lesen auch Anlegen, Ändern und Löschen', async () => {
    const { model } = mountMatrix(['notices:create', 'notices:read', 'notices:update', 'notices:delete', 'components:read'])
    await cell('notices:read').setValue(false)
    expect(model.value).toEqual(['components:read'])
  })

  it('zeigt Lesen als gesetzt, wenn nur Ändern vergeben ist (Alt-Rolle ohne read)', () => {
    mountMatrix(['notices:update'])
    expect(cell('notices:read').element.checked).toBe(true)
    expect(cell('notices:update').element.checked).toBe(true)
    expect(cell('notices:delete').element.checked).toBe(false)
  })

  it('Zeilenschalter setzt und entzieht alle vier Aktionen, Spaltenschalter eine Aktion in allen Seiten-Tabs', async () => {
    const { model } = mountMatrix(['foo:bar'])
    await rowAll('notices').setValue(true)
    expect(model.value).toEqual(['foo:bar', 'notices:create', 'notices:read', 'notices:update', 'notices:delete'])
    await rowAll('notices').setValue(false)
    expect(model.value).toEqual(['foo:bar'])

    await column('update').setValue(true)
    const pageResources = RESOURCES.filter(r => r.scope === 'page').map(r => r.key)
    for (const res of pageResources) expect(model.value).toContain(`${res}:update`)
    expect(model.value.some(p => p.startsWith('users:'))).toBe(false)
    await column('update').setValue(false)
    expect(model.value.some(p => p.endsWith(':update'))).toBe(false)
    expect(model.value).toContain('foo:bar')
  })

  it('Gesamtschalter wählt alle Seiten-Tabs, aber keine globalen', async () => {
    const { model } = mountMatrix()
    await column('all').setValue(true)
    expect(model.value).toContain('sso:delete')
    expect(model.value).toContain('pages:create')
    expect(model.value.some(p => p.startsWith('roles:'))).toBe(false)
    expect(column('all').element.checked).toBe(true)
    await column('all').setValue(false)
    expect(model.value).toEqual([])
  })

  it('stellt Teilauswahl als unbestimmt dar', () => {
    mountMatrix(['components:read'])
    expect(rowAll('components').element.indeterminate).toBe(true)
    expect(column('read').element.indeterminate).toBe(true)
    expect(column('delete').element.indeterminate).toBe(false)
    expect(column('all').element.indeterminate).toBe(true)
  })

  it('sperrt globale Tabs und kennzeichnet sie mit „nur Global Admin“', async () => {
    const { model } = mountMatrix()
    for (const res of ['overview', 'users', 'roles', 'tags', 'audit', 'monitoring', 'system_settings']) {
      expect(rowAll(res).attributes('disabled')).toBeDefined()
      expect(cell(`${res}:read`).attributes('disabled')).toBeDefined()
      expect(wrapper.find(`tr[data-area="${res}"] .perm-badge`).text()).toBe('nur Global Admin')
    }
    expect(rowAll('components').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('tr[data-area="components"] .perm-badge').exists()).toBe(false)
    expect(model.value).toEqual([])
  })

  it('schaltet das Sonderrecht admin_dashboard und lässt fremde Einträge stehen', async () => {
    const { model } = mountMatrix(['beta:feature'])
    const dash = wrapper.find('.perm-access input[value="admin_dashboard"]')
    expect(wrapper.find('.perm-access h4').text()).toBe('Zugriff')
    expect(wrapper.find('.perm-access .perm-label').text()).toBe('Admin-Dashboard')
    await dash.setValue(true)
    expect(model.value).toEqual(['beta:feature', 'admin_dashboard'])
    await dash.setValue(false)
    expect(model.value).toEqual(['beta:feature'])
  })

  it('zeigt unbekannte Sonderrechte mit dem rohen Schlüssel', () => {
    mountMatrix([], ['beta_feature'])
    expect(wrapper.find('.perm-access .perm-label').text()).toBe('beta_feature')
  })

  it('blendet den Abschnitt „Zugriff“ ohne Sonderrechte aus', () => {
    mountMatrix([], [])
    expect(wrapper.find('.perm-access').exists()).toBe(false)
  })

  it('ist per Tastatur bedienbar: jede Zelle ist eine fokussierbare Checkbox, der Scrollbereich ebenfalls', async () => {
    const { model } = mountMatrix()
    const scroll = wrapper.find('.perm-matrix-scroll')
    expect(scroll.attributes('tabindex')).toBe('0')
    expect(scroll.attributes('role')).toBe('region')
    expect(scroll.attributes('aria-label')).toBeTruthy()

    const box = cell('components:create')
    expect(box.attributes('type')).toBe('checkbox')
    expect(box.attributes('tabindex')).toBeUndefined()
    box.element.focus()
    expect(document.activeElement).toBe(box.element)
    // Leertaste löst bei Checkboxen ein Klick-Ereignis aus.
    await box.trigger('click')
    expect(model.value).toContain('components:create')
    expect(model.value).toContain('components:read')
  })

  it('hat keine axe-Funde', async () => {
    mountMatrix(['components:update', 'incidents:read'])
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})

// Layout-Regel gegen Abschneiden: Die Matrix lief im Formular rechts über und
// verlor die letzte Spalte samt Text. Die Regeln halten sie in der Formularbreite.
describe('PermissionMatrix: Layout bleibt in der Formularbreite', () => {
  const dir = dirname(fileURLToPath(import.meta.url))
  const css = readFileSync(resolve(dir, 'PermissionMatrix.vue'), 'utf8').split('<style scoped>')[1]
  const rule = (selector) => {
    const m = css.match(new RegExp(`(?:^|\\n)${selector.replace(/[.[\]]/g, '\\$&')}\\s*\\{([^}]*)\\}`))
    return m ? m[1] : null
  }

  it('begrenzt das fieldset auf die Breite des Formulars', () => {
    const body = rule('.perm-matrix')
    expect(body).toMatch(/min-width:\s*0/)
    expect(body).toMatch(/max-width:\s*100%/)
  })

  it('scrollt nur innerhalb der Matrix, schneidet nie ab', () => {
    const body = rule('.perm-matrix-scroll')
    expect(body).toMatch(/overflow-x:\s*auto/)
    expect(body).toMatch(/max-width:\s*100%/)
    expect(css).not.toMatch(/overflow(-x)?:\s*hidden/)
  })

  it('nutzt feste Spaltenbreiten und bricht den Text der Bereichsspalte um', () => {
    expect(rule('.perm-table')).toMatch(/table-layout:\s*fixed/)
    expect(rule('.perm-table')).toMatch(/width:\s*100%/)
    expect(rule('.perm-table .perm-action-col')).toMatch(/width:\s*2\.9rem/)
    expect(rule('.perm-table .perm-area-col')).toMatch(/overflow-wrap:\s*break-word/)
    expect(rule('.perm-label')).toMatch(/overflow-wrap:\s*anywhere/)
    expect(css).not.toMatch(/white-space:\s*nowrap/)
  })

  it('kommt ohne feste Hex-Farben aus', () => {
    expect(css).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
  })
})
