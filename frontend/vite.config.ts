import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

import identity from './src/generated/brand.json' with { type: 'json' }

// brandTitle fills the %BRAND_NAME% placeholder in index.html from the generated brand
// file, so the page title comes from internal/brand like everything else. It runs before
// Vite's own %ENV% replacement.
function brandTitle(): Plugin {
  return {
    name: 'brand-title',
    transformIndexHtml: {
      order: 'pre',
      handler: (html) => html.replaceAll('%BRAND_NAME%', identity.name),
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), brandTitle()],
  server: {
    host: '127.0.0.1',
    port: 9245,
    strictPort: true,
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
