import { ref } from 'vue'
import { useApi } from './useApi'

// Stage 1 von 5 der Mehrsprachigkeit: Zugriff auf den globalen
// Sprachkatalog (Language) und die pro Statusseite aktivierten Sprachen +
// Default (StatusPageLanguage). Ersetzt die vorherige, implizit auf en/de
// fixierte Annahme - Admin-Oberflächen wie die
// Mail-Templates (AdminNotificationTemplates.vue) nutzen dieselben
// Composable-Instanzen (module-scoped refs), damit z.B. der Tenant-Wechsel
// in einer View sich sofort in allen anderen sichtbar aktiven Views spiegelt.

const catalog = ref([])
const pageLanguages = ref([])

export function useContentLanguages() {
  const { apiFetch } = useApi()

  const loadCatalog = async () => {
    catalog.value = await apiFetch('/api/languages')
    return catalog.value
  }

  const loadPageLanguages = async (pageId) => {
    if (!pageId) {
      pageLanguages.value = []
      return pageLanguages.value
    }
    pageLanguages.value = await apiFetch(`/api/pages/${pageId}/languages`)
    return pageLanguages.value
  }

  const savePageLanguages = async (pageId, codes, defaultCode) => {
    pageLanguages.value = await apiFetch(`/api/pages/${pageId}/languages`, {
      method: 'PUT',
      body: JSON.stringify({ codes, default: defaultCode })
    })
    return pageLanguages.value
  }

  const createLanguage = async (code, name) => {
    const created = await apiFetch('/api/languages', {
      method: 'POST',
      body: JSON.stringify({ code, name })
    })
    await loadCatalog()
    return created
  }

  const updateLanguage = async (id, fields) => {
    const updated = await apiFetch(`/api/languages/${id}`, {
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
