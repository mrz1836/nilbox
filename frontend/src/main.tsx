import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import './index.css'
import { applyTextSize, textSizeFromURL } from './lib/textSize'
import { reportFirstPaint } from './platform'

const container = document.getElementById('root')
if (container === null) {
  throw new Error('index.html is missing the #root element')
}

// The desktop shell opens the page with the stored text size in its URL; showing it
// before the first render means the first frame is already at that size.
const textSize = textSizeFromURL(window.location.search)
if (textSize !== null) {
  applyTextSize(textSize)
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
