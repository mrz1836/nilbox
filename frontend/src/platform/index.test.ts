import { CancellablePromise } from '@wailsio/runtime/cancellable'
import { describe, expect, it, vi } from 'vitest'

import { routineSpec } from '../test/fixtures'
import * as platform from './index'

// The bound calls the platform hands out as generated need no test of their own; these
// cover the calls the platform adds something to.
const mocks = vi.hoisted(() => ({
  inbox: { CheckFiles: vi.fn(), ItemsByID: vi.fn() },
  settings: {
    SetWindowSize: vi.fn(),
    SetUpdateMode: vi.fn(),
    SetAutopilotEnabled: vi.fn(),
    SetAutopilotPaused: vi.fn(),
    SetAutopilotDailyCap: vi.fn(),
  },
  autopilot: {
    Glance: vi.fn(),
    RoutineNames: vi.fn(),
    List: vi.fn(),
    Duplicate: vi.fn(),
    Delete: vi.fn(),
    SetEnabled: vi.fn(),
    SetMode: vi.fn(),
    Matching: vi.fn(),
    Watching: vi.fn(),
    Engine: vi.fn(),
    Activity: vi.fn(),
    ActivityFrom: vi.fn(),
    ItemHistory: vi.fn(),
    LatestMatches: vi.fn(),
    Undo: vi.fn(),
    TryAgain: vi.fn(),
    LetActAgain: vi.fn(),
    ClearHistory: vi.fn(),
    Approve: vi.fn(),
    ApproveBacklog: vi.fn(),
    SkipBacklog: vi.fn(),
    PostAsComment: vi.fn(),
    Skip: vi.fn(),
    RunNow: vi.fn(),
    PreviewActions: vi.fn(),
    Catalog: vi.fn(),
    Suggest: vi.fn(),
    Describe: vi.fn(),
    Test: vi.fn(),
    Save: vi.fn(),
  },
  scripts: { Run: vi.fn(), Repos: vi.fn(), List: vi.fn() },
  update: {
    Status: vi.fn(),
    CheckNow: vi.fn(),
    Download: vi.fn(),
    Restart: vi.fn(),
    HideBanner: vi.fn(),
    ShowBanner: vi.fn(),
    AckUpdated: vi.fn(),
    ShowInstallLog: vi.fn(),
    MoveToApplications: vi.fn(),
    OpenInApplications: vi.fn(),
    ShowAppInFinder: vi.fn(),
  },
  events: { On: vi.fn<(name: string, callback: (event: { data: unknown }) => void) => () => void>() },
  browser: { OpenURL: vi.fn() },
  clipboard: { SetText: vi.fn() },
  window: { SetSize: vi.fn() },
  dialogs: { OpenFile: vi.fn(), SaveFile: vi.fn() },
}))

