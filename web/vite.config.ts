/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// SSE 走 POST，浏览器原生 EventSource 不支持 POST，
// 因此前端用 fetch + ReadableStream 消费 /api/debate。
// dev 下把 /api 代理到本地后端（默认 8080），避免跨域。
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: false,
  },
})
