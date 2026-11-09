import { createElement, Fragment, type ReactElement, type ReactNode } from 'react'

// A renderer for the GitHub-flavoured markdown that pull request descriptions are written
// in. Anyone can write one, an outside contributor or a bot, so it trusts none of it:
//
//   - it builds React elements only, never markup;
//   - no attribute from the text reaches the page but a link's checked URL;
//   - it never makes an <img>, or any other element that loads something: an image
//     becomes a link to it, so nothing remote loads until the user opens it;
//   - a link opens outside the app, through the caller, and only an http, https, or
//     mailto URL is a link at all; any other is shown as text;
//   - HTML in the text keeps the meaning of a small set of tags (paragraphs, lists,
//     tables, emphasis, code, details), drops the rest along with their attributes, and
//     drops what a script, style, or frame holds entirely.
//
// The text may be long and malformed. Every scan is linear or bounded, nesting is
// capped, and a construct that does not close renders as text.

export interface MarkdownOptions {
  // openLink opens a link's URL outside the app (the caller passes platform.openExternal).
  openLink: (url: string) => void
}

// MAX_LENGTH is the most text rendered; past it the text is cut and a note says so.
export const MAX_LENGTH = 200_000

// MAX_DEPTH bounds how deep blocks nest (quotes, lists, details), MAX_FRAMES how many
// emphasis markers stay open at once, and MAX_TAGS how deep HTML tags nest. Past each the
// rest renders flat, so no text nests deep enough to exhaust the stack.
const MAX_DEPTH = 12
const MAX_FRAMES = 64
const MAX_TAGS = 32

// MAX_TARGET bounds how far a link's target is looked for, and MAX_LABEL a reference
// label's length, so text full of brackets still renders in linear time.
const MAX_TARGET = 2048
const MAX_LABEL = 999

// The classes each element takes. Sizes are in rem, so they follow the text size setting,
// and colors come from the theme tokens, so they read in both themes.
const LINK = 'cursor-pointer text-left text-accent underline-offset-2 hover:underline'
const INLINE_CODE = 'rounded bg-raised px-1 py-0.5 font-mono text-[0.75rem]'
const CODE_BLOCK =
  'overflow-x-auto rounded-md border border-border bg-raised px-3 py-2 font-mono text-[0.75rem] leading-normal whitespace-pre text-text'
const QUOTE = 'space-y-2 border-l-2 border-muted/50 pl-3 text-muted'
const RULE = 'border-border'
const BULLETS = 'list-disc space-y-0.5 pl-5'
const NUMBERS = 'list-decimal space-y-0.5 pl-5'
const ITEM = 'space-y-1'
const TASK_ITEM = '-ml-5 flex list-none items-start gap-1.5'
const TABLE = 'border-collapse text-[0.75rem]'
const CELL = 'border border-border px-2 py-1 align-top'
const HEAD_CELL = `${CELL} bg-raised font-semibold`
const DETAILS = 'rounded-md border border-border px-3 py-2'
const SUMMARY = 'cursor-pointer font-medium'
const KBD = 'rounded border border-border bg-raised px-1 font-mono text-[0.75rem]'
const MARK = 'rounded-sm bg-warning/25 text-inherit'
const STRONG = 'font-semibold'

type Level = 1 | 2 | 3 | 4 | 5 | 6

// HEADINGS are kept small: the text sits inside a sheet, under the sheet's own title.
const HEADINGS: Record<Level, { tag: string; className: string }> = {
  1: { tag: 'h1', className: 'border-b border-border pb-1 text-[0.9375rem] leading-snug font-semibold' },
  2: { tag: 'h2', className: 'border-b border-border pb-1 text-[0.875rem] leading-snug font-semibold' },
  3: { tag: 'h3', className: 'text-[0.8125rem] leading-snug font-semibold' },
  4: { tag: 'h4', className: 'text-[0.8125rem] leading-snug font-semibold' },
  5: { tag: 'h5', className: 'text-[0.75rem] leading-snug font-semibold' },
  6: { tag: 'h6', className: 'text-[0.75rem] leading-snug font-semibold text-muted' },
}

type Align = 'left' | 'center' | 'right' | 'none'

const ALIGN: Record<Align, string> = {
  left: 'text-left',
  center: 'text-center',
  right: 'text-right',
  none: 'text-left',
}

// renderMarkdown renders text as React elements. It never throws, and it never makes an
// element that loads anything.
export function renderMarkdown(text: string, options: MarkdownOptions): ReactNode {
  const cut = text.length > MAX_LENGTH
  const source = (cut ? text.slice(0, MAX_LENGTH) : text).replace(/\r\n?/g, '\n').replaceAll('\u0000', '\ufffd')
  const refs = new Map<string, string>()
  const blocks = parseBlocks(source.split('\n').map(expandTabs), 0, refs)
  const nodes: ReactNode[] = renderBlocks(blocks, { openLink: options.openLink, inLink: false, flow: false, refs })

  if (cut) {
    nodes.push(
      <p key="cut" className="text-muted italic">
        … The rest is too long to show here.
      </p>,
    )
  }

  return nodes
}

// Context is what rendering inline text needs: how to open a link, whether the text is
// already inside one (a link holds no other link), and the reference definitions.
interface Context {
  openLink: (url: string) => void
  inLink: boolean
  // flow is whether the text sits where blocks may go (a table cell), rather than where
  // only inline content may (a paragraph, a heading, a link).
  flow: boolean
  refs: ReadonlyMap<string, string>
}

// ---------------------------------------------------------------------------------------
// Blocks
// ---------------------------------------------------------------------------------------

interface Cell {
  text: string
  align: Align
}

interface ListItem {
  // task is whether a task-list item is ticked, or null for a plain item.
  task: boolean | null
  blocks: Block[]
}

type Block =
  | { kind: 'heading'; level: Level; text: string }
  | { kind: 'paragraph'; text: string }
  | { kind: 'code'; text: string }
  | { kind: 'quote'; blocks: Block[] }
  | { kind: 'list'; ordered: boolean; start: number; items: ListItem[] }
  | { kind: 'rule' }
  | { kind: 'table'; head: Cell[]; rows: Cell[][] }
  | { kind: 'details'; open: boolean; summary: string; blocks: Block[] }
  | { kind: 'html'; text: string }

// Scan is one run of the block parser over a container's lines (the document, a quote, a
// list item, a details body). It may rewrite a line it has reached, to leave text that
// follows a closing tag on that line for the next block.
interface Scan {
  lines: string[]
  depth: number
  refs: Map<string, string>
  blocks: Block[]
}

// parseBlocks splits lines into blocks. Past MAX_DEPTH it stops looking for structure and
// keeps the lines as one paragraph.
function parseBlocks(lines: string[], depth: number, refs: Map<string, string>): Block[] {
  if (depth > MAX_DEPTH) {
    const text = lines.join('\n').trim()

    return text === '' ? [] : [{ kind: 'paragraph', text }]
  }

  const s: Scan = { lines, depth, refs, blocks: [] }
  let i = 0

  while (i < lines.length) {
    if (isBlank(lineAt(lines, i))) {
      i += 1
      continue
    }

    i =
      indentedCode(s, i) ??
      fenced(s, i) ??
      atxHeading(s, i) ??
      rule(s, i) ??
      quote(s, i) ??
      details(s, i) ??
      htmlBlock(s, i) ??
      list(s, i) ??
      table(s, i) ??
      paragraph(s, i)
  }

  return s.blocks
}

