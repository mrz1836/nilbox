import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { MoveResult, UpdateLocationKind, UpdateStatus } from '../platform'
import { moveResult, updateLocation, updateReady, updateStatus } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { offerOf, type RestartStep } from './update'
import { useMoveToApplications } from './useMove'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// failure is a call refused as the desktop runtime delivers it, with the backend's words.
function failure(message: string): Error {
  return new Error(message.toLowerCase(), { cause: { message } })
}

// held is a call that waits until released, then answers with result.
function held(result: MoveResult) {
  const gate: { open?: () => void } = {}
  const call = new Promise<MoveResult>((resolve) => {
    gate.open = () => {
      resolve(result)
    }
  })

  return {
    call,
    release: () => {
      gate.open?.()
    },
  }
}

// idle is the restart with nothing under way.
const idle: RestartStep = { step: 'idle' }

// elsewhere is a release build in Downloads, which moving lets update.
const elsewhere = updateStatus({ location: updateLocation() })

// move renders the move for a status, which it can change, offered as the update's offer
// (offerOf) says, as useUpdateControl hands it.
function move(status: UpdateStatus | null = elsewhere) {
  return renderHook((s: UpdateStatus | null) => useMoveToApplications(offerOf(s, idle), s?.location ?? null), {
    initialProps: status,
  })
}

