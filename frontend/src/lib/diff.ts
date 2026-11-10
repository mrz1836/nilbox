// A pull request file's patch, as GitHub sends it, read into the rows the diff view shows,
// and the rules for which of those rows take a review comment. The backend reads patches
// with the same rules (internal/review/hunks.go) and refuses any comment they don't allow
// before it reaches GitHub, so the two must agree line for line: the tests of each read
// the same sample patches and expect the same lines.

// DiffSide is which version of a file a comment is on: LEFT is the base version, where
// removed lines are, and RIGHT the head version, where added and unchanged lines are.
export type DiffSide = 'LEFT' | 'RIGHT'

// DiffRowKind is what a row of a diff is: a hunk's header, a line both versions share
// (context), an added or a removed line, or a note such as "\ No newline at end of file".
export type DiffRowKind = 'hunk' | 'context' | 'add' | 'del' | 'note'

// DiffRow is one row of a diff. text is the line without its leading marker, or the whole
// header line for a hunk's header. oldLine and newLine are the line's numbers in the base
// and head versions, null on a side the row is not on. hunk is the index of the hunk the
// row belongs to, its header included.
export interface DiffRow {
  kind: DiffRowKind
  text: string
  oldLine: number | null
  newLine: number | null
  hunk: number
}

// patchMaxLine is the largest line number or count a hunk header may name, as in the
// backend; a header naming a larger one is malformed.
const patchMaxLine = 1_000_000_000

// headerPattern matches a hunk header, "@@ -a,b +c,d @@" with optional section text after
// it, and captures its four numbers. A count left out (",b" or ",d") means 1.
const headerPattern = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/

