// Terminal output as the log view shows it: a run's log, read as it arrives, turned into
// lines of styled text. Only the basics show: the eight colors and their bright forms,
// mapped to the theme's own colors, bold, and reset. Every other escape is dropped, so a
// log never shows the raw bytes of one, and nothing here builds markup: a span is text and
// a style, which the view renders as text.
//
// A line is written the way a terminal writes it. A carriage return goes back to the
// line's start, and what follows overwrites the line from there, so a progress bar that
// redraws itself leaves only its last state; erasing the line (CSI K) and moving along it
// (CSI G, C, D) are followed for the same reason, and show nothing themselves. A line
// feed ends the line, and \r\n is a plain line end.
//
// The log arrives in chunks, and an escape can be cut between two of them. The state
// carries what was cut off (pending) into the next chunk, with the style and the line
// being written, so feeding a log in chunks reads it exactly as feeding it whole.

// AnsiTone is the theme color a span is drawn in.
export type AnsiTone = 'muted' | 'danger' | 'success' | 'warning' | 'info' | 'accent' | 'text'

// AnsiSpan is a run of text in one style: its color, or null for the log's own, and
// whether it is bold.
export interface AnsiSpan {
  readonly text: string
  readonly tone: AnsiTone | null
  readonly bold: boolean
}

// AnsiLine is one line of the log, as spans; an empty line has none.
export type AnsiLine = readonly AnsiSpan[]

// Style is what the text written next is drawn in.
interface Style {
  readonly tone: AnsiTone | null
  readonly bold: boolean
}

// AnsiState is a log read so far: its finished lines, the line being written and where in
// it the next text goes, the style in force, and an escape cut off at the end of the last
// chunk.
//
// The finished lines are the first count of rows. A state read on from appends its lines
// to the same array, unless another state already has, so reading on costs what the new
// text does, never what the log holds; the lines a state counts never change. Read them
// with linesFrom and lineCount.
export interface AnsiState {
  readonly rows: readonly AnsiLine[]
  readonly count: number
  readonly open: AnsiLine
  readonly cursor: number
  readonly style: Style
  readonly pending: string
}

const plain: Style = { tone: null, bold: false }

// emptyAnsi is a log with nothing read yet.
export const emptyAnsi: AnsiState = { rows: [], count: 0, open: [], cursor: 0, style: plain, pending: '' }

// toneOf is one of the eight colors (black, red, green, yellow, blue, magenta, cyan,
// white, by their numbers 0 to 7) as the theme draws it: black and white would vanish on
// one theme or the other, so they take the muted and the plain text colors. The bright
// forms take the same.
function toneOf(color: number): AnsiTone {
  switch (color) {
    case 1:
      return 'danger'
    case 2:
      return 'success'
    case 3:
      return 'warning'
    case 4:
    case 6:
      return 'info'
    case 5:
      return 'accent'
    case 7:
      return 'text'
    default:
      return 'muted'
  }
}

const ESC = 0x1b

// width is how many characters a line holds.
function width(line: AnsiLine): number {
  let n = 0

  for (const span of line) {
    n += span.text.length
  }

  return n
}

// cut returns the part of line from column from up to column to, as spans.
function cut(line: AnsiLine, from: number, to: number): AnsiSpan[] {
  const out: AnsiSpan[] = []
  let at = 0

  for (const span of line) {
    const end = at + span.text.length

    if (end > from && at < to) {
      const text = span.text.slice(Math.max(from - at, 0), to - at)

      out.push(text === span.text ? span : { ...span, text })
    }

    at = end
  }

  return out
}

// joined merges neighbouring spans of the same style, and drops empty ones.
function joined(spans: readonly AnsiSpan[]): AnsiSpan[] {
  const out: AnsiSpan[] = []

  for (const span of spans) {
    const last = out.at(-1)

    if (span.text === '') {
      continue
    }

    if (last !== undefined && last.tone === span.tone && last.bold === span.bold) {
      out[out.length - 1] = { ...last, text: last.text + span.text }
    } else {
      out.push(span)
    }
  }

  return out
}