describe('useMoveToApplications', () => {
  it('asks on launch wherever moving lets a release or rehearsal build update', () => {
    for (const kind of ['elsewhere', 'translocated', 'diskImage'] as const) {
      for (const flavor of ['release', 'rehearsal'] as const) {
        const { result, unmount } = move(updateStatus({ flavor, location: updateLocation(kind) }))

        expect(result.current.dialog).toEqual({ variant: 'move', work: null, leaving: false, error: '' })
        unmount()
      }
    }
  })

  it('never asks where the offer has no move: in Applications, in a build from source, during a restart, or before the status loads', () => {
    const quiet: [string, UpdateStatus | null][] = [
      ['loading', null],
      ['in Applications', updateStatus()],
      ...(['applications', 'userApplications'] as UpdateLocationKind[]).map((kind): [string, UpdateStatus] => [
        kind,
        updateStatus({ location: updateLocation(kind) }),
      ]),
      [
        'a source build',
        updateStatus({ flavor: 'source', enabled: false, location: { ...updateLocation(), movable: false } }),
      ],
      ['a build that updates nothing', updateStatus({ enabled: false, location: updateLocation() })],
      ['a restart finishing', updateReady({ state: 'finishing', location: updateLocation() })],
    ]

    for (const [name, status] of quiet) {
      const { result, unmount } = move(status)

      expect({ name, dialog: result.current.dialog }).toEqual({ name, dialog: null })
      unmount()
    }
  })

  it('asks once the status loads', () => {
    const { result, rerender } = move(null)

    expect(result.current.dialog).toBeNull()
    rerender(elsewhere)
    expect(result.current.dialog?.variant).toBe('move')
    expect(result.current.location).toEqual(elsewhere.location)
  })

  it('moves once, showing the work, and stays working while the app quits for the copy', async () => {
    const answer = held(moveResult())
    platform.moveToApplications.mockReturnValue(answer.call)
    const { result } = move()

    act(() => {
      result.current.confirm()
    })
    expect(result.current.dialog).toEqual({ variant: 'move', work: 'moving', leaving: false, error: '' })

    // A second click, or the banner's button, starts nothing more.
    act(() => {
      result.current.confirm()
      result.current.move()
      result.current.dismiss()
    })
    expect(platform.moveToApplications).toHaveBeenCalledOnce()

    await act(async () => {
      answer.release()
      await answer.call
    })

    expect(result.current.dialog).toEqual({ variant: 'move', work: 'moving', leaving: true, error: '' })

    act(() => {
      result.current.confirm()
      result.current.dismiss()
    })
    expect(platform.moveToApplications).toHaveBeenCalledOnce()
    expect(result.current.dialog).not.toBeNull()
  })

  it('offers to open a copy already in Applications, and opens it', async () => {
    platform.moveToApplications.mockResolvedValue(moveResult({ outcome: 'exists' }))
    const { result } = move()

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog).toEqual({ variant: 'exists', work: null, leaving: false, error: '' })
    })

    act(() => {
      result.current.confirm()
    })
    expect(result.current.dialog?.work).toBe('opening')
    await waitFor(() => {
      expect(result.current.dialog).toEqual({ variant: 'exists', work: 'opening', leaving: true, error: '' })
    })
    expect(platform.openInApplications).toHaveBeenCalledOnce()
    expect(platform.moveToApplications).toHaveBeenCalledOnce()
  })

  it('shows a failure’s words, changes nothing, and lets the user try again', async () => {
    platform.moveToApplications.mockRejectedValueOnce(
      failure('/Applications already has a file named Example.app, so nothing was moved.'),
    )
    const { result } = move()

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog).toEqual({
        variant: 'move',
        work: null,
        leaving: false,
        error: '/Applications already has a file named Example.app, so nothing was moved.',
      })
    })

    act(() => {
      result.current.confirm()
    })
    expect(result.current.dialog?.error).toBe('')
    await waitFor(() => {
      expect(result.current.dialog?.leaving).toBe(true)
    })
    expect(platform.moveToApplications).toHaveBeenCalledTimes(2)
  })

  it('shows Open it’s failure, turning the question back to the move its words say to make', async () => {
    platform.moveToApplications.mockResolvedValue(moveResult({ outcome: 'exists' }))
    platform.openInApplications.mockRejectedValue(failure('The copy in /Applications didn’t pass its signature check.'))
    const { result } = move()

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog?.variant).toBe('exists')
    })
    act(() => {
      result.current.confirm()
    })

    await waitFor(() => {
      expect(result.current.dialog).toEqual({
        variant: 'move',
        work: null,
        leaving: false,
        error: 'The copy in /Applications didn’t pass its signature check.',
      })
    })

    // Moving again looks again: a copy still there is offered again.
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog).toEqual({ variant: 'exists', work: null, leaving: false, error: '' })
    })
    expect(platform.moveToApplications).toHaveBeenCalledTimes(2)
  })

  it('puts the question away for this launch on Not now, and the banner and About move at once', async () => {
    platform.moveToApplications.mockResolvedValueOnce(moveResult({ outcome: 'exists' }))
    const { result, rerender } = move()

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog?.variant).toBe('exists')
    })

    act(() => {
      result.current.dismiss()
    })
    expect(result.current.dialog).toBeNull()

    // A status read again never asks again in this launch.
    rerender({ ...elsewhere })
    expect(result.current.dialog).toBeNull()

    act(() => {
      result.current.move()
    })
    expect(result.current.dialog).toEqual({ variant: 'move', work: 'moving', leaving: false, error: '' })
    await waitFor(() => {
      expect(result.current.dialog?.leaving).toBe(true)
    })
    expect(platform.moveToApplications).toHaveBeenCalledTimes(2)
  })

  it('gives way to the quit’s own question, and moves again from the start', async () => {
    const { result } = move()

    // Asked before anything went ahead, the quit's question changes nothing here.
    act(() => {
      emit('app:quitRequested')
    })
    expect(result.current.dialog?.variant).toBe('move')

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.dialog?.leaving).toBe(true)
    })

    act(() => {
      emit('app:quitRequested')
    })
    expect(result.current.dialog).toBeNull()

    platform.moveToApplications.mockResolvedValueOnce(moveResult({ outcome: 'exists' }))
    act(() => {
      result.current.move()
    })
    await waitFor(() => {
      expect(result.current.dialog).toEqual({ variant: 'exists', work: null, leaving: false, error: '' })
    })
  })

  it('gives way to the quit’s question even when it comes before the move’s answer', async () => {
    const answer = held(moveResult())
    platform.moveToApplications.mockReturnValue(answer.call)
    const { result } = move()

    act(() => {
      result.current.confirm()
    })
    act(() => {
      emit('app:quitRequested')
    })
    expect(result.current.dialog?.work).toBe('moving')

    await act(async () => {
      answer.release()
      await answer.call
    })

    expect(result.current.dialog).toBeNull()

    // A move started afresh is not closed by the question asked before it.
    act(() => {
      result.current.move()
    })
    await waitFor(() => {
      expect(result.current.dialog?.leaving).toBe(true)
    })
  })

  it('shows the copy the user opened in Finder, and says why when it can’t', async () => {
    const { result } = move()

    act(() => {
      result.current.reveal()
    })
    await waitFor(() => {
      expect(platform.showAppInFinder).toHaveBeenCalledOnce()
    })
    expect(result.current.dialog?.error).toBe('')

    platform.showAppInFinder.mockRejectedValue(failure('Finder couldn’t show the file: gone.'))
    act(() => {
      result.current.reveal()
    })
    await waitFor(() => {
      expect(result.current.dialog?.error).toBe('Finder couldn’t show the file: gone.')
    })
  })
})
