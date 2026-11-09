// The filter language the toolbar's search field speaks on every page that filters, parsed
// in the window: nothing a query names leaves it, and matching reads only what the page
// already holds.
//
// A query is words and key:value filters, GitHub-style:
//
//   - Words AND together, each a case- and accent-insensitive part of what an item says
//     (its haystack). "A quoted phrase" matches as a phrase, and -word excludes.
//   - key:value counts only for the keys a page declares (its QuerySpec); anything else
//     stays text, so a title such as "fix: x" still matches. -key:value excludes, and
//     key:a,b means either. A quoted value keeps its commas: label:"type: bug, ui".
//   - Repeated keys: either for a property an item has one of (repo:a repo:b), both for
//     one it can have several of (label:bug label:ui), as an item can satisfy them.
//   - A value matches as part of an item's value, so results narrow while typing, and
//     exactly when it names a value some item has, as a picked suggestion does.
//   - #123 matches the number exactly, and a bare 123 matches it too.

import { counted, formatCount } from './format'

// QuerySpec is what a page's filter understands.
export interface QuerySpec {
  // keys are the keys the page knows, in lower case.
  keys: readonly string[]
  // multi are the keys of properties an item can have several values of, such as label,
  // where repeated filters must all match; repeated filters of any other key match
  // either.
  multi?: readonly string[]
}

// Term is a word or a quoted phrase.
export interface Term {
  // text is the word or phrase, folded (see fold).
  text: string
  negated: boolean
  // number is the item number the term names: #123, or a bare 123; null when none.
  number: number | null
  // numberOnly reports #123, which matches only the number.
  numberOnly: boolean
}

// Filter is a key:value filter, with every value it may match.
export interface Filter {
  key: string
  // values are folded; any of them matches.
  values: string[]
  negated: boolean
}

export interface Query {
  terms: Term[]
  filters: Filter[]
}

// Token is one whitespace-separated part of a query, quotes keeping spaces in.
interface Token {
  start: number
  end: number
  negated: boolean
  // key is the token's key, lower case, when the page knows it, else ''.
  key: string
  // body is what follows the '-' and the key: a key's values, or the word or phrase.
  body: string
  // bodyStart is where body starts in the query.
  bodyStart: number
}

const space = /\s/
const keyPattern = /^([a-z][\w-]*):/i

// readToken reads the token at [start, end) of src.
function readToken(src: string, start: number, end: number, keys: ReadonlySet<string>): Token {
  const negated = src[start] === '-' && end - start > 1
  const at = negated ? start + 1 : start
  const match = keyPattern.exec(src.slice(at, end))
  const key = match?.[1]?.toLowerCase() ?? ''

  if (match !== null && keys.has(key)) {
    const bodyStart = at + match[0].length

    return { start, end, negated, key, body: src.slice(bodyStart, end), bodyStart }
  }

  return { start, end, negated, key: '', body: src.slice(at, end), bodyStart: at }
}

// tokenize splits src at whitespace outside quotes.
function tokenize(src: string, keys: ReadonlySet<string>): Token[] {
  const tokens: Token[] = []
  let i = 0

  while (i < src.length) {
    if (space.test(src.charAt(i))) {
      i++
      continue
    }

    const start = i
    let quoted = false

    while (i < src.length && (quoted || !space.test(src.charAt(i)))) {
      if (src.charAt(i) === '"') {
        quoted = !quoted
      }

      i++
    }

    tokens.push(readToken(src, start, i, keys))
  }

  return tokens
}

// splitValues splits a key's values at the commas outside quotes, dropping the quotes and
// any empty value.
function splitValues(body: string): string[] {
  const parts: string[] = []
  let quoted = false
  let current = ''

  for (const ch of body) {
    if (ch === '"') {
      quoted = !quoted
    } else if (ch === ',' && !quoted) {
      parts.push(current)
      current = ''
    } else {
      current += ch
    }
  }

  parts.push(current)

  return parts.map((p) => p.trim()).filter((p) => p !== '')
}

const hashNumber = /^#(\d+)$/
const bareNumber = /^\d+$/

