import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { InspectorState } from '../../platform'
import { platform } from '../../test/platform'
import { AboutSection } from './AboutSection'
import { InspectorCard } from './InspectorCard'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// withInspector gives the window an inspector in state, as a build from source's agent
// does, and returns it and set, which changes its state as the app's word would.
function withInspector(state: InspectorState) {
  let current = state
  const watchers = new Set<() => void>()
  const inspector = {
    state: () => current,
    subscribe: (watcher: () => void) => {
      watchers.add(watcher)

      return () => {
        watchers.delete(watcher)
      }
    },
    setEnabled: vi.fn(),
    clearNotes: vi.fn(),
  }

  platform.inspector.mockReturnValue(inspector)

  const set = (next: Partial<InspectorState>) => {
    current = { ...current, ...next }

    act(() => {
      for (const watcher of watchers) {
        watcher()
      }
    })
  }

  return { inspector, set }
}

const state = (over: Partial<InspectorState> = {}): InspectorState => ({
  enabled: true,
  pointing: false,
  open: 2,
  kept: 3,
  ...over,
})

describe('InspectorCard', () => {
  it('shows nothing in a build without the inspector, as a release', () => {
    const { container } = render(<InspectorCard />)

    expect(container).toBeEmptyDOMElement()
  })

  it('shows in About only where the window has the inspector', () => {
    render(<AboutSection />)
    expect(screen.queryByRole('region', { name: 'Inspector' })).not.toBeInTheDocument()

    withInspector(state())
    render(<AboutSection />)
    expect(screen.getByRole('region', { name: 'Inspector' })).toBeInTheDocument()
  })

  it('turns pointing on and off, and follows the inspector as it changes', async () => {
    const { inspector, set } = withInspector(state())
    render(<InspectorCard />)

    const pointing = screen.getByRole('switch', { name: 'Point at elements' })
    expect(pointing).toBeChecked()

    await userEvent.click(pointing)
    expect(inspector.setEnabled).toHaveBeenCalledWith(false)

    set({ enabled: false })
    expect(pointing).not.toBeChecked()
  })

  it('says how many notes are open and done, and clears them', async () => {
    const { inspector, set } = withInspector(state())
    render(<InspectorCard />)

    expect(screen.getByText(/^2 open, 1 done\./)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Clear notes' }))
    expect(inspector.clearNotes).toHaveBeenCalledOnce()

    set({ open: 1, kept: 1 })
    expect(screen.getByText(/^1 open\. Notes are kept/)).toBeInTheDocument()

    set({ open: 0, kept: 0 })
    expect(screen.getByText(/^None yet\./)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear notes' })).toBeDisabled()
  })
})
