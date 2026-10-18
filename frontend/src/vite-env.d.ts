/// <reference types="vite/client" />

// Build-time configuration (see README → Deployment).
interface ImportMetaEnv {
  /**
   * Absolute origin of the API when it is not served from the same origin as
   * the app. Leave unset for the dev proxy / Firebase Hosting `/api/**` rewrite.
   * Example: https://fcrt-api.fly.dev
   */
  readonly VITE_API_BASE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
