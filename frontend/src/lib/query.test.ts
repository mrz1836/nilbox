import { describe, expect, it } from 'vitest'

import {
  fold,
  foldMapped,
  haystack,
  insertSuggestion,
  isEmpty,
  markRanges,
  markTerms,
  matchesFilter,
  matchesTerms,
  parseQuery,
  quoteValue,
  segments,
  suggest,
  toggleFilter,
  tokenAt,
  weakCache,
  type Facet,
  type Query,
  type QuerySpec,
  type SuggestOptions,
} from './query'

const spec: QuerySpec = { keys: ['repo', 'label', 'is'], multi: ['label', 'is'] }

describe('parseQuery', () => {
  const cases: [name: string, src: string, want: Query][] = [
    ['an empty query', '   ', { terms: [], filters: [] }],
    [
      'words, folded',
      'Fix  Café',
      {
        terms: [
          { text: 'fix', negated: false, number: null, numberOnly: false },
          { text: 'cafe', negated: false, number: null, numberOnly: false },
        ],
        filters: [],
      },
    ],
    [
      'a quoted phrase and an excluded word',
      '"widget cache" -draft',
      {
        terms: [
          { text: 'widget cache', negated: false, number: null, numberOnly: false },
          { text: 'draft', negated: true, number: null, numberOnly: false },
        ],
        filters: [],
      },
    ],
    [
      'an unterminated phrase, to the end',
      '"widget ca',
      { terms: [{ text: 'widget ca', negated: false, number: null, numberOnly: false }], filters: [] },
    ],
    [
      '#123 and a bare number',
      '#123 45',
      {
        terms: [
          { text: '#123', negated: false, number: 123, numberOnly: true },
          { text: '45', negated: false, number: 45, numberOnly: false },
        ],
        filters: [],
      },
    ],
    [
      'a lone dash as text, and empty quotes as nothing',
      '- ""',
      { terms: [{ text: '-', negated: false, number: null, numberOnly: false }], filters: [] },
    ],
    [
      'an unknown key as text',
      'fix: Org:x',
      {
        terms: [
          { text: 'fix:', negated: false, number: null, numberOnly: false },
          { text: 'org:x', negated: false, number: null, numberOnly: false },
        ],
        filters: [],
      },
    ],
    [
      'a known key, in any case, with either value',
      'Repo:Acme/One,acme/two',
      { terms: [], filters: [{ key: 'repo', values: ['acme/one', 'acme/two'], negated: false }] },
    ],
    [
      'a quoted value keeping its commas',
      'label:"type: bug, ui",x',
      { terms: [], filters: [{ key: 'label', values: ['type: bug, ui', 'x'], negated: false }] },
    ],
    ['a key still waiting for its value', 'repo: label:,', { terms: [], filters: [] }],
    [
      'repeats of a single-valued key as either',
      'repo:a repo:b -repo:c -repo:d',
      {
        terms: [],
        filters: [
          { key: 'repo', values: ['a', 'b'], negated: false },
          { key: 'repo', values: ['c'], negated: true },
          { key: 'repo', values: ['d'], negated: true },
        ],
      },
    ],
    [
      'repeats of a multi-valued key as both',
      'label:bug label:ui',
      {
        terms: [],
        filters: [
          { key: 'label', values: ['bug'], negated: false },
          { key: 'label', values: ['ui'], negated: false },
        ],
      },
    ],
  ]

  it.each(cases)('reads %s', (_name, src, want) => {
    expect(parseQuery(src, spec)).toEqual(want)
  })

  it('treats every key as single-valued without a multi list', () => {
    expect(parseQuery('label:a label:b', { keys: ['label'] }).filters).toEqual([
      { key: 'label', values: ['a', 'b'], negated: false },
    ])
  })

  it('reports a query that filters nothing', () => {
    expect(isEmpty(parseQuery('repo:', spec))).toBe(true)
    expect(isEmpty(parseQuery('x', spec))).toBe(false)
    expect(isEmpty(parseQuery('is:pr', spec))).toBe(false)
  })
})

