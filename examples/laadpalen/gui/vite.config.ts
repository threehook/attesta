import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The page only calls its own origin; in development /api is forwarded to the laadpalen backend, in the cluster nginx does it (nginx.conf).
export default defineConfig({
  plugins: [react()],
  server: { port: 5174, proxy: { '/api': 'http://localhost:8787' } },
})
