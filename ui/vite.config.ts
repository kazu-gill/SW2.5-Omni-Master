import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        timeout: 180000,
        proxyTimeout: 180000,
      },
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
})
