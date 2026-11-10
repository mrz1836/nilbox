import { describe, expect, it } from 'vitest'

import { ansiLines, lineCount } from './ansi'
import {
  appended,
  earlierRead,
  emptyTail,
  followed,
  fromTail,
  hasEarlier,
  pinnedAt,
  revealed,
  shownFrom,
  tailLines,
  withEarlier,
  type Chunk,
  type LogTail,
} from './logTail'

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

// chunk is text read at offset, from a log that ends with it.
function chunk(text: string, offset = 0): Chunk {
  return { text, offset, next: offset + bytes(text), size: offset + bytes(text) }
}

// tail is the log as a read of its tail at offset found text.
function tail(text: string, offset = 0): LogTail {
  return fromTail(chunk(text, offset), emptyTail)
}

// texts lists the text of every line held.
function texts(log: LogTail): string[] {
  return ansiLines(log.ansi).map((line) => line.map((s) => s.text).join(''))
}

// firstShown is the text of the first line shown.
function firstShown(log: LogTail): string {
  return texts(log)[shownFrom(log)] ?? ''
}

describe('the log’s tail window', () => {
  it('reads the tail, and shows its newest lines, following the output', () => {
    const log = tail(numbered(0, tailLines + 500), 100)

    expect(log).toMatchObject({ start: 100, size: log.next, pinned: null, reads: 1 })
    expect(shownFrom(log)).toBe(500)
    expect(firstShown(log)).toBe('line 500')
    expect(hasEarlier(log)).toBe(true)
    expect(fromTail(chunk(''), log).reads).toBe(2)
  })

  it('has nothing earlier when the whole log shows', () => {
    const log = tail(numbered(0, 10))

    expect(shownFrom(log)).toBe(0)
    expect(hasEarlier(log)).toBe(false)
  })

  it('slides along as output arrives while it follows, and holds still once pinned', () => {
    const log = tail(numbered(0, tailLines))
    const grown = appended(log, chunk(numbered(tailLines, tailLines + 3), log.next))

    expect(grown.next).toBe(log.next + bytes(numbered(tailLines, tailLines + 3)))
    expect(grown.size).toBe(grown.next)
    expect(firstShown(grown)).toBe('line 3')

    const pinned = pinnedAt(grown)
    const more = appended(pinned, chunk(numbered(tailLines + 3, tailLines + 9), grown.next))

    expect(firstShown(more)).toBe('line 3')
    expect(lineCount(more.ansi)).toBe(tailLines + 9)
    expect(firstShown(followed(more))).toBe('line 9')
  })

  it('joins new output at the end without copying the lines it holds', () => {
    const log = tail(numbered(0, 10))
    const grown = appended(log, chunk(numbered(10, 11), log.next))

    expect(grown.ansi.rows).toBe(log.ansi.rows)
  })

  describe('letting the oldest output go', () => {
    // over is lines 0 to 9 held from offset 50, then lines 10 to 19 read after them, with
    // the view as pin leaves it, holding at most keep characters.
    function over(pin: (log: LogTail) => LogTail, keep = 100) {
      const log = pin(tail(numbered(0, 10), 50))

      return appended(log, chunk(numbered(10, 20), log.next), keep)
    }

    it('lets the oldest lines go, down to half of what it keeps, while it follows', () => {
      const grown = over((log) => log)
      // Every line takes 7 characters; those that end within the last 50 stay.
      const kept = numbered(13, 20)

      expect(grown.raw).toBe(kept)
      expect(texts(grown)).toEqual(kept.trimEnd().split('\n'))
      expect(grown.start).toBe(50 + bytes(numbered(0, 13)))
      expect(grown.pinned).toBeNull()
      expect(hasEarlier(grown)).toBe(true)
    })

    it('lets only the lines above the window go while it is pinned', () => {
      const grown = over((log) => ({ ...log, pinned: 4 }))

      expect(texts(grown)[0]).toBe('line 4')
      expect(grown.pinned).toBe(0)
      expect(firstShown(grown)).toBe('line 4')
      expect(grown.start).toBe(50 + bytes(numbered(0, 4)))
    })

    it('keeps every line while the window is pinned at the first, up to twice what it keeps', () => {
      expect(over(pinnedAt).raw).toBe(numbered(0, 20))
    })

    it('lets the oldest lines go whatever shows once it holds twice what it keeps', () => {
      const grown = over(pinnedAt, 60)

      expect(grown.raw).toBe(numbered(16, 20))
      expect(grown.pinned).toBe(0)
      expect(firstShown(grown)).toBe('line 16')
    })

    it('keeps it all while it holds no more than it keeps', () => {
      expect(over((log) => log, 1_000).raw).toBe(numbered(0, 20))
    })

    it('keeps a line too long to cut until it ends', () => {
      const log = tail('start ')
      const long = 'x'.repeat(200)

      expect(appended(log, chunk(long, log.next), 100).raw).toBe(`start ${long}`)
    })
  })

  it('shows the lines held above the window before it reads any', () => {
    const log = tail(numbered(0, tailLines * 2 + 100))

    expect(earlierRead(log)).toBeNull()
    expect(firstShown(revealed(log))).toBe('line 100')
    expect(firstShown(revealed(revealed(log)))).toBe('line 0')
    expect(earlierRead(revealed(revealed(log)))).toEqual({ offset: 0, limit: 0 })
  })

  it('reads the part just before what it holds, up to a chunk', () => {
    expect(earlierRead(tail('x\n', 1_000))).toEqual({ offset: 0, limit: 1_000 })
    expect(earlierRead(tail('x\n', 300_000))).toEqual({ offset: 300_000 - 262_144, limit: 262_144 })
  })

  it('joins earlier output above the window, completing the line cut at the tail’s start', () => {
    const before = 'line 0\nline 1\nline 2\npa'
    const earlier = withEarlier(tail('rt of line 3\nline 4\n', bytes(before)), chunk(before))

    expect(earlier.start).toBe(0)
    expect(texts(earlier)).toEqual(['line 0', 'line 1', 'line 2', 'part of line 3', 'line 4'])
    expect(shownFrom(earlier)).toBe(0)
  })

  it('shows as many more lines as it adds, the window’s old first line staying put below them', () => {
    const before = numbered(0, tailLines + 10)
    const earlier = withEarlier(tail(numbered(tailLines + 10, tailLines + 20), bytes(before)), chunk(before))

    expect(firstShown(earlier)).toBe('line 10')
    expect(earlier.pinned).toBe(10)
  })

  it('keeps the log’s own replacement characters', () => {
    const before = '�a\n'
    const earlier = withEarlier(tail('b\n', bytes(before)), chunk(before))

    expect(earlier.raw).toBe('�a\nb\n')
    expect(earlier.start).toBe(0)
  })

  it('cuts a read widened past what it holds at its start, between characters', () => {
    // The backend reads at least four bytes, so a read of a log's first byte takes the
    // characters after it too: here é (two bytes) and c.
    const earlier = withEarlier(tail('éc', 1), { text: 'aéc', offset: 0, next: 4, size: 4 })

    expect(earlier.raw).toBe('aéc')
    expect(earlier.start).toBe(0)

    // An emoji takes two UTF-16 units; the cut never splits one.
    expect(withEarlier(tail('😀z', 1), { text: 'a😀', offset: 0, next: 5, size: 6 }).raw).toBe('a😀z')
  })
})
