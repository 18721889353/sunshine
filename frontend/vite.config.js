import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../cmd/sunshine/server/static',
    emptyOutDir: true
  },
  server: {
    port: 518,
    proxy: {
      '/api': {
        target: 'http://localhost:24631',
        changeOrigin: true
      }
    }
  }
})
