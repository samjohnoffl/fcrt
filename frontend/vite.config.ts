/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

/** API proxy target; override with VITE_PROXY_TARGET when the API is not on :8080. */
function proxyTarget(): string {
  const env = (globalThis as { process?: { env?: Record<string, string | undefined> } })
    .process?.env
  return env?.VITE_PROXY_TARGET ?? 'http://localhost:8080'
}

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        // Override with VITE_PROXY_TARGET when the API is not on :8080.
        // (globalThis cast avoids requiring @types/node just for process.env.)
        target: proxyTarget(),
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/tests/setup.ts'],
    css: false,
  },
})