// sequenceEnd returns where the escape sequence that starts at i (an ESC) ends: the index
// just past it, or -1 when the text ends inside it. A malformed sequence ends before the
// character that broke it, which is then read as it is.
function sequenceEnd(s: string, i: number): number {
  if (i + 1 >= s.length) {
    return -1
  }

  const kind = s.charCodeAt(i + 1)

  // CSI: parameters and intermediates, then a final byte.
  if (kind === 0x5b) {
    for (let j = i + 2; j < s.length; j++) {
      const c = s.charCodeAt(j)

      if (c >= 0x40 && c <= 0x7e) {
        return j + 1
      }

      if (c < 0x20 || c > 0x3f) {
        return j
      }
    }

    return -1
  }

  // OSC, DCS, SOS, PM, and APC: a string that ends at BEL or ST (ESC \). A line feed ends
  // one too, so a stray introducer can't hide the rest of the log.
  if (kind === 0x5d || kind === 0x50 || kind === 0x58 || kind === 0x5e || kind === 0x5f) {
    for (let j = i + 2; j < s.length; j++) {
      const c = s.charCodeAt(j)

      if (c === 0x07 || c === 0x9c) {
        return j + 1
      }

      if (c === 0x0a) {
        return j
      }

      if (c === ESC) {
        if (j + 1 >= s.length) {
          return -1
        }

        return s.charCodeAt(j + 1) === 0x5c ? j + 2 : j
      }
    }

    return -1
  }

  // Intermediates, then a final byte, as in ESC ( B.
  if (kind >= 0x20 && kind <= 0x2f) {
    for (let j = i + 2; j < s.length; j++) {
      const c = s.charCodeAt(j)

      if (c >= 0x30 && c <= 0x7e) {
        return j + 1
      }

      if (c < 0x20 || c > 0x2f) {
        return j
      }
    }

    return -1
  }

  // A two-character escape, such as ESC 7 or ESC M; an ESC before anything else stands
  // alone.
  return kind >= 0x30 && kind <= 0x7e ? i + 2 : i + 1
}

// numbers reads a CSI's parameters: each one's numbers, split at its colons, an empty one
// read as 0.
function numbers(params: string): number[][] {
  return params.split(';').map((p) => p.split(':').map((n) => (n === '' ? 0 : Number(n))))
}

// extendedTone is the tone of a 256-color foreground: the sixteen basic colors keep
// theirs, and any other is the log's own color.
function extendedTone(index: number | undefined): AnsiTone | null {
  return index !== undefined && index < 16 ? toneOf(index % 8) : null
}

// sgr applies a Select Graphic Rendition's parameters to style: reset, bold on and off,
// and the foreground colors. The rest (dim, italics, underlines, backgrounds) is dropped.
function sgr(params: string, style: Style): Style {
  const groups = numbers(params)
  let { tone, bold } = style
  // skip counts the parameters an extended color took after its own.
  let skip = 0

  for (const [k, [first, ...sub]] of groups.entries()) {
    // Every parameter has a first number; Number keeps the type checker sure of it.
    const code = Number(first)

    if (skip > 0) {
      skip--
    } else if (code === 0) {
      tone = null
      bold = false
    } else if (code === 1) {
      bold = true
    } else if (code === 22) {
      bold = false
    } else if (code === 39) {
      tone = null
    } else if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
      tone = toneOf(code % 10)
    } else if (code === 38 || code === 48) {
      // An extended color: 5 and an index of the 256, or 2 and its red, green, and blue,
      // written after colons in the same parameter or as the parameters that follow.
      const colons = sub.length > 0
      const [form, index] = colons ? sub : [groups[k + 1]?.[0], groups[k + 2]?.[0]]

      if (!colons) {
        skip = form === 5 ? 2 : form === 2 ? 4 : 0
      }

      if (code === 38) {
        tone = form === 5 ? extendedTone(index) : null
      }
    }
  }

  return { tone, bold }
}

