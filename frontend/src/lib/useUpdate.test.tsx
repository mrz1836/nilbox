import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { UpdateStatus } from '../platform'
import { runGoing, scriptRun, updateReady, updateStatus } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { useRestartToUpdate, useUpdate, useUpdateControl } from './useUpdate'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// failure is a call refused as the desktop runtime delivers it, with the backend's words.
function failure(message: string): Error {
  return new Error(message.toLowerCase(), { cause: { message } })
}

// pending is a call that never answers, so a step stays under way.
function pending<T>(): Promise<T> {
  return new Promise<T>(() => undefined)
}

describe('useUpdate', () => {
  it('reads the status, and reads it again on every change', async () => {
    const { result } = renderHook(() => useUpdate())

    expect(result.current.status).toBeNull()
    await waitFor(() => {
      expect(result.current.status?.state).toBe('idle')
    })

    platform.getUpdateStatus.mockResolvedValue(updateReady())
    act(() => {
      emit('updates:updated')
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('ready')
    })
  })

  it('shows only the latest read’s answer, never one a newer read replaced', async () => {
    let answerFirst: (status: UpdateStatus) => void = () => undefined
    platform.getUpdateStatus
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            answerFirst = resolve
          }),
      )
      .mockResolvedValueOnce(updateReady({ state: 'downloading', progress: 60 }))
    const { result } = renderHook(() => useUpdate())

    act(() => {
      emit('updates:updated')
    })
    await waitFor(() => {
      expect(result.current.status?.progress).toBe(60)
    })

    await act(async () => {
      answerFirst(updateStatus())
      await Promise.resolve()
    })

    expect(result.current.status?.progress).toBe(60)
  })
})

// flow renders the restart with unsent sheets counted by unsent.
function flow(unsent: () => number = () => 0) {
  return renderHook(({ count }) => useRestartToUpdate(count), { initialProps: { count: unsent } })
}

describe('useRestartToUpdate', () => {
  it('restarts at once when nothing would be lost', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockReturnValue(pending())
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
    expect(platform.downloadUpdate).not.toHaveBeenCalled()
    expect(platform.listScriptRuns).not.toHaveBeenCalled()
  })

  it('goes back to nothing under way once Restart returns without quitting', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'idle' })
    })
  })

  it('does nothing for a second start while one is under way', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockReturnValue(pending())
    const { result } = flow()

    act(() => {
      result.current.start()
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    act(() => {
      result.current.start()
    })

    expect(platform.getUpdateStatus).toHaveBeenCalledOnce()
    expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
  })

  it('downloads a version only found first, showing it downloads, then restarts', async () => {
    let downloaded: (status: UpdateStatus) => void = () => undefined
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(
      new Promise((resolve) => {
        downloaded = resolve
      }),
    )
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'downloading' })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    await act(async () => {
      downloaded(updateReady())
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
    })
  })

  it('downloads again when a newer version turned up meanwhile, and waits for one downloading', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'downloading', progress: 10 }))
    platform.downloadUpdate
      .mockResolvedValueOnce(updateReady({ state: 'available', latest: '0.3.1' }))
      .mockResolvedValueOnce(updateReady({ latest: '0.3.1' }))
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
    })
    expect(platform.downloadUpdate).toHaveBeenCalledTimes(2)
  })

  it('says why a download failed, and starts again from Try again', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockRejectedValueOnce(failure('Couldn’t download 0.3.0: it stalled.'))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'Couldn’t download 0.3.0: it stalled.' })
    })

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
    })
  })

  it('checks first when no newer version is known, as a Try again after a failed install may find', async () => {
    platform.checkForUpdates.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
    })
    expect(platform.checkForUpdates).toHaveBeenCalledOnce()
    expect(platform.downloadUpdate).toHaveBeenCalledOnce()
  })

  it('shows the check under way, and says what it found when it finds nothing', async () => {
    let checked: (status: UpdateStatus) => void = () => undefined
    platform.checkForUpdates.mockReturnValueOnce(
      new Promise((resolve) => {
        checked = resolve
      }),
    )
    platform.checkForUpdates.mockResolvedValueOnce(updateStatus({ lastError: 'Couldn’t reach the releases.' }))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'checking' })
    })

    await act(async () => {
      checked(updateStatus())
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: `No newer version of ${brand.name} is out.` })
    })

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'Couldn’t reach the releases.' })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
  })

  it('says why the check, the status, or a build that doesn’t update stopped it', async () => {
    platform.checkForUpdates.mockRejectedValueOnce(failure('This build can’t check.'))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'This build can’t check.' })
    })

    platform.getUpdateStatus.mockRejectedValueOnce(failure('The app is closing.'))
    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'The app is closing.' })
    })

    platform.getUpdateStatus.mockResolvedValueOnce(
      updateStatus({ enabled: false, state: 'off', off: 'Updates are off in builds made from source.' }),
    )
    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'Updates are off in builds made from source.' })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
  })

  it('restarts nothing where the app can’t update, showing the banner that says why again', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const elsewhere = updateReady({ location: { ok: false, kind: 'elsewhere', target: '/Applications/Example.app' } })
    platform.getUpdateStatus.mockResolvedValue(elsewhere)
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'idle' })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
    expect(logged).not.toHaveBeenCalled()

    // A banner that can't be shown again is logged; nothing restarts either way.
    platform.showUpdateBanner.mockRejectedValueOnce(failure('No.'))
    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(logged).toHaveBeenCalledExactlyOnceWith('could not show the update’s banner:', 'No.')
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
  })

  it('asks first while runs go, naming them, and restarts confirmed', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValueOnce({ restarted: false, runsGoing: [runGoing()], message: '' })
    platform.restartToUpdateNow.mockReturnValueOnce(pending())
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    act(() => {
      result.current.confirm()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [true]])
  })

  it('asks about the runs as a whole when Go couldn’t list them', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValueOnce({ restarted: false, runsGoing: [], message: '' })
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: null, sheets: 0 } })
    })
  })

  it('asks first while sheets hold unsent text, naming the runs going too, without asking Go', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.listScriptRuns.mockResolvedValue([
      scriptRun({ id: 7, kind: 'release', title: 'Release v1.2.0', repo: 'acme-org/sdk', canStop: true }),
      scriptRun({ id: 8 }),
    ])
    const { result } = flow(() => 2)

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({
        step: 'asking',
        question: {
          runs: [
            {
              id: 7,
              kind: 'release',
              title: 'Release v1.2.0',
              repo: 'acme-org/sdk',
              dir: '~/projects/example-repo',
              stopRefused: '',
            },
          ],
          sheets: 2,
        },
      })
    })
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '' })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(true)
    })
  })

  it('counts the sheets once the download is done, so text written meanwhile counts', async () => {
    let downloaded: (status: UpdateStatus) => void = () => undefined
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(
      new Promise((resolve) => {
        downloaded = resolve
      }),
    )
    const { result, rerender } = flow(() => 0)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'downloading' })
    })

    rerender({ count: () => 1 })
    await act(async () => {
      downloaded(updateReady())
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [], sheets: 1 } })
    })
  })

  it('asks about the text alone when the runs can’t be listed, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.listScriptRuns.mockRejectedValue(failure('The runs are busy.'))
    const { result } = flow(() => 1)

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [], sheets: 1 } })
    })
    expect(logged).toHaveBeenCalledExactlyOnceWith(
      'could not list the runs a restart would stop:',
      'The runs are busy.',
    )
  })

  it('leaves everything as it was on Not now, and can start again', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, runsGoing: [runGoing()], message: '' })
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step.step).toBe('asking')
    })

    act(() => {
      result.current.cancel()
    })
    expect(result.current.step).toEqual({ step: 'idle' })

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledTimes(2)
    })
  })

  it('shows why a restart failed until it is put away', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t back up your data first: the disk is full.'))
    const { result } = flow()

    // Nothing to put away yet.
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.step).toEqual({ step: 'idle' })

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({
        step: 'failed',
        message: 'Couldn’t back up your data first: the disk is full.',
      })
    })

    act(() => {
      result.current.dismiss()
    })
    expect(result.current.step).toEqual({ step: 'idle' })
  })

  it('runs the same restart when the menu bar asks for one', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, runsGoing: [runGoing()], message: '' })
    const { result } = flow()

    act(() => {
      emit('updates:restart')
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    // Asked again while it asks, it does nothing.
    act(() => {
      emit('updates:restart')
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
  })
})