// suggestionFence matches a line that opens a suggestion block: after any spaces or tabs,
// a fence of three or more backticks and then, after any spaces or tabs, "suggestion".
const suggestionFence = /^[ \t]*`{3,}[ \t]*suggestion/

// collapseAtLines is how many changed lines a file may have and still start expanded.
const collapseAtLines = 400

// generatedNames are the basenames, in lower case, of lock files and other files a tool
// writes, which a reviewer rarely reads.
const generatedNames = new Set([
  'go.sum',
  'go.work.sum',
  'package-lock.json',
  'npm-shrinkwrap.json',
  'pnpm-lock.yaml',
  'yarn.lock',
  'bun.lockb',
  'bun.lock',
  'cargo.lock',
  'gemfile.lock',
  'composer.lock',
  'poetry.lock',
  'pipfile.lock',
  'uv.lock',
  'flake.lock',
  'mix.lock',
  'podfile.lock',
  'packages.lock.json',
])

// generatedDirs are the folder names, in lower case, that hold vendored, installed,
// built, or snapshot files.
const generatedDirs = new Set(['vendor', 'node_modules', 'dist', '__snapshots__'])

// generatedSuffixes are the endings, in lower case, of minified, mapped, generated, and
// snapshot files.
const generatedSuffixes = [
  '.min.js',
  '.min.css',
  '.map',
  '.pb.go',
  '_generated.go',
  '.gen.go',
  '.generated.ts',
  '.snap',
]

// HunkRange is one side of an open hunk: the first line its header names, how many lines
// the header says it covers, and how many of them its rows have numbered so far.
interface HunkRange {
  start: number
  count: number
  shown: number
}

// OpenHunk is the hunk whose rows are being read: its base and head sides.
interface OpenHunk {
  before: HunkRange
  after: HunkRange
}

// parseNumber reads a header number, or returns null for one above patchMaxLine or none
// at all.
function parseNumber(digits: string | undefined): number | null {
  const n = Number(digits)

  return n <= patchMaxLine ? n : null
}

// parseRange reads one side of a hunk header from its start and count digits, count being
// undefined when the header leaves it out. A side with lines must start at line 1 or later;
// an empty one names the line it follows, which can be 0.
function parseRange(start: string | undefined, count: string | undefined): HunkRange | null {
  const first = parseNumber(start)
  const lines = count === undefined ? 1 : parseNumber(count)

  if (first === null || lines === null || (lines > 0 && first === 0)) {
    return null
  }

  return { start: first, count: lines, shown: 0 }
}

// parseHeader reads a hunk header line into an open hunk, or returns null for a line
// that is not a well-formed header.
function parseHeader(line: string): OpenHunk | null {
  const m = headerPattern.exec(line)
  if (m === null) {
    return null
  }

  const before = parseRange(m[1], m[2])
  const after = parseRange(m[3], m[4])

  return before === null || after === null ? null : { before, after }
}

// rowKind is the kind of row a line after a hunk header is, by its leading marker, or
// null for a line that is no row, an empty one included.
function rowKind(line: string): Exclude<DiffRowKind, 'hunk'> | null {
  switch (line.charAt(0)) {
    case ' ':
      return 'context'
    case '-':
      return 'del'
    case '+':
      return 'add'
    case '\\':
      return 'note'
    default:
      return null
  }
}

// room reports whether a side's header leaves room for another row on it.
function room(range: HunkRange): boolean {
  return range.shown < range.count
}

// nextLine gives the next line number on a side and counts it as shown.
function nextLine(range: HunkRange): number {
  const line = range.start + range.shown
  range.shown += 1

  return line
}

// takeRow reads one line of an open hunk into a row, numbering it on each side it is on,
// or returns null when the line ends the hunk: a line that is no row, or a row on a side
// whose header has no room left.
function takeRow(open: OpenHunk, line: string, hunk: number): DiffRow | null {
  const kind = rowKind(line)
  if (kind === null) {
    return null
  }

  const before = kind === 'context' || kind === 'del'
  const after = kind === 'context' || kind === 'add'

  if ((before && !room(open.before)) || (after && !room(open.after))) {
    return null
  }

  return {
    kind,
    text: line.slice(1),
    oldLine: before ? nextLine(open.before) : null,
    newLine: after ? nextLine(open.after) : null,
    hunk,
  }
}

// parsePatch reads a patch into rows. It never fails: text before the first hunk header
// is ignored, a malformed header starts no hunk (the lines up to the next good header are
// skipped), and a hunk ends early at a line that is not a row or at a row its header has
// no room left for, skipping the lines after it up to the next header. A trailing "\r" on
// any line is dropped. Hunks are numbered from 0 in the order of their good headers.
export function parsePatch(patch: string): DiffRow[] {
  const rows: DiffRow[] = []
  let hunk = -1
  let open: OpenHunk | null = null

  for (const raw of patch.split('\n')) {
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw

    if (line.startsWith('@@')) {
      open = parseHeader(line)

      if (open !== null) {
        hunk += 1
        rows.push({ kind: 'hunk', text: line, oldLine: null, newLine: null, hunk })
      }

      continue
    }

    if (open !== null) {
      const row = takeRow(open, line, hunk)

      if (row === null) {
        open = null
      } else {
        rows.push(row)
      }
    }
  }

  return rows
}

// rowSide is the side a comment on the row goes on: RIGHT for an added or a context row,
// LEFT for a removed row, and null for a header or a note, which take no comment. A
// context row can also take a comment on the base side, by its oldLine; canComment
// allows both.
export function rowSide(row: DiffRow): DiffSide | null {
  if (row.kind === 'add' || row.kind === 'context') {
    return 'RIGHT'
  }

  return row.kind === 'del' ? 'LEFT' : null
}

// rowLine is the row's line number on its rowSide, or null for a row with no side.
export function rowLine(row: DiffRow): number | null {
  const side = rowSide(row)
  if (side === null) {
    return null
  }

  return side === 'RIGHT' ? row.newLine : row.oldLine
}

// hunkOf is the index of the first hunk in which line takes a comment on side, or null
// when none does: the line must be a context row's number on either side, an added row's
// on the head side, or a removed row's on the base side. Unlike rowSide, which puts a
// context row on the head side, it finds a context row by its number on either side.
function hunkOf(rows: readonly DiffRow[], side: DiffSide, line: number): number | null {
  const row = rows.find((r) => (side === 'RIGHT' ? r.newLine : r.oldLine) === line)

  return row === undefined ? null : row.hunk
}

// canComment reports whether line takes a comment on side.
export function canComment(rows: readonly DiffRow[], side: DiffSide, line: number): boolean {
  return hunkOf(rows, side, line) !== null
}

// canRange reports whether the lines start to end take one comment on side: start must
// come before end, and both must take a comment in the same hunk. A range of one line is
// a one-line comment, which canComment decides.
export function canRange(rows: readonly DiffRow[], side: DiffSide, start: number, end: number): boolean {
  if (start === end) {
    return canComment(rows, side, end)
  }

  const first = hunkOf(rows, side, start)

  return start < end && first !== null && first === hunkOf(rows, side, end)
}

// canSuggest reports whether a suggested change can go on side: only on the head version,
// whose lines a suggestion replaces.
export function canSuggest(side: DiffSide): boolean {
  return side === 'RIGHT'
}

// suggestionBlock is a suggestion block that replaces the commented lines with lines.
export function suggestionBlock(lines: readonly string[]): string {
  return '```suggestion\n' + lines.join('\n') + '\n```'
}

// isSuggestion reports whether a comment's body suggests a change: whether one of its
// lines opens a suggestion block, ignoring any spaces or tabs before the fence.
export function isSuggestion(body: string): boolean {
  return body.split('\n').some((line) => suggestionFence.test(line))
}

// isGenerated reports whether path is a lock file, a generated or minified file, or a
// file under a vendored, installed, built, or snapshot folder, which a reviewer rarely
// reads. It matches without regard to case.
export function isGenerated(path: string): boolean {
  const lower = path.toLowerCase()
  const slash = lower.lastIndexOf('/')
  const name = lower.slice(slash + 1)

  return (
    generatedNames.has(name) ||
    generatedSuffixes.some((suffix) => name.endsWith(suffix)) ||
    lower
      .slice(0, slash + 1)
      .split('/')
      .some((dir) => generatedDirs.has(dir))
  )
}

// startsCollapsed reports whether a file's diff starts folded away: a generated file, or
// one with more than collapseAtLines changed lines.
export function startsCollapsed(file: { path: string; additions: number; deletions: number }): boolean {
  return isGenerated(file.path) || file.additions + file.deletions > collapseAtLines
}
