import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ScriptRunChunk } from '../../platform'
import { scriptRunChunk } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { LogView, pollMs, retryMs } from './LogView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

afterEach(() => {
  vi.useRealTimers()
})

// numbered is lines "line from" up to before "line to", each ended.
function numbered(from: number, to: number): string {
  let text = ''

  for (let n = from; n < to; n++) {
    text += `line ${String(n)}\n`
  }

  return text
}

// bytes is how many bytes text takes in the log.
function bytes(text: string): number {
  return new TextEncoder().encode(text).length
}

// chunk is a part of the log: text read at offset, of a log size bytes long so far,
// ended when done.
function chunk(text: string, offset: number, { size = offset + bytes(text), done = true } = {}): ScriptRunChunk {
  return scriptRunChunk({ text, offset, next: offset + bytes(text), size, done })
}

// output is the log's scrolling region.
function output(): HTMLElement {
  return screen.getByRole('region', { name: 'Output' })
}

// shown lists the lines the log shows, as text.
function shown(): string[] {
  return [...output().querySelectorAll(':scope > div > div')].map((line) => line.textContent)
}

// settle lets the reads under way answer, and runs the timers due within ms.
async function settle(ms = 0) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

// lay lays the log out as a browser would, each line 20 pixels high in a view 100 high,
// and returns where it is scrolled to, and a way to scroll it.
function lay() {
  const el = output()
  let top = 0

  Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => shown().length * 20 })
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => 100 })
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = v
    },
  })

  return {
    top: () => top,
    end: () => el.scrollHeight,
    scrollTo: (to: number) => {
      top = to
      fireEvent.scroll(el)
    },
  }
}

