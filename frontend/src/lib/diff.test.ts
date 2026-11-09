import { describe, expect, it } from 'vitest'

import {
  canComment,
  canRange,
  canSuggest,
  type DiffRow,
  type DiffSide,
  isGenerated,
  isSuggestion,
  parsePatch,
  rowLine,
  rowSide,
  startsCollapsed,
  suggestionBlock,
} from './diff'

// The sample patches below are the ones internal/review/hunks_test.go reads too, with the
// same lines accepted on each side, so the two parsers are held to one answer.

// multiHunkPatch is a realistic two-hunk patch. Its first hunk numbers lines 1 to 4 of
// the base and 1 to 6 of the head; its second, lines 10 to 15 and 12 to 18.
const multiHunkPatch = [
  '@@ -1,4 +1,6 @@',
  ' package main',
  ' ',
  '-import "fmt"',
  '+import (',
  '+\t"fmt"',
  '+)',
  ' ',
  '@@ -10,6 +12,7 @@ func main() {',
  ' \ta := 1',
  ' \tb := 2',
  '-\tfmt.Println(a)',
  '+\tfmt.Println(a, b)',
  '+\tfmt.Println(b)',
  ' \tc := 3',
  ' \t_ = c',
  ' }',
].join('\n')

// junkPatch has git's file headers before its first hunk, and a malformed header whose
// lines are skipped.
const junkPatch = [
  'diff --git a/x b/x',
  'index 1234567..89abcde 100644',
  '--- a/x',
  '+++ b/x',
  ' stray context',
  '@@ -1,2 +1,2 @@',
  ' a',
  '-b',
  '+c',
  '@@ -x,1 +5 @@',
  '+skipped',
  '@@ -10,1 +10,1 @@',
  '-d',
  '+e',
].join('\n')

// samplePatches are patches with the lines each takes comments on, on each side.
const samplePatches: { name: string; text: string; left: number[]; right: number[] }[] = [
  { name: 'empty', text: '', left: [], right: [] },
  {
    name: 'multiple hunks',
    text: multiHunkPatch,
    left: [1, 2, 3, 4, 10, 11, 12, 13, 14, 15],
    right: [1, 2, 3, 4, 5, 6, 12, 13, 14, 15, 16, 17, 18],
  },
  { name: 'new file', text: '@@ -0,0 +1,3 @@\n+one\n+two\n+three', left: [], right: [1, 2, 3] },
  { name: 'deleted file', text: '@@ -1,2 +0,0 @@\n-one\n-two', left: [1, 2], right: [] },
  { name: 'omitted counts', text: '@@ -1 +1 @@\n-old\n+new', left: [1], right: [1] },
  {
    name: 'no newline at end of file',
    text: '@@ -1,2 +1,2 @@\n keep\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file',
    left: [1, 2],
    right: [1, 2],
  },
  {
    name: 'CRLF line endings',
    text: '@@ -1,2 +1,2 @@ section\r\n line\r\n-old\r\n+new\r\n',
    left: [1, 2],
    right: [1, 2],
  },
  { name: 'junk and a malformed header', text: junkPatch, left: [1, 2, 10], right: [1, 2, 10] },
  {
    name: "rows past the header's counts",
    text: '@@ -1,1 +1,1 @@\n a\n+extra\n b\n@@ -5,1 +5,2 @@\n c',
    left: [1, 5],
    right: [1, 5],
  },
  { name: 'a line that is not a row', text: '@@ -1,3 +1,3 @@\n a\njunk\n b', left: [1], right: [1] },
  { name: 'an empty line', text: '@@ -1,3 +1,3 @@\n a\n\n b', left: [1], right: [1] },
  { name: 'a context row with one side full', text: '@@ -1,1 +1,2 @@\n a\n b', left: [1], right: [1] },
  { name: 'a removed row past the count', text: '@@ -1,1 +1,1 @@\n-a\n-b\n+c', left: [1], right: [] },
  { name: 'a range from line 0', text: '@@ -0,1 +1,1 @@\n-a\n+b', left: [], right: [] },
  { name: 'a number too large', text: '@@ -1000000001 +1 @@\n-a\n+b', left: [], right: [] },
  { name: 'a count too large', text: '@@ -1,1000000001 +1 @@\n-a\n+b', left: [], right: [] },
  { name: 'a number past int', text: '@@ -1 +99999999999999999999999 @@\n-a\n+b', left: [], right: [] },
  { name: 'a combined diff', text: '@@@ -1,2 -1,2 +1,3 @@@\n  a\n+ b', left: [], right: [] },
  { name: 'a header missing a count', text: '@@ -1, +1 @@\n a', left: [], right: [] },
  { name: 'the largest line', text: '@@ -1000000000 +1000000000 @@\n x', left: [1e9], right: [1e9] },
  { name: 'a note before any row', text: '@@ -3 +3 @@\n\\ note\n x', left: [3], right: [3] },
]

