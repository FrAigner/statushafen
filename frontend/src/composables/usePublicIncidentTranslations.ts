import { createTranslationLoader } from './useTranslationFactory'

export const usePublicIncidentTranslations = createTranslationLoader(
  (id) => `/api/incidents/${id}/translations`
)