describe('fold', () => {
  it('lowercases, drops accents, and straightens apostrophes', () => {
    expect(fold('ÉCOLE Façade')).toBe('ecole facade')
    expect(fold('Plain ASCII')).toBe('plain ascii')
    expect(fold('Codex’s ‘effort’')).toBe("codex's 'effort'")
  })

  it('maps each folded character back to where it came from', () => {
    // "é" written as e and a combining accent folds to one character.
    expect(foldMapped('Café x')).toEqual({ text: 'cafe x', at: [0, 1, 2, 3, 5, 6, 7] })
    // İ folds to i, and an emoji keeps both of its code units.
    expect(foldMapped('İ😀a')).toEqual({ text: 'i😀a', at: [0, 1, 1, 3, 4] })
    expect(foldMapped('ab')).toEqual({ text: 'ab', at: [0, 1, 2] })
  })

  it('folds the parts of a haystack apart', () => {
    expect(haystack(['Ünïcode', 'Title'])).toBe('unicode\ntitle')
  })
})

describe('weakCache', () => {
  it('builds once for each object', () => {
    let builds = 0
    const lengthOf = weakCache((x: { s: string }) => {
      builds += 1

      return x.s === '' ? undefined : x.s.length
    })
    const a = { s: 'abc' }
    const empty = { s: '' }

    expect(lengthOf(a)).toBe(3)
    expect(lengthOf(a)).toBe(3)
    expect(lengthOf(empty)).toBeUndefined()
    expect(lengthOf(empty)).toBeUndefined()
    expect(lengthOf({ s: 'abc' })).toBe(3)
    expect(builds).toBe(3)
  })
})

describe('matching', () => {
  const terms = (src: string) => parseQuery(src, spec).terms

  it.each([
    ['cache', true],
    ['cache widget', true],
    ['cache -widget', false],
    ['"cache widget"', false],
    ['"the widget"', true],
    ['#7', true],
    ['#70', false],
    ['7', true],
    ['-7', false],
    ['2', true], // part of the text
    ['9', false],
  ])('matches %s against the haystack and number', (src, want) => {
    expect(matchesTerms(terms(src), 'add the widget cache\n2026', 7)).toBe(want)
  })

  it('matches #123 only by the number', () => {
    expect(matchesTerms(terms('#7'), '#7 in the text')).toBe(false)
  })

  it('matches a value as a part, or exactly when some item has it', () => {
    const known = new Set(['bug', 'bugfix'])
    const filter = (src: string) => parseQuery(src, spec).filters[0] ?? { key: '', values: [], negated: false }

    expect(matchesFilter(filter('label:bu'), ['bugfix'], known)).toBe(true)
    expect(matchesFilter(filter('label:bug'), ['bugfix'], known)).toBe(false)
    expect(matchesFilter(filter('label:bug'), ['bug', 'ui'], known)).toBe(true)
    expect(matchesFilter(filter('label:x,bug'), ['bug'], known)).toBe(true)
    expect(matchesFilter(filter('-label:bug'), ['bug'], known)).toBe(false)
    expect(matchesFilter(filter('-label:bug'), ['ui'], known)).toBe(true)
  })

  it('matches no filter on an item that can’t tell, so an excluding one keeps it', () => {
    const known = new Set<string>()

    expect(matchesFilter({ key: 'label', values: ['bug'], negated: false }, null, known)).toBe(false)
    expect(matchesFilter({ key: 'label', values: ['bug'], negated: true }, null, known)).toBe(true)
  })
})