const FENCE = /^( {0,3})(`{3,}|~{3,})(.*)$/
const HEADING = /^ {0,3}(#{1,6})(?: +(.*))?$/
const RULE_LINE = /^ {0,3}(?:(?:\* *){3,}|(?:- *){3,}|(?:_ *){3,})$/
const QUOTE_MARK = /^ {0,3}> ?/
const SETEXT = /^ {0,3}(=+|-+) *$/
const MARKER = /^ {0,3}([-+*]|\d{1,9}[.)])( *)/
const TASK_MARK = /^\[([ xX])\](?: +|$)/
const DELIMITER_CELL = /^:?-+:?$/
const DEFINITION = /^ {0,3}\[((?:[^\\[\]]|\\.){1,999})\]: *(<[^<>]*>|\S+)(?: +(?:"[^"]*"|'[^']*'|\([^)]*\)))? *$/
const DETAILS_OPEN = /^ {0,3}<details(?:\s[^<>]*)?>/i
const DETAILS_TAG = /<details(?:\s[^<>]*)?>|<\/details\s*>/gi
const SUMMARY_OPEN = /^\s*<summary(?:\s[^<>]*)?>/i
const SUMMARY_CLOSE = /<\/summary\s*>/i
const RAW_OPEN = /^ {0,3}<(?:script|pre|style|textarea)(?:[\s>]|$)/i
const RAW_CLOSE = /<\/(?:script|pre|style|textarea)\s*>/i
const COMMENT_OPEN = /^ {0,3}<!--/
const COMMENT_CLOSE = /-->/
const BLOCK_TAG = /^ {0,3}<\/?([a-zA-Z][a-zA-Z0-9]*)(?:[\s/>]|$)/

// BLOCK_TAGS are the tags that start an HTML block wherever a line starts with one, even
// inside a paragraph, as CommonMark has it.
const BLOCK_TAGS = new Set(
  (
    'address article aside base basefont blockquote body caption center col colgroup dd details dialog dir div ' +
    'dl dt fieldset figcaption figure footer form frame frameset h1 h2 h3 h4 h5 h6 head header hr html iframe ' +
    'legend li link main menu menuitem nav noframes ol optgroup option p param search section summary table ' +
    'tbody td tfoot th thead title tr track ul'
  ).split(' '),
)

// indentedCode reads a code block indented four spaces.
function indentedCode(s: Scan, i: number): number | null {
  if (indentOf(lineAt(s.lines, i)) < 4) {
    return null
  }

  const body: string[] = []
  let j = i

  for (; j < s.lines.length; j += 1) {
    const line = lineAt(s.lines, j)

    if (!isBlank(line) && indentOf(line) < 4) {
      break
    }

    body.push(line.slice(4))
  }

  while (isBlank(last(body))) {
    body.pop()
  }

  s.blocks.push({ kind: 'code', text: body.join('\n') })

  return j
}

// fenced reads a code block between ``` or ~~~ fences. One left open runs to the end.
function fenced(s: Scan, i: number): number | null {
  const open = FENCE.exec(lineAt(s.lines, i))

  if (open === null) {
    return null
  }

  const indent = group(open, 1).length
  const fence = group(open, 2)

  if (fence.startsWith('`') && group(open, 3).includes('`')) {
    return null
  }

  const body: string[] = []
  let j = i + 1

  for (; j < s.lines.length; j += 1) {
    const line = lineAt(s.lines, j)
    const close = FENCE.exec(line)

    if (close !== null && group(close, 2).startsWith(fence) && group(close, 3).trim() === '') {
      break
    }

    body.push(line.slice(Math.min(indent, indentOf(line))))
  }

  s.blocks.push({ kind: 'code', text: body.join('\n') })

  return Math.min(j + 1, s.lines.length)
}

// atxHeading reads a heading written with #s, dropping any closing #s.
function atxHeading(s: Scan, i: number): number | null {
  const heading = HEADING.exec(lineAt(s.lines, i))

  if (heading === null) {
    return null
  }

  let text = group(heading, 2).trim()
  let end = text.length

  while (end > 0 && text.charAt(end - 1) === '#') {
    end -= 1
  }

  if (end === 0) {
    text = ''
  } else if (text.charAt(end - 1) === ' ') {
    text = text.slice(0, end).trim()
  }

  s.blocks.push({ kind: 'heading', level: group(heading, 1).length as Level, text })

  return i + 1
}

// rule reads a horizontal rule.
function rule(s: Scan, i: number): number | null {
  if (!RULE_LINE.test(lineAt(s.lines, i))) {
    return null
  }

  s.blocks.push({ kind: 'rule' })

  return i + 1
}

// quote reads a blockquote: its > lines, and the lines that lazily continue its last
// paragraph. What it holds is parsed as blocks of its own.
function quote(s: Scan, i: number): number | null {
  if (!QUOTE_MARK.test(lineAt(s.lines, i))) {
    return null
  }

  const inner: string[] = []
  let j = i

  for (; j < s.lines.length; j += 1) {
    const line = lineAt(s.lines, j)
    const mark = QUOTE_MARK.exec(line)

    if (mark !== null) {
      inner.push(line.slice(mark[0].length))
    } else if (!isBlank(line) && !isBlank(last(inner)) && !startsBlock(line)) {
      inner.push(line)
    } else {
      break
    }
  }

  s.blocks.push({ kind: 'quote', blocks: parseBlocks(inner, s.depth + 1, s.refs) })

  return j
}

// details reads a <details> element: its summary, and what it holds parsed as blocks, up
// to the </details> that closes it, counting any nested inside. One left open runs to
// the end. Text after the closing tag, on its line, is left for the next block.
function details(s: Scan, i: number): number | null {
  const opener = DETAILS_OPEN.exec(lineAt(s.lines, i))

  if (opener === null) {
    return null
  }

  const from = opener[0].length
  const close = closeOfDetails(s.lines, i, from)
  const end = close === null ? s.lines.length - 1 : close.line
  const body = s.lines.slice(i, end + 1)
  let next = end + 1

  if (close !== null) {
    const line = lineAt(s.lines, end)
    const rest = line.slice(close.end)

    body[body.length - 1] = line.slice(0, close.start)

    if (!isBlank(rest)) {
      s.lines[end] = rest
      next = end
    }
  }

  body[0] = lineAt(body, 0).slice(from)

  const { summary, content } = splitSummary(body.join('\n'))

  s.blocks.push({
    kind: 'details',
    open: attributes(opener[0]).has('open'),
    summary,
    blocks: parseBlocks(content.split('\n'), s.depth + 1, s.refs),
  })

  return next
}

// closeOfDetails finds the </details> that closes the <details> opened on line i, whose
// opening tag ends at column from: its line, and where the closing tag starts and ends.
function closeOfDetails(
  lines: readonly string[],
  i: number,
  from: number,
): { line: number; start: number; end: number } | null {
  let nested = 0

  for (let j = i; j < lines.length; j += 1) {
    const line = lineAt(lines, j)

    DETAILS_TAG.lastIndex = j === i ? from : 0

    for (let tag = DETAILS_TAG.exec(line); tag !== null; tag = DETAILS_TAG.exec(line)) {
      if (tag[0].charAt(1) !== '/') {
        nested += 1
      } else if (nested > 0) {
        nested -= 1
      } else {
        return { line: j, start: tag.index, end: tag.index + tag[0].length }
      }
    }
  }

  return null
}

// splitSummary splits what a <details> holds into its <summary>, on one line, and the
// rest. Without a summary it reads "Details", as the browser shows.
function splitSummary(content: string): { summary: string; content: string } {
  const open = SUMMARY_OPEN.exec(content)

  if (open === null) {
    return { summary: 'Details', content }
  }

  const rest = content.slice(open[0].length)
  const close = SUMMARY_CLOSE.exec(rest)

  if (close === null) {
    return { summary: oneLine(rest), content: '' }
  }

  return { summary: oneLine(rest.slice(0, close.index)), content: rest.slice(close.index + close[0].length) }
}

// htmlKind says whether a line starts an HTML block, and which kind: raw (a script, pre,
// style, or textarea) and comment blocks run to their closing tag, block and tag blocks
// to a blank line. A tag block is a lone tag on its line, which can't interrupt a
// paragraph.
function htmlKind(line: string): 'raw' | 'comment' | 'block' | 'tag' | null {
  if (RAW_OPEN.test(line)) {
    return 'raw'
  }

  if (COMMENT_OPEN.test(line)) {
    return 'comment'
  }

  const tag = BLOCK_TAG.exec(line)

  if (tag !== null && BLOCK_TAGS.has(group(tag, 1).toLowerCase())) {
    return 'block'
  }

  const text = line.trim()
  const lone = readTag(text, 0)

  return lone !== null && text.slice(lone.end).trim() === '' ? 'tag' : null
}

// htmlBlock reads a block of HTML.
function htmlBlock(s: Scan, i: number): number | null {
  const kind = htmlKind(lineAt(s.lines, i))

  if (kind === null) {
    return null
  }

  let j = i

  if (kind === 'raw' || kind === 'comment') {
    const end = kind === 'raw' ? RAW_CLOSE : COMMENT_CLOSE

    while (j < s.lines.length && !end.test(lineAt(s.lines, j))) {
      j += 1
    }

    j = Math.min(j + 1, s.lines.length)
  } else {
    while (j < s.lines.length && !isBlank(lineAt(s.lines, j))) {
      j += 1
    }
  }

  s.blocks.push({ kind: 'html', text: s.lines.slice(i, j).join('\n') })

  return j
}

interface Marker {
  // type is the bullet, or an ordered list's delimiter; a change of type starts a new list.
  type: string
  ordered: boolean
  start: number
  // offset is the column the item's content starts at, which its later lines indent to.
  offset: number
  rest: string
}

// listMarker reads a list item's marker at the start of a line.
function listMarker(line: string): Marker | null {
  const marker = MARKER.exec(line)

  if (marker === null) {
    return null
  }

  const after = marker[0].length
  const spaces = group(marker, 2).length

  if (spaces === 0 && after < line.length) {
    return null
  }

  const mark = group(marker, 1)
  const ordered = mark.length > 1
  // An item that is empty on its first line, or whose text is indented as code, starts
  // its content one space past the marker.
  const offset = after === line.length || spaces > 4 ? after - spaces + 1 : after

  return {
    type: mark.charAt(mark.length - 1),
    ordered,
    start: ordered ? parseInt(mark, 10) : 1,
    offset,
    rest: line.slice(offset),
  }
}

// list reads a list: items with the same marker type, each holding the lines indented to
// its content, and the lines that lazily continue its last paragraph.
function list(s: Scan, i: number): number | null {
  const first = listMarker(lineAt(s.lines, i))

  if (first === null) {
    return null
  }

  const items: ListItem[] = []
  let j = i

  while (j < s.lines.length) {
    const line = lineAt(s.lines, j)
    const marker = listMarker(line)

    if (marker === null || marker.type !== first.type || RULE_LINE.test(line)) {
      break
    }

    const body = [marker.rest]

    for (j += 1; j < s.lines.length; j += 1) {
      const next = lineAt(s.lines, j)

      if (isBlank(next)) {
        body.push('')
      } else if (indentOf(next) >= marker.offset) {
        body.push(next.slice(marker.offset))
      } else if (!isBlank(last(body)) && !startsBlock(next) && listMarker(next) === null) {
        body.push(next.trim())
      } else {
        break
      }
    }

    const task = TASK_MARK.exec(lineAt(body, 0))

    if (task !== null) {
      body[0] = lineAt(body, 0).slice(task[0].length)
    }

    items.push({
      task: task === null ? null : group(task, 1) !== ' ',
      blocks: parseBlocks(body, s.depth + 1, s.refs),
    })
  }

  s.blocks.push({ kind: 'list', ordered: first.ordered, start: first.start, items })

  return j
}

// tableHead reads a table's header row and the delimiter row under it, which must have
// as many cells, or returns null when line i starts no table.
function tableHead(lines: readonly string[], i: number): { head: Cell[]; align: Align[] } | null {
  const line = lineAt(lines, i)
  const delimiter = lineAt(lines, i + 1)

  if (!line.includes('|') || !delimiter.includes('|')) {
    return null
  }

  const align: Align[] = []

  for (const cell of splitRow(delimiter)) {
    if (!DELIMITER_CELL.test(cell)) {
      return null
    }

    const left = cell.startsWith(':')
    const right = cell.endsWith(':')

    align.push(left && right ? 'center' : right ? 'right' : left ? 'left' : 'none')
  }

  const head = splitRow(line)

  return head.length === align.length ? { head: cells(head, align), align } : null
}

// table reads a table: its head, then rows up to a blank line or another block.
function table(s: Scan, i: number): number | null {
  const start = tableHead(s.lines, i)

  if (start === null) {
    return null
  }

  const rows: Cell[][] = []
  let j = i + 2

  for (; j < s.lines.length; j += 1) {
    const line = lineAt(s.lines, j)

    if (isBlank(line) || startsBlock(line)) {
      break
    }

    rows.push(cells(splitRow(line), start.align))
  }

  s.blocks.push({ kind: 'table', head: start.head, rows })

  return j
}

// splitRow splits a table row at its unescaped pipes.
function splitRow(line: string): string[] {
  let row = line.trim()

  if (row.startsWith('|')) {
    row = row.slice(1)
  }

  if (row.endsWith('|') && !row.endsWith('\\|')) {
    row = row.slice(0, -1)
  }

  const out: string[] = []
  let cell = ''

  for (let k = 0; k < row.length; k += 1) {
    const c = row.charAt(k)

    if (c === '\\' && row.charAt(k + 1) === '|') {
      cell += '|'
      k += 1
    } else if (c === '|') {
      out.push(cell.trim())
      cell = ''
    } else {
      cell += c
    }
  }

  out.push(cell.trim())

  return out
}

// cells fits a row to the table's columns: a short row is padded, a long one cut.
function cells(row: readonly string[], align: readonly Align[]): Cell[] {
  return align.map((a, n) => ({ text: row[n] ?? '', align: a }))
}

// paragraph reads link reference definitions, or else a paragraph: lines up to a blank
// line or a block that can interrupt one. A paragraph over a === or --- line is a heading.
function paragraph(s: Scan, i: number): number {
  let j = i

  for (; j < s.lines.length; j += 1) {
    const definition = DEFINITION.exec(lineAt(s.lines, j))

    if (definition === null) {
      break
    }

    const label = normalizeLabel(group(definition, 1))
    const target = group(definition, 2)

    if (!s.refs.has(label)) {
      s.refs.set(label, unescapeText(decodeEntities(target.startsWith('<') ? target.slice(1, -1) : target)))
    }
  }

  if (j > i) {
    return j
  }

  const text = [lineAt(s.lines, i).trim()]

  for (j = i + 1; j < s.lines.length; j += 1) {
    const line = lineAt(s.lines, j)

    if (isBlank(line)) {
      break
    }

    const underline = SETEXT.exec(line)

    if (underline !== null) {
      s.blocks.push({ kind: 'heading', level: group(underline, 1).startsWith('=') ? 1 : 2, text: text.join('\n') })

      return j + 1
    }

    if (startsBlock(line) || tableHead(s.lines, j) !== null) {
      break
    }

    text.push(line.trim())
  }

  s.blocks.push({ kind: 'paragraph', text: text.join('\n') })

  return j
}

// startsBlock reports whether a line starts a block that interrupts a paragraph: a fence,
// heading, quote, rule, HTML block other than a lone tag, or a list item that isn't empty
// (an ordered one only from 1).
function startsBlock(line: string): boolean {
  if (indentOf(line) > 3) {
    return false
  }

  const html = htmlKind(line)
  const marker = listMarker(line)

  return (
    FENCE.test(line) ||
    HEADING.test(line) ||
    QUOTE_MARK.test(line) ||
    RULE_LINE.test(line) ||
    (html !== null && html !== 'tag') ||
    (marker !== null && !isBlank(marker.rest) && (!marker.ordered || marker.start === 1))
  )
}

// ---------------------------------------------------------------------------------------
// Inline text
// ---------------------------------------------------------------------------------------

const ASCII_PUNCTUATION = /^[!-/:-@[-`{-~]$/
const ESCAPED = /\\([!-/:-@[-`{-~])/g
const WHITESPACE = /\s/
const PUNCTUATION = /[\p{P}\p{S}]/u
const AUTOLINK = /<([a-zA-Z][a-zA-Z0-9+.-]{1,31}:[^\s<>]*)>/y
const EMAIL = /<([a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)*)>/y
const BARE_URL = /https?:\/\/[a-zA-Z0-9][^\s<]*/iy
const URL_TRAILER = `?!.,:;*_~'"`

type Delim = '*' | '_' | '~'

// Frame is a run of emphasis markers still open, and what has come after it.
interface Frame {
  delim: Delim
  count: number
  items: Item[]
}

// InlineParser renders a paragraph's text (or a heading's, a cell's, a summary's) in one
// pass. Emphasis follows CommonMark's delimiter rules: each closing run of * _ or ~~
// matches the nearest open run of its kind, and any open runs between them become text.
class InlineParser {
  private readonly s: string
  private readonly ctx: Context
  private readonly brackets: Map<number, number>
  // root holds the top-level items. Its delim is never matched: a closing run looks for
  // its opener only while one of its kind is open, and any such opener sits above root.
  private readonly root: Frame = { delim: '*', count: 0, items: [] }
  private readonly frames: Frame[] = [this.root]
  private top: Frame = this.root
  private readonly open: Record<Delim, number> = { '*': 0, _: 0, '~': 0 }
  private buf = ''
  // unmatchedTicks holds backtick run lengths with no closing run left in the text, and
  // commentsClose is false once a <!-- had no -->, so neither is looked for again.
  private readonly unmatchedTicks = new Set<number>()
  private commentsClose = true

  constructor(s: string, ctx: Context) {
    this.s = s
    this.ctx = ctx
    this.brackets = matchBrackets(s)
  }

  parse(): ReactNode[] {
    let i = 0

    while (i < this.s.length) {
      const c = this.s.charAt(i)
      const next = this.step(c, i)

      if (next < 0) {
        this.buf += c
        i += 1
      } else {
        i = next
      }
    }

    this.flush()

    while (this.frames.length > 1) {
      this.flattenTop()
    }

    return buildHtml(this.root.items, this.ctx, this.ctx.flow)
  }

  // step reads the construct at i, which starts with c, and returns where text resumes
  // after it, or -1 when c is plain text there.
  private step(c: string, i: number): number {
    switch (c) {
      case '\\':
        return this.escape(i)
      case '`':
        return this.code(i)
      case '*':
      case '_':
      case '~':
        return this.delimiter(i, c)
      case '\n':
        this.push(<br />)

        return i + 1
      case '!':
        return this.image(i)
      case '[':
        return this.link(i)
      case '<':
        return this.angle(i)
      case 'h':
      case 'H':
        return this.bareUrl(i)
      default:
        return -1
    }
  }

  private flush(): void {
    if (this.buf !== '') {
      this.top.items.push(decodeEntities(this.buf))
      this.buf = ''
    }
  }

  private push(item: Item): void {
    this.flush()
    this.top.items.push(item)
  }

  // escape reads a backslash: before punctuation it makes the punctuation plain text,
  // and before a line end it is a line break.
  private escape(i: number): number {
    const next = this.s.charAt(i + 1)

    if (next === '\n') {
      this.push(<br />)

      return i + 2
    }

    if (!ASCII_PUNCTUATION.test(next)) {
      return -1
    }

    this.push(next)

    return i + 2
  }

  // code reads a code span, closed by the next run of as many backticks.
  private code(i: number): number {
    const n = runLength(this.s, i, '`')
    const close = this.unmatchedTicks.has(n) ? -1 : this.closingTicks(i + n, n)

    if (close < 0) {
      this.buf += '`'.repeat(n)

      return i + n
    }

    const raw = this.s.slice(i + n, close).replaceAll('\n', ' ')
    const text = raw.startsWith(' ') && raw.endsWith(' ') && raw.trim() !== '' ? raw.slice(1, -1) : raw

    this.push(<code className={INLINE_CODE}>{text}</code>)

    return close + n
  }

  private closingTicks(from: number, n: number): number {
    for (let j = from; ;) {
      const k = this.s.indexOf('`', j)

      if (k < 0) {
        this.unmatchedTicks.add(n)

        return -1
      }

      const m = runLength(this.s, k, '`')

      if (m === n) {
        return k
      }

      j = k + m
    }
  }

  // delimiter reads a run of * _ or ~ (only ~~ strikes through): it closes what it can,
  // and opens with what is left when it can open.
  private delimiter(i: number, c: Delim): number {
    const n = runLength(this.s, i, c)

    if (c === '~' && n !== 2) {
      this.buf += c.repeat(n)

      return i + n
    }

    const before = this.s.charAt(i - 1)
    const after = this.s.charAt(i + n)
    const left = !isSpace(after) && (!isPunct(after) || isSpace(before) || isPunct(before))
    const right = !isSpace(before) && (!isPunct(before) || isSpace(after) || isPunct(after))
    // An underscore inside a word (snake_case) neither opens nor closes.
    const canOpen = left && (c !== '_' || !right || isPunct(before))
    const canClose = right && (c !== '_' || !left || isPunct(after))

    this.flush()

    const rest = canClose ? this.close(c, n) : n

    if (rest > 0) {
      if (canOpen && this.frames.length < MAX_FRAMES) {
        this.top = { delim: c, count: rest, items: [] }
        this.frames.push(this.top)
        this.open[c] += 1
      } else {
        this.top.items.push(c.repeat(rest))
      }
    }

    return i + n
  }

  // close matches a closing run of n against the open runs of its kind, nearest first,
  // and returns how much of the run is left.
  private close(c: Delim, n: number): number {
    let count = n

    while (count > 0 && this.open[c] > 0) {
      while (this.top.delim !== c) {
        this.flattenTop()
      }

      const opener = this.top
      const use = Math.min(2, count, opener.count)
      const element = emphasis(c, use, buildHtml(opener.items, this.ctx, false))

      opener.count -= use
      count -= use

      if (opener.count > 0) {
        opener.items = [element]
      } else {
        this.popFrame()
        this.top.items.push(element)
      }
    }

    return count
  }

  private popFrame(): void {
    this.open[this.top.delim] -= 1
    this.frames.pop()
    this.top = last(this.frames)
  }

  // flattenTop gives up the innermost open run: its markers become text.
  private flattenTop(): void {
    const frame = this.top

    this.popFrame()
    this.top.items.push(frame.delim.repeat(frame.count))

    for (const item of frame.items) {
      this.top.items.push(item)
    }
  }

  // image reads ![alt](url), which becomes a link to the image labelled with its alt text.
  private image(i: number): number {
    if (this.s.charAt(i + 1) !== '[') {
      return -1
    }

    const target = this.target(i + 1)

    if (target === null) {
      return -1
    }

    const alt = unescapeText(decodeEntities(this.s.slice(i + 2, target.close))).trim()

    this.push(imageLink(target.url, alt === '' ? 'image' : alt, this.ctx))

    return target.end
  }

  // link reads [text](url) or a reference link. A link holds no other link.
  private link(i: number): number {
    if (this.ctx.inLink) {
      return -1
    }

    const target = this.target(i)

    if (target === null) {
      return -1
    }

    const label = new InlineParser(this.s.slice(i + 1, target.close), {
      ...this.ctx,
      inLink: true,
      flow: false,
    }).parse()

    this.push(linkTo(target.url, keyed(label), this.ctx))

    return target.end
  }

  // target reads what follows the bracketed text opened at i: an inline (url), or else
  // a reference, [label], [], or nothing, that a definition names.
  private target(i: number): { close: number; end: number; url: string } | null {
    const close = this.brackets.get(i)

    if (close === undefined) {
      return null
    }

    if (this.s.charAt(close + 1) === '(') {
      const inline = destination(this.s, close + 2)

      if (inline !== null) {
        return { close, end: inline.end, url: inline.url }
      }
    }

    if (this.ctx.refs.size === 0) {
      return null
    }

    let end = close + 1
    let from = i + 1
    let to = close
    const second = this.s.charAt(close + 1) === '[' ? this.brackets.get(close + 1) : undefined

    if (second !== undefined) {
      end = second + 1

      if (second > close + 2) {
        from = close + 2
        to = second
      }
    }

    if (to - from > MAX_LABEL) {
      return null
    }

    const url = this.ctx.refs.get(normalizeLabel(this.s.slice(from, to)))

    return url === undefined ? null : { close, end, url }
  }

  // angle reads what starts with <: a comment (dropped), an autolink, or an HTML tag.
  private angle(i: number): number {
    const { s } = this

    if (s.startsWith('<!--', i)) {
      const end = this.commentsClose ? s.indexOf('-->', i + 4) : -1

      if (end < 0) {
        this.commentsClose = false

        return -1
      }

      return end + 3
    }

    const auto = matchAt(AUTOLINK, s, i)

    if (auto !== null) {
      const url = group(auto, 1)

      this.push(linkTo(url, url, this.ctx))

      return i + auto[0].length
    }

    const email = matchAt(EMAIL, s, i)

    if (email !== null) {
      const address = group(email, 1)

      this.push(linkTo(`mailto:${address}`, address, this.ctx))

      return i + email[0].length
    }

    const tag = readTag(s, i)

    if (tag === null) {
      return -1
    }

    if (tag.token !== null) {
      this.push(tag.token)
    }

    return tag.end
  }

  // bareUrl reads a URL written out in the text, at the start of a word, without any
  // punctuation that ends the sentence or an unmatched closing parenthesis.
  private bareUrl(i: number): number {
    const before = this.s.charAt(i - 1)

    if (!isSpace(before) && !'*_~('.includes(before)) {
      return -1
    }

    const found = matchAt(BARE_URL, this.s, i)

    if (found === null) {
      return -1
    }

    const raw = found[0]
    const opens = raw.split('(').length
    let closes = raw.split(')').length
    let end = raw.length

    for (;;) {
      const c = raw.charAt(end - 1)

      if (URL_TRAILER.includes(c)) {
        end -= 1
      } else if (c === ')' && closes > opens) {
        end -= 1
        closes -= 1
      } else {
        break
      }
    }

    const url = raw.slice(0, end)

    this.push(linkTo(url, url, this.ctx))

    return i + end
  }
}

// emphasis wraps children in the element a run of markers makes.
function emphasis(c: Delim, use: number, children: readonly ReactNode[]): ReactElement {
  if (c === '~') {
    return <del>{keyed(children)}</del>
  }

  return use === 2 ? <strong className={STRONG}>{keyed(children)}</strong> : <em>{keyed(children)}</em>
}

// matchBrackets pairs each [ with its ], skipping escaped ones, by position.
function matchBrackets(s: string): Map<number, number> {
  const pairs = new Map<number, number>()
  const open: number[] = []

  for (let k = 0; k < s.length; k += 1) {
    const c = s.charAt(k)

    if (c === '\\') {
      k += 1
    } else if (c === '[') {
      open.push(k)
    } else if (c === ']') {
      const start = open.pop()

      if (start !== undefined) {
        pairs.set(start, k)
      }
    }
  }

  return pairs
}

// destination reads a link's (url "title") from just inside its parenthesis, and returns
// the URL and where the link ends. The title is dropped: a link shows its URL instead.
function destination(s: string, from: number): { url: string; end: number } | null {
  const limit = Math.min(s.length, from + MAX_TARGET)
  let j = skipSpace(s, from, limit)
  let url: string

  if (s.charAt(j) === '<') {
    let k = j + 1

    while (k < limit && !'<>\n'.includes(s.charAt(k))) {
      k += 1
    }

    if (s.charAt(k) !== '>') {
      return null
    }

    url = s.slice(j + 1, k)
    j = k + 1
  } else {
    const start = j
    let depth = 0

    for (; j < limit; j += 1) {
      const c = s.charAt(j)

      if (c === '\\') {
        j += 1
      } else if (c === '(') {
        depth += 1
      } else if (c === ')') {
        if (depth === 0) {
          break
        }

        depth -= 1
      } else if (isSpace(c)) {
        break
      }
    }

    url = s.slice(start, j)
  }

  j = skipSpace(s, j, limit)

  const quote = s.charAt(j)

  if (quote === '"' || quote === "'" || quote === '(') {
    const end = quote === '(' ? ')' : quote
    let k = j + 1

    while (k < limit && s.charAt(k) !== end) {
      k += s.charAt(k) === '\\' ? 2 : 1
    }

    if (k >= limit) {
      return null
    }

    j = skipSpace(s, k + 1, limit)
  }

  return s.charAt(j) === ')' ? { url: unescapeText(decodeEntities(url)), end: j + 1 } : null
}

function skipSpace(s: string, from: number, limit: number): number {
  let j = from

  while (j < limit && isSpace(s.charAt(j))) {
    j += 1
  }

  return j
}

// ---------------------------------------------------------------------------------------
// HTML
// ---------------------------------------------------------------------------------------

const OPEN_TAG =
  /<([a-zA-Z][a-zA-Z0-9-]*)((?:\s+[a-zA-Z_:][a-zA-Z0-9_.:-]*(?:\s*=\s*(?:[^\s"'=<>`]+|'[^']*'|"[^"]*"))?)*)\s*\/?>/y
const CLOSE_TAG = /<\/([a-zA-Z][a-zA-Z0-9-]*)\s*>/y
const ATTRIBUTE = /([a-zA-Z_:][a-zA-Z0-9_.:-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g

// HIDDEN are the elements whose content is never shown: it is code, styling, or a frame's,
// not text.
const HIDDEN = new Set(
  'script style textarea title iframe noscript noembed noframes xmp template svg math plaintext'.split(' '),
)

// TagToken is an HTML tag met in the text. Of its attributes only the ones the renderer
// reads matter: a link's href, an image's src and alt, and details' open.
class TagToken {
  readonly name: string
  readonly close: boolean
  readonly attrs: ReadonlyMap<string, string>

  constructor(name: string, close: boolean, attrs: ReadonlyMap<string, string>) {
    this.name = name
    this.close = close
    this.attrs = attrs
  }
}

// Item is what inline text and HTML blocks are read into before their tags are paired.
type Item = ReactNode | TagToken

// readTag reads an opening or closing tag at i. For an element whose content is hidden,
// it reads on past the closing tag, and returns no token.
function readTag(s: string, i: number): { token: TagToken | null; end: number } | null {
  const open = matchAt(OPEN_TAG, s, i)

  if (open !== null) {
    const name = group(open, 1).toLowerCase()
    const end = i + open[0].length

    if (HIDDEN.has(name)) {
      const close = new RegExp(`</${name}\\s*>`, 'gi')

      close.lastIndex = end

      const found = close.exec(s)

      return { token: null, end: found === null ? s.length : found.index + found[0].length }
    }

    return { token: new TagToken(name, false, attributes(group(open, 2))), end }
  }

  const close = matchAt(CLOSE_TAG, s, i)

  return close === null
    ? null
    : { token: new TagToken(group(close, 1).toLowerCase(), true, new Map()), end: i + close[0].length }
}

// attributes reads a tag's attributes, with their entities decoded.
function attributes(text: string): Map<string, string> {
  const found = new Map<string, string>()

  for (const attribute of text.matchAll(ATTRIBUTE)) {
    found.set(group(attribute, 1).toLowerCase(), decodeEntities(attribute[2] ?? attribute[3] ?? attribute[4] ?? ''))
  }

  return found
}

// htmlItems reads an HTML block into text and tags. A comment is dropped, and one left
// open drops the rest, as a browser does.
function htmlItems(s: string): Item[] {
  const items: Item[] = []

  for (let i = 0; i < s.length;) {
    if (s.charAt(i) !== '<') {
      const lt = s.indexOf('<', i)
      const end = lt < 0 ? s.length : lt

      items.push(decodeEntities(s.slice(i, end)))
      i = end
    } else if (s.startsWith('<!--', i)) {
      const close = s.indexOf('-->', i + 4)

      i = close < 0 ? s.length : close + 3
    } else {
      const tag = readTag(s, i)

      if (tag === null) {
        items.push('<')
        i += 1
      } else {
        if (tag.token !== null) {
          items.push(tag.token)
        }

        i = tag.end
      }
    }
  }

  return items
}

interface TagSpec {
  // as is the element the tag renders as, with className; no attribute of the tag's own
  // carries over.
  as: string
  className: string
  // closes lists the open tags that this one ends, as an HTML parser would: a new list
  // item ends the last, and a block ends an open paragraph.
  closes: readonly string[]
  // block is whether the element is a block, which an element holding only inline
  // content can't hold; inline is whether it holds only inline content (a paragraph, a
  // heading, emphasis, a link). A block tag met inside such an element is dropped, and
  // its content kept, so the page never nests a block where HTML forbids one.
  block: boolean
  inline: boolean
  // noText drops whitespace between the element's children, which only some elements
  // (a list, a table row) may not hold.
  noText: boolean
}

const PHRASE: readonly string[] = []
const ENDS_P = ['p']

// phrase is an inline element.
function phrase(as: string, className: string): TagSpec {
  return { as, className, closes: PHRASE, block: false, inline: true, noText: false }
}

// textBlock is a block that holds only inline content: a paragraph, a heading.
function textBlock(as: string, className: string, closes: readonly string[]): TagSpec {
  return { as, className, closes, block: true, inline: true, noText: false }
}

// container is a block that holds blocks.
function container(as: string, className: string, closes: readonly string[], noText: boolean): TagSpec {
  return { as, className, closes, block: true, inline: false, noText }
}

// TAGS are the HTML tags whose meaning is kept. Any other tag is dropped, and its content
// kept as text.
const TAGS = new Map<string, TagSpec>([
  ['a', phrase('a', LINK)],
  ['b', phrase('strong', STRONG)],
  ['strong', phrase('strong', STRONG)],
  ['i', phrase('em', '')],
  ['em', phrase('em', '')],
  ['cite', phrase('em', '')],
  ['code', phrase('code', INLINE_CODE)],
  ['tt', phrase('code', INLINE_CODE)],
  ['samp', phrase('code', INLINE_CODE)],
  ['kbd', phrase('kbd', KBD)],
  ['sub', phrase('sub', '')],
  ['sup', phrase('sup', '')],
  ['del', phrase('del', '')],
  ['s', phrase('del', '')],
  ['strike', phrase('del', '')],
  ['ins', phrase('ins', '')],
  ['u', phrase('u', '')],
  ['mark', phrase('mark', MARK)],
  ['small', phrase('small', '')],
  ['var', phrase('var', '')],
  ['p', textBlock('p', '', ENDS_P)],
  ['h1', textBlock('h1', HEADINGS[1].className, ENDS_P)],
  ['h2', textBlock('h2', HEADINGS[2].className, ENDS_P)],
  ['h3', textBlock('h3', HEADINGS[3].className, ENDS_P)],
  ['h4', textBlock('h4', HEADINGS[4].className, ENDS_P)],
  ['h5', textBlock('h5', HEADINGS[5].className, ENDS_P)],
  ['h6', textBlock('h6', HEADINGS[6].className, ENDS_P)],
  ['pre', textBlock('pre', CODE_BLOCK, ENDS_P)],
  ['dt', textBlock('div', STRONG, ['dt', 'dd', 'p'])],
  ['summary', textBlock('summary', SUMMARY, PHRASE)],
  ['ul', container('ul', BULLETS, ENDS_P, true)],
  ['ol', container('ol', NUMBERS, ENDS_P, true)],
  ['li', container('li', ITEM, ['li', 'p'], false)],
  ['dl', container('div', 'space-y-1', ENDS_P, false)],
  ['dd', container('div', 'pl-4', ['dt', 'dd', 'p'], false)],
  ['blockquote', container('blockquote', QUOTE, ENDS_P, false)],
  ['table', container('table', TABLE, ENDS_P, true)],
  ['caption', container('caption', 'pb-1 text-muted', PHRASE, false)],
  ['thead', container('thead', '', PHRASE, true)],
  ['tbody', container('tbody', '', PHRASE, true)],
  ['tfoot', container('tfoot', '', PHRASE, true)],
  ['tr', container('tr', '', ['tr', 'td', 'th'], true)],
  ['th', container('th', HEAD_CELL, ['td', 'th'], false)],
  ['td', container('td', CELL, ['td', 'th'], false)],
  ['details', container('details', DETAILS, ENDS_P, false)],
  ...'div center section article header footer main nav aside figure figcaption address hgroup search'
    .split(' ')
    .map((name): [string, TagSpec] => [name, container('div', 'space-y-2', ENDS_P, false)]),
])

const FLOW_ROOT = container('div', '', PHRASE, false)
const INLINE_ROOT = phrase('span', '')
const TBODY = container('tbody', '', PHRASE, true)

// OpenTag is an element being built: its tag, the link URL for a link, and its children.
interface OpenTag {
  name: string
  spec: TagSpec
  url: string | null
  children: ReactNode[]
}

// buildHtml pairs the tags among items into elements, for a place that holds blocks
// (flow) or only inline content. A closing tag with no open one is dropped, and open
// ones close at the end, as a browser forgives them. A link holds no other link, an
// image becomes a link (or its alt text inside a link), a block tag where only inline
// content may go is dropped, and past MAX_TAGS deep further tags are dropped.
function buildHtml(items: readonly Item[], ctx: Context, flow: boolean): ReactNode[] {
  const root: OpenTag = { name: '', spec: flow ? FLOW_ROOT : INLINE_ROOT, url: null, children: [] }
  const stack = [root]
  let top = root
  let links = ctx.inLink ? 1 : 0
  // inline counts the open elements, the root among them, that hold only inline content.
  let inline = flow ? 0 : 1
  const skipped = new Map<string, number>()

  const close = (): void => {
    const done = top

    stack.pop()
    top = last(stack)

    if (done.url !== null) {
      links -= 1
    }

    if (done.spec.inline) {
      inline -= 1
    }

    top.children.push(renderTag(done, ctx))
  }

  const skip = (name: string): void => {
    skipped.set(name, (skipped.get(name) ?? 0) + 1)
  }

  for (const item of items) {
    if (!(item instanceof TagToken)) {
      if (!(top.spec.noText && typeof item === 'string' && item.trim() === '')) {
        top.children.push(item)
      }

      continue
    }

    const { name } = item

    if (item.close) {
      const pending = skipped.get(name) ?? 0
      const at = stack.findLastIndex((tag) => tag.name === name)

      if (pending > 0) {
        skipped.set(name, pending - 1)
      } else if (at > 0) {
        while (stack.length > at) {
          close()
        }
      }

      continue
    }

    if (name === 'br') {
      top.children.push(<br />)
      continue
    }

    if (name === 'hr') {
      if (inline === 0) {
        top.children.push(<hr className={RULE} />)
      }

      continue
    }

    if (name === 'img') {
      const alt = (item.attrs.get('alt') ?? '').trim()

      if (alt !== '') {
        top.children.push(imageLink(item.attrs.get('src') ?? '', alt, { ...ctx, inLink: links > 0 }))
      }

      continue
    }

    const tag = TAGS.get(name)

    if (tag === undefined) {
      continue
    }

    const url = name === 'a' && links === 0 ? safeUrl(item.attrs.get('href') ?? '') : null

    if ((name === 'a' && url === null) || stack.length > MAX_TAGS) {
      skip(name)
      continue
    }

    while (tag.closes.includes(top.name)) {
      close()
    }

    if (tag.block && inline > 0) {
      skip(name)
      continue
    }

    if (name === 'tr' && top.name === 'table') {
      top = { name: 'tbody', spec: TBODY, url: null, children: [] }
      stack.push(top)
    }

    top = { name, spec: tag, url, children: [] }
    stack.push(top)

    if (url !== null) {
      links += 1
    }

    if (tag.inline) {
      inline += 1
    }
  }

  while (stack.length > 1) {
    close()
  }

  return root.children
}

// renderTag renders a built element: a link as a link, a table in a scrolling box, and
// any other as its kept element with the app's classes.
function renderTag(tag: OpenTag, ctx: Context): ReactNode {
  const children = keyed(tag.children)

  if (tag.url !== null) {
    return linkButton(tag.url, children, ctx)
  }

  if (tag.name === 'table') {
    return (
      <div className="overflow-x-auto">
        <table className={TABLE}>{children}</table>
      </div>
    )
  }

  return createElement(tag.spec.as, { className: tag.spec.className }, children)
}

// ---------------------------------------------------------------------------------------
// Links
// ---------------------------------------------------------------------------------------

const SCHEMES = new Set(['http:', 'https:', 'mailto:'])

// safeUrl returns the URL a link may open, or null when it is not an http, https, or
// mailto URL: a relative one, or a javascript:, data:, or file: one.
function safeUrl(raw: string): string | null {
  try {
    const url = new URL(raw.trim())

    return SCHEMES.has(url.protocol) ? url.href : null
  } catch {
    return null
  }
}

// linkTo renders label as a link to raw, or as text when raw is no URL a link may open or
// the label is already inside a link.
function linkTo(raw: string, label: ReactNode, ctx: Context): ReactNode {
  const url = ctx.inLink ? null : safeUrl(raw)

  return url === null ? label : linkButton(url, label, ctx)
}

// linkButton is a link: a button that opens url outside the app. It is a button rather
// than an anchor so that nothing, not a middle click or a context menu, can load the URL
// in the app's own window.
function linkButton(url: string, label: ReactNode, ctx: Context): ReactElement {
  return (
    <button
      type="button"
      title={url}
      className={LINK}
      onClick={(event) => {
        event.preventDefault()
        ctx.openLink(url)
      }}
    >
      {label}
    </button>
  )
}

// imageLink stands in for an image: a link to it, labelled with its alt text. The image
// itself never loads.
function imageLink(src: string, alt: string, ctx: Context): ReactNode {
  return linkTo(
    src,
    <>
      <span aria-hidden="true">🖼 </span>
      {alt}
    </>,
    ctx,
  )
}

// ---------------------------------------------------------------------------------------
// Rendering blocks
// ---------------------------------------------------------------------------------------

function renderBlocks(blocks: readonly Block[], ctx: Context): ReactElement[] {
  return blocks.map((block, n) => <Fragment key={n}>{renderBlock(block, ctx)}</Fragment>)
}

function renderBlock(block: Block, ctx: Context): ReactNode {
  switch (block.kind) {
    case 'heading':
      return createElement(
        HEADINGS[block.level].tag,
        { className: HEADINGS[block.level].className },
        inline(block.text, ctx),
      )
    case 'paragraph':
      return <p>{inline(block.text, ctx)}</p>
    case 'code':
      return (
        <pre className={CODE_BLOCK}>
          <code>{block.text}</code>
        </pre>
      )
    case 'quote':
      return <blockquote className={QUOTE}>{renderBlocks(block.blocks, ctx)}</blockquote>
    case 'list':
      return renderList(block, ctx)
    case 'rule':
      return <hr className={RULE} />
    case 'table':
      return renderTable(block, ctx)
    case 'details':
      return (
        <details open={block.open} className={DETAILS}>
          <summary className={SUMMARY}>{inline(block.summary, ctx)}</summary>
          <div className="mt-2 space-y-2">{renderBlocks(block.blocks, ctx)}</div>
        </details>
      )
    case 'html': {
      const nodes = buildHtml(htmlItems(block.text), ctx, true)

      return nodes.length === 0 ? null : <div className="space-y-2">{keyed(nodes)}</div>
    }
  }
}

function renderList(block: Extract<Block, { kind: 'list' }>, ctx: Context): ReactElement {
  const items = block.items.map((item, n) =>
    item.task === null ? (
      <li key={n} className={ITEM}>
        {renderBlocks(item.blocks, ctx)}
      </li>
    ) : (
      <li key={n} className={TASK_ITEM}>
        <input
          type="checkbox"
          className="checkbox mt-0.5"
          checked={item.task}
          disabled
          readOnly
          aria-label={item.task ? 'Done' : 'Not done'}
        />
        <div className="min-w-0 flex-1 space-y-1">{renderBlocks(item.blocks, ctx)}</div>
      </li>
    ),
  )

  return block.ordered ? (
    <ol start={block.start} className={NUMBERS}>
      {items}
    </ol>
  ) : (
    <ul className={BULLETS}>{items}</ul>
  )
}

function renderTable(block: Extract<Block, { kind: 'table' }>, ctx: Context): ReactElement {
  // A cell may hold blocks, such as a list written in HTML.
  const cellCtx = { ...ctx, flow: true }

  return (
    <div className="overflow-x-auto">
      <table className={TABLE}>
        <thead>
          <tr>
            {block.head.map((cell, n) => (
              <th key={n} className={`${HEAD_CELL} ${ALIGN[cell.align]}`}>
                {inline(cell.text, cellCtx)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {block.rows.map((row, r) => (
            <tr key={r}>
              {row.map((cell, n) => (
                <td key={n} className={`${CELL} ${ALIGN[cell.align]}`}>
                  {inline(cell.text, cellCtx)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function inline(text: string, ctx: Context): ReactNode[] {
  return keyed(new InlineParser(text, ctx).parse())
}

// keyed gives each node of a built list a key, by its place.
function keyed(nodes: readonly ReactNode[]): ReactNode[] {
  return nodes.map((node, n) => <Fragment key={n}>{node}</Fragment>)
}

// ---------------------------------------------------------------------------------------
// Text helpers
// ---------------------------------------------------------------------------------------

const ENTITY = /&(?:#(\d{1,7}|[xX][0-9a-fA-F]{1,6})|([a-zA-Z][a-zA-Z0-9]{1,31}));/g

const NAMED_ENTITIES = new Map([
  ['amp', '&'],
  ['lt', '<'],
  ['gt', '>'],
  ['quot', '"'],
  ['apos', "'"],
  ['nbsp', '\u00a0'],
  ['copy', '©'],
  ['reg', '®'],
  ['trade', '™'],
  ['hellip', '…'],
  ['mdash', '—'],
  ['ndash', '–'],
  ['lsquo', '‘'],
  ['rsquo', '’'],
  ['ldquo', '“'],
  ['rdquo', '”'],
  ['bull', '•'],
  ['middot', '·'],
  ['times', '×'],
  ['rarr', '→'],
  ['larr', '←'],
  ['zwj', '\u200d'],
  ['zwnj', '\u200c'],
])

// decodeEntities turns character references into their characters: numeric ones, and the
// named ones PR descriptions use. An unknown name is left as written.
function decodeEntities(text: string): string {
  if (!text.includes('&')) {
    return text
  }

  // Exactly one of number and name takes part in a match.
  return text.replace(ENTITY, (whole: string, number: string | undefined, name: string) => {
    if (number === undefined) {
      return NAMED_ENTITIES.get(name) ?? whole
    }

    const hex = number.startsWith('x') || number.startsWith('X')
    const code = hex ? parseInt(number.slice(1), 16) : parseInt(number, 10)
    const valid = code > 0 && code <= 0x10ffff && (code < 0xd800 || code > 0xdfff)

    return valid ? String.fromCodePoint(code) : '\ufffd'
  })
}

function unescapeText(text: string): string {
  return text.replace(ESCAPED, '$1')
}

function normalizeLabel(label: string): string {
  return label.trim().replace(/\s+/g, ' ').toLowerCase()
}

function oneLine(text: string): string {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
    .join(' ')
}

function expandTabs(line: string): string {
  if (!line.includes('\t')) {
    return line
  }

  let out = ''

  for (const c of line) {
    out += c === '\t' ? ' '.repeat(4 - (out.length % 4)) : c
  }

  return out
}

function indentOf(line: string): number {
  let n = 0

  while (line.charAt(n) === ' ') {
    n += 1
  }

  return n
}

function isBlank(line: string): boolean {
  return line.trim() === ''
}

function isSpace(c: string): boolean {
  return c === '' || WHITESPACE.test(c)
}

function isPunct(c: string): boolean {
  return PUNCTUATION.test(c)
}

function runLength(s: string, i: number, c: string): number {
  let j = i

  while (s.charAt(j) === c) {
    j += 1
  }

  return j - i
}

function matchAt(pattern: RegExp, s: string, i: number): RegExpExecArray | null {
  pattern.lastIndex = i

  return pattern.exec(s)
}

// lineAt returns line i, or '' past either end.
function lineAt(lines: readonly string[], i: number): string {
  return lines[i] ?? ''
}

// last returns the last of items, which callers never leave empty.
function last<T>(items: readonly T[]): T {
  return items[items.length - 1] as T
}

// group returns a match's group n, or '' when the group took no part in it.
function group(match: readonly (string | undefined)[], n: number): string {
  return match[n] ?? ''
}