// readTerm reads a word or phrase token as a term, or null when it says nothing.
function readTerm(token: Token): Term | null {
  const text = fold(token.body.replaceAll('"', '').trim())

  if (text === '') {
    return null
  }

  const hash = hashNumber.exec(text)
  const number = hash !== null ? Number(hash[1]) : bareNumber.test(text) ? Number(text) : null

  return { text, negated: token.negated, number, numberOnly: hash !== null }
}

// parseQuery reads a query against what a page understands. A key with no value yet, as
// while "repo:" is typed, filters nothing.
export function parseQuery(src: string, spec: QuerySpec): Query {
  const multi = new Set(spec.multi)
  const terms: Term[] = []
  const filters: Filter[] = []
  // either holds the one positive filter of each single-valued key, which its repeats
  // add their values to.
  const either = new Map<string, Filter>()

  for (const token of tokenize(src, new Set(spec.keys))) {
    if (token.key === '') {
      const term = readTerm(token)

      if (term !== null) {
        terms.push(term)
      }

      continue
    }

    const values = splitValues(token.body).map(fold)
    const merged = token.negated || multi.has(token.key) ? undefined : either.get(token.key)

    if (values.length === 0) {
      continue
    }

    if (merged !== undefined) {
      merged.values.push(...values)
      continue
    }

    const filter = { key: token.key, values, negated: token.negated }

    filters.push(filter)

    if (!token.negated && !multi.has(token.key)) {
      either.set(token.key, filter)
    }
  }

  return { terms, filters }
}

// isEmpty reports a query that filters nothing.
export function isEmpty(query: Query): boolean {
  return query.terms.length === 0 && query.filters.length === 0
}

// unplain finds a character other than printable ASCII, a tab, or a newline; text without
// one folds by lowercasing alone.
const unplain = /[^\t\n -~]/
const combining = /\p{M}/gu

// Folded is text folded for matching, with where each of its characters came from.
export interface Folded {
  text: string
  // at[i] is the index in the original of the character text[i] came from, and
  // at[text.length] is the original's length, so a range of text maps back to one of the
  // original.
  at: number[]
}

// apostrophes are the curly apostrophes the app's own words use (Codex’s), which fold to
// the straight one a keyboard types.
const apostrophes = /[‘’]/g

// foldChar folds one character: its accents dropped, in lower case, a curly apostrophe
// straight.
function foldChar(ch: string): string {
  return ch
    .normalize('NFD')
    .replace(combining, '')
    .toLowerCase()
    .normalize('NFD')
    .replace(combining, '')
    .replace(apostrophes, "'")
}

// foldMapped folds text one character at a time, keeping the map back to the original.
// Folding can change the text's length ("é" written as e and an accent, "İ"), and the map
// keeps a match lined up with the text it was found in.
export function foldMapped(s: string): Folded {
  if (!unplain.test(s)) {
    return { text: s.toLowerCase(), at: Array.from({ length: s.length + 1 }, (_, i) => i) }
  }

  let text = ''
  const at: number[] = []
  let i = 0

  for (const ch of s) {
    const folded = foldChar(ch)

    for (let k = 0; k < folded.length; k++) {
      at.push(i)
    }

    text += folded
    i += ch.length
  }

  at.push(i)

  return { text, at }
}

// fold is text as it matches: in lower case, without accents.
export function fold(s: string): string {
  return unplain.test(s) ? foldMapped(s).text : s.toLowerCase()
}

// weakCache remembers what build makes of each object for as long as the object lives.
// The inbox keeps an unchanged item's object from one snapshot to the next (lib/share),
// so an item's haystack is folded once, however often the query changes.
export function weakCache<T extends object, R>(build: (x: T) => R): (x: T) => R {
  const cache = new WeakMap<T, R>()

  return (x) => {
    if (cache.has(x)) {
      return cache.get(x) as R
    }

    const made = build(x)

    cache.set(x, made)

    return made
  }
}

// haystack folds the parts of what an item says into the text its words match, keeping
// the parts apart so a phrase never spans two of them.
export function haystack(parts: readonly string[]): string {
  return fold(parts.join('\n'))
}

