import { describe, expect, it } from 'vitest'

import {
  ansiLines,
  dropLines,
  emptyAnsi,
  feedAnsi,
  lineCount,
  linesFrom,
  parseAnsi,
  plainText,
  type AnsiLine,
  type AnsiSpan,
  type AnsiTone,
} from './ansi'

// texts is each line's text, styles and all left out.
function texts(lines: readonly AnsiLine[]): string[] {
  return lines.map((line) => line.map((span) => span.text).join(''))
}

// span is a span of text, in the log's own color unless given.
function span(text: string, tone: AnsiTone | null = null, bold = false): AnsiSpan {
  return { text, tone, bold }
}

describe('parseAnsi', () => {
  it('reads plain text into lines, an unfinished last line included', () => {
    expect(texts(parseAnsi('one\ntwo\n\nfour'))).toEqual(['one', 'two', '', 'four'])
    expect(texts(parseAnsi('one\n'))).toEqual(['one'])
    expect(parseAnsi('')).toEqual([])
  })

  it('keeps tabs as they are', () => {
    expect(texts(parseAnsi('a\tb'))).toEqual(['a\tb'])
  })

  it.each<[number, AnsiTone]>([
    [30, 'muted'],
    [31, 'danger'],
    [32, 'success'],
    [33, 'warning'],
    [34, 'info'],
    [35, 'accent'],
    [36, 'info'],
    [37, 'text'],
    [90, 'muted'],
    [91, 'danger'],
    [92, 'success'],
    [93, 'warning'],
    [94, 'info'],
    [95, 'accent'],
    [96, 'info'],
    [97, 'text'],
  ])('draws color %i in the theme’s %s', (code, tone) => {
    expect(parseAnsi(`\x1b[${String(code)}mhi`)).toEqual([[span('hi', tone)]])
  })

  it('turns bold on and off, and resets the color and bold', () => {
    expect(parseAnsi('\x1b[1;31mA\x1b[22mB\x1b[39mC\x1b[1mD\x1b[0mE\x1b[1;32mF\x1b[mG')).toEqual([
      [
        span('A', 'danger', true),
        span('B', 'danger'),
        span('C'),
        span('D', null, true),
        span('E'),
        span('F', 'success', true),
        span('G'),
      ],
    ])
  })

  it('carries the style across lines until it is reset', () => {
    expect(parseAnsi('\x1b[33mwarn\nstill\x1b[0m\nplain')).toEqual([
      [span('warn', 'warning')],
      [span('still', 'warning')],
      [span('plain')],
    ])
  })

  it('reads an empty parameter as 0', () => {
    expect(parseAnsi('\x1b[31mred\x1b[;mplain')).toEqual([[span('red', 'danger'), span('plain')]])
  })

  it('reads the sixteen basic colors of the 256, and draws any other in the log’s own color', () => {
    expect(
      parseAnsi('\x1b[38;5;1ma\x1b[38;5;10mb\x1b[38;5;196mc\x1b[31m\x1b[38:5:4md\x1b[31m\x1b[38;2;1;2;3me'),
    ).toEqual([[span('a', 'danger'), span('b', 'success'), span('c'), span('d', 'info'), span('e')]])
  })

  it('reads what follows an extended color as parameters of their own', () => {
    // 38;5;n takes two after it, and 38;2;r;g;b four; then bold still applies.
    expect(parseAnsi('\x1b[38;5;2;1ma\x1b[0;38;2;9;9;9;1;33mb')).toEqual([
      [span('a', 'success', true), span('b', 'warning', true)],
    ])
  })

  it('drops a background color, the extended ones too, and keeps the foreground', () => {
    expect(parseAnsi('\x1b[32;41ma\x1b[48;5;1mb\x1b[48;2;1;2;3mc\x1b[48:5:1md\x1b[49me')).toEqual([
      [span('abcde', 'success')],
    ])
  })

  it('drops an extended color it can’t read', () => {
    expect(parseAnsi('\x1b[31m\x1b[38;7ma\x1b[31m\x1b[38mb\x1b[31m\x1b[38;5mc')).toEqual([[span('abc')]])
  })

  it('drops the styles it doesn’t draw: dim, italics, underlines, and the like', () => {
    expect(parseAnsi('\x1b[2;3;4;5;7;9mtext\x1b[24m')).toEqual([[span('text')]])
  })

  it('drops cursor movement, screen erasing, and private modes', () => {
    const lines = parseAnsi('\x1b[?25la\x1b[2Jb\x1b[1;1Hc\x1b[3Ad\x1b[?25h\x1b[>4;1me\x1b[3Kf')

    expect(texts(lines)).toEqual(['abcdef'])
  })

  it('drops OSC sequences, ended by BEL or ST, such as links and titles', () => {
    const text = '\x1b]0;window title\x07a \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ b\x1b]2;t\x9cc'

    expect(texts(parseAnsi(text))).toEqual(['a link bc'])
  })

  it('drops the other string sequences: DCS, SOS, PM, and APC', () => {
    expect(texts(parseAnsi('a\x1bPq#0;1\x1b\\b\x1bXsos\x07c\x1b^pm\x07d\x1b_apc\x07e'))).toEqual(['abcde'])
  })

  it('ends a string sequence at a line feed, so a stray one can’t hide the log', () => {
    expect(texts(parseAnsi('a\x1b]unterminated\nb'))).toEqual(['a', 'b'])
  })

  it('ends a string sequence at an ESC that starts another sequence', () => {
    expect(parseAnsi('\x1b]title\x1b[31mred')).toEqual([[span('red', 'danger')]])
  })

  it('drops two-character escapes, charset choices, and a lone ESC', () => {
    expect(texts(parseAnsi('a\x1b7b\x1b8c\x1b(Bd\x1b=e\x1bMf\x1b\x01g\x1b$(Bh'))).toEqual(['abcdefgh'])
  })

  it('ends a broken escape where it breaks, and reads what broke it', () => {
    // A line feed inside a CSI, or a control after an intermediate, ends it.
    expect(texts(parseAnsi('a\x1b[31\nb\x1b(\x01c\x1b[3\x1b[32md'))).toEqual(['a', 'bcd'])
    expect(parseAnsi('\x1b[3\x1b[32md')).toEqual([[span('d', 'success')]])
    expect(texts(parseAnsi('a\x1b(\u00e9b'))).toEqual(['a\u00e9b'])
  })

  it('drops the other control characters', () => {
    expect(texts(parseAnsi('a\x00b\x07c\x7fd\x85e\x9bf'))).toEqual(['abcdef'])
  })

  it('goes back to the line’s start at a carriage return, and overwrites from there', () => {
    expect(texts(parseAnsi('abcdef\rXY\n'))).toEqual(['XYcdef'])
    expect(texts(parseAnsi(' 10%\r 50%\r100%\ndone'))).toEqual(['100%', 'done'])
  })

  it('reads \\r\\n as a plain line end', () => {
    expect(texts(parseAnsi('one\r\ntwo\r\n'))).toEqual(['one', 'two'])
  })

  it('keeps the style of what it overwrote outside the new text', () => {
    expect(parseAnsi('\x1b[31mabcdef\x1b[0m\rX\x1b[32mY')).toEqual([
      [span('X'), span('Y', 'success'), span('cdef', 'danger')],
    ])
  })

  it('erases the line as a terminal does, so a redrawn line leaves no trail', () => {
    expect(texts(parseAnsi('Downloading 100%\r\x1b[KDone\n'))).toEqual(['Done'])
    expect(texts(parseAnsi('spinner ⠋\x1b[2K\rDone'))).toEqual(['Done'])
    expect(texts(parseAnsi('abcdef\x1b[3D\x1b[1K!'))).toEqual(['   !ef'])
    expect(texts(parseAnsi('abc\x1b[2Kd'))).toEqual(['   d'])
    expect(texts(parseAnsi('\x1b[1Ka'))).toEqual(['a'])
  })

  it('moves along the line, and goes back a character at a backspace', () => {
    expect(texts(parseAnsi('abcdef\x1b[3GX\x1b[GY\x1b[2CZ\x1b[DW'))).toEqual(['YbXWef'])
    expect(texts(parseAnsi('ab\x1b[9DX\x1b[4CY'))).toEqual(['Xb   Y'])
    expect(texts(parseAnsi('|\b/\b-\b\\'))).toEqual(['\\'])
    expect(texts(parseAnsi('\bx'))).toEqual(['x'])
  })
})

