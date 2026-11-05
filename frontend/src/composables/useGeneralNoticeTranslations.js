import { createAdminTranslationLoader } from './useAdminTranslationFactory'

export const useGeneralNoticeTranslations = createAdminTranslationLoader({
  list: (id) => `/api/notices/${id}/translations`,
  listAll: (id) => `/api/notices/${id}/translations/all`,
  confirm: (id) => `/api/notice-translations/${id}/confirm`,
  delete: (id) => `/api/notice-translations/${id}`
})
