import { createAdminTranslationLoader } from './useAdminTranslationFactory'

export const useIncidentTranslations = createAdminTranslationLoader({
  list: (id) => `/api/incidents/${id}/translations`,
  listAll: (id) => `/api/incidents/${id}/translations/all`,
  confirm: (id) => `/api/incident-translations/${id}/confirm`,
  delete: (id) => `/api/incident-translations/${id}`
})
