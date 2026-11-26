import { ref } from 'vue'
import { useApi } from './useApi'
import type { TenantId } from './useTenant'
import type { Language, PageLanguage } from '../types/api'

// Stage 1 of 5 of multilingualism: access to the global language catalog
// (Language) and the languages activated per status page + default
// (StatusPageLanguage). Replaces the previous assumption implicitly fixed to
// en/de. Admin screens such as the mail templates
// (AdminNotificationTemplates.vue) use the same composable instances
// (module-scoped refs), so that e.g. a tenant switch in one view is reflected
// immediately in all other visibly active views.

const catalog = ref<Language[]>([])
const pageLanguages = ref<PageLanguage[]>([])

export function useContentLanguages() {
  const { apiFetch } = useApi()

  const loadCatalog = async () => {
    catalog.value = await apiFetch<Language[]>('/api/languages')
    return catalog.value
  }

  const loadPageLanguages = async (pageId: TenantId | null | undefined) => {
    if (!pageId) {
      pageLanguages.value = []
      return pageLanguages.value
    }
    // An empty response (null) counts as "no languages" right away: views compute
    // their translation menus from this list on the next render.
    pageLanguages.value = (await apiFetch<PageLanguage[] | null>(`/api/pages/${pageId}/languages`)) ?? []
    return pageLanguages.value
  }

  const savePageLanguages = async (pageId: TenantId | null, codes: string[], defaultCode: string) => {
    pageLanguages.value = await apiFetch<PageLanguage[]>(`/api/pages/${pageId}/languages`, {
      method: 'PUT',
      body: JSON.stringify({ codes, default: defaultCode })
    })
    return pageLanguages.value
  }

  const createLanguage = async (code: string, name: string) => {
    const created = await apiFetch<Language>('/api/languages', {
      method: 'POST',
      body: JSON.stringify({ code, name })
    })
    await loadCatalog()
    return created
  }

  const updateLanguage = async (id: number | string, fields: Record<string, unknown>) => {
    const updated = await apiFetch<Language>(`/api/languages/${id}`, {
      method: 'PUT',
      body: JSON.stringify(fields)
    })
    await loadCatalog()
    return updated
  }

  return {
    catalog,
    pageLanguages,
    loadCatalog,
    loadPageLanguages,
    savePageLanguages,
    createLanguage,
    updateLanguage
  }
}
