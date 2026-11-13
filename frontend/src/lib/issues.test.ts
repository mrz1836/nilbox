import { describe, expect, it } from 'vitest'

import {
  closeIssueOffer,
  details,
  issue,
  issueComment,
  issueEvent,
  issueOffer,
  issueResult,
  item,
} from '../test/fixtures'
import {
  changedSince,
  closeNote,
  earlierCount,
  issueHeldBack,
  issueLook,
  issueTone,
  keepsDraft,
  newSince,
  quoteInto,
  rowKey,
  selectedIssueAction,
  standingTone,
  timelineRows,
  withReply,
  writeFirst,
} from './issues'

describe('issueLook', () => {
  it('colors an issue by its state', () => {
    expect(issueLook('OPEN', '')).toEqual({ color: 'text-success', words: 'Open issue' })
    expect(issueLook('OPEN', 'REOPENED').color).toBe('text-success')
    expect(issueLook('CLOSED', 'COMPLETED')).toEqual({ color: 'text-info', words: 'Closed as completed' })
    expect(issueLook('CLOSED', '')).toEqual({ color: 'text-info', words: 'Closed as completed' })
    expect(issueLook('CLOSED', 'NOT_PLANNED')).toEqual({ color: 'text-muted', words: 'Closed as not planned' })
    expect(issueLook('CLOSED', 'DUPLICATE')).toEqual({ color: 'text-muted', words: 'Closed as a duplicate' })
  })
})

describe('standingTone', () => {
  it('colors where an issue stands', () => {
    expect(standingTone('gone')).toBe('danger')
    expect(standingTone('locked')).toBe('warning')
    expect(standingTone('fix_merged')).toBe('success')
    expect(standingTone('fix_open')).toBe('success')
    expect(standingTone('assigned')).toBe('muted')
  })
})

describe('selectedIssueAction', () => {
  it('runs the chosen action while it is offered enabled, else the primary', () => {
    const is = issue()

    expect(selectedIssueAction(is, '')).toBe('comment')
    expect(selectedIssueAction(is, 'close_not_planned')).toBe('close_not_planned')
    expect(
      selectedIssueAction(
        issue({ offers: [issueOffer(), closeIssueOffer({ enabled: false, reason: 'Wait.' })] }),
        'close_completed',
      ),
    ).toBe('comment')
    expect(selectedIssueAction(issue({ offers: [], primary: '' }), 'close_completed')).toBe('')
  })
})

describe('issueHeldBack and closeNote', () => {
  it('holds a comment with nothing to say back', () => {
    expect(issueHeldBack(issueOffer(), '  ')).toBe(writeFirst)
    expect(issueHeldBack(issueOffer(), 'Done.')).toBe('')
    expect(issueHeldBack(undefined, '')).toBe('')
  })

  it('says what a close posts first, and holds one back GitHub would refuse the text of', () => {
    expect(closeNote(closeIssueOffer(), 'Done.')).toBe('Your text is posted as a comment first.')
    expect(closeNote(closeIssueOffer(), ' ')).toBe('')
    expect(closeNote(issueOffer(), 'Done.')).toBe('')
    expect(closeNote(undefined, 'Done.')).toBe('')
    expect(issueHeldBack(closeIssueOffer(), 'Done.')).toBe('')
    expect(issueHeldBack(closeIssueOffer(), '')).toBe('')

    const blocked = closeIssueOffer({ commentBlocked: 'The conversation is locked.' })
    expect(closeNote(blocked, 'Done.')).toBe('The conversation is locked.')
    expect(issueHeldBack(blocked, 'Done.')).toBe('The conversation is locked.')
    expect(issueHeldBack(blocked, ''), 'a close with no text goes').toBe('')
  })
})

describe('issueTone and keepsDraft', () => {
  it('warns of a close that did not happen as asked', () => {
    expect(issueTone(issueResult())).toBe('success')
    expect(issueTone(issueResult({ outcome: 'closed' }))).toBe('success')
    expect(issueTone(issueResult({ outcome: 'commented_not_closed' }))).toBe('warning')
    expect(issueTone(issueResult({ outcome: 'new_activity' }))).toBe('warning')
    expect(issueTone(issueResult({ outcome: 'already_closed' }))).toBe('warning')
    expect(keepsDraft(issueResult({ outcome: 'new_activity' }), '')).toBe(true)
    expect(keepsDraft(issueResult(), 'Done.')).toBe(false)
    expect(keepsDraft(issueResult({ outcome: 'already_closed', commented: false }), 'Done.')).toBe(true)
    expect(keepsDraft(issueResult({ outcome: 'already_closed', commented: false }), ' ')).toBe(false)
    expect(keepsDraft(issueResult({ outcome: 'already_closed', commented: true }), 'Done.')).toBe(false)
  })
})

describe('changedSince', () => {
  it('compares the live card with the read', () => {
    const is = issue()

    expect(changedSince(is, item({ updatedAt: is.updatedAt }))).toBe(false)
    expect(changedSince(is, item({ updatedAt: '2026-06-15T11:30:00Z' }))).toBe(true)
    expect(changedSince(is, item({ updatedAt: '2026-06-15T11:00:00.500Z' })), 'GitHub’s whole seconds').toBe(false)
    expect(changedSince(is, item({ updatedAt: is.updatedAt, details: details({ comments: 3 }) }))).toBe(true)
    expect(changedSince(is, item({ updatedAt: is.updatedAt, details: details({ comments: 2 }) }))).toBe(false)
    expect(changedSince(is, undefined)).toBe(false)
    expect(changedSince(issue({ fetchedAt: '' }), item({ updatedAt: '2026-06-15T11:30:00Z' }))).toBe(false)
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

describe('quoteInto', () => {
  it('quotes each line, after what the draft holds', () => {
    expect(quoteInto('', 'Can repro.\n\nOn 1.4.')).toBe('> Can repro.\n>\n> On 1.4.\n\n')
    expect(quoteInto('Thanks!  \n', ' Can repro. ')).toBe('Thanks!\n\n> Can repro.\n\n')
  })
})

describe('withReply', () => {
  it('fills an empty comment, takes the place of the last reply, and goes after what the user wrote', () => {
    expect(withReply({ body: '  ', filled: '' }, 'Thanks!')).toBe('Thanks!')
    expect(withReply({ body: 'Thanks!', filled: 'Thanks!' }, 'Looking into it.')).toBe('Looking into it.')
    expect(withReply({ body: 'Fixed in #14.\n', filled: 'Thanks!' }, 'Thanks!')).toBe('Fixed in #14.\n\nThanks!')
  })
})

describe('earlierCount', () => {
  it('counts what comes before the page', () => {
    expect(earlierCount(40, 3, 'cursor')).toBe(37)
    expect(earlierCount(40, 40, 'cursor'), 'a page whose total is out of date still offers more').toBe(1)
    expect(earlierCount(40, 3, '')).toBe(0)
  })
})