describe('marks', () => {
  it('joins the words and phrases to look for, each once, but not the excluded ones or numbers', () => {
    expect(markTerms(parseQuery('Cache "the widget" -draft #7 cache 12 is:pr', spec))).toBe('cache\nthe widget\n12')
  })

  it('finds marks in text, merging the ones that overlap', () => {
    expect(markRanges('Add the widget cache', '')).toEqual([])
    expect(markRanges('Add the widget cache', 'zzz')).toEqual([])
    expect(markRanges('Add the widget cache', 'the\nwidget\nhe wid')).toEqual([[4, 14]])
    expect(markRanges('a cache, A CACHE', 'cache\na')).toEqual([
      [0, 1],
      [2, 7],
      [9, 10],
      [11, 16],
    ])
  })

  it('lines marks up with text whose folding changes its length', () => {
    const text = 'Café au lait'

    expect(markRanges(text, 'cafe')).toEqual([[0, 5]])
    expect(markRanges(text, 'au')).toEqual([[6, 8]])
  })
})

describe('editing the query', () => {
  it('puts the caret after a key, and past a space after anything else', () => {
    expect(insertSuggestion('rep', { from: 0, to: 3, text: 'repo:' })).toEqual({ value: 'repo:', caret: 5 })
    expect(insertSuggestion('label:ba x', { from: 6, to: 8, text: 'bug' })).toEqual({
      value: 'label:bug x',
      caret: 10,
    })
    expect(insertSuggestion('is:p', { from: 0, to: 4, text: 'is:pr' })).toEqual({ value: 'is:pr ', caret: 6 })
  })

  it('quotes a value only when it has to', () => {
    expect(quoteValue('bug')).toBe('bug')
    expect(quoteValue('good first issue')).toBe('"good first issue"')
    expect(quoteValue('a,b')).toBe('"a,b"')
    expect(quoteValue('say "hi"')).toBe('"say hi"')
  })

  it.each([
    ['', 'label', 'bug', 'label:bug'],
    ['cache ', 'label', 'bug', 'cache label:bug'],
    ['label:bug', 'label', 'bug', ''],
    ['x Label:BUG y', 'label', 'bug', 'x y'],
    ['x label:"good first" y', 'label', 'good first', 'x y'],
    ['x', 'label', 'good first', 'x label:"good first"'],
    ['-label:bug', 'label', 'bug', '-label:bug label:bug'],
    ['label:bug,ui', 'label', 'bug', 'label:bug,ui label:bug'],
    ['repo:bug', 'label', 'bug', 'repo:bug label:bug'],
    ['label: x', 'label', 'x', 'label: x label:x'],
  ])('toggles a filter in %j', (src, key, value, want) => {
    expect(toggleFilter(src, key, value, spec)).toBe(want)
  })

  it('splits a query into the runs the field tints', () => {
    expect(segments('', spec)).toEqual([])
    expect(segments('cache', spec)).toEqual([{ text: 'cache', key: false }])
    expect(segments('-label:bug fix: is:pr', spec)).toEqual([
      { text: '-label:', key: true },
      { text: 'bug fix: ', key: false },
      { text: 'is:', key: true },
      { text: 'pr', key: false },
    ])
  })
})

describe('tokenAt', () => {
  it('finds an empty token between words', () => {
    expect(tokenAt('', 0, spec)).toEqual({ start: 0, end: 0, negated: false, key: '', partial: '', from: 0, to: 0 })
    expect(tokenAt('ab cd', 3, spec)).toMatchObject({ start: 3, end: 3, partial: '' })
  })

  it('finds a word up to the caret', () => {
    expect(tokenAt('ab -cdef', 6, spec)).toEqual({
      start: 3,
      end: 8,
      negated: true,
      key: '',
      partial: 'cd',
      from: 4,
      to: 8,
    })
    expect(tokenAt('"ab cd', 4, spec)).toMatchObject({ partial: 'ab ', from: 0, to: 6 })
  })

  it('finds the value of a key the caret is in', () => {
    expect(tokenAt('x label:', 8, spec)).toEqual({
      start: 2,
      end: 8,
      negated: false,
      key: 'label',
      partial: '',
      from: 8,
      to: 8,
    })
    expect(tokenAt('label:ui,ba,z', 11, spec)).toMatchObject({ key: 'label', partial: 'ba', from: 9, to: 11 })
    expect(tokenAt('label:"a, b",c', 14, spec)).toMatchObject({ partial: 'c', from: 13, to: 14 })
    expect(tokenAt('label:"go', 9, spec)).toMatchObject({ partial: 'go', from: 6, to: 9 })
    // A space ends the key, so what follows is a word of its own.
    expect(tokenAt('label: go', 9, spec)).toMatchObject({ key: '', partial: 'go', from: 7 })
  })
})