// probes are the lines the Go tests ask about: 0 to 20 and those near the largest line.
const probes = [...Array.from({ length: 21 }, (_, i) => i), 1e9 - 1, 1e9, 1e9 + 1]

// numbered lists the line numbers rows give on side, in order: what a context row and
// an added row number on the head side, or a context row and a removed row on the base.
function numbered(rows: readonly DiffRow[], side: DiffSide): number[] {
  return rows.flatMap((row) => {
    const line = side === 'RIGHT' ? row.newLine : row.oldLine

    return line === null ? [] : [line]
  })
}

// row is a parsed row, with no line numbers unless given.
function row(
  kind: DiffRow['kind'],
  text: string,
  hunk: number,
  oldLine: number | null,
  newLine: number | null,
): DiffRow {
  return { kind, text, oldLine, newLine, hunk }
}

describe('parsePatch', () => {
  it.each(samplePatches)('takes comments on the same lines as the backend: $name', ({ text, left, right }) => {
    const rows = parsePatch(text)

    expect(numbered(rows, 'LEFT')).toEqual(left)
    expect(numbered(rows, 'RIGHT')).toEqual(right)
    expect(probes.filter((line) => canComment(rows, 'LEFT', line))).toEqual(left)
    expect(probes.filter((line) => canComment(rows, 'RIGHT', line))).toEqual(right)
  })

  it('reads every row of every hunk', () => {
    expect(parsePatch(multiHunkPatch)).toEqual([
      row('hunk', '@@ -1,4 +1,6 @@', 0, null, null),
      row('context', 'package main', 0, 1, 1),
      row('context', '', 0, 2, 2),
      row('del', 'import "fmt"', 0, 3, null),
      row('add', 'import (', 0, null, 3),
      row('add', '\t"fmt"', 0, null, 4),
      row('add', ')', 0, null, 5),
      row('context', '', 0, 4, 6),
      row('hunk', '@@ -10,6 +12,7 @@ func main() {', 1, null, null),
      row('context', '\ta := 1', 1, 10, 12),
      row('context', '\tb := 2', 1, 11, 13),
      row('del', '\tfmt.Println(a)', 1, 12, null),
      row('add', '\tfmt.Println(a, b)', 1, null, 14),
      row('add', '\tfmt.Println(b)', 1, null, 15),
      row('context', '\tc := 3', 1, 13, 16),
      row('context', '\t_ = c', 1, 14, 17),
      row('context', '}', 1, 15, 18),
    ])
  })

  it('keeps notes and drops carriage returns', () => {
    expect(parsePatch('@@ -1 +1 @@ section\r\n-old\r\n\\ No newline at end of file\r\n+new\r\n')).toEqual([
      row('hunk', '@@ -1 +1 @@ section', 0, null, null),
      row('del', 'old', 0, 1, null),
      row('note', ' No newline at end of file', 0, null, null),
      row('add', 'new', 0, null, 1),
    ])
  })

  it('numbers hunks by their good headers and skips the rest', () => {
    expect(parsePatch(junkPatch)).toEqual([
      row('hunk', '@@ -1,2 +1,2 @@', 0, null, null),
      row('context', 'a', 0, 1, 1),
      row('del', 'b', 0, 2, null),
      row('add', 'c', 0, null, 2),
      row('hunk', '@@ -10,1 +10,1 @@', 1, null, null),
      row('del', 'd', 1, 10, null),
      row('add', 'e', 1, null, 10),
    ])
  })

  it('reads an insertion after a line and a header with no rows', () => {
    expect(parsePatch('@@ -5,0 +6,1 @@\n+new\n@@ -0,0 +0,0 @@')).toEqual([
      row('hunk', '@@ -5,0 +6,1 @@', 0, null, null),
      row('add', 'new', 0, null, 6),
      row('hunk', '@@ -0,0 +0,0 @@', 1, null, null),
    ])
  })

  it('reads a patch with no hunks as no rows', () => {
    expect(parsePatch('Binary files differ\n')).toEqual([])
  })
})