// matchesTerms reports whether text matches every term: each word or phrase a part of
// hay, or the item's number, and none of the excluded ones.
export function matchesTerms(terms: readonly Term[], hay: string, number?: number): boolean {
  return terms.every((t) => {
    const hit = (!t.numberOnly && hay.includes(t.text)) || (t.number !== null && t.number === number)

    return hit !== t.negated
  })
}

// matchesFilter reports whether an item's values for a filter's key match it: any value
// part of one of have, or equal to one when it names a value some item has (known).
// have is null when the item can't tell, as before its details are read: it matches no
// such filter, so an excluding one keeps it.
export function matchesFilter(filter: Filter, have: readonly string[] | null, known: ReadonlySet<string>): boolean {
  if (have === null) {
    return filter.negated
  }

  const hit = filter.values.some((want) => (known.has(want) ? have.includes(want) : have.some((v) => v.includes(want))))

  return hit !== filter.negated
}

// markTerms is the words and phrases a query looks for, folded, as one string a row can
// take as a prop and keep from one query to the next while they stay the same. Highlight
// marks them.
export function markTerms(query: Query): string {
  const marks = new Set(query.terms.filter((t) => !t.negated && !t.numberOnly).map((t) => t.text))

  return [...marks].join('\n')
}

// markRanges finds marks (as markTerms joins them) in text: each range a match, in order,
// with overlapping ranges merged.
export function markRanges(text: string, marks: string): [number, number][] {
  if (marks === '') {
    return []
  }

  const folded = foldMapped(text)
  const found: [number, number][] = []

  for (const term of marks.split('\n')) {
    for (let i = folded.text.indexOf(term); i !== -1; i = folded.text.indexOf(term, i + term.length)) {
      found.push([folded.at[i] ?? 0, folded.at[i + term.length] ?? text.length])
    }
  }

  found.sort((a, b) => a[0] - b[0])

  const merged: [number, number][] = []

  for (const range of found) {
    const last = merged.at(-1)

    if (last !== undefined && range[0] <= last[1]) {
      last[1] = Math.max(last[1], range[1])
    } else {
      merged.push(range)
    }
  }

  return merged
}

// Edit is a change to a query: [from, to) replaced with text.
export interface Edit {
  from: number
  to: number
  text: string
}

// Edited is the query a choice leaves behind, and where the caret is in it.
export interface Edited {
  value: string
  caret: number
}

// insertSuggestion makes a chosen suggestion's edit, then puts the caret after it: right
// after a key, which waits for its value, and past a space after anything else, ready
// for the next word.
export function insertSuggestion(src: string, edit: Edit): Edited {
  const before = src.slice(0, edit.from) + edit.text
  const after = src.slice(edit.to)

  if (edit.text.endsWith(':')) {
    return { value: before + after, caret: before.length }
  }

  return { value: before + (after.startsWith(' ') ? '' : ' ') + after, caret: before.length + 1 }
}

