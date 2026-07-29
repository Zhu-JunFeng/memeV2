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
      '/api': {
        target: 'http://47.251.140.83',
        changeOrigin: true
      }
    }
  }
})
