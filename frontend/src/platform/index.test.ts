import { CancellablePromise } from '@wailsio/runtime/cancellable'
import { describe, expect, it, vi } from 'vitest'

import * as platform from './index'

// The bound calls the platform hands out as generated need no test of their own; these
// cover the calls the platform adds something to.
const mocks = vi.hoisted(() => ({
  inbox: { CheckFiles: vi.fn() },
  settings: { SetWindowSize: vi.fn() },
  scripts: { Run: vi.fn(), Repos: vi.fn(), List: vi.fn() },
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
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/settings', () => ({ Service: mocks.settings }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/scripts', () => ({ Service: mocks.scripts }))

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