// feedAnsi reads more of a log: text, the next chunk, after what state has read, and
// returns the state after text. What state counts stays as it was: the new lines join its
// rows only while no other state has added to them (a state fed twice, as React may do
// with an update), and a copy of its own lines otherwise.
export function feedAnsi(state: AnsiState, text: string): AnsiState {
  const s = state.pending + text
  const lines: AnsiLine[] =
    state.count > 0 && state.rows.length === state.count
      ? // Only states read on from this one ever add to rows, and none has yet.
        (state.rows as AnsiLine[])
      : state.rows.slice(0, state.count)
  let open: AnsiSpan[] = state.open.slice()
  let size = width(open)
  let cursor = state.cursor
  let style = state.style
  let pending = ''

  // write puts text at the cursor, over what is there, as a terminal does.
  const write = (chunk: string) => {
    const span: AnsiSpan = { text: chunk, tone: style.tone, bold: style.bold }

    if (cursor > size) {
      open.push({ text: ' '.repeat(cursor - size), tone: null, bold: false })
      size = cursor
    }

    if (cursor === size) {
      const last = open.at(-1)

      if (last !== undefined && last.tone === span.tone && last.bold === span.bold) {
        open[open.length - 1] = { ...last, text: last.text + chunk }
      } else {
        open.push(span)
      }
    } else {
      open = joined([...cut(open, 0, cursor), span, ...cut(open, cursor + chunk.length, size)])
    }

    cursor += chunk.length
    size = Math.max(size, cursor)
  }

  // erase clears part of the line: from the cursor to its end, from its start to the
  // cursor, or all of it.
  const erase = (mode: number) => {
    if (mode === 0) {
      open = cut(open, 0, cursor)
      size = Math.min(size, cursor)
    } else if (mode === 1) {
      const blank = Math.min(cursor + 1, size)

      open = joined([{ text: ' '.repeat(blank), tone: null, bold: false }, ...cut(open, blank, size)])
    } else if (mode === 2) {
      open = []
      size = 0
    }
  }

  // control follows a CSI the log view acts on: colors, erasing the line, and moving
  // along it. Every other one is dropped.
  const control = (sequence: string) => {
    const params = sequence.slice(2, -1)
    const n = Number(params.split(';')[0]) || 0

    if (!/^[\d;:]*$/.test(params)) {
      return
    }

    switch (sequence.at(-1)) {
      case 'm':
        style = sgr(params, style)
        break
      case 'K':
        erase(n)
        break
      case 'G':
        cursor = Math.max(n, 1) - 1
        break
      case 'C':
        cursor += Math.max(n, 1)
        break
      case 'D':
        cursor = Math.max(cursor - Math.max(n, 1), 0)
        break
    }
  }

  let run = 0
  let i = 0

  while (i < s.length) {
    const c = s.charCodeAt(i)

    // Printable text, tabs among it, runs on until a control character.
    if ((c >= 0x20 && c !== 0x7f && (c < 0x80 || c > 0x9f)) || c === 0x09) {
      i++
      continue
    }

    if (i > run) {
      write(s.slice(run, i))
    }

    if (c === 0x0a) {
      lines.push(open)
      open = []
      size = 0
      cursor = 0
      i++
    } else if (c === 0x0d) {
      cursor = 0
      i++
    } else if (c === 0x08) {
      cursor = Math.max(cursor - 1, 0)
      i++
    } else if (c === ESC) {
      const end = sequenceEnd(s, i)

      if (end === -1) {
        pending = s.slice(i)
        i = s.length
      } else {
        if (s.charCodeAt(i + 1) === 0x5b) {
          control(s.slice(i, end))
        }

        i = end
      }
    } else {
      // Every other control character shows nothing.
      i++
    }

    run = i
  }

  if (run < s.length) {
    write(s.slice(run))
  }

  return { rows: lines, count: lines.length, open, cursor, style, pending }
}

// lineCount is how many lines a log has: the finished ones, and the one being written, if
// it holds anything.
export function lineCount(state: AnsiState): number {
  return state.count + (state.open.length === 0 ? 0 : 1)
}

// linesFrom lists a log's lines from the line numbered from (counting from 0): the
// finished ones, then the one being written, if it holds anything. It costs what it
// lists, so a view lists only the lines it shows.
export function linesFrom(state: AnsiState, from: number): AnsiLine[] {
  const lines = state.rows.slice(from, state.count)

  if (state.open.length > 0 && from <= state.count) {
    lines.push(state.open)
  }

  return lines
}

// ansiLines lists all of a log's lines.
export function ansiLines(state: AnsiState): readonly AnsiLine[] {
  return linesFrom(state, 0)
}

// dropLines lets a log's first n finished lines go, as a view does with the oldest
// output; the lines that stay keep their style.
export function dropLines(state: AnsiState, n: number): AnsiState {
  return { ...state, rows: state.rows.slice(n, state.count), count: state.count - n }
}

// parseAnsi reads a whole log into lines.
export function parseAnsi(text: string): readonly AnsiLine[] {
  return ansiLines(feedAnsi(emptyAnsi, text))
}

// plainText is a log as the view shows it, as plain text: every escape gone, and every
// overwritten line as it was left. It ends with a line feed when the log does.
export function plainText(text: string): string {
  const state = feedAnsi(emptyAnsi, text)
  const body = ansiLines(state)
    .map((line) => line.map((span) => span.text).join(''))
    .join('\n')

  return state.open.length === 0 && state.count > 0 ? `${body}\n` : body
}
