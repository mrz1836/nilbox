// The prompt editor's light reading of Go template text: enough to color it, to place
// the backend's diagnostics in it, and to offer variables while typing. Whether a prompt
// is valid is always decided by the backend, which parses it with Go's own template
// package, so nothing here needs to be complete.

import type { Diagnostic } from '../platform'

export type TokenKind =
  'text' | 'delim' | 'keyword' | 'variable' | 'unknown' | 'function' | 'string' | 'comment' | 'code'

export interface Token {
  kind: TokenKind
  text: string
}

// Segment is a run of text drawn one way: its token kind, and the severity of a
// diagnostic that underlines it, if any.
export interface Segment {
  text: string
  kind: TokenKind
  underline: '' | 'error' | 'warning'
}

const keywords = new Set(['if', 'else', 'end', 'with', 'range', 'define', 'block', 'template', 'break', 'continue'])

const functions = new Set([
  'and',
  'or',
  'not',
  'eq',
  'ne',
  'lt',
  'le',
  'gt',
  'ge',
  'len',
  'printf',
  'print',
  'println',
  'call',
  'index',
  'slice',
  'html',
  'js',
  'urlquery',
])

const identStart = /[A-Za-z_]/
const identChar = /\w/

// tokenize splits a prompt into plain text and the pieces of each {{ action }}. known
// holds the variable names, so a misspelled one can be colored as unknown.
export function tokenize(src: string, known: ReadonlySet<string>): Token[] {
  const tokens: Token[] = []
  let pos = 0

  while (pos < src.length) {
    const open = src.indexOf('{{', pos)

    if (open < 0) {
      tokens.push({ kind: 'text', text: src.slice(pos) })
      break
    }

    if (open > pos) {
      tokens.push({ kind: 'text', text: src.slice(pos, open) })
    }

    pos = readAction(src, open, known, tokens)
  }

  return tokens
}

