import { createAdminTranslationLoader } from './useAdminTranslationFactory'

export const useComponentTranslations = createAdminTranslationLoader({
  list: (id) => `/api/components/${id}/translations`,
  listAll: (id) => `/api/components/${id}/translations/all`,
  confirm: (id) => `/api/component-translations/${id}/confirm`,
  delete: (id) => `/api/component-translations/${id}`
})
