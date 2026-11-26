import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { stubConfirm } from '../test-support/confirm'
import { useAdminTranslationsPanel } from './useAdminTranslationsPanel'
import type { AdminTranslationsPanelOptions } from './useAdminTranslationsPanel'

type TranslationsApi = AdminTranslationsPanelOptions['translationsApi']

const apiFetch = vi.fn()
vi.mock('./useApi', () => ({ useApi: () => ({ apiFetch }) }))
vi.mock('./useTranslationEnabled', () => ({ useTranslationEnabled: () => ({ load: async () => true }) }))
vi.mock('./useTenant', () => ({ useTenant: () => ({ activeTenantId: ref(1) }) }))
vi.mock('./useContentLanguages', () => ({ useContentLanguages: () => ({ catalog: ref([]), loadCatalog: async () => {} }) }))
vi.mock('./useResourcePerms', () => ({ useResourcePerms: () => ({ canCreate: ref(true), canUpdate: ref(true), canDelete: ref(true) }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function makeTranslationsApi() {
  return {
    translations: ref([]),
    loadTranslations: vi.fn(async () => {}),
    createTranslation: vi.fn(async () => {}),
    confirmTranslation: vi.fn(async () => {}),
    deleteTranslation: vi.fn(async () => {}),
  }
}

async function mountPanel(translationsApi: ReturnType<typeof makeTranslationsApi>) {
  let panel!: ReturnType<typeof useAdminTranslationsPanel>
  mount({
    setup() {
      panel = useAdminTranslationsPanel({
        entityId: ref('7'), apiSegment: 'incidents', contentField: 'message',
        // The doubles resolve to undefined instead of the API's rows.
        translationsApi: translationsApi as unknown as TranslationsApi, resource: 'incidents', overviewPath: '/admin/incidents',
      })
      return () => null
    },
  })
  await flushPromises()
  return panel
}

describe('useAdminTranslationsPanel', () => {
  beforeEach(() => {
    apiFetch.mockReset().mockResolvedValue({ ID: 7, Title: 'Ausfall' })
  })

  it('shows the error and resets the busy state when confirming fails', async () => {
    const api = makeTranslationsApi()
    api.confirmTranslation.mockRejectedValueOnce(new Error('kaputt'))
    const panel = await mountPanel(api)

    await panel.handleConfirm(3)

    expect(panel.alertMsg.value).toBe('translations.confirmErrorkaputt')
    expect(panel.alertType.value).toBe('error')
    expect(panel.confirmingId.value).toBeNull()
  })

  it('deletes after confirmation, reloads the list and reports success', async () => {
    stubConfirm(true)
    const api = makeTranslationsApi()
    const panel = await mountPanel(api)
    api.loadTranslations.mockClear()

    await panel.handleDelete(3)

    expect(api.deleteTranslation).toHaveBeenCalledWith(apiFetch, 3)
    expect(api.loadTranslations).toHaveBeenCalledWith(apiFetch, 7)
    expect(panel.alertMsg.value).toBe('translations.deleted')
    expect(panel.alertType.value).toBe('success')
    expect(panel.deletingId.value).toBeNull()
  })
})
