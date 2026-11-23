import { Buffer } from 'node:buffer'
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

// chunkLimit fails the build when a JS chunk is over build.chunkSizeWarningLimit, in kB of
// 1,000 bytes as Vite counts them: Vite only warns, and the frontend gate builds, so a chunk
// that outgrows the limit stops the gate rather than scrolling past in its output. It
// measures each chunk as written, after Vite's own generateBundle work (the preload lists
// it fills in), as Vite's size report does.
function chunkLimit(): Plugin {
  let limit = 0

  return {
    name: 'chunk-limit',
    apply: 'build',
    configResolved(config) {
      limit = config.build.chunkSizeWarningLimit
    },
    writeBundle(_options, bundle) {
      const over = Object.values(bundle).flatMap((file) => {
        const kB = file.type === 'chunk' ? Buffer.byteLength(file.code) / 1000 : 0

        return kB > limit ? [`${file.fileName} (${kB.toFixed(2)} kB)`] : []
      })

      if (over.length > 0) {
        this.error(`over the ${String(limit)} kB chunk limit (build.chunkSizeWarningLimit): ${over.join(', ')}`)
      }
    },
  }
}

export default defineConfig({
  // React Compiler, run through Babel, memoizes components and hooks, so render
  // performance needs no hand-written useMemo or useCallback.
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss(), brandTitle(), chunkLimit()],
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
    // 13.3, whose WebKit is Safari 16.4, so the bundle targets that engine: the window's CSS
    // needs color-mix() (Safari 16.2) and @property (Safari 16.4).
    target: 'safari16.4',
    // Lightning CSS minifies the CSS for that target and rewrites what the engine lacks:
    // the theme tokens' light-dark() (src/generated/theme.css) becomes variables that the
    // root's color-scheme switches, as Safari 16.4 has no light-dark().
    cssMinify: 'lightningcss',
    // The bundle loads from the app binary, not over a network. Before its first paint the
    // window loads the entry chunk, the inbox's, about 473 kB, and the shared chunk it
    // preloads (codeSplitting below), about 394 kB: about 867 kB of JS in all, with 120 kB
    // of CSS, which magex frontend:gate holds to a budget after this build
    // (magefiles/firstpaint.go). The other pages are chunks of their own, loaded on their
    // first visit (App.tsx), and so are the review and issue sheets, the inbox-nil party,
    // and the settings' search index, loaded once the inbox has painted (InboxView.tsx,
    // App.tsx). The limit is a tripwire for unexpected growth of any chunk, set above the
    // biggest, and chunkLimit above makes it an error, so the frontend gate's build fails
    // on a chunk over it.
    chunkSizeWarningLimit: 750,
    rolldownOptions: {
      checks: {
        // Rolldown warns when plugins take most of a build. Here one always does, by
        // design: the React Compiler runs through Babel on every source file, and the
        // bundling itself is quick. node_modules is already left out of it (the Babel
        // plugin's default), so the notice would show on every build and say nothing new.
        bundlerTimings: false,
      },
      output: {
        // Most of the modules the first paint loads ($initial) that a lazily loaded chunk
        // imports too go into one chunk, shared, which the entry preloads: React, the
        // platform module, the shared controls, the card's parts. Rolldown still keeps some
        // of them in the entry itself, and a page imports those from it. Left to itself,
        // Rolldown puts such modules in common chunks by which chunks import them, then
        // merges into the entry each it can without a cycle between chunks, so one new
        // import (lib/tone from the account menu, once) could fold several into the entry
        // at once, 37 kB toward the chunk limit. Grouped, they still load before the first
        // paint and a page that imports one finds it loaded.
        codeSplitting: {
          groups: [{ name: 'shared', tags: ['$initial'], minShareCount: 2 }],
        },
      },
    },
  },
})