// Create stands in for the runtime's registry of event data creators, which the generated
// event bindings freeze when they load.
vi.mock('@wailsio/runtime', () => ({
  Events: mocks.events,
  Browser: mocks.browser,
  Clipboard: mocks.clipboard,
  Window: mocks.window,
  Dialogs: mocks.dialogs,
  Create: { Events: {} },
}))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/inbox', () => ({ Service: mocks.inbox }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/autopilot', () => ({ Service: mocks.autopilot }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/settings', () => ({ Service: mocks.settings }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/scripts', () => ({ Service: mocks.scripts }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/update', () => ({ Service: mocks.update }))

const url = 'https://github.com/acme-org/example-repo/pull/7'

describe('on', () => {
  it('passes each event’s data on, and returns the function that stops listening', () => {
    const unsubscribe = vi.fn()
    let handler: (event: { data: unknown }) => void = () => undefined
    mocks.events.On.mockImplementation((_name, callback) => {
      handler = callback

      return unsubscribe
    })
    const listener = vi.fn()

    const stop = platform.on('inbox:reveal', listener)

    expect(mocks.events.On).toHaveBeenCalledExactlyOnceWith('inbox:reveal', expect.any(Function))
    handler({ data: { itemId: 7, url } })
    handler({ data: { itemId: 0, url } })

    expect(listener.mock.calls).toEqual([[{ itemId: 7, url }], [{ itemId: 0, url }]])

    stop()
    expect(unsubscribe).toHaveBeenCalledOnce()
  })
})

describe('openExternal', () => {
  it('opens the URL in the browser', () => {
    mocks.browser.OpenURL.mockResolvedValue(undefined)

    platform.openExternal(url)

    expect(mocks.browser.OpenURL).toHaveBeenCalledExactlyOnceWith(url)
  })

  it('logs a URL the browser could not open', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const failure = new Error('no browser')
    mocks.browser.OpenURL.mockRejectedValue(failure)

    platform.openExternal(url)

    await vi.waitFor(() => {
      expect(logged).toHaveBeenCalledExactlyOnceWith(`could not open ${url} in the browser`, failure)
    })
  })
})

describe('copyText', () => {
  it('puts the text on the clipboard', async () => {
    mocks.clipboard.SetText.mockResolvedValue(undefined)

    await platform.copyText('gh auth refresh -s read:project')

    expect(mocks.clipboard.SetText).toHaveBeenCalledExactlyOnceWith('gh auth refresh -s read:project')
  })

  it('rejects when the clipboard refuses the text', async () => {
    const failure = new Error('no clipboard')
    mocks.clipboard.SetText.mockRejectedValue(failure)

    await expect(platform.copyText('gh auth refresh -s read:project')).rejects.toBe(failure)
  })
})

describe('checkItemFiles', () => {
  const check: platform.FileCheck = {
    status: 'clear',
    head: 'abc',
    checked: 2,
    total: 2,
    complete: true,
    groups: [],
    checkedAt: '2026-06-15T12:00:00Z',
    error: '',
  }

  it('returns the check, and fails for an item whose files are not checked', async () => {
    mocks.inbox.CheckFiles.mockResolvedValueOnce(check).mockResolvedValueOnce(null)

    await expect(platform.checkItemFiles(4)).resolves.toBe(check)
    await expect(platform.checkItemFiles(5)).rejects.toThrow('not checked')
    expect(mocks.inbox.CheckFiles).toHaveBeenCalledWith(4)
  })

  it('cancels the bound call when the caller cancels', async () => {
    const cancelled = vi.fn()
    mocks.inbox.CheckFiles.mockReturnValue(new CancellablePromise<platform.FileCheck>(() => undefined, cancelled))

    await platform.checkItemFiles(4).cancel()

    expect(cancelled).toHaveBeenCalledOnce()
  })
})

describe('setWindowSize', () => {
  const settings = { windowSize: { width: 1440, height: 900 } }

  it('stores the window size, then resizes the open window', async () => {
    const calls: string[] = []
    mocks.settings.SetWindowSize.mockImplementation(() => {
      calls.push('store')
      return Promise.resolve(settings)
    })
    mocks.window.SetSize.mockImplementation(() => {
      calls.push('resize')
      return Promise.resolve()
    })

    await expect(platform.setWindowSize({ width: 1440, height: 900 })).resolves.toBe(settings)

    expect(mocks.settings.SetWindowSize).toHaveBeenCalledWith({ width: 1440, height: 900 })
    expect(mocks.window.SetSize).toHaveBeenCalledWith(1440, 900)
    expect(calls).toEqual(['store', 'resize'])
  })

  it('leaves the window alone when the size is refused', async () => {
    mocks.settings.SetWindowSize.mockRejectedValue(new Error('invalid window size'))

    await expect(platform.setWindowSize({ width: 100, height: 100 })).rejects.toThrow('invalid window size')
    expect(mocks.window.SetSize).not.toHaveBeenCalled()
  })
})

describe('file dialogs', () => {
  const prompts = [{ name: 'Prompts', pattern: '*.json' }]

  it('asks for one file to open, and resolves with its path', async () => {
    mocks.dialogs.OpenFile.mockResolvedValue('/exports/shared.json')

    await expect(
      platform.chooseFileToOpen({ title: 'Import prompts', button: 'Import', filters: prompts }),
    ).resolves.toBe('/exports/shared.json')
    expect(mocks.dialogs.OpenFile).toHaveBeenCalledExactlyOnceWith({
      Title: 'Import prompts',
      ButtonText: 'Import',
      Filters: [{ DisplayName: 'Prompts', Pattern: '*.json' }],
      CanChooseFiles: true,
      CanChooseDirectories: false,
      AllowsMultipleSelection: false,
    })
  })

  it('asks for one folder, and resolves with its path', async () => {
    mocks.dialogs.OpenFile.mockResolvedValue('/srv/work')

    await expect(platform.chooseFolder({ title: 'Choose a repository location', button: 'Add' })).resolves.toBe(
      '/srv/work',
    )
    expect(mocks.dialogs.OpenFile).toHaveBeenCalledExactlyOnceWith({
      Title: 'Choose a repository location',
      ButtonText: 'Add',
      CanChooseFiles: false,
      CanChooseDirectories: true,
      CanCreateDirectories: false,
      AllowsMultipleSelection: false,
    })
  })

  it('asks where to save, suggesting a name, and resolves with the path', async () => {
    mocks.dialogs.SaveFile.mockResolvedValue('')

    await expect(
      platform.chooseFileToSave({ title: 'Export prompts', button: 'Export', filters: prompts, filename: 'all.json' }),
    ).resolves.toBe('')
    expect(mocks.dialogs.SaveFile).toHaveBeenCalledExactlyOnceWith({
      Title: 'Export prompts',
      ButtonText: 'Export',
      Filters: [{ DisplayName: 'Prompts', Pattern: '*.json' }],
      Filename: 'all.json',
      CanCreateDirectories: true,
    })

    await platform.chooseFileToSave({ title: 'Export prompts', button: 'Export', filters: prompts })
    expect(mocks.dialogs.SaveFile).toHaveBeenLastCalledWith(expect.objectContaining({ Filename: '' }))
  })
})

describe('runScript', () => {
  const request: platform.ScriptRunRequest = {
    dir: '~/projects/sdk',
    ref: 't3:Test',
    inputs: {},
    args: [],
    runsIn: '',
    confirmed: '',
    remember: false,
    anyway: false,
  }
  const question: platform.ScriptQuestion = {
    ref: 't3:Test',
    name: 'Test',
    source: 't3',
    command: 'magex test',
    trust: 'ask',
    trusted: '',
    trustNote: '',
    sha256: 'c0ffee01',
  }

  // refused is a run refused as the desktop runtime delivers it: the full text, and the
  // cause the backend encoded.
  function refused(cause: unknown): Error {
    return new Error('the command needs the user’s trust: t3:Test', { cause })
  }

  it('resolves with the run’s ID once it started', async () => {
    mocks.scripts.Run.mockResolvedValue(42)

    await expect(platform.runScript(request)).resolves.toEqual({ started: true, id: 42 })
    expect(mocks.scripts.Run).toHaveBeenCalledExactlyOnceWith(request)
  })

  it('resolves with the question a refusal carries, and the backend’s words for it', async () => {
    mocks.scripts.Run.mockRejectedValueOnce(refused({ message: 'Confirm “Test” before it runs.', detail: question }))
    mocks.scripts.Run.mockRejectedValueOnce(refused({ detail: question }))

    await expect(platform.runScript(request)).resolves.toEqual({
      started: false,
      question,
      message: 'Confirm “Test” before it runs.',
    })
    await expect(platform.runScript(request)).resolves.toEqual({ started: false, question, message: '' })
  })

  it('rejects with any other failure', async () => {
    const failures = [
      new Error('plain'),
      'not an error',
      refused(null),
      refused({ message: 'A release is using acme-org/sdk right now.' }),
      refused({ message: 'odd', detail: 'a string' }),
      refused({ message: 'odd', detail: { ...question, sha256: 7 } }),
    ]

    for (const failure of failures) {
      mocks.scripts.Run.mockRejectedValueOnce(failure)

      await expect(platform.runScript(request)).rejects.toBe(failure)
    }
  })

  it('cancels the bound call when the caller cancels', async () => {
    const cancelled = vi.fn()
    mocks.scripts.Run.mockReturnValue(new CancellablePromise<number>(() => undefined, cancelled))

    await platform.runScript(request).cancel()

    expect(cancelled).toHaveBeenCalledOnce()
  })
})

describe('Autopilot’s calls', () => {
  it('reach Autopilot’s bindings with their arguments, and the inbox’s rows of a routine’s Watching list', async () => {
    const calls: [() => PromiseLike<unknown>, ReturnType<typeof vi.fn>, unknown[]][] = [
      [() => platform.getAutopilotGlance(), mocks.autopilot.Glance, []],
      [() => platform.listRoutines(), mocks.autopilot.List, []],
      [() => platform.routineNames(), mocks.autopilot.RoutineNames, []],
      [() => platform.duplicateRoutine(3), mocks.autopilot.Duplicate, [3]],
      [() => platform.deleteRoutine(3), mocks.autopilot.Delete, [3]],
      [() => platform.setRoutineEnabled(3, true, 'fromNow'), mocks.autopilot.SetEnabled, [3, true, 'fromNow']],
      [() => platform.setRoutineMode(3, 'ask', 'run'), mocks.autopilot.SetMode, [3, 'ask', 'run']],
      [() => platform.routineWatching(3), mocks.autopilot.Watching, [3]],
      [() => platform.getAutopilotEngine(), mocks.autopilot.Engine, []],
      [() => platform.autopilotActivity(12, 100), mocks.autopilot.Activity, [12, 100]],
      [() => platform.autopilotActivityFrom(12, 40, 500), mocks.autopilot.ActivityFrom, [12, 40, 500]],
      [() => platform.autopilotItemHistory(1), mocks.autopilot.ItemHistory, [1]],
      [() => platform.latestAutopilotMatches([1, 2]), mocks.autopilot.LatestMatches, [[1, 2]]],
      [() => platform.undoAutopilot([51]), mocks.autopilot.Undo, [[51]]],
      [() => platform.tryRoutineAgain(3, 1), mocks.autopilot.TryAgain, [3, 1]],
      [() => platform.letRoutineActAgain(3, 1), mocks.autopilot.LetActAgain, [3, 1]],
      [() => platform.clearAutopilotHistory(), mocks.autopilot.ClearHistory, []],
      [() => platform.approveMatches([5]), mocks.autopilot.Approve, [[5]]],
      [() => platform.approveAutopilotBacklog(3), mocks.autopilot.ApproveBacklog, [3]],
      [() => platform.skipAutopilotBacklog(3, [21, 22]), mocks.autopilot.SkipBacklog, [3, [21, 22]]],
      [() => platform.postReviewAsComment(5), mocks.autopilot.PostAsComment, [5]],
      [() => platform.skipMatch(5), mocks.autopilot.Skip, [5]],
      [
        () => platform.previewAutopilotActions({ id: 3, name: 'Rebase', mode: 'ask', spec: routineSpec() }, 9),
        mocks.autopilot.PreviewActions,
        [{ id: 3, name: 'Rebase', mode: 'ask', spec: routineSpec() }, 9],
      ],
      [() => platform.itemsByID([1, 2]), mocks.inbox.ItemsByID, [[1, 2]]],
      [() => platform.setAutopilotEnabled(true), mocks.settings.SetAutopilotEnabled, [true]],
      [() => platform.setAutopilotPaused(true), mocks.settings.SetAutopilotPaused, [true]],
      [
        () => platform.setAutopilotDailyCap({ amount: 5, unit: 'usd' }),
        mocks.settings.SetAutopilotDailyCap,
        [{ amount: 5, unit: 'usd' }],
      ],
    ]

    for (const [call, binding, args] of calls) {
      binding.mockResolvedValue('answer')

      await expect(call()).resolves.toBe('answer')
      expect(binding).toHaveBeenCalledExactlyOnceWith(...args)
    }
  })

  it('asks the question about what a routine matches now with the routine as saved', async () => {
    const draft = { id: 3, name: 'Snooze passing bot PRs', spec: routineSpec() }
    mocks.autopilot.Matching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 0, incomplete: false })

    await expect(platform.routineMatching(draft)).resolves.toEqual({
      count: 3,
      pullRequests: 3,
      issues: 0,
      unknown: 0,
      incomplete: false,
    })
    expect(mocks.autopilot.Matching).toHaveBeenCalledExactlyOnceWith(draft)
  })

  it('reach the builder’s bindings with the draft as it stands', async () => {
    const draft = { id: 0, name: 'Tell me about security failures', spec: routineSpec() }
    const calls: [() => PromiseLike<unknown>, ReturnType<typeof vi.fn>, unknown[]][] = [
      [() => platform.getAutopilotCatalog(), mocks.autopilot.Catalog, []],
      [() => platform.suggestForRoutines(), mocks.autopilot.Suggest, []],
      [() => platform.describeRoutine(draft), mocks.autopilot.Describe, [draft]],
      [() => platform.testRoutine(draft), mocks.autopilot.Test, [draft]],
      [() => platform.saveRoutine(draft), mocks.autopilot.Save, [draft]],
    ]

    for (const [call, binding, args] of calls) {
      binding.mockResolvedValue('answer')

      await expect(call()).resolves.toBe('answer')
      expect(binding).toHaveBeenCalledExactlyOnceWith(...args)
    }
  })
})