describe('rowSide and rowLine', () => {
  it('put each row on the side a comment on it goes', () => {
    const rows = parsePatch('@@ -1,2 +1,2 @@\n keep\n-old\n+new\n\\ No newline at end of file')

    expect(rows.map(rowSide)).toEqual([null, 'RIGHT', 'LEFT', 'RIGHT', null])
    expect(rows.map(rowLine)).toEqual([null, 1, 2, 2, null])
  })
})

describe('canComment', () => {
  const rows = parsePatch(multiHunkPatch)

  it('takes added and removed lines on their sides, and context lines on either', () => {
    expect(canComment(rows, 'RIGHT', 3)).toBe(true)
    expect(canComment(rows, 'RIGHT', 16)).toBe(true)
    expect(canComment(rows, 'LEFT', 12)).toBe(true)
    expect(canComment(rows, 'LEFT', 4)).toBe(true)
    expect(canComment(rows, 'RIGHT', 6)).toBe(true)
    expect(canComment(rows, 'LEFT', 1)).toBe(true)
  })

  it('finds a row by its number on the side asked', () => {
    // Base line 13 is the context row the head numbers 16, and head line 13 is the one
    // the base numbers 11.
    expect(canComment(rows, 'LEFT', 13)).toBe(true)
    expect(canComment(rows, 'RIGHT', 13)).toBe(true)
    expect(canComment(rows, 'LEFT', 18)).toBe(false)
    expect(canComment(rows, 'RIGHT', 10)).toBe(false)
  })

  it('refuses lines the diff does not show', () => {
    expect(canComment(rows, 'RIGHT', 7)).toBe(false)
    expect(canComment(rows, 'RIGHT', 9)).toBe(false)
    expect(canComment(rows, 'RIGHT', 19)).toBe(false)
    expect(canComment(rows, 'LEFT', 16)).toBe(false)
    expect(canComment(rows, 'RIGHT', 0)).toBe(false)
    expect(canComment(rows, 'LEFT', -1)).toBe(false)
  })

  it('refuses every line of a file with no patch', () => {
    expect(canComment(parsePatch(''), 'RIGHT', 1)).toBe(false)
    expect(canComment(parsePatch('Binary files differ'), 'RIGHT', 1)).toBe(false)
  })
})

describe('canRange', () => {
  const rows = parsePatch(multiHunkPatch)

  it('takes a range within one hunk', () => {
    expect(canRange(rows, 'RIGHT', 3, 5)).toBe(true)
    expect(canRange(rows, 'LEFT', 10, 15)).toBe(true)
    expect(canRange(rows, 'RIGHT', 14, 15)).toBe(true)
    expect(canRange(rows, 'LEFT', 11, 12)).toBe(true)
  })

  it('refuses a range with an end outside the diff', () => {
    expect(canRange(rows, 'RIGHT', 8, 12)).toBe(false)
    expect(canRange(rows, 'RIGHT', 5, 8)).toBe(false)
  })

  it('refuses a range across hunks', () => {
    expect(canRange(rows, 'RIGHT', 6, 12)).toBe(false)
    expect(canRange(rows, 'LEFT', 4, 10)).toBe(false)
  })

  it('refuses a backward range', () => {
    expect(canRange(rows, 'RIGHT', 5, 3)).toBe(false)
  })

  it('treats a range of one line as a one-line comment', () => {
    expect(canRange(rows, 'RIGHT', 3, 3)).toBe(true)
    expect(canRange(rows, 'RIGHT', 7, 7)).toBe(false)
  })
})

