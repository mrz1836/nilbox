import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, onTestFinished, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { DownloadAnswer, RestartAnswer, UpdateStatus } from '../platform'
import { runGoing, scriptRun, updateReady, updateStatus } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { settlingWords } from './update'
import { useRestartToUpdate, useUpdate, useUpdateControl, waitFor as waitForSettling } from './useUpdate'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// failure is a call refused as the desktop runtime delivers it, with the backend's words.
function failure(message: string): Error {
  return new Error(message.toLowerCase(), { cause: { message } })
}

// got is a download that went through, with the status after it.
function got(status: UpdateStatus): DownloadAnswer {
  return { downloaded: true, status }
}

// settling is the words Go refuses with while the last update may still be finishing, which
// the window says in its own (settlingWords), and held those after a held quit.
const settling = 'The last update is still finishing. Try again in a moment.'
const held = 'Quit Example and open it again, then restart to update.'

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
    let downloaded: (answer: DownloadAnswer) => void = () => undefined
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
      downloaded(got(updateReady()))
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
    })
  })

  it('downloads again when a newer version turned up meanwhile, and waits for one downloading', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'downloading', progress: 10 }))
    platform.downloadUpdate
      .mockResolvedValueOnce(got(updateReady({ state: 'available', latest: '0.3.1' })))
      .mockResolvedValueOnce(got(updateReady({ latest: '0.3.1' })))
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
    let downloaded: (answer: DownloadAnswer) => void = () => undefined
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
      downloaded(got(updateReady()))
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [], sheets: 1 } })
    })
  })

  it('asks about the text alone when the runs can’t be listed, and stops no run it didn’t name', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.listScriptRuns.mockRejectedValue(failure('The runs are busy.'))
    platform.restartToUpdateNow.mockResolvedValueOnce({ restarted: false, runsGoing: [runGoing()], message: '' })
    platform.restartToUpdateNow.mockReturnValueOnce(pending())
    const { result, rerender } = flow(() => 1)

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

    // The question named no run, so confirming it lets Go refuse one going, which asks
    // again, naming it, with the sheets counted again.
    rerender({ count: () => 2 })
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 2 } })
    })
    expect(platform.restartToUpdateNow.mock.calls).toEqual([[false]])

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [true]])
  })

  it('asks about a run started while it asked about the text alone', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValueOnce({ restarted: false, runsGoing: [runGoing()], message: '' })
    const { result } = flow(() => 1)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [], sheets: 1 } })
    })

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 1 } })
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
  })

  it('counts the text written while Go backed up, when it then refuses for the runs going', async () => {
    let answered: (answer: RestartAnswer) => void = () => undefined
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockReturnValueOnce(
      new Promise<RestartAnswer>((resolve) => {
        answered = resolve
      }),
    )
    const { result, rerender } = flow(() => 0)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })

    rerender({ count: () => 1 })
    await act(async () => {
      answered({ restarted: false, runsGoing: [runGoing()], message: '' })
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 1 } })
    })
  })

  it('confirms nothing unless it asks', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = flow()

    act(() => {
      result.current.confirm()
    })
    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.step).toEqual({ step: 'idle' })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
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

  it('waits quietly while the last update is still finishing, until the update next changes', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, refusal: 'settling', message: settling })
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'waiting', message: settlingWords })
    })

    // Waiting is no failure: there is nothing to put away.
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.step).toEqual({ step: 'waiting', message: settlingWords })

    // The start settling says the update changed; the restart can go ahead again.
    act(() => {
      emit('updates:updated')
    })
    expect(result.current.step).toEqual({ step: 'idle' })

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledTimes(2)
    })
  })

  it('waits quietly for the download too, and offers the update again after a while', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    onTestFinished(() => {
      vi.useRealTimers()
    })
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockResolvedValue({ downloaded: false, refusal: 'settling', message: settling })
    const { result } = flow()

    await act(async () => {
      result.current.start()
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(result.current.step).toEqual({ step: 'waiting', message: settlingWords })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(waitForSettling - 1)
    })
    expect(result.current.step.step).toBe('waiting')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(result.current.step).toEqual({ step: 'idle' })
  })

  it('leaves a step that isn’t a wait as it is when the update changes', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t install 0.3.0.'))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step.step).toBe('failed')
    })

    act(() => {
      emit('updates:updated')
    })
    expect(result.current.step).toEqual({ step: 'failed', message: 'Couldn’t install 0.3.0.' })
  })

  it('says what to do after a held quit, for the rest of the run, with nothing to retry', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, refusal: 'reopen', message: held })
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'reopen', message: held })
    })

    act(() => {
      result.current.dismiss()
      emit('updates:updated')
    })
    expect(result.current.step).toEqual({ step: 'reopen', message: held })
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

  it('leaves the hiding to the update once it says so, so the banner comes back when its day is up', async () => {
    platform.getUpdateStatus.mockResolvedValueOnce(updateReady())
    platform.getUpdateStatus.mockResolvedValue(updateReady({ bannerHidden: true }))
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.later()
    })
    await waitFor(() => {
      expect(result.current.status?.bannerHidden).toBe(true)
    })
    expect(result.current.banner).toEqual({ kind: 'none' })

    // A day on, the update stops hiding it, and so does the window.
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    act(() => {
      emit('updates:updated')
    })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
    })
  })

  it('shows the banner hidden with Later again once a restart is asked for', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.hideUpdateBanner.mockRejectedValue(failure('Locked.'))
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t install 0.3.0: no space.'))
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.later()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })

    // The menu bar's Update to 0.3.0… runs with the banner showing.
    act(() => {
      emit('updates:restart')
    })
    await waitFor(() => {
      expect(result.current.banner.kind).toBe('failed')
    })
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
  })

  it('hides a download’s banner on Later while it downloads, and shows the restart after', async () => {
    let downloaded: (answer: DownloadAnswer) => void = () => undefined
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(
      new Promise<DownloadAnswer>((resolve) => {
        downloaded = resolve
      }),
    )
    platform.restartToUpdateNow.mockReturnValue(pending())
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('available')
    })
    act(() => {
      result.current.restart()
    })
    await waitFor(() => {
      expect(result.current.banner).toMatchObject({ kind: 'working', text: 'Downloading 0.3.0…' })
    })

    act(() => {
      result.current.later()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })
    expect(platform.hideUpdateBanner).not.toHaveBeenCalled()

    await act(async () => {
      downloaded(got(updateReady()))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(result.current.banner).toMatchObject({ kind: 'working', text: 'Restarting into 0.3.0…' })
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

  it('shows the banner again for a notification’s click, lifting Later, and says whether it shows anything', async () => {
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

  it('says whether the banner shows by the status it has when it can’t be shown again, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.showUpdateBanner.mockRejectedValue(failure('Locked.'))
    const { result } = control()

    let shows = true
    await act(async () => {
      shows = await result.current.reveal()
    })
    // Before the status loads, nothing shows.
    expect(shows).toBe(false)

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    await act(async () => {
      shows = await result.current.reveal()
    })

    expect(shows).toBe(true)
    expect(logged).toHaveBeenCalledWith('could not show the update’s banner:', 'Locked.')
  })

  it('says the banner shows nothing while Download it and tell me downloads quietly', async () => {
    const quiet = updateReady({ state: 'available', mode: 'download' })
    platform.getUpdateStatus.mockResolvedValue(quiet)
    platform.showUpdateBanner.mockResolvedValue(quiet)
    const { result } = control()

    await waitFor(() => {
      expect(result.current.status).not.toBeNull()
    })
    let shows = true
    await act(async () => {
      shows = await result.current.reveal()
    })

    expect(shows).toBe(false)
  })

  it('says the banner shows nothing for a failure put away, whose version is never offered as ready', async () => {
    const failed = updateReady({ installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: false } })
    platform.getUpdateStatus.mockResolvedValue(failed)
    platform.showUpdateBanner.mockResolvedValue(failed)
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('lastLaunch')
    })
    let shows = false
    await act(async () => {
      shows = await result.current.reveal()
    })
    expect(shows).toBe(true)

    act(() => {
      result.current.dismiss()
    })
    await act(async () => {
      shows = await result.current.reveal()
    })
    expect(shows).toBe(false)
  })

  it('offers what the banner and About offer', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    const { result } = control()

    expect(result.current.offer.action).toBeNull()
    await waitFor(() => {
      expect(result.current.offer).toEqual({
        action: 'restart',
        label: 'Restart to update',
        manual: false,
        move: false,
      })
    })
  })

  it('puts away the last launch’s failure for the session, never offering that version as ready', async () => {
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

    expect(result.current.banner).toEqual({ kind: 'none' })

    // With nothing to put away, it does nothing.
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })
  })

  it('puts away a failure of an older version, then offers the newer one ready', async () => {
    platform.getUpdateStatus.mockResolvedValue(
      updateReady({ latest: '0.3.1', installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: false } }),
    )
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('lastLaunch')
    })
    act(() => {
      result.current.dismiss()
    })

    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.1' })
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

  it('runs the same restart when the menu bar asks for one', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, runsGoing: [runGoing()], message: '' })
    const { result } = control()

    act(() => {
      emit('updates:restart')
    })

    await waitFor(() => {
      expect(result.current.flow).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    // Asked again while it asks, it does nothing.
    act(() => {
      emit('updates:restart')
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
  })

  it('shows what to do after a held quit, Later or not, and asks Go for no restart again', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, refusal: 'reopen', message: held })
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.restart()
    })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'reopen', message: held })
    })

    act(() => {
      result.current.later()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })

    // The menu bar's Restart to Update shows it again, and restarts nothing.
    act(() => {
      emit('updates:restart')
    })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'reopen', message: held })
    })
    expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
  })
})