// readAction adds the tokens of the action starting at src[open] and returns where it
// ends.
function readAction(src: string, open: number, known: ReadonlySet<string>, tokens: Token[]): number {
  const comment = /^\{\{-?\s*\/\*/.exec(src.slice(open))

  if (comment !== null) {
    const closeComment = src.indexOf('*/', open + comment[0].length)
    const close = closeComment < 0 ? -1 : src.indexOf('}}', closeComment)
    const end = close < 0 ? src.length : close + 2

    tokens.push({ kind: 'comment', text: src.slice(open, end) })

    return end
  }

  const openDelim = src.startsWith('{{-', open) ? '{{-' : '{{'
  tokens.push({ kind: 'delim', text: openDelim })

  const close = src.indexOf('}}', open + openDelim.length)
  const innerEnd = close < 0 ? src.length : close > 0 && src[close - 1] === '-' ? close - 1 : close
  const inner = src.slice(open + openDelim.length, Math.max(innerEnd, open + openDelim.length))

  readInner(inner, known, tokens)

  if (close < 0) {
    return src.length
  }

  tokens.push({ kind: 'delim', text: src.slice(Math.max(innerEnd, open + openDelim.length), close + 2) })

  return close + 2
}

// readInner adds the tokens inside an action's braces.
function readInner(inner: string, known: ReadonlySet<string>, tokens: Token[]) {
  let i = 0

  while (i < inner.length) {
    const ch = inner.charAt(i)
    const end = innerTokenEnd(inner, i)
    const text = inner.slice(i, end)

    tokens.push({ kind: innerKind(text, ch, known), text })
    i = end
  }
}

// innerTokenEnd returns where the token starting at inner[i] ends.
function innerTokenEnd(inner: string, i: number): number {
  const ch = inner.charAt(i)

  if (ch === '"' || ch === '`' || ch === "'") {
    for (let j = i + 1; j < inner.length; j++) {
      if (inner[j] === '\\' && ch !== '`') {
        j++
      } else if (inner[j] === ch) {
        return j + 1
      }
    }

    return inner.length
  }

  if (ch === '.' || ch === '$' || identStart.test(ch)) {
    let j = i + 1

    while (
      j < inner.length &&
      (identChar.test(inner.charAt(j)) || (inner[j] === '.' && identStart.test(inner.charAt(j + 1))))
    ) {
      j++
    }

    return j
  }

  if (/\s/.test(ch)) {
    let j = i + 1

    while (j < inner.length && /\s/.test(inner.charAt(j))) {
      j++
    }

    return j
  }

  return i + 1
}

// innerKind classifies one token inside an action.
function innerKind(text: string, first: string, known: ReadonlySet<string>): TokenKind {
  if (first === '"' || first === '`' || first === "'") {
    return 'string'
  }

  if (first === '.' && text.length > 1) {
    return fieldKind(text.slice(1), known)
  }

  if (first === '$') {
    return text.startsWith('$.') ? fieldKind(text.slice(2), known) : 'code'
  }

  if (keywords.has(text)) {
    return 'keyword'
  }

  return functions.has(text) ? 'function' : 'code'
}

// fieldKind colors a variable path: a single known name is a variable, anything else is
// unknown.
function fieldKind(path: string, known: ReadonlySet<string>): TokenKind {
  return known.has(path) ? 'variable' : 'unknown'
}

// lineStarts returns the index where each line of src starts.
function lineStarts(src: string): number[] {
  const starts = [0]

  for (let i = 0; i < src.length; i++) {
    if (src[i] === '\n') {
      starts.push(i + 1)
    }
  }

  return starts
}

// diagnosticRange returns the [start, end) indexes a diagnostic covers in src, or null
// for one about the whole prompt. A diagnostic without a length covers the word it
// points at, or one character.
export function diagnosticRange(src: string, d: Diagnostic): [number, number] | null {
  if (d.line < 1) {
    return null
  }

  const start = (lineStarts(src)[d.line - 1] ?? src.length) + Math.max(d.column - 1, 0)

  if (start >= src.length) {
    return [Math.max(src.length - 1, 0), src.length]
  }

  if (d.length > 0) {
    return [start, Math.min(start + d.length, src.length)]
  }

  let end = start + 1

  while (end < src.length && /[\w.$]/.test(src.charAt(end))) {
    end++
  }

  return [start, end]
}

// highlight splits a prompt into segments to draw: each token's text, cut further where
// a diagnostic's underline starts or stops. Errors win over warnings where they overlap.
export function highlight(src: string, known: ReadonlySet<string>, diagnostics: readonly Diagnostic[]): Segment[] {
  const marks: ('' | 'error' | 'warning')[] = new Array<'' | 'error' | 'warning'>(src.length).fill('')

  for (const d of diagnostics) {
    const range = diagnosticRange(src, d)

    if (range === null) {
      continue
    }

    for (let i = range[0]; i < range[1]; i++) {
      if (marks[i] !== 'error') {
        marks[i] = d.severity === 'error' ? 'error' : 'warning'
      }
    }
  }

  const segments: Segment[] = []
  let pos = 0

  for (const token of tokenize(src, known)) {
    let start = 0

    for (let i = 1; i <= token.text.length; i++) {
      if (i === token.text.length || marks[pos + i] !== marks[pos + start]) {
        segments.push({ text: token.text.slice(start, i), kind: token.kind, underline: marks[pos + start] ?? '' })
        start = i
      }
    }

    pos += token.text.length
  }

  return segments
}

// Completion is where the editor can offer variables: the text from `from` to `to` (a
// dot and the letters typed after it, or nothing right after "{{") is replaced with the
// chosen ".Name", and "}}" is added when the action is not closed yet.
export interface Completion {
  from: number
  to: number
  prefix: string
  close: boolean
}

// completionAt returns where variables can be offered for a caret at index caret, or
// null when it is not inside an open action, is inside a comment, or follows anything
// but a dot or the action's opening.
export function completionAt(src: string, caret: number): Completion | null {
  const before = src.slice(0, caret)
  const open = before.lastIndexOf('{{')

  if (open < 0 || before.includes('}}', open)) {
    return null
  }

  const action = before.slice(open)

  if (/^\{\{-?\s*\/\*/.test(action)) {
    return null
  }

  const after = src.slice(caret)
  const nextClose = after.indexOf('}}')
  const nextOpen = after.indexOf('{{')
  const lineEnd = after.indexOf('\n')
  const closed = nextClose >= 0 && (nextOpen < 0 || nextClose < nextOpen) && (lineEnd < 0 || nextClose < lineEnd)

  const dotted = /(^|[\s($])\.(\w*)$/.exec(action.slice(2))

  if (dotted !== null) {
    const prefix = dotted[2] ?? ''

    return { from: caret - prefix.length - 1, to: caret, prefix, close: !closed }
  }

  if (/^\{\{-?\s*$/.test(action)) {
    return { from: caret, to: caret, prefix: '', close: !closed }
  }

  return null
}

// matchVariables returns the names that match what was typed: those starting with it
// first, then those containing it, ignoring case.
export function matchVariables(names: readonly string[], prefix: string): string[] {
  const p = prefix.toLowerCase()
  const starts = names.filter((n) => n.toLowerCase().startsWith(p))
  const contains = names.filter((n) => !n.toLowerCase().startsWith(p) && n.toLowerCase().includes(p))

  return [...starts, ...contains]
}

// offsetOf returns the index of a 1-based line and column in src, clamped to the text.
export function offsetOf(src: string, line: number, column: number): number {
  const start = lineStarts(src)[Math.max(line, 1) - 1] ?? src.length

  return Math.min(start + Math.max(column - 1, 0), src.length)
}
