import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  cacheDir: '.vite-cache',
  plugins: [vue()],
  test: {
    environment: 'jsdom'
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8890'
    }
  }
})
