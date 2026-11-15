import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { LocationState, Settings } from '../../platform'
import { locationState, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { queryOf } from '../../test/query'
import { ClonesStep } from './ClonesStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

afterEach(() => {
  vi.unstubAllGlobals()
})

// Stateful holds the settings as the window does, taking each one a setter returns.
function Stateful({ states }: { states: LocationState[] | undefined }) {
  const [shown, setShown] = useState<Settings>(settings())
  const query = queryOf(states)

  return <ClonesStep settings={shown} onSaved={setShown} states={query} />
}

// noMotion asks for less motion, so the clone count shows at once.
function noMotion() {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
}

describe('ClonesStep', () => {
  it('counts the clones directly inside the clone root', () => {
    noMotion()
    render(<Stateful states={[locationState({ clones: 42 })]} />)

    expect(screen.getByTestId('clone-count')).toHaveTextContent('42')
    expect(screen.getByText('Found them. New clones go here too.')).toBeInTheDocument()
    expect(platform.suggestCloneRoots).not.toHaveBeenCalled()
  })

  it('counts up to the clones found, frame by frame', () => {
    const frames: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => frames.push(cb))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())

    render(<Stateful states={[locationState({ clones: 40 })]} />)

    const count = screen.getByTestId('clone-count')
    expect(count).toHaveTextContent('0')

    act(() => {
      frames.shift()?.(1000)
    })
    act(() => {
      frames.shift()?.(1350)
    })
    expect(Number(count.textContent)).toBeGreaterThan(0)
    expect(Number(count.textContent)).toBeLessThan(40)

    act(() => {
      frames.shift()?.(1700)
    })
    expect(count).toHaveTextContent('40')
  })

  it('waits for the clone root, then says what is wrong with one not found', () => {
    const { rerender } = render(<Stateful states={undefined} />)

    expect(screen.queryByTestId('clone-count')).not.toBeInTheDocument()

    rerender(<Stateful states={[locationState({ state: 'missing', clones: 0 })]} />)

    expect(screen.getByText('Not found')).toBeInTheDocument()
  })

  it('suggests the folders where clones live, to use as the clone root or add beside it', async () => {
    platform.suggestCloneRoots.mockResolvedValue([
      { path: '~/code', clones: 23 },
      { path: '~/src/acme-org', clones: 8 },
    ])
    render(<Stateful states={[locationState({ state: 'missing', clones: 0 })]} />)

    const found = within(await screen.findByRole('region', { name: 'Folders with clones' }))
    expect(found.getByText('~/code')).toBeInTheDocument()
    expect(found.getByText(/23 clones/)).toBeInTheDocument()

    await userEvent.click(found.getAllByRole('button', { name: 'Use as clone root' })[0] as HTMLElement)

    expect(platform.setCloneRoot).toHaveBeenCalledWith('~/code')
    expect(await screen.findByRole('status')).toHaveTextContent('~/code is your clone root.')

    await userEvent.click(found.getByRole('button', { name: 'Add ~/src/acme-org as a location' }))

    expect(platform.addCloneLocations).toHaveBeenCalledWith(['~/src/acme-org'])
    expect(await screen.findByRole('status')).toHaveTextContent('Added ~/src/acme-org beside the clone root.')
  })

  it('starts fresh in the clone root when no clones turn up', async () => {
    render(<Stateful states={[locationState({ state: 'missing', clones: 0 })]} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Start fresh in ~/projects' }))

    expect(platform.createCloneRoot).toHaveBeenCalledWith('~/projects')
    expect(await screen.findByRole('status')).toHaveTextContent('Made ~/projects for your clones.')
  })

  it('suggests folders for an empty clone root, but never offers to make it again', async () => {
    noMotion()
    platform.suggestCloneRoots.mockResolvedValue([])
    render(<Stateful states={[locationState({ clones: 0 })]} />)

    await waitFor(() => {
      expect(platform.suggestCloneRoots).toHaveBeenCalled()
    })
    expect(screen.queryByRole('button', { name: /Start fresh/ })).not.toBeInTheDocument()
    expect(screen.getByTestId('clone-count')).toHaveTextContent('0')
  })

  it('uses another folder chosen in the system’s dialog', async () => {
    noMotion()
    platform.chooseFolder.mockResolvedValue('/Volumes/work/repos')
    render(<Stateful states={[locationState()]} />)

    await userEvent.click(screen.getByRole('button', { name: 'Choose another folder…' }))

    expect(platform.setCloneRoot).toHaveBeenCalledWith('/Volumes/work/repos')
  })

  it('changes nothing when the dialog is cancelled', async () => {
    noMotion()
    render(<Stateful states={[locationState()]} />)

    await userEvent.click(screen.getByRole('button', { name: 'Choose another folder…' }))

    expect(platform.setCloneRoot).not.toHaveBeenCalled()
  })

  it('says why a change was refused', async () => {
    noMotion()
    platform.chooseFolder.mockResolvedValue('/nowhere')
    platform.setCloneRoot.mockRejectedValue(new Error('/nowhere doesn’t exist.'))
    render(<Stateful states={[locationState()]} />)

    await userEvent.click(screen.getByRole('button', { name: 'Choose another folder…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('/nowhere doesn’t exist.')
  })

  it('folds the other repository locations away', async () => {
    noMotion()
    render(<Stateful states={[locationState()]} />)

    await userEvent.click(screen.getByText('Clones somewhere else too?'))

    expect(screen.getByRole('button', { name: 'Choose…' })).toBeInTheDocument()
  })
})
