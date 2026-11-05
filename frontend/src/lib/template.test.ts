import { describe, expect, it } from 'vitest'

import type { Diagnostic } from '../platform'
import { completionAt, diagnosticRange, highlight, matchVariables, offsetOf, tokenize } from './template'

const known = new Set(['Title', 'URL', 'Number', 'InWorktree', 'Branch'])

// kinds lists each token as "kind:text".
function kinds(src: string): string[] {
  return tokenize(src, known).map((t) => `${t.kind}:${t.text}`)
}

function diag(overrides: Partial<Diagnostic> = {}): Diagnostic {
  return { severity: 'error', line: 1, column: 1, length: 0, message: 'x', ...overrides }
}

describe('tokenize', () => {
  it('splits text from actions and colors what is inside them', () => {
    expect(kinds('Review {{.Title}} now')).toEqual([
      'text:Review ',
      'delim:{{',
      'variable:.Title',
      'delim:}}',
      'text: now',
    ])
  })

  it('reads keywords, functions, strings, unknown variables, and trim markers', () => {
    expect(kinds('{{- if eq .Checks "failing" -}}x{{end}}')).toEqual([
      'delim:{{-',
      'code: ',
      'keyword:if',
      'code: ',
      'function:eq',
      'code: ',
      'unknown:.Checks',
      'code: ',
      'string:"failing"',
      'code: ',
      'delim:-}}',
      'text:x',
      'delim:{{',
      'keyword:end',
      'delim:}}',
    ])
  })

  it('reads root variables, local variables, and dotted paths', () => {
    expect(kinds('{{$.Title}}{{$n := .Number}}{{.Title.Len}}')).toEqual([
      'delim:{{',
      'variable:$.Title',
      'delim:}}',
      'delim:{{',
      'code:$n',
      'code: ',
      'code::',
      'code:=',
      'code: ',
      'variable:.Number',
      'delim:}}',
      'delim:{{',
      'unknown:.Title.Len',
      'delim:}}',
    ])
  })

  it('keeps comments whole, closed or not', () => {
    expect(kinds('a {{/* note */}} b')).toEqual(['text:a ', 'comment:{{/* note */}}', 'text: b'])
    expect(kinds('a {{/* open')).toEqual(['text:a ', 'comment:{{/* open'])
  })

  it('reads an unclosed action to the end', () => {
    expect(kinds('x {{ .Title')).toEqual(['text:x ', 'delim:{{', 'code: ', 'variable:.Title'])
  })

  it('reads strings with escapes, and raw strings', () => {
    expect(kinds('{{printf "a\\"b" `c`}}').filter((t) => t.startsWith('string'))).toEqual([
      'string:"a\\"b"',
      'string:`c`',
    ])
  })

  it('always gives back the whole text', () => {
    for (const src of ['', 'plain', '{{', '}}', '{{-}}', '{{ .X -}', 'a{{.Title}}b{{/*c*/}}d{{if}}{{end']) {
      expect(
        tokenize(src, known)
          .map((t) => t.text)
          .join(''),
      ).toBe(src)
    }
  })
})

describe('diagnosticRange', () => {
  const src = 'One\nTwo {{.Titel}}\n'

  it('places a diagnostic by line, column, and length', () => {
    expect(diagnosticRange(src, diag({ line: 2, column: 7, length: 6 }))).toEqual([10, 16])
    expect(src.slice(10, 16)).toBe('.Titel')
  })

  it('covers the word for a diagnostic without a length', () => {
    expect(diagnosticRange(src, diag({ line: 2, column: 7 }))).toEqual([10, 16])
  })

  it('has no range for a problem with the whole prompt', () => {
    expect(diagnosticRange(src, diag({ line: 0 }))).toBeNull()
  })

  it('stays inside the text', () => {
    expect(diagnosticRange(src, diag({ line: 9, column: 1 }))).toEqual([src.length - 1, src.length])
    expect(diagnosticRange('', diag())).toEqual([0, 0])
  })
})

describe('highlight', () => {
  it('cuts tokens where an underline starts and stops, errors over warnings', () => {
    const src = 'Go {{.Titel}}'
    const segments = highlight(src, known, [
      diag({ line: 1, column: 6, length: 6 }),
      diag({ severity: 'warning', line: 1, column: 1, length: 5 }),
    ])

    expect(segments.map((s) => [s.text, s.kind, s.underline])).toEqual([
      ['Go ', 'text', 'warning'],
      ['{{', 'delim', 'warning'],
      ['.Titel', 'unknown', 'error'],
      ['}}', 'delim', ''],
    ])
  })

  it('draws plain text plainly', () => {
    expect(highlight('plain', known, [])).toEqual([{ text: 'plain', kind: 'text', underline: '' }])
  })
})

describe('completionAt', () => {
  it('offers variables right after an action opens', () => {
    expect(completionAt('Go {{', 5)).toEqual({ from: 5, to: 5, prefix: '', close: true })
    expect(completionAt('Go {{- ', 7)).toEqual({ from: 7, to: 7, prefix: '', close: true })
  })

  it('offers variables after a dot, replacing what was typed', () => {
    expect(completionAt('Go {{.Ti', 8)).toEqual({ from: 5, to: 8, prefix: 'Ti', close: true })
    expect(completionAt('{{if .In}}x{{end}}', 8)).toEqual({ from: 5, to: 8, prefix: 'In', close: false })
    expect(completionAt('{{if eq .Ch', 11)).toEqual({ from: 8, to: 11, prefix: 'Ch', close: true })
  })

  it('does not close an action that is already closed on the line', () => {
    expect(completionAt('{{.}}', 3)).toEqual({ from: 2, to: 3, prefix: '', close: false })
    expect(completionAt('{{.\n}}', 3)).toEqual({ from: 2, to: 3, prefix: '', close: true })
  })

  it('offers nothing outside an action, in a comment, or mid-word', () => {
    expect(completionAt('Go .Title', 9)).toBeNull()
    expect(completionAt('{{.Title}} x', 12)).toBeNull()
    expect(completionAt('{{/* .Ti', 8)).toBeNull()
    expect(completionAt('{{if x', 6)).toBeNull()
  })
})

describe('matchVariables', () => {
  it('lists names starting with what was typed, then names containing it', () => {
    expect(matchVariables(['Title', 'URL', 'InWorktree', 'Worktree'], 'wor')).toEqual(['Worktree', 'InWorktree'])
    expect(matchVariables(['Title', 'URL'], '')).toEqual(['Title', 'URL'])
    expect(matchVariables(['Title'], 'zz')).toEqual([])
  })
})

describe('offsetOf', () => {
  it('turns a line and column into an index, clamped to the text', () => {
    expect(offsetOf('ab\ncd', 2, 2)).toBe(4)
    expect(offsetOf('ab\ncd', 1, 1)).toBe(0)
    expect(offsetOf('ab\ncd', 9, 1)).toBe(5)
    expect(offsetOf('ab', 1, 99)).toBe(2)
    expect(offsetOf('ab', 0, 0)).toBe(0)
  })
})
