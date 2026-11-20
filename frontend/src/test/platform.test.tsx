import { render, screen } from '@testing-library/react'
import { useEffect, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { on } from '../platform'
import { listening, send } from './platform'

vi.mock('../platform', () => import('./platform').then((fake) => fake.platform))

// Progress shows how many inbox changes it heard of, and the last progress the shell sent.
function Progress() {
  const [updates, setUpdates] = useState(0)
  const [phase, setPhase] = useState('none')

  useEffect(() => {
    const stops = [
      on('inbox:updated', () => {
        setUpdates((n) => n + 1)
      }),
      on('inbox:progress', (progress) => {
        setPhase(`${progress.phase} ${String(progress.done)}/${String(progress.total)}`)
      }),
    ]

    return () => {
      for (const stop of stops) {
        stop()
      }
    }
  }, [])

  return (
    <p>
      {updates} updates, {phase}
    </p>
  )
}

describe('send', () => {
  it('sends an event inside act, so what it changes shows at once', () => {
    render(<Progress />)

    send('inbox:updated')
    send('inbox:updated')
    send('inbox:progress', { phase: 'details', done: 3, total: 8 })

    expect(screen.getByText('2 updates, details 3/8')).toBeInTheDocument()
  })

  it('fails when nothing listens for the event', () => {
    const { unmount } = render(<Progress />)
    unmount()

    expect(listening('inbox:updated')).toBe(0)
    expect(() => {
      send('inbox:updated')
    }).toThrow('nothing listens for inbox:updated')
  })
})
