// Build-time variables from frontend/.env (see .env.example). Vite inlines only VITE_* variables.

// Without this, ImportMetaEnv has an index signature and a typo such as VITE_API_ULR is not an error.
interface ViteTypeOptions {
  strictImportMetaEnv: unknown
}

// Both are optional: the code falls back to '' (same origin, no subdomain convention).
// MODE, DEV, PROD, SSR and BASE_URL come from vite/client.
interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_APP_BASE_DOMAINS?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
