import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import './index.css'
import { reportFirstPaint } from './platform'

const container = document.getElementById('root')
if (container === null) {
  throw new Error('index.html is missing the #root element')
}

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