describe('suggestions', () => {
  it('go only on the head version', () => {
    expect(canSuggest('RIGHT')).toBe(true)
    expect(canSuggest('LEFT')).toBe(false)
  })

  it('wrap the lines in a suggestion block', () => {
    expect(suggestionBlock(['\tfmt.Println(a)', '\treturn nil'])).toBe(
      '```suggestion\n\tfmt.Println(a)\n\treturn nil\n```',
    )
    expect(isSuggestion(suggestionBlock(['x']))).toBe(true)
  })

  it.each([
    { name: 'a suggestion block', body: '```suggestion\nreturn nil\n```', want: true },
    { name: 'after text', body: 'Try:\n```suggestion\nreturn nil\n```', want: true },
    { name: 'indented with spaces', body: '   ```suggestion\nx\n```', want: true },
    { name: 'indented with a tab', body: '\t```suggestion\nx\n```', want: true },
    { name: 'a longer fence', body: '````suggestion\nx\n````', want: true },
    { name: 'space before the word', body: '``` suggestion\nx\n```', want: true },
    { name: 'a CRLF line', body: '```suggestion\r\nx\r\n```', want: true },
    { name: 'an empty suggestion', body: '```suggestion\n```', want: true },
    { name: 'a code block', body: '```go\nreturn nil\n```', want: false },
    { name: 'two backticks', body: '``suggestion\nx\n``', want: false },
    { name: 'mid-line', body: 'Use a ```suggestion block.', want: false },
    { name: 'another case', body: '```Suggestion\nx\n```', want: false },
    { name: 'plain text', body: 'suggestion', want: false },
    { name: 'empty', body: '', want: false },
    { name: 'after a lone carriage return', body: 'x\r```suggestion', want: false },
  ])('are read from a comment the way the backend reads them: $name', ({ body, want }) => {
    expect(isSuggestion(body)).toBe(want)
  })
})

describe('isGenerated', () => {
  it.each([
    'go.sum',
    'go.work.sum',
    'frontend/pnpm-lock.yaml',
    'package-lock.json',
    'npm-shrinkwrap.json',
    'yarn.lock',
    'bun.lockb',
    'bun.lock',
    'Cargo.lock',
    'Gemfile.lock',
    'composer.lock',
    'poetry.lock',
    'Pipfile.lock',
    'uv.lock',
    'flake.lock',
    'mix.lock',
    'ios/Podfile.lock',
    'src/App/packages.lock.json',
    'GO.SUM',
    'vendor/github.com/acme-org/lib/lib.go',
    'web/node_modules/react/index.js',
    'dist/app.js',
    'src/__snapshots__/view.test.tsx.snap',
    'Vendor/x.go',
    'static/app.min.js',
    'static/app.min.css',
    'static/app.js.map',
    'api/v1/service.pb.go',
    'internal/queries/models_generated.go',
    'internal/mock.gen.go',
    'src/api.generated.ts',
    'src/view.snap',
  ])('folds %s away', (path) => {
    expect(isGenerated(path)).toBe(true)
  })

  it.each([
    'main.go',
    'src/app.ts',
    'docs/go.sum.md',
    'vendor',
    'src/vendored/x.go',
    'src/distance.ts',
    'src/lib/dist.ts',
    'package.json',
    'lock.yaml',
    'src/app.js',
  ])('shows %s', (path) => {
    expect(isGenerated(path)).toBe(false)
  })
})

describe('startsCollapsed', () => {
  it('folds generated and large files away', () => {
    expect(startsCollapsed({ path: 'go.sum', additions: 1, deletions: 0 })).toBe(true)
    expect(startsCollapsed({ path: 'src/app.ts', additions: 300, deletions: 101 })).toBe(true)
    expect(startsCollapsed({ path: 'src/app.ts', additions: 300, deletions: 100 })).toBe(false)
    expect(startsCollapsed({ path: 'src/app.ts', additions: 0, deletions: 0 })).toBe(false)
  })
})