describe('feedAnsi', () => {
  // inChunks reads text cut at every place in turn, and returns what each reading made.
  function everyCut(text: string): readonly AnsiLine[][] {
    const out: AnsiLine[][] = []

    for (let at = 0; at <= text.length; at++) {
      const state = feedAnsi(feedAnsi(emptyAnsi, text.slice(0, at)), text.slice(at))

      out.push([...ansiLines(state)])
    }

    return out
  }

  it('reads a log cut anywhere, an escape included, the same as whole', () => {
    const text = 'a\x1b[1;31mred\x1b[0m\r\nb\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x07\rB\x1b[K\n\x1b[38;5;2mc'
    const whole = parseAnsi(text)

    for (const lines of everyCut(text)) {
      expect(lines).toEqual(whole)
    }
  })

  it('holds an escape cut short until the rest arrives', () => {
    const cut = feedAnsi(emptyAnsi, 'plain\x1b[3')

    expect(cut.pending).toBe('\x1b[3')
    expect(texts(ansiLines(cut))).toEqual(['plain'])

    expect(feedAnsi(cut, '1mred').open).toEqual([span('plain'), span('red', 'danger')])
  })

  it('holds a lone ESC, and an unterminated string, at the end of a chunk', () => {
    expect(feedAnsi(emptyAnsi, 'a\x1b').pending).toBe('\x1b')
    expect(feedAnsi(emptyAnsi, 'a\x1b]8;;http').pending).toBe('\x1b]8;;http')
    expect(feedAnsi(emptyAnsi, 'a\x1b]8;;http\x1b').pending).toBe('\x1b]8;;http\x1b')
    expect(feedAnsi(emptyAnsi, 'a\x1b(').pending).toBe('\x1b(')
  })

  it('leaves the lines of the state it was given as they were', () => {
    const before = feedAnsi(emptyAnsi, 'one\ntwo')
    const lines = texts(ansiLines(before))
    const { count, cursor, open, pending } = before

    feedAnsi(before, '\rTWO\nthree\nfour\n')

    expect(texts(ansiLines(before))).toEqual(lines)
    expect(before).toMatchObject({ count, cursor, open, pending })
  })

  it('reads on from a state without copying its lines, and keeps them, so a view can keep them', () => {
    const before = feedAnsi(emptyAnsi, 'one\ntwo\n')
    const after = feedAnsi(before, 'three\n')

    expect(after.rows).toBe(before.rows)
    expect(linesFrom(after, 0)[0]).toBe(linesFrom(before, 0)[0])
    expect(texts(ansiLines(after))).toEqual(['one', 'two', 'three'])
    expect(texts(ansiLines(before))).toEqual(['one', 'two'])
  })

  it('reads on from a state read on from already, as React may, on a copy of its lines', () => {
    const before = feedAnsi(emptyAnsi, 'one\n')
    const first = feedAnsi(before, 'two\n')
    const again = feedAnsi(before, 'TWO\n')

    expect(again.rows).not.toBe(before.rows)
    expect(texts(ansiLines(first))).toEqual(['one', 'two'])
    expect(texts(ansiLines(again))).toEqual(['one', 'TWO'])
    expect(texts(ansiLines(feedAnsi(again, 'three\n')))).toEqual(['one', 'TWO', 'three'])
  })

  it('never adds to the empty state’s lines', () => {
    feedAnsi(emptyAnsi, 'one\n')

    expect(emptyAnsi.rows).toEqual([])
  })

  it('counts the lines it lists', () => {
    expect(lineCount(emptyAnsi)).toBe(0)
    expect(lineCount(feedAnsi(emptyAnsi, 'a\nb'))).toBe(2)
    expect(lineCount(feedAnsi(emptyAnsi, 'a\nb\n'))).toBe(2)
  })

  it('lists the lines from one on, the one being written last', () => {
    const state = feedAnsi(emptyAnsi, 'a\nb\nc')

    expect(texts(linesFrom(state, 1))).toEqual(['b', 'c'])
    expect(texts(linesFrom(state, 2))).toEqual(['c'])
    expect(linesFrom(state, 3)).toEqual([])
    expect(linesFrom(feedAnsi(emptyAnsi, 'a\n'), 1)).toEqual([])
  })

  it('lets the first lines go, and reads on in the style that was in force', () => {
    const state = dropLines(feedAnsi(emptyAnsi, 'a\n\x1b[31mb\nc'), 1)

    expect(lineCount(state)).toBe(2)
    expect(ansiLines(feedAnsi(state, '\nd\n'))).toEqual([
      [span('b', 'danger')],
      [span('c', 'danger')],
      [span('d', 'danger')],
    ])
  })
})

describe('plainText', () => {
  it('is the log as shown, without its escapes', () => {
    expect(plainText('\x1b[31mred\x1b[0m\r\n10%\r100%\n')).toBe('red\n100%\n')
    expect(plainText('a\nb')).toBe('a\nb')
    expect(plainText('')).toBe('')
  })
})
