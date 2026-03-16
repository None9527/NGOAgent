import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: './', // Required for Capacitor/APK: assets use relative paths
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/v1': {
        target: 'http://127.0.0.1:19997',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:19997',
        changeOrigin: true,
      },
    },
  },
})
