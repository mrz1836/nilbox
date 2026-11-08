import { CancellablePromise } from '@wailsio/runtime/cancellable'
import { describe, expect, it, vi } from 'vitest'

import * as platform from './index'

// The bound calls the platform hands out as generated need no test of their own; these
// cover the calls the platform adds something to.
const mocks = vi.hoisted(() => ({
  inbox: { CheckFiles: vi.fn() },
  settings: { SetWindowSize: vi.fn() },
  events: { On: vi.fn() },
  browser: { OpenURL: vi.fn() },
  window: { SetSize: vi.fn() },
}))

// Create stands in for the runtime's registry of event data creators, which the generated
// event bindings freeze when they load.
vi.mock('@wailsio/runtime', () => ({
  Events: mocks.events,
  Browser: mocks.browser,
  Window: mocks.window,
  Create: { Events: {} },
}))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/inbox', () => ({ Service: mocks.inbox }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/settings', () => ({ Service: mocks.settings }))

const url = 'https://github.com/acme-org/example-repo/pull/7'

describe('on', () => {
  it('passes each event’s data on, and returns the function that stops listening', () => {
    const unsubscribe = vi.fn()
    mocks.events.On.mockReturnValue(unsubscribe)
    const listener = vi.fn()

    const stop = platform.on('inbox:reveal', listener)

    expect(mocks.events.On).toHaveBeenCalledExactlyOnceWith('inbox:reveal', expect.any(Function))
    const handler = mocks.events.On.mock.calls[0]?.[1] as (event: { data: unknown }) => void
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
