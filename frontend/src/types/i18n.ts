// Minimal shape of vue-i18n's t() as the plain helper modules need it. Callers pass the composer's t
// (or a stub in tests); keeping it this narrow keeps the helpers free of vue-i18n's generic overloads.
export type TranslateFn = (key: string, named?: Record<string, unknown>) => string
