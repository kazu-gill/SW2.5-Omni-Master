import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// /api と /ws はオーケストレーター (既定 :8080、OMNI_API=host:port で変更) へ。
// ターンの POST は 202 ですぐ返るので、長いタイムアウトは要らない。
const api = process.env.OMNI_API ?? 'localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: `http://${api}`, changeOrigin: true },
      '/ws': { target: `ws://${api}`, ws: true },
    },
  },
})
