import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminIncidentTranslations from './AdminIncidentTranslations.vue'
import AdminComponentTranslations from './AdminComponentTranslations.vue'
import AdminGeneralNoticeTranslations from './AdminGeneralNoticeTranslations.vue'

// Die Admin-Übersetzungsseiten luden ihre Tabelle über die
// öffentliche Route, die nur bestätigte Übersetzungen liefert. Ein frisch
// angelegter Entwurf verschwand nach dem Speichern, "Bestätigen" erschien
// nie. Der Fake-Server hier verhält sich wie das Backend: die öffentliche
// Route filtert auf `confirmed`, nur `/translations/all` liefert Entwürfe.

const views = [
  {
    name: 'Vorfall', component: AdminIncidentTranslations, prop: 'incidentId', segment: 'incidents',
    contentSelector: '#message-input', contentKey: 'Message',
    entityRoute: (url) => url === '/api/pages/1/incidents/1' && { ID: 1, Title: 'Ausfall' },
  },
  {
    name: 'Komponente', component: AdminComponentTranslations, prop: 'componentId', segment: 'components',
    contentSelector: '#content-input', contentKey: 'Description',
    entityRoute: (url) => url === '/api/pages/1/components' && [{ ID: 1, Name: 'Datenbank' }],
  },
  {
    name: 'Ankündigung', component: AdminGeneralNoticeTranslations, prop: 'noticeId', segment: 'notices',
    contentSelector: '#content-input', contentKey: 'Message',
    entityRoute: (url) => url === '/api/pages/1/notices/all' && [{ ID: 1, Title: 'Wartung' }],
  },
]

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// fakeBackend hält die Übersetzungen als Zustand und bildet die
// Status-Filterung der echten Routen nach.
function fakeBackend(view) {
  const rows = [{ ID: 1, LanguageCode: 'de', Title: 'Bestätigt', [view.contentKey]: 'x', Status: 'confirmed' }]
  const collection = `/api/${view.segment}/1/translations`
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    const entity = method === 'GET' && view.entityRoute(url)
    if (entity) return jsonResponse(entity)
    if (method === 'GET' && url === '/api/languages') {
      return jsonResponse([{ ID: 1, Code: 'de', Name: 'Deutsch' }, { ID: 2, Code: 'fr', Name: 'Français' }])
    }
    if (method === 'GET' && url === collection) return jsonResponse(rows.filter(r => r.Status === 'confirmed'))
    // Kopie wie nach dem JSON-Parsen: dieselbe Array-Instanz erneut zu liefern würde
    // Vues Änderungserkennung (und die Tabelle in ResponsiveTable) umgehen.
    if (method === 'GET' && url === `${collection}/all`) return jsonResponse(rows.map(r => ({ ...r })))
    if (method === 'POST' && url === collection) {
      const body = JSON.parse(options.body)
      const row = { ID: rows.length + 1, LanguageCode: body.language_code, Title: body.title, Status: 'draft' }
      rows.push(row)
      return jsonResponse(row, 201)
    }
    if (method === 'PATCH' && /-translations\/\d+\/confirm$/.test(url)) {
      const row = rows.find(r => url.includes(`-translations/${r.ID}/`))
      row.Status = 'confirmed'
      return jsonResponse(row)
    }
    throw new Error(`Unerwarteter Fetch: ${method} ${url}`)
  })
}

// Bestätigen ist bei Komponenten und Ankündigungen ein Eintrag im Zeilenmenü (Richtung C),
// bei Vorfällen noch ein Zeilenknopf.
const confirmButtons = (wrapper) => wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))

describe('Admin-Übersetzungen: Entwürfe bleiben sichtbar', () => {
  let wrapper

  beforeEach(() => {
    i18n.global.locale.value = 'de'
    // Neu/Bestätigen hängen an *:write auf der aktiven Seite.
    useAuth().setAuth('jwt-1', { permissions: '' })
    useTenant().setMyPages([{ ID: 1, Permissions: 'incidents:write,components:write,notices:write' }])
    useTenant().setActiveTenant('1')
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
  })

  it.each(views)('$name: neuer Entwurf bleibt nach dem Speichern in der Tabelle und lässt sich bestätigen', async (view) => {
    global.fetch = fakeBackend(view)
    wrapper = mount(view.component, {
      global: { plugins: [i18n], stubs: onyxFormStubs },
      props: { [view.prop]: '1' },
    })
    await flushPromises()
    expect(confirmButtons(wrapper)).toHaveLength(0)

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await wrapper.find('#lang-select').setValue('fr')
    await wrapper.find('#title-input').setValue('Entwurf FR')
    await wrapper.find(view.contentSelector).setValue('Inhalt')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const draftRow = wrapper.findAll('tbody tr').find(r => r.text().includes('Entwurf FR'))
    expect(draftRow, 'Entwurf fehlt nach dem Speichern').toBeTruthy()
    // der Status steht als Badge mit Text in der UI-Sprache (Vorfall) bzw. als Rohwert.
    expect(draftRow.text()).toMatch(/draft|Entwurf/)
    expect(confirmButtons(wrapper)).toHaveLength(1)

    await confirmButtons(wrapper)[0].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr').find(r => r.text().includes('Entwurf FR')).text()).toMatch(/confirmed|Bestätigt/)
    expect(confirmButtons(wrapper)).toHaveLength(0)
  })
})