describe('runRoutineNow', () => {
  function refused(cause: unknown): Error {
    return new Error('no such match', { cause })
  }

  it('runs the routine on the item, and resolves with the match it recorded', async () => {
    mocks.autopilot.RunNow.mockResolvedValue({ id: 40, words: 'It starts as soon as a slot frees.' })

    await expect(platform.runRoutineNow(3, 1, false)).resolves.toEqual({
      ran: true,
      match: { id: 40, words: 'It starts as soon as a slot frees.' },
    })
    expect(mocks.autopilot.RunNow).toHaveBeenCalledExactlyOnceWith(3, 1, false)
  })

  it('resolves refused, with why, when the routine doesn’t match the item, by its code alone', async () => {
    mocks.autopilot.RunNow.mockRejectedValueOnce(
      refused({ message: 'It doesn’t match it: checks pass.', detail: { code: 'noMatch' } }),
    )

    await expect(platform.runRoutineNow(3, 1, false)).resolves.toEqual({
      ran: false,
      notMatching: 'It doesn’t match it: checks pass.',
    })
  })

  it('rejects with any other failure', async () => {
    const failures = [
      new Error('plain'),
      'not an error',
      refused(null),
      refused({ message: 'The item isn’t stored on this Mac.' }),
      refused({ message: 'odd', detail: 'a string' }),
      refused({ message: 'odd', detail: { code: 'settling' } }),
      refused({ message: 'odd', detail: { code: 7 } }),
      // Words alone are never read as the refusal.
      refused({ message: 'It doesn’t match it: checks pass.' }),
    ]

    for (const failure of failures) {
      mocks.autopilot.RunNow.mockRejectedValueOnce(failure)

      await expect(platform.runRoutineNow(3, 1, true)).rejects.toBe(failure)
    }
  })

  it('cancels the bound call when the caller cancels', async () => {
    const cancelled = vi.fn()
    mocks.autopilot.RunNow.mockReturnValue(new CancellablePromise<unknown>(() => undefined, cancelled))

    await platform.runRoutineNow(3, 1, false).cancel()

    expect(cancelled).toHaveBeenCalledOnce()
  })
})