// quoteValue writes a value so it reads back as itself: quoted when it holds a space, a
// comma, or a quote, which it cannot hold, so it loses.
export function quoteValue(value: string): string {
  return /[\s,"]/.test(value) ? `"${value.replaceAll('"', '')}"` : value
}

// toggleFilter adds key:value to a query, or takes it out when the query has it as a
// filter of its own, as clicking a label on a card does.
export function toggleFilter(src: string, key: string, value: string, spec: QuerySpec): string {
  const want = fold(value)
  const found = tokenize(src, new Set(spec.keys)).find((t) => {
    const [only, ...more] = splitValues(t.body)

    return !t.negated && t.key === key && only !== undefined && more.length === 0 && fold(only) === want
  })

  if (found !== undefined) {
    return `${src.slice(0, found.start).trimEnd()} ${src.slice(found.end).trimStart()}`.trim()
  }

  const base = src.trimEnd()
  const token = `${key}:${quoteValue(value)}`

  return base === '' ? token : `${base} ${token}`
}

// Segment is a run of a query as the field draws it: a key it understood, tinted, or
// anything else.
export interface Segment {
  text: string
  key: boolean
}

// segments splits a query into the runs the field draws, so a key the page understands
// (with its '-') stands out as it is typed.
export function segments(src: string, spec: QuerySpec): Segment[] {
  const out: Segment[] = []
  let at = 0

  for (const t of tokenize(src, new Set(spec.keys))) {
    if (t.key === '') {
      continue
    }

    if (t.start > at) {
      out.push({ text: src.slice(at, t.start), key: false })
    }

    out.push({ text: src.slice(t.start, t.bodyStart), key: true })
    at = t.bodyStart
  }

  if (at < src.length) {
    out.push({ text: src.slice(at), key: false })
  }

  return out
}

// Completion is what the caret is in: the token it ends or sits inside, or an empty one
// in the space between them.
export interface Completion {
  start: number
  end: number
  negated: boolean
  // key is the token's key, when the page knows it, else ''.
  key: string
  // partial is what is typed of the value or word up to the caret, unquoted.
  partial: string
  // from and to are the part a chosen suggestion replaces: the value the caret is in, up
  // to the next comma, or the word after the token's '-'.
  from: number
  to: number
}

// valueBounds finds the value the caret is in, within a key's values (body, starting at
// bodyStart): from after the last comma before the caret to the next comma or the end,
// commas inside quotes not counting.
function valueBounds(body: string, bodyStart: number, caret: number): { from: number; to: number } {
  let from = bodyStart
  let to = bodyStart + body.length
  let quoted = false

  for (let i = 0; i < body.length; i++) {
    const ch = body.charAt(i)
    const at = bodyStart + i

    if (ch === '"') {
      quoted = !quoted
    } else if (ch === ',' && !quoted) {
      if (at < caret) {
        from = at + 1
      } else {
        to = at
        break
      }
    }
  }

  return { from, to }
}

// tokenAt is the token the caret ends or sits inside, for completing it, or an empty one
// at the caret when it sits between tokens.
export function tokenAt(src: string, caret: number, spec: QuerySpec): Completion {
  const token = tokenize(src, new Set(spec.keys)).find((t) => t.start < caret && caret <= t.end)

  if (token === undefined) {
    return { start: caret, end: caret, negated: false, key: '', partial: '', from: caret, to: caret }
  }

  if (token.key === '') {
    const from = Math.min(token.bodyStart, caret)

    return {
      start: token.start,
      end: token.end,
      negated: token.negated,
      key: '',
      partial: src.slice(from, caret).replaceAll('"', ''),
      from,
      to: token.end,
    }
  }

  const { from, to } = valueBounds(token.body, token.bodyStart, caret)

  return {
    start: token.start,
    end: token.end,
    negated: token.negated,
    key: token.key,
    partial: src.slice(from, Math.max(from, caret)).replaceAll('"', '').trimStart(),
    from,
    to,
  }
}

// Suggestion is one choice the field offers under itself.
export interface Suggestion {
  // id tells the choices apart.
  id: string
  label: string
  // detail is the filter it writes, as in "is:pr", shown quieter beside the label.
  detail: string
  // count is how much it would match, ready to show.
  count: string
  edit: Edit
}

export interface Suggestions {
  // label names the list for assistive tech.
  label: string
  options: readonly Suggestion[]
  // auto reports a list the field opens on its own: on an empty query, or while a key's
  // value is typed. Any other waits for ↓.
  auto: boolean
  // pickFirst starts the choice at the first option, ready for Enter or Tab, as a key's
  // values do; otherwise nothing is chosen until ↓.
  pickFirst?: boolean
  // hint is a line at the list's foot, or ''.
  hint: string
}

// Suggester offers the choices for a query with the caret at caret.
export type Suggester = (src: string, caret: number) => Suggestions

// Facet is one way to narrow a page's items, offered on an empty query: a whole filter
// such as is:pr, or a key alone (value ''), whose values the list offers next.
export interface Facet {
  label: string
  key: string
  value?: string
  // noun counts a key's values, as in ['repo', 'repos'].
  noun?: readonly [string, string]
  // optional facets show only while some item has one.
  optional?: boolean
}

// SuggestOptions are what a page offers suggestions from.
export interface SuggestOptions<T> {
  spec: QuerySpec
  items: readonly T[]
  matches: (item: T, query: Query) => boolean
  // values lists an item's values for a key, as they read.
  values: (item: T, key: string) => readonly string[]
  facets: readonly Facet[]
  // hint is the line under the facets: what words match, and how to exclude.
  hint: string
  // nouns name each key's values for the list's label, as in "Labels".
  nouns?: Readonly<Record<string, string>>
}

// mostSuggested is how many values the list offers at most.
const mostSuggested = 20

// valueSuggestions offers the values of the key being typed among the items pool holds,
// those that hold what is typed, the most common first.
function valueSuggestions<T>(at: Completion, pool: readonly T[], o: SuggestOptions<T>): Suggestions {
  const counts = new Map<string, { value: string; n: number }>()

  for (const item of pool) {
    const seen = new Set<string>()

    for (const value of o.values(item, at.key)) {
      const key = fold(value)

      if (seen.has(key)) {
        continue
      }

      seen.add(key)

      const entry = counts.get(key)

      if (entry === undefined) {
        counts.set(key, { value, n: 1 })
      } else {
        entry.n += 1
      }
    }
  }

  const want = fold(at.partial)
  const options = [...counts.entries()]
    .filter(([key]) => key.includes(want))
    .sort(([ka, a], [kb, b]) => b.n - a.n || ka.localeCompare(kb))
    .slice(0, mostSuggested)
    .map(([, { value, n }]) => ({
      id: `${at.key}:${value}`,
      label: value,
      detail: '',
      count: formatCount(n),
      edit: { from: at.from, to: at.to, text: quoteValue(value) },
    }))

  return { label: o.nouns?.[at.key] ?? at.key, options, auto: true, pickFirst: true, hint: '' }
}

// facetCount is what a facet counts among the items pool holds: the items a whole filter
// matches, or the values of a key.
function facetCount<T>(facet: Facet, pool: readonly T[], o: SuggestOptions<T>): number {
  if (facet.value !== undefined) {
    const query = parseQuery(`${facet.key}:${quoteValue(facet.value)}`, o.spec)

    return pool.filter((item) => o.matches(item, query)).length
  }

  return new Set(pool.flatMap((item) => o.values(item, facet.key).map(fold))).size
}

// suggest offers what fits where the caret is, counted among the items the rest of the
// query matches: a key's values while one is typed, or else the page's facets, those
// that start with the word typed, if any do.
export function suggest<T>(src: string, caret: number, o: SuggestOptions<T>): Suggestions {
  const at = tokenAt(src, caret, o.spec)
  const rest = parseQuery(src.slice(0, at.start) + src.slice(at.end), o.spec)
  const pool = isEmpty(rest) ? o.items : o.items.filter((item) => o.matches(item, rest))

  if (at.key !== '') {
    return valueSuggestions(at, pool, o)
  }

  const word = fold(at.partial)
  const offered = o.facets
    .map((facet) => ({ facet, n: facetCount(facet, pool, o) }))
    .filter(({ facet, n }) => facet.optional !== true || n > 0)
  const written = (f: Facet) => `${f.key}:${f.value === undefined ? '' : quoteValue(f.value)}`
  const starting = offered.filter(({ facet }) => written(facet).startsWith(word) || fold(facet.label).startsWith(word))
  // An empty word starts every facet. A word none starts keeps its place, and the choice
  // follows it as a word of its own.
  const replace = starting.length > 0
  const shown = replace ? starting : offered
  const sign = at.negated ? '-' : ''

  const options = shown.map(({ facet, n }) => ({
    id: written(facet),
    label: facet.label,
    detail: written(facet),
    count: facet.noun === undefined ? formatCount(n) : counted(n, facet.noun[0], facet.noun[1]),
    edit: replace
      ? { from: at.start, to: at.end, text: sign + written(facet) }
      : { from: at.end, to: at.end, text: ` ${written(facet)}` },
  }))

  return { label: 'Filters', options, auto: src.trim() === '', hint: o.hint }
}
