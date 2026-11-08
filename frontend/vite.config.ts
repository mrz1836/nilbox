import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
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
  // React Compiler, run through Babel, memoizes components and hooks, so render
  // performance needs no hand-written useMemo or useCallback.
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss(), brandTitle()],
  server: {
    host: '127.0.0.1',
    port: 9245,
    strictPort: true,
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    // The oldest supported macOS (LSMinimumSystemVersion in build/darwin/Info.plist) is
    // 13, whose WebKit is Safari 16, so the bundle targets that engine.
    target: 'safari16',
    // The bundle loads from the app binary, not over a network, so one chunk is fine. The
    // limit stays as a tripwire for unexpected growth.
    chunkSizeWarningLimit: 700,
  },
})