// control renders the window's update as App holds it.
function control(unsent: () => number = () => 0) {
  return renderHook(() => {
    const update = useUpdate()
    const restart = useRestartToUpdate(unsent)

    return useUpdateControl(restart, update)
  })
}

describe('useUpdateControl', () => {
  it('shows the banner the status calls for', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = control()

    expect(result.current.banner).toEqual({ kind: 'none' })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
    })
    expect(result.current.flow).toEqual({ step: 'idle' })
  })

  it('hides the banner at once on Later, and reads the status again', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })

    act(() => {
      result.current.later()
    })

    expect(result.current.banner).toEqual({ kind: 'none' })
    await waitFor(() => {
      expect(platform.hideUpdateBanner).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(platform.getUpdateStatus).toHaveBeenCalledTimes(2)
    })
  })

  it('keeps the banner hidden for the session when Later can’t be stored, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.hideUpdateBanner.mockRejectedValue(failure('Locked.'))
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.later()
    })

    await waitFor(() => {
      expect(logged).toHaveBeenCalledExactlyOnceWith('could not hide the update’s banner:', 'Locked.')
    })
    expect(result.current.banner).toEqual({ kind: 'none' })
  })

  it('shows the banner again for a notification’s click, lifting Later, and says whether an update is on its way', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.later()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })

    let pendingUpdate = false
    await act(async () => {
      pendingUpdate = await result.current.reveal()
    })

    expect(pendingUpdate).toBe(true)
    expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })

    platform.showUpdateBanner.mockResolvedValueOnce(updateStatus())
    await act(async () => {
      pendingUpdate = await result.current.reveal()
    })
    expect(pendingUpdate).toBe(false)
  })

  it('counts on an update when the banner can’t be shown again, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.showUpdateBanner.mockRejectedValue(failure('Locked.'))
    const { result } = control()

    let pendingUpdate = false
    await act(async () => {
      pendingUpdate = await result.current.reveal()
    })

    expect(pendingUpdate).toBe(true)
    expect(logged).toHaveBeenCalledExactlyOnceWith('could not show the update’s banner:', 'Locked.')
  })

  it('puts away the last launch’s failure for the session, then shows the update ready', async () => {
    platform.getUpdateStatus.mockResolvedValue(
      updateReady({ installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: false } }),
    )
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('lastLaunch')
    })
    act(() => {
      result.current.dismiss()
    })

    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })

    // With nothing to put away, it does nothing.
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
  })

  it('puts away a restart’s failure, showing the banner as it was', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t install 0.3.0: no space.'))
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.restart()
    })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'failed', message: 'Couldn’t install 0.3.0: no space.' })
    })

    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
  })
})
