import { describe, expect, it } from 'vitest'

import { issueComment, issueEvent, lineComment, reviewEntry } from '../test/fixtures'
import { commentCount, earlierCount, entryAnchor, newSince, quoteInto, rowKey, timelineRows } from './timeline'

describe('earlierCount', () => {
  it('counts what comes before the page', () => {
    expect(earlierCount(40, 3, 'cursor')).toBe(37)
    expect(earlierCount(40, 40, 'cursor'), 'a page whose total is out of date still offers more').toBe(1)
    expect(earlierCount(40, 3, '')).toBe(0)
  })
})

describe('quoteInto', () => {
  it('quotes each line, after what the draft holds', () => {
    expect(quoteInto('', 'Can repro.\n\nOn 1.4.')).toBe('> Can repro.\n>\n> On 1.4.\n\n')
    expect(quoteInto('Thanks!  \n', ' Can repro. ')).toBe('Thanks!\n\n> Can repro.\n\n')
  })
})

describe('timelineRows', () => {
  it('groups the labels one person changed in one minute', () => {
    const bug = { name: 'bug', color: '#d73a4a' }
    const triage = { name: 'triage', color: '' }
    const rows = timelineRows([
      issueEvent({ id: 'l1', at: '2026-06-13T12:00:05Z' }),
      issueEvent({ id: 'l2', at: '2026-06-13T12:00:40Z', kind: 'unlabeled', label: triage }),
      issueEvent({ id: 'l3', at: '2026-06-13T12:01:00Z', label: { name: 'p1', color: '' } }),
      issueEvent({ id: 'l4', at: '2026-06-13T12:01:00Z', actor: 'octo-third', label: bug }),
      issueEvent({ id: 'l5', at: '2026-06-13T12:01:00Z', kind: 'unlabeled', label: null }),
      issueComment({ id: 'c1' }),
    ])

    expect(rows).toEqual([
      {
        kind: 'labels',
        id: 'l1',
        actor: 'octo-other',
        actorIsBot: false,
        mine: false,
        at: '2026-06-13T12:00:05Z',
        added: [bug],
        removed: [triage],
      },
      {
        kind: 'labels',
        id: 'l3',
        actor: 'octo-other',
        actorIsBot: false,
        mine: false,
        at: '2026-06-13T12:01:00Z',
        added: [{ name: 'p1', color: '' }],
        removed: [],
      },
      {
        kind: 'labels',
        id: 'l4',
        actor: 'octo-third',
        actorIsBot: false,
        mine: false,
        at: '2026-06-13T12:01:00Z',
        added: [bug],
        removed: [],
      },
      { kind: 'item', item: issueEvent({ id: 'l5', at: '2026-06-13T12:01:00Z', kind: 'unlabeled', label: null }) },
      { kind: 'item', item: issueComment({ id: 'c1' }) },
    ])
  })
})

describe('newSince', () => {
  const rows = timelineRows([
    issueComment({ id: 'a', at: '2026-06-15T09:00:00Z' }),
    issueComment({ id: 'b', at: '2026-06-15T10:00:00Z', mine: true }),
    issueEvent({ id: 'l', at: '2026-06-15T10:30:00Z', mine: true }),
    issueComment({ id: 'c', at: '2026-06-15T11:00:00Z' }),
  ])

  it('finds the first row newer than the last visit that is not the user’s', () => {
    expect(newSince(rows, '2026-06-15T09:30:00Z')).toBe(3)
    expect(newSince(rows, '2026-06-15T08:00:00Z')).toBe(0)
    expect(newSince(rows, '2026-06-15T11:00:00Z')).toBe(-1)
    expect(newSince(rows, '')).toBe(-1)
    expect(newSince(timelineRows([issueEvent({ at: '2026-06-15T10:30:00Z' })]), '2026-06-15T09:30:00Z')).toBe(0)
  })

  it('keys each row', () => {
    expect(rows.map(rowKey)).toEqual(['a', 'b', 'l', 'c'])
  })
})

describe('rowKey and entryAnchor', () => {
  it('names each row by its first entry, and its anchor by the key', () => {
    const rows = timelineRows([issueEvent({ id: 'l1' }), issueComment({ id: 'IC_kwDO/1+2' })])

    expect(rows.map(rowKey)).toEqual(['l1', 'IC_kwDO/1+2'])
    expect(entryAnchor('IC_kwDO/1+2')).toBe('entry-IC_kwDO_1_2')
  })
})

describe('commentCount', () => {
  it('counts the comments, the reviews that say something, and their line comments', () => {
    const entries = [
      issueComment({ id: 'c1' }),
      issueComment({ id: 'c2', minimized: true }),
      reviewEntry({ lineComments: [lineComment(), lineComment({ id: 'PRRC_2' })], moreLineComments: 3 }),
      reviewEntry({ id: 'PRR_2', body: '', reviewState: 'APPROVED', lineComments: [] }),
      reviewEntry({ id: 'PRR_3', body: '  ', lineComments: [lineComment({ id: 'PRRC_3' })] }),
      issueEvent(),
    ]

    expect(commentCount(entries, '')).toBe('9')
    expect(commentCount(entries, 'cursor'), 'more may come before the entries held').toBe('9+')
    expect(commentCount([], '')).toBe('0')
  })
})