describe('the update’s calls', () => {
  it('reach the update’s bindings, and the setting’s setter', async () => {
    const calls: [() => PromiseLike<unknown>, ReturnType<typeof vi.fn>][] = [
      [platform.getUpdateStatus, mocks.update.Status],
      [platform.checkForUpdates, mocks.update.CheckNow],
      [platform.hideUpdateBanner, mocks.update.HideBanner],
      [platform.showUpdateBanner, mocks.update.ShowBanner],
      [platform.ackUpdated, mocks.update.AckUpdated],
      [platform.showInstallLog, mocks.update.ShowInstallLog],
      [platform.moveToApplications, mocks.update.MoveToApplications],
      [platform.openInApplications, mocks.update.OpenInApplications],
      [platform.showAppInFinder, mocks.update.ShowAppInFinder],
    ]

    for (const [call, binding] of calls) {
      binding.mockResolvedValue('answer')

      await expect(call()).resolves.toBe('answer')
      expect(binding).toHaveBeenCalledOnce()
    }

    mocks.settings.SetUpdateMode.mockResolvedValue({ updateMode: 'notify' })
    await expect(platform.setUpdateMode('notify')).resolves.toEqual({ updateMode: 'notify' })
    expect(mocks.settings.SetUpdateMode).toHaveBeenCalledExactlyOnceWith('notify')
  })
})

