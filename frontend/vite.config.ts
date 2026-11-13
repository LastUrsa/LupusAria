import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'

export default defineConfig({
  base: './',
  define: {
    __RAFFLE_REHEARSAL_TRACE__: process.env.LUPUSARIA_REHEARSAL_TRACE
      ? JSON.stringify(JSON.parse(readFileSync(process.env.LUPUSARIA_REHEARSAL_TRACE, 'utf8')))
      : 'null'
  },
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts'
  },
  server: {
    strictPort: true
  }
})