describe('suggest', () => {
  interface Thing {
    repo: string
    labels: string[]
    pr: boolean
  }

  const things: Thing[] = [
    { repo: 'acme/one', labels: ['bug', 'ui'], pr: true },
    { repo: 'acme/one', labels: ['bug'], pr: false },
    { repo: 'acme/two', labels: ['good first'], pr: true },
  ]

  const values = (t: Thing, key: string): string[] => (key === 'repo' ? [t.repo] : key === 'label' ? t.labels : [])

  const facets: Facet[] = [
    { label: 'Pull requests', key: 'is', value: 'pr' },
    { label: 'Repository', key: 'repo', noun: ['repo', 'repos'] },
    { label: 'Label', key: 'label' },
    { label: 'Uncommon', key: 'is', value: 'nothing', optional: true },
  ]

  const options: SuggestOptions<Thing> = {
    spec,
    items: things,
    matches: (t, q) =>
      q.filters.every((f) => {
        const have = f.key === 'is' ? (t.pr ? ['pr'] : []) : values(t, f.key)

        return f.values.some((v) => have.some((h) => h.includes(v))) !== f.negated
      }) && q.terms.every((term) => t.repo.includes(term.text) !== term.negated),
    values,
    facets,
    hint: 'Matches repos',
    nouns: { label: 'Labels' },
  }

  it('offers the facets on an empty query, counted, and opens on its own', () => {
    const got = suggest('', 0, options)

    expect(got).toMatchObject({ label: 'Filters', auto: true, hint: 'Matches repos' })
    expect(got.options.map((o) => [o.label, o.detail, o.count])).toEqual([
      ['Pull requests', 'is:pr', '2'],
      ['Repository', 'repo:', '2 repos'],
      ['Label', 'label:', '3'],
    ])
    expect(got.options[0]?.edit).toEqual({ from: 0, to: 0, text: 'is:pr' })
  })

  it('counts the facets among what the rest of the query matches', () => {
    const got = suggest('label:ui ', 9, options)

    expect(got.auto).toBe(false)
    expect(got.options.map((o) => o.count)).toEqual(['1', '1 repo', '2'])
  })

  it('offers the facets a word starts, replacing it, with its dash', () => {
    const got = suggest('one -re', 7, options)

    expect(got.options.map((o) => o.label)).toEqual(['Repository'])
    expect(got.options[0]?.edit).toEqual({ from: 4, to: 7, text: '-repo:' })
  })

  it('offers every facet after a word none starts', () => {
    const got = suggest('zz', 2, options)

    expect(got.options).toHaveLength(3)
    expect(got.options[0]?.edit).toEqual({ from: 2, to: 2, text: ' is:pr' })
  })

  it('offers a key’s values that hold what is typed, most common first', () => {
    const got = suggest('label:', 6, options)

    expect(got).toMatchObject({ label: 'Labels', auto: true, pickFirst: true, hint: '' })
    expect(got.options.map((o) => [o.label, o.count])).toEqual([
      ['bug', '2'],
      ['good first', '1'],
      ['ui', '1'],
    ])
    expect(suggest('label:Go', 8, options).options.map((o) => o.edit)).toEqual([
      { from: 6, to: 8, text: '"good first"' },
    ])
    expect(suggest('repo:', 5, options).options.map((o) => o.label)).toEqual(['acme/one', 'acme/two'])
    expect(suggest('repo:', 5, options).label).toBe('repo')
  })

  it('counts a value once per item, among what the rest matches', () => {
    const twice = { ...options, values: (t: Thing, key: string) => [...values(t, key), ...values(t, key)] }

    expect(suggest('acme/two label:', 15, twice).options.map((o) => [o.label, o.count])).toEqual([['good first', '1']])
  })
})