describe('restartToUpdateNow', () => {
  const release: platform.RunGoing = {
    id: 7,
    kind: 'release',
    title: 'Release v1.2.0',
    repo: 'acme-org/sdk',
    dir: '~/projects/sdk',
    stopRefused: '',
    outlives: false,
  }

  // refused is a restart refused as the desktop runtime delivers it: the full text, and
  // the cause the backend encoded.
  function refused(cause: unknown): Error {
    return new Error('runs are going in the app', { cause })
  }

  it('asks Go to restart, confirmed or not, and resolves with the status should it return', async () => {
    mocks.update.Restart.mockResolvedValue({ state: 'installing' })

    await expect(platform.restartToUpdateNow(false)).resolves.toEqual({
      restarted: true,
      status: { state: 'installing' },
    })
    await platform.restartToUpdateNow(true)
    expect(mocks.update.Restart.mock.calls).toEqual([[false], [true]])
  })

  it('resolves with the runs a refusal names, and the backend’s words for it', async () => {
    mocks.update.Restart.mockRejectedValueOnce(
      refused({ message: 'Restarting stops Release v1.2.0 in acme-org/sdk.', detail: { runsGoing: [release] } }),
    )
    mocks.update.Restart.mockRejectedValueOnce(refused({ detail: { runsGoing: [] } }))

    await expect(platform.restartToUpdateNow(false)).resolves.toEqual({
      restarted: false,
      runsGoing: [release],
      message: 'Restarting stops Release v1.2.0 in acme-org/sdk.',
    })
    await expect(platform.restartToUpdateNow(false)).resolves.toEqual({
      restarted: false,
      runsGoing: [],
      message: '',
    })
  })

  it('reads the runs defensively: an entry it can’t read is left out, a list it can’t read is none', async () => {
    mocks.update.Restart.mockRejectedValueOnce(
      refused({
        message: 'Runs are going.',
        detail: { runsGoing: [release, null, 'a run', { ...release, id: '7' }, { ...release, title: undefined }] },
      }),
    )
    mocks.update.Restart.mockRejectedValueOnce(refused({ message: 'Runs are going.', detail: { runsGoing: 'many' } }))

    await expect(platform.restartToUpdateNow(false)).resolves.toEqual({
      restarted: false,
      runsGoing: [release],
      message: 'Runs are going.',
    })
    await expect(platform.restartToUpdateNow(false)).resolves.toEqual({
      restarted: false,
      runsGoing: [],
      message: 'Runs are going.',
    })
  })

  it('resolves with a refusal the window answers on its own, by its code, never its words', async () => {
    mocks.update.Restart.mockRejectedValueOnce(
      refused({ message: 'The last update is still finishing.', detail: { code: 'settling' } }),
    )
    mocks.update.Restart.mockRejectedValueOnce(
      refused({ message: 'The app is quitting to finish the update.', detail: { code: 'finishing' } }),
    )
    mocks.update.Restart.mockRejectedValueOnce(
      refused({ message: '0.3.0 didn’t install before.', detail: { code: 'uninstallable' } }),
    )

    await expect(platform.restartToUpdateNow(true)).resolves.toEqual({
      restarted: false,
      refusal: 'settling',
      message: 'The last update is still finishing.',
    })
    await expect(platform.restartToUpdateNow(true)).resolves.toEqual({
      restarted: false,
      refusal: 'finishing',
      message: 'The app is quitting to finish the update.',
    })
    await expect(platform.restartToUpdateNow(true)).resolves.toEqual({
      restarted: false,
      refusal: 'uninstallable',
      message: '0.3.0 didn’t install before.',
    })
  })

  it('rejects with any other failure', async () => {
    const failures = [
      new Error('plain'),
      'not an error',
      refused(null),
      refused({ message: 'Couldn’t install 0.3.0: no space.' }),
      refused({ message: 'odd', detail: 'a string' }),
      refused({ message: 'odd', detail: { question: 'not runs' } }),
      refused({ message: 'odd', detail: { code: 'a code this build doesn’t know' } }),
      refused({ message: 'odd', detail: { code: 7 } }),
      // Words alone are never read as a refusal.
      refused({ message: 'The last update is still finishing. Try again in a moment.' }),
    ]

    for (const failure of failures) {
      mocks.update.Restart.mockRejectedValueOnce(failure)

      await expect(platform.restartToUpdateNow(false)).rejects.toBe(failure)
    }
  })

  it('cancels the bound call when the caller cancels', async () => {
    const cancelled = vi.fn()
    mocks.update.Restart.mockReturnValue(new CancellablePromise<unknown>(() => undefined, cancelled))

    await platform.restartToUpdateNow(false).cancel()

    expect(cancelled).toHaveBeenCalledOnce()
  })
})