describe('LogView', () => {
  it('reads the log’s tail and shows it, a line for each', async () => {
    platform.scriptRunOutput.mockResolvedValue(chunk('one\ntwo\n\nfour', 0))
    render(<LogView runId={7} live={false} />)

    expect(screen.getByText('Reading the output…')).toBeInTheDocument()
    expect(await within(output()).findByText('four')).toBeInTheDocument()
    expect(shown()).toEqual(['one', 'two', '', 'four'])
    expect(platform.scriptRunOutput).toHaveBeenCalledExactlyOnceWith(7, -1, 256 * 1024)
    expect(screen.queryByText('Reading the output…')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show earlier output' })).not.toBeInTheDocument()
  })

  it('draws the colors in the theme’s and bold in bold, and drops every other escape', async () => {
    platform.scriptRunOutput.mockResolvedValue(
      chunk(
        '\x1b[1;31merror\x1b[0m: \x1b]8;;https://example.com\x07see\x1b]8;;\x07 \x1b[32mok\x1b[0m\x1b[?25l\x1b[2J\x1b(B\x07\n',
        0,
      ),
    )
    render(<LogView runId={7} live={false} />)

    const error = await within(output()).findByText('error')

    expect(error).toHaveClass('text-danger', 'font-bold')
    expect(within(output()).getByText('ok')).toHaveClass('text-success')
    expect(within(output()).getByText('ok')).not.toHaveClass('font-bold')
    expect(within(output()).getByText(': see')).not.toHaveAttribute('class')
    expect(shown()).toEqual(['error: see ok'])
    for (const dropped of ['\x1b', '\x07', ']8;;', '[?25l', '(B']) {
      expect(output().textContent).not.toContain(dropped)
    }
  })

  it('draws bold text in the log’s own color', async () => {
    platform.scriptRunOutput.mockResolvedValue(chunk('\x1b[1mBold\x1b[0m\n', 0))
    render(<LogView runId={7} live={false} />)

    expect(await within(output()).findByText('Bold')).toHaveClass('font-bold')
    expect(within(output()).getByText('Bold')).not.toHaveClass('text-text')
  })

  it('overwrites a line at a carriage return, as a terminal does, and reads \\r\\n as a line end', async () => {
    platform.scriptRunOutput.mockResolvedValue(chunk('Downloading  10%\rDownloading 100%\r\nchecksum ok\r\n', 0))
    render(<LogView runId={7} live={false} />)

    expect(await within(output()).findByText('Downloading 100%')).toBeInTheDocument()
    expect(shown()).toEqual(['Downloading 100%', 'checksum ok'])
  })

  it('says when an ended run printed nothing', async () => {
    platform.scriptRunOutput.mockResolvedValue(chunk('', 0))
    render(<LogView runId={7} live={false} />)

    expect(await screen.findByText('No output.')).toBeInTheDocument()
  })

  describe('the tail window', () => {
    it('shows the newest 2,000 lines, and the ones above them on Show earlier output', async () => {
      platform.scriptRunOutput.mockResolvedValue(chunk(numbered(0, 2_500), 0))
      render(<LogView runId={7} live={false} />)

      expect(await within(output()).findByText('line 2499')).toBeInTheDocument()
      expect(shown()).toHaveLength(2_000)
      expect(shown()[0]).toBe('line 500')
      expect(within(output()).queryByText('line 499')).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Show earlier output' }))

      expect(shown()).toHaveLength(2_500)
      expect(shown()[0]).toBe('line 0')
      // The lines were held, so nothing more was read.
      expect(platform.scriptRunOutput).toHaveBeenCalledOnce()
      expect(screen.queryByRole('button', { name: 'Show earlier output' })).not.toBeInTheDocument()
    })

    it('reads the part of the log before its tail on Show earlier output', async () => {
      const earlier = numbered(0, 9)
      const tail = 'rest of line 9\nline 10\n'

      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk(tail, bytes(earlier)))
        .mockResolvedValueOnce(chunk(earlier, 0, { size: bytes(earlier + tail), done: false }))
      render(<LogView runId={7} live={false} />)

      expect(await within(output()).findByText('rest of line 9')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Show earlier output' }))

      expect(await within(output()).findByText('line 0')).toBeInTheDocument()
      expect(platform.scriptRunOutput).toHaveBeenLastCalledWith(7, 0, bytes(earlier))
      expect(shown()).toEqual([...numbered(0, 8).trimEnd().split('\n'), 'line 8', 'rest of line 9', 'line 10'])
      expect(screen.queryByRole('button', { name: 'Show earlier output' })).not.toBeInTheDocument()
    })

    it('says it is reading earlier output until it has', async () => {
      let answer: (c: ScriptRunChunk) => void = () => undefined

      platform.scriptRunOutput.mockResolvedValueOnce(chunk('tail\n', 14)).mockReturnValueOnce(
        new Promise((resolve) => {
          answer = resolve
        }),
      )
      render(<LogView runId={7} live={false} />)

      await userEvent.click(await screen.findByRole('button', { name: 'Show earlier output' }))

      expect(screen.getByRole('button', { name: 'Reading earlier output…' })).toBeDisabled()

      act(() => {
        answer(chunk('head\nmiddle\n', 0, { size: 19 }))
      })

      expect(await within(output()).findByText('head')).toBeInTheDocument()
      expect(shown()).toEqual(['head', 'middle', 'tail'])
    })

    it('says why earlier output couldn’t be read', async () => {
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk('tail\n', 14))
        .mockRejectedValueOnce(new Error('the run is no longer recorded'))
      render(<LogView runId={7} live={false} />)

      await userEvent.click(await screen.findByRole('button', { name: 'Show earlier output' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('the run is no longer recorded')
      expect(shown()).toEqual(['tail'])
    })

    it('reads the log again from its tail when it was cut down meanwhile', async () => {
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk('tail\n', 14))
        .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 0, next: 0, size: 3, restart: true }))
        .mockResolvedValueOnce(chunk('cut\n', 0))
      render(<LogView runId={7} live={false} />)

      await userEvent.click(await screen.findByRole('button', { name: 'Show earlier output' }))

      expect(await within(output()).findByText('cut')).toBeInTheDocument()
      expect(platform.scriptRunOutput).toHaveBeenLastCalledWith(7, -1, 256 * 1024)
      expect(shown()).toEqual(['cut'])
    })

    it('reads the log again from its tail when earlier output comes from a log grown smaller', async () => {
      // The read is within the log cut down, so the backend reads it as asked: only the
      // size says the log is not the one held.
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk('tail\n', 14))
        .mockResolvedValueOnce(chunk('other\n', 0, { size: 12 }))
        .mockResolvedValueOnce(chunk('cut\n', 0))
      render(<LogView runId={7} live={false} />)

      await userEvent.click(await screen.findByRole('button', { name: 'Show earlier output' }))

      expect(await within(output()).findByText('cut')).toBeInTheDocument()
      expect(platform.scriptRunOutput).toHaveBeenLastCalledWith(7, -1, 256 * 1024)
      expect(shown()).toEqual(['cut'])
    })
  })

  describe('a live run', () => {
    it('reads on from where it left off, at once while more is there, then every so often until done', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk('one\n', 0, { size: 8, done: false }))
        .mockResolvedValueOnce(chunk('two\n', 4, { size: 8, done: false }))
        .mockResolvedValueOnce(chunk('', 8, { done: false }))
        .mockResolvedValueOnce(chunk('three\n', 8, { done: true }))
      render(<LogView runId={7} live />)

      await settle()
      expect(shown()).toEqual(['one', 'two'])
      expect(platform.scriptRunOutput.mock.calls).toEqual([
        [7, -1, 256 * 1024],
        [7, 4, 256 * 1024],
      ])

      await settle(pollMs)
      expect(platform.scriptRunOutput).toHaveBeenLastCalledWith(7, 8, 256 * 1024)
      expect(shown()).toEqual(['one', 'two'])

      await settle(pollMs)
      expect(shown()).toEqual(['one', 'two', 'three'])

      // The log is done: nothing more is read.
      await settle(pollMs * 10)
      expect(platform.scriptRunOutput).toHaveBeenCalledTimes(4)
    })

    it('says no output came yet', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput.mockResolvedValue(chunk('', 0, { done: false }))
      render(<LogView runId={7} live />)

      await settle()

      expect(screen.getByText('No output yet.')).toBeInTheDocument()
    })

    it('stops reading once it is gone', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput.mockResolvedValue(chunk('', 0, { done: false }))
      const { unmount } = render(<LogView runId={7} live />)

      await settle()
      unmount()
      await settle(pollMs * 10)

      expect(platform.scriptRunOutput).toHaveBeenCalledOnce()
    })

    it('cancels a read under way as it goes, and takes no answer after', async () => {
      const cancelled = vi.fn()
      let answer: (c: ScriptRunChunk) => void = () => undefined
      const read = new Promise<ScriptRunChunk>((resolve) => {
        answer = resolve
      })

      platform.scriptRunOutput.mockReturnValue(Object.assign(read, { cancel: cancelled }))
      const { unmount } = render(<LogView runId={7} live />)

      unmount()
      expect(cancelled).toHaveBeenCalledOnce()

      // A read that answers anyway is let go: nothing more is read.
      await act(async () => {
        answer(chunk('late\n', 0, { done: false }))
        await read
      })

      expect(platform.scriptRunOutput).toHaveBeenCalledOnce()
    })

    it('reads the log again from its tail when it was cut down as the run ended', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk(numbered(0, 3), 100, { done: false }))
        .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 121, next: 121, size: 40, restart: true }))
        .mockResolvedValueOnce(chunk('head\n…\ntail\n', 0))
      render(<LogView runId={7} live />)

      await settle()
      expect(shown()).toEqual(['line 0', 'line 1', 'line 2'])

      await settle(pollMs)
      // The read again from the tail waits on a timer of its own.
      await settle(1)

      expect(shown()).toEqual(['head', '…', 'tail'])
      expect(platform.scriptRunOutput).toHaveBeenLastCalledWith(7, -1, 256 * 1024)
    })

    it('reads the log again from its tail when it grew smaller while the view was behind', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      // The view read up to 121 of 200 bytes; the run ended, and its log was cut down to
      // 150, so the read from 121 is within it and comes back with what is there now.
      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk(numbered(0, 3), 100, { size: 200, done: false }))
        .mockResolvedValueOnce(chunk('[cut]\n', 121, { size: 150, done: false }))
        .mockResolvedValueOnce(chunk('head\n…\ntail\n', 0))
      render(<LogView runId={7} live />)

      await settle()
      await settle(1)

      expect(shown()).toEqual(['head', '…', 'tail'])
      expect(platform.scriptRunOutput.mock.calls).toEqual([
        [7, -1, 256 * 1024],
        [7, 121, 256 * 1024],
        [7, -1, 256 * 1024],
      ])
    })

    it('tries a failed read again, and clears the failure once one works', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput
        .mockRejectedValueOnce(new Error('database is locked'))
        .mockResolvedValueOnce(chunk('back\n', 0))
      render(<LogView runId={7} live />)

      await settle()
      expect(screen.getByRole('alert')).toHaveTextContent('database is locked')
      expect(screen.queryByText('Reading the output…')).not.toBeInTheDocument()

      await settle(retryMs)

      expect(shown()).toEqual(['back'])
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('leaves a failed read of an ended run’s log there', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      platform.scriptRunOutput.mockRejectedValue(new Error('the run is no longer recorded'))
      render(<LogView runId={7} live={false} />)

      await settle(retryMs * 3)

      expect(screen.getByRole('alert')).toHaveTextContent('the run is no longer recorded')
      expect(platform.scriptRunOutput).toHaveBeenCalledOnce()
    })

    it('lets earlier output read for a log since read again go', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      let answer: (c: ScriptRunChunk) => void = () => undefined

      platform.scriptRunOutput
        .mockResolvedValueOnce(chunk('tail\n', 5, { done: false }))
        .mockReturnValueOnce(
          new Promise((resolve) => {
            answer = resolve
          }),
        )
        .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 10, next: 10, size: 4, restart: true }))
        .mockResolvedValueOnce(chunk('new\n', 0))
      render(<LogView runId={7} live />)

      await settle()
      fireEvent.click(screen.getByRole('button', { name: 'Show earlier output' }))
      await settle(pollMs)
      // The read again from the tail waits on a timer of its own.
      await settle(1)
      expect(shown()).toEqual(['new'])

      await act(async () => {
        answer(chunk('head\n', 0, { size: 10 }))
        await vi.advanceTimersByTimeAsync(0)
      })

      expect(shown()).toEqual(['new'])
    })
  })

  describe('following the output', () => {
    // live answers the reads of a live run: lines 0 to 9 first, then each further read
    // the lines given, then nothing new.
    function live(...more: [number, number][]) {
      let offset = bytes(numbered(0, 10))

      platform.scriptRunOutput.mockResolvedValueOnce(chunk(numbered(0, 10), 0, { done: false }))

      for (const [from, to] of more) {
        const text = numbered(from, to)

        platform.scriptRunOutput.mockResolvedValueOnce(chunk(text, offset, { done: false }))
        offset += bytes(text)
      }

      platform.scriptRunOutput.mockResolvedValue(chunk('', offset, { done: false }))
    }

    it('keeps the newest output in view as it arrives', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      live([10, 12])
      render(<LogView runId={7} live />)

      await settle()
      const view = lay()

      await settle(pollMs)

      expect(shown()).toHaveLength(12)
      expect(view.top()).toBe(view.end())
      expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()

      // A scroll that stays at the end keeps following.
      view.scrollTo(view.end() - 100)
      expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()
    })

    it('stops following once scrolled up, and offers Jump to latest, which follows again', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      live([10, 12], [12, 15])
      render(<LogView runId={7} live />)

      await settle()
      const view = lay()

      view.scrollTo(0)
      expect(screen.getByRole('button', { name: 'Jump to latest' })).toBeInTheDocument()

      await settle(pollMs)
      await settle(pollMs)

      // New lines joined the end, out of view; the view stayed where it was.
      expect(shown()).toHaveLength(15)
      expect(view.top()).toBe(0)

      fireEvent.click(screen.getByRole('button', { name: 'Jump to latest' }))

      expect(view.top()).toBe(view.end())
      expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()
    })

    it('follows again once scrolled back to the end', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      live([10, 11])
      render(<LogView runId={7} live />)

      await settle()
      const view = lay()

      view.scrollTo(0)
      view.scrollTo(view.end() - 100)
      expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()

      await settle(pollMs)

      expect(view.top()).toBe(view.end())
    })

    it('holds the window where it is while scrolled up, and slides it again once following', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
      live([10, 2_005])
      render(<LogView runId={7} live />)

      await settle()
      const view = lay()

      view.scrollTo(0)
      await settle(pollMs)

      // Pinned at the first line, the window grows past 2,000 lines rather than move.
      expect(shown()).toHaveLength(2_005)
      expect(shown()[0]).toBe('line 0')

      fireEvent.click(screen.getByRole('button', { name: 'Jump to latest' }))

      expect(shown()).toHaveLength(2_000)
      expect(shown()[0]).toBe('line 5')
    })
  })
})
