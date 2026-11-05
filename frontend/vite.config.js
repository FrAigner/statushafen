import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { cloudflareRocketLoaderExclude } from './vite-plugins/cloudflareRocketLoaderExclude.js'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), cloudflareRocketLoaderExclude()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        // E2E_BACKEND_URL erlaubt ein eigenes Test-Backend, Standard bleibt :8080.
        target: process.env.E2E_BACKEND_URL || 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      }
    }
  }
})