describe('downloadUpdate', () => {
  it('downloads the newer version, and resolves with the status, ready', async () => {
    mocks.update.Download.mockResolvedValue({ state: 'ready' })

    await expect(platform.downloadUpdate()).resolves.toEqual({ downloaded: true, status: { state: 'ready' } })
    expect(mocks.update.Download).toHaveBeenCalledOnce()
  })

  it('resolves with a wait while the last update is still finishing', async () => {
    mocks.update.Download.mockRejectedValue(
      new Error('the last update may still be finishing', {
        cause: { message: 'The last update is still finishing. Try again in a moment.', detail: { code: 'settling' } },
      }),
    )

    await expect(platform.downloadUpdate()).resolves.toEqual({
      downloaded: false,
      refusal: 'settling',
      message: 'The last update is still finishing. Try again in a moment.',
    })
  })

  it('rejects with any other failure', async () => {
    const failure = new Error('stalled', { cause: { message: 'Couldn’t download 0.3.0: it stalled.' } })
    mocks.update.Download.mockRejectedValue(failure)

    await expect(platform.downloadUpdate()).rejects.toBe(failure)
  })

  it('cancels the bound call when the caller cancels', async () => {
    const cancelled = vi.fn()
    mocks.update.Download.mockReturnValue(new CancellablePromise<unknown>(() => undefined, cancelled))

    await platform.downloadUpdate().cancel()

    expect(cancelled).toHaveBeenCalledOnce()
  })
})
