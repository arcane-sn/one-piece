import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Dev server on 5301 proxies /api to the Go backend on 5300 (loopback only).
export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5301,
    proxy: { '/api': 'http://127.0.0.1:5300' },
  },
})
