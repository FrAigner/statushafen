import { createTranslationLoader } from './useTranslationFactory'

export const usePublicComponentTranslations = createTranslationLoader(
  (id) => `/api/components/${id}/translations`
)
