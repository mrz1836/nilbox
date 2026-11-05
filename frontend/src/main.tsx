import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import './index.css'
import { followTheme } from './lib/theme'
import { reportFirstPaint } from './platform'

const container = document.getElementById('root')
if (container === null) {
  throw new Error('index.html is missing the #root element')
}

// The stylesheet already matches the system appearance; this pins it on <html> before
// the first render and keeps following system changes for the life of the window.
followTheme('system')

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// The first animation frame runs before the browser paints the rendered tree, and the
// second after it has, so the report marks the first frame actually on screen.
requestAnimationFrame(() => {
  requestAnimationFrame(() => {
    reportFirstPaint().catch((err: unknown) => {
      console.error('could not report the first paint', err)
    })
  })
})
