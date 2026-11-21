import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { DownloadAnswer, RestartAnswer, RunGoing, UpdateStatus } from '../platform'
import { deferred } from '../test/async'
import { runGoing, scriptRun, updateLocation, updateReady, updateStatus } from '../test/fixtures'
import { platform, send } from '../test/platform'
import { finishingWords, settlingWords } from './update'
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

// settling is the words Go refuses with while the last update may still be finishing, and
// quitting those while a restart whose quit was held sees the app out: the window says
// each in its own (settlingWords, finishingWords). uninstallable is Go's refusal of an
// update that failed to install for good.
const settling = 'The last update is still finishing. Try again in a moment.'
const quitting = 'Example is quitting to finish the update.'
const uninstallable = '0.3.0 didn’t install before, and would fail the same way again.'

// refusedFor is Restart refusing to stop the runs going unasked, naming them (none when Go
// couldn't list them).
function refusedFor(runs: RunGoing[]): RestartAnswer {
  return { restarted: false, runsGoing: runs, message: '' }
}

describe('useUpdate', () => {
  it('reads the status, and reads it again on every change', async () => {
    const { result } = renderHook(() => useUpdate())

    expect(result.current.status).toBeNull()
    await waitFor(() => {
      expect(result.current.status?.state).toBe('idle')
    })

    platform.getUpdateStatus.mockResolvedValue(updateReady())
    send('updates:updated')

    await waitFor(() => {
      expect(result.current.status?.state).toBe('ready')
    })
  })

  it('shows only the latest read’s answer, never one a newer read replaced', async () => {
    const first = deferred<UpdateStatus>()
    platform.getUpdateStatus
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValueOnce(updateReady({ state: 'downloading', progress: 60 }))
    const { result } = renderHook(() => useUpdate())

    send('updates:updated')
    await waitFor(() => {
      expect(result.current.status?.progress).toBe(60)
    })

    await act(async () => {
      first.resolve(updateStatus())
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
  // Every restart below starts from 0.3.0 ready unless a test says otherwise.
  beforeEach(() => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
  })

  it('restarts at once when nothing would be lost', async () => {
    platform.restartToUpdateNow.mockReturnValue(deferred<RestartAnswer>().promise)
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
    expect(platform.showUpdateBanner).not.toHaveBeenCalled()
  })

  it('shows the banner Later hid again before it restarts, so how the restart ends shows', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ bannerHidden: true }))
    platform.restartToUpdateNow.mockReturnValue(deferred<RestartAnswer>().promise)
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    expect(platform.showUpdateBanner).toHaveBeenCalledOnce()
    expect(platform.showUpdateBanner.mock.invocationCallOrder[0]).toBeLessThan(
      platform.restartToUpdateNow.mock.invocationCallOrder[0] ?? 0,
    )
  })

  it('asks nothing while a restart whose quit was held sees the app out, text unsent or not', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'finishing' }))
    const { result } = flow(() => 1)

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'waiting', message: finishingWords })
    })
    expect(platform.listScriptRuns).not.toHaveBeenCalled()
    expect(platform.downloadUpdate).not.toHaveBeenCalled()
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
  })

  it('asks nothing and starts nothing while a restart is under way already', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'installing' }))
    const { result } = flow(() => 1)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'idle' })
    })
    expect(platform.listScriptRuns).not.toHaveBeenCalled()
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    // It holds nothing back: a later start reads the update again.
    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(platform.getUpdateStatus).toHaveBeenCalledTimes(2)
    })
  })

  it('readies a newer version a check found while Restart waited, and restarts into it, the answer standing', async () => {
    const notify = (overrides: Partial<UpdateStatus> = {}) => updateReady({ mode: 'notify', ...overrides })
    platform.getUpdateStatus.mockResolvedValue(notify())
    platform.listScriptRuns.mockResolvedValue([])
    // Restart waited for a check, which found 0.3.1, so nothing was ready to install.
    platform.restartToUpdateNow.mockImplementationOnce(() => {
      platform.getUpdateStatus.mockResolvedValue(notify({ state: 'available', latest: '0.3.1' }))

      return Promise.reject(failure('There’s no update ready to install yet.'))
    })
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
    platform.downloadUpdate.mockResolvedValue(got(notify({ latest: '0.3.1' })))
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
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false]])
    })
    expect(platform.downloadUpdate).toHaveBeenCalledOnce()
    // The text was asked about: it isn't asked about again for the newer version.
    expect(result.current.step).toEqual({ step: 'restarting' })
    expect(platform.listScriptRuns).toHaveBeenCalledOnce()
  })

  it('readies a newer version once, then says why the restart failed', async () => {
    platform.restartToUpdateNow.mockImplementationOnce(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', latest: '0.3.1' }))

      return Promise.reject(failure('There’s no update ready to install yet.'))
    })
    platform.restartToUpdateNow.mockImplementationOnce(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', latest: '0.3.2' }))

      return Promise.reject(failure('There’s no update ready to install yet.'))
    })
    platform.downloadUpdate.mockResolvedValue(got(updateReady({ latest: '0.3.1' })))
    const { result } = flow()

    act(() => {
      result.current.start()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: 'There’s no update ready to install yet.' })
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledTimes(2)
    expect(platform.downloadUpdate).toHaveBeenCalledOnce()
  })

  it('goes back to nothing under way once Restart returns without quitting', async () => {
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
    platform.restartToUpdateNow.mockReturnValue(deferred<RestartAnswer>().promise)
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
    const download = deferred<DownloadAnswer>()
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(download.promise)
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'downloading' })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    await act(async () => {
      download.resolve(got(updateReady()))
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
    platform.getUpdateStatus.mockResolvedValue(updateStatus())
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
    platform.getUpdateStatus.mockResolvedValue(updateStatus())
    const check = deferred<UpdateStatus>()
    platform.checkForUpdates.mockReturnValueOnce(check.promise)
    platform.checkForUpdates.mockResolvedValueOnce(updateStatus({ lastError: 'Couldn’t reach the releases.' }))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'checking' })
    })

    await act(async () => {
      check.resolve(updateStatus())
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
    platform.getUpdateStatus.mockResolvedValue(updateStatus())
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
    const elsewhere = updateReady({ location: updateLocation() })
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

  it('asks first while runs go, naming them, and restarts confirmed once Go names no other', async () => {
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
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
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false], [true]])
    })
    expect(result.current.step).toEqual({ step: 'restarting' })
  })

  it('asks again, naming it, about a run started while it asked, and stops it only once confirmed', async () => {
    const pull = runGoing({ id: 8, kind: 'gitPull', title: 'Pull main', repo: 'acme-org/api' })
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing(), pull]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing(), pull]))
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    // The pull started while the dialog named the release alone: confirming asks Go
    // first, which names both, so nothing stops yet.
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing(), pull], sheets: 0 } })
    })
    expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false]])

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false], [false], [true]])
    })
  })

  it('restarts unconfirmed when the runs it named ended while it asked', async () => {
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step.step).toBe('asking')
    })

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })
    expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false]])
  })

  it('stops the runs it asked about as a whole while Go still can’t list them, and asks once it can', async () => {
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([]))
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: null, sheets: 0 } })
    })

    // Go lists them now: the question named none by name, so it asks again, naming them.
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    // Go can't list them again: a question that named them can't tell a new one apart,
    // so it asks about the runs as a whole, which confirming then stops.
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: null, sheets: 0 } })
    })
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false], [false], [false], [true]])
    })
  })

  it('asks about the runs as a whole when Go couldn’t list them', async () => {
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
          runs: [runGoing({ dir: '~/projects/example-repo' })],
          sheets: 2,
        },
      })
    })
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '', withAgents: true })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()

    // Go names the release the question listed: confirmed, it stops it.
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [true]])
    })
  })

  it('asks again about a run started while it asked about the text and the runs it listed', async () => {
    const pull = runGoing({ id: 8, kind: 'gitPull', title: 'Pull main', repo: 'acme-org/api' })
    platform.listScriptRuns.mockResolvedValue([
      scriptRun({ id: 7, kind: 'release', title: 'Release v1.2.0', repo: 'acme-org/sdk', canStop: true }),
    ])
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing(), pull]))
    const { result } = flow(() => 1)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step.step).toBe('asking')
    })

    act(() => {
      result.current.confirm()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing(), pull], sheets: 1 } })
    })
    expect(platform.restartToUpdateNow).toHaveBeenCalledExactlyOnceWith(false)
  })

  it('counts the sheets once the download is done, so text written meanwhile counts', async () => {
    const download = deferred<DownloadAnswer>()
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(download.promise)
    const { result, rerender } = flow(() => 0)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'downloading' })
    })

    rerender({ count: () => 1 })
    await act(async () => {
      download.resolve(got(updateReady()))
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [], sheets: 1 } })
    })
  })

  it('asks about the text alone when the runs can’t be listed, and stops no run it didn’t name', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.listScriptRuns.mockRejectedValue(failure('The runs are busy.'))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockResolvedValueOnce(refusedFor([runGoing()]))
    platform.restartToUpdateNow.mockReturnValueOnce(deferred<RestartAnswer>().promise)
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
      expect(platform.restartToUpdateNow.mock.calls).toEqual([[false], [false], [true]])
    })
    expect(result.current.step).toEqual({ step: 'restarting' })
  })

  it('asks about a run started while it asked about the text alone', async () => {
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
    const restart = deferred<RestartAnswer>()
    platform.restartToUpdateNow.mockReturnValueOnce(restart.promise)
    const { result, rerender } = flow(() => 0)

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'restarting' })
    })

    rerender({ count: () => 1 })
    await act(async () => {
      restart.resolve({ restarted: false, runsGoing: [runGoing()], message: '' })
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 1 } })
    })
  })

  it('confirms nothing unless it asks', async () => {
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
    send('updates:updated')
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
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t install 0.3.0.'))
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step.step).toBe('failed')
    })

    send('updates:updated')
    expect(result.current.step).toEqual({ step: 'failed', message: 'Couldn’t install 0.3.0.' })
  })

  it('waits quietly while a restart whose quit was held sees the app out', async () => {
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, refusal: 'finishing', message: quitting })
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'waiting', message: finishingWords })
    })
  })

  it('fails with nothing to try again for an update that failed to install for good', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockResolvedValue({ downloaded: false, refusal: 'uninstallable', message: uninstallable })
    const { result } = flow()

    act(() => {
      result.current.start()
    })
    await waitFor(() => {
      expect(result.current.step).toEqual({ step: 'failed', message: uninstallable, final: true })
    })
    expect(platform.restartToUpdateNow).not.toHaveBeenCalled()
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
  // Every banner below starts from 0.3.0 ready unless a test says otherwise.
  beforeEach(() => {
    platform.getUpdateStatus.mockResolvedValue(updateReady())
  })

  it('shows the banner the status calls for', async () => {
    const { result } = control()

    expect(result.current.banner).toEqual({ kind: 'none' })
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
    })
    expect(result.current.flow).toEqual({ step: 'idle' })
  })

  it('leaves the banner’s word about where the app runs to the move dialog while it asks', async () => {
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', location: updateLocation() }))
    const { result } = control()

    // The dialog asks on launch, so the banner says nothing about moving meanwhile.
    await waitFor(() => {
      expect(result.current.move.dialog).not.toBeNull()
    })
    expect(result.current.banner).toEqual({ kind: 'none' })

    act(() => {
      result.current.move.dismiss()
    })

    expect(result.current.move.dialog).toBeNull()
    expect(result.current.banner).toEqual({ kind: 'location', version: '0.3.0' })
  })

  it('hides the banner at once on Later, and reads the status again', async () => {
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
    send('updates:updated')
    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
    })
  })

  it('shows the banner hidden with Later again once a restart is asked for', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
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
    send('updates:restart')
    await waitFor(() => {
      expect(result.current.banner.kind).toBe('failed')
    })
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
  })

  it('shows the banner the update hid for Later again once a restart is asked for', async () => {
    platform.hideUpdateBanner.mockImplementation(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady({ bannerHidden: true }))

      return Promise.resolve(updateReady({ bannerHidden: true }))
    })
    platform.showUpdateBanner.mockImplementation(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady())

      return Promise.resolve(updateReady())
    })
    platform.restartToUpdateNow.mockRejectedValue(failure('Couldn’t install 0.3.0: no space.'))
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

    send('updates:restart')
    await waitFor(() => {
      expect(result.current.banner.kind).toBe('failed')
    })
    expect(platform.showUpdateBanner).toHaveBeenCalledOnce()

    // The update tells the window it shows the banner again, as it does once it lifts Later.
    send('updates:updated')
    await waitFor(() => {
      expect(result.current.status?.bannerHidden).toBe(false)
    })
    act(() => {
      result.current.dismiss()
    })
    expect(result.current.banner).toEqual({ kind: 'ready', version: '0.3.0' })
  })

  it('hides a download’s banner on Later while it downloads, and shows the restart after', async () => {
    const download = deferred<DownloadAnswer>()
    platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'available', mode: 'notify' }))
    platform.downloadUpdate.mockReturnValue(download.promise)
    platform.restartToUpdateNow.mockReturnValue(deferred<RestartAnswer>().promise)
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
      download.resolve(got(updateReady()))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(result.current.banner).toMatchObject({ kind: 'working', text: 'Restarting into 0.3.0…' })
    })
  })

  it('keeps the banner hidden for the session when Later can’t be stored, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
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
    const failed = updateReady({
      installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: false, retryable: true },
    })
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
    const { result } = control()

    expect(result.current.offer.action).toBeNull()
    await waitFor(() => {
      expect(result.current.offer).toEqual({
        action: 'restart',
        label: 'Restart to update',
        page: '',
        move: false,
      })
    })
  })

  it('puts away the last launch’s failure for the session, never offering that version as ready', async () => {
    platform.getUpdateStatus.mockResolvedValue(
      updateReady({
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: false, retryable: true },
      }),
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
      updateReady({
        latest: '0.3.1',
        installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: false, retryable: true },
      }),
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
    platform.restartToUpdateNow.mockResolvedValue({ restarted: false, runsGoing: [runGoing()], message: '' })
    const { result } = control()

    send('updates:restart')

    await waitFor(() => {
      expect(result.current.flow).toEqual({ step: 'asking', question: { runs: [runGoing()], sheets: 0 } })
    })

    // Asked again while it asks, it does nothing.
    send('updates:restart')
    expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
  })

  it('says the update is finishing once a held quit’s restart returns, with nothing to click', async () => {
    platform.restartToUpdateNow.mockImplementation(() => {
      platform.getUpdateStatus.mockResolvedValue(updateReady({ state: 'finishing' }))

      return Promise.resolve({ restarted: true, status: updateReady({ state: 'finishing' }) })
    })
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.restart()
    })
    await waitFor(() => {
      expect(platform.restartToUpdateNow).toHaveBeenCalledOnce()
    })
    // The update tells the window it changed, as it does once it finishes.
    send('updates:updated')

    await waitFor(() => {
      expect(result.current.banner).toEqual({
        kind: 'working',
        text: 'Finishing the update…',
        progress: null,
        detail: `${brand.name} will reopen in a moment.`,
      })
    })
    expect(result.current.flow).toEqual({ step: 'idle' })
    expect(result.current.offer.action).toBeNull()
  })

  it('offers only the release page after a restart refused for an update that fails for good', async () => {
    platform.restartToUpdateNow.mockResolvedValue({
      restarted: false,
      refusal: 'uninstallable',
      message: uninstallable,
    })
    const { result } = control()

    await waitFor(() => {
      expect(result.current.banner.kind).toBe('ready')
    })
    act(() => {
      result.current.restart()
    })

    await waitFor(() => {
      expect(result.current.banner).toEqual({ kind: 'failed', message: uninstallable })
    })
    expect(result.current.offer).toEqual({
      action: null,
      label: '',
      page: 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0',
      move: false,
    })
  })
})
