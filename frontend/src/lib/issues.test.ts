import { describe, expect, it } from 'vitest'

import type { RefState } from '../platform'
import {
  closeIssueOffer,
  details,
  issue,
  issueOffer,
  issueRef,
  issueResult,
  item,
  pullRef,
  subIssue,
} from '../test/fixtures'
import {
  changedSince,
  closedWords,
  closeNote,
  issueHeldBack,
  issueLook,
  issueTone,
  keepsDraft,
  refLook,
  selectedIssueAction,
  standingTone,
  subIssueGroups,
  withReply,
  writeFirst,
  type IssueLook,
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

  it('shows a reason GitHub adds later, which Go passes on unchecked, as completed', () => {
    // The window takes the reason as Go's JSON decodes, whatever it holds. A method's
    // parameters are compared both ways, so this one takes the decoded value with no cast.
    const fromGo: { look(state: RefState, reason: unknown): IssueLook } = { look: issueLook }

    expect(fromGo.look('CLOSED', JSON.parse('"WONT_FIX"'))).toEqual({
      color: 'text-info',
      words: 'Closed as completed',
    })
  })
})

describe('refLook', () => {
  it('marks an issue by its state, colored as issueLook colors it', () => {
    expect(refLook(issueRef())).toEqual({ mark: 'issue', color: 'text-success', words: 'Open issue' })
    expect(refLook(issueRef({ state: 'OPEN', stateReason: 'REOPENED' })).mark).toBe('issue')
    expect(refLook(issueRef({ state: 'CLOSED', stateReason: 'COMPLETED' }))).toEqual({
      mark: 'issue-done',
      color: 'text-info',
      words: 'Closed as completed',
    })
    expect(refLook(issueRef({ state: 'CLOSED', stateReason: '' })).mark).toBe('issue-done')
    expect(refLook(issueRef({ state: 'CLOSED', stateReason: 'NOT_PLANNED' }))).toEqual({
      mark: 'issue-skipped',
      color: 'text-muted',
      words: 'Closed as not planned',
    })
    expect(refLook(issueRef({ state: 'CLOSED', stateReason: 'DUPLICATE' }))).toEqual({
      mark: 'issue-skipped',
      color: 'text-muted',
      words: 'Closed as a duplicate',
    })
  })

  it('marks a pull request by its state, as a card’s links do', () => {
    expect(refLook(pullRef())).toEqual({ mark: 'pr', color: 'text-success', words: 'Open pull request' })
    expect(refLook(pullRef({ draft: true }))).toEqual({ mark: 'pr', color: 'text-muted', words: 'Draft pull request' })
    expect(refLook(pullRef({ state: 'MERGED' }))).toEqual({
      mark: 'pr-merged',
      color: 'text-info',
      words: 'Merged pull request',
    })
    expect(refLook(pullRef({ state: 'CLOSED' }))).toEqual({
      mark: 'pr-closed',
      color: 'text-muted',
      words: 'Closed pull request',
    })
  })
})

describe('subIssueGroups', () => {
  it('sorts the sub-issues into open and done, each in GitHub’s order', () => {
    const open20 = subIssue()
    const done21 = subIssue({ issue: issueRef({ number: 21, state: 'CLOSED', stateReason: 'NOT_PLANNED' }) })
    const open22 = subIssue({ issue: issueRef({ number: 22 }) })
    const done23 = subIssue({ issue: issueRef({ number: 23, state: 'CLOSED', stateReason: 'COMPLETED' }) })

    expect(subIssueGroups([open20, done21, open22, done23])).toEqual({ open: [open20, open22], done: [done21, done23] })
    expect(subIssueGroups([])).toEqual({ open: [], done: [] })
  })
})

describe('closedWords', () => {
  it('says how an issue was closed, when not as completed', () => {
    expect(closedWords('NOT_PLANNED')).toBe('not planned')
    expect(closedWords('DUPLICATE')).toBe('duplicate')
    expect(closedWords('COMPLETED')).toBe('')
    expect(closedWords('')).toBe('')
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

describe('withReply', () => {
  it('fills an empty comment, takes the place of the last reply, and goes after what the user wrote', () => {
    expect(withReply({ body: '  ', filled: '' }, 'Thanks!')).toBe('Thanks!')
    expect(withReply({ body: 'Thanks!', filled: 'Thanks!' }, 'Looking into it.')).toBe('Looking into it.')
    expect(withReply({ body: 'Fixed in #14.\n', filled: 'Thanks!' }, 'Thanks!')).toBe('Fixed in #14.\n\nThanks!')
  })
})
