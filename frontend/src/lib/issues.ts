// The issue sheet's words and choices, worked out from what the sheet read. They are pure,
// so the sheet, its parts, and their tests share them.

import type {
  InboxItem,
  Issue,
  IssueAction,
  IssueLinked,
  IssueOffer,
  IssueResult,
  IssueStandingState,
  IssueStateReason,
  IssueSubIssue,
  RefState,
} from '../platform'
import { toneText, type Tone } from './tone'
import { unlisted } from './unlisted'

// IssueLook is how an issue's state shows beside its title: the icon's color, and what the
// icon says to a screen reader.
export interface IssueLook {
  color: string
  words: string
}

// closedAsCompleted is how an issue closed as completed shows.
const closedAsCompleted: IssueLook = { color: toneText.info, words: 'Closed as completed' }

// issueLook is an issue's state as its icon shows it: open in the success color, closed as
// completed in the info color, and closed as not planned or as a duplicate muted. Go passes
// GitHub's reason on unchecked, so one GitHub adds later shows as completed.
export function issueLook(state: RefState, reason: IssueStateReason): IssueLook {
  if (state === 'OPEN') {
    return { color: toneText.success, words: 'Open issue' }
  }

  switch (reason) {
    case 'NOT_PLANNED':
      return { color: toneText.muted, words: 'Closed as not planned' }
    case 'DUPLICATE':
      return { color: toneText.muted, words: 'Closed as a duplicate' }
    case '':
    case 'COMPLETED':
    case 'REOPENED':
      return closedAsCompleted
    default:
      return unlisted(reason, closedAsCompleted)
  }
}

// RefMarkKind is the mark a linked pull request or issue shows: an issue open, done, or
// closed without being done (not planned, or a duplicate), and a pull request open (or a
// draft), merged, or closed unmerged.
export type RefMarkKind = 'issue' | 'issue-done' | 'issue-skipped' | 'pr' | 'pr-merged' | 'pr-closed'

// RefLook is how a linked pull request or issue shows its state: its mark, the mark's color,
// and what the mark says to a screen reader.
export interface RefLook extends IssueLook {
  mark: RefMarkKind
}

// issueMark is an issue's mark, as issueLook reads its state: closed as not planned or as a
// duplicate is closed without being done, and any other close is done.
function issueMark(state: RefState, reason: IssueStateReason): RefMarkKind {
  if (state === 'OPEN') {
    return 'issue'
  }

  return reason === 'NOT_PLANNED' || reason === 'DUPLICATE' ? 'issue-skipped' : 'issue-done'
}

// refLook is how any linked pull request or issue shows its state. An issue is colored as
// issueLook colors it. A pull request is colored as a card's links are: open in the success
// color, a draft muted, merged in the info color, and closed unmerged muted.
export function refLook(ref: IssueLinked): RefLook {
  if (ref.kind !== 'pr') {
    return { ...issueLook(ref.state, ref.stateReason), mark: issueMark(ref.state, ref.stateReason) }
  }

  switch (ref.state) {
    case 'MERGED':
      return { mark: 'pr-merged', color: toneText.info, words: 'Merged pull request' }
    case 'CLOSED':
      return { mark: 'pr-closed', color: toneText.muted, words: 'Closed pull request' }
    default:
      return ref.draft
        ? { mark: 'pr', color: toneText.muted, words: 'Draft pull request' }
        : { mark: 'pr', color: toneText.success, words: 'Open pull request' }
  }
}

// SubIssueGroups are an issue's sub-issues as its Sub-issues tab lists them: those still
// open, then those done (closed as completed, as not planned, or as a duplicate), each in
// GitHub's order.
export interface SubIssueGroups {
  open: IssueSubIssue[]
  done: IssueSubIssue[]
}

// subIssueGroups sorts an issue's sub-issues into open and done.
export function subIssueGroups(subIssues: readonly IssueSubIssue[]): SubIssueGroups {
  return {
    open: subIssues.filter((s) => s.issue.state === 'OPEN'),
    done: subIssues.filter((s) => s.issue.state !== 'OPEN'),
  }
}

// doneShownUpTo is how many done sub-issues the Sub-issues tab lists open; past it, they
// fold away under their count until the user opens them.
export const doneShownUpTo = 5

// closedWords says how a done issue was closed when that was not as completed: "not
// planned", or "duplicate"; '' for one completed.
export function closedWords(reason: IssueStateReason): string {
  switch (reason) {
    case 'NOT_PLANNED':
      return 'not planned'
    case 'DUPLICATE':
      return 'duplicate'
    default:
      return ''
  }
}

// standingTones is each standing's color: a fix is good news, a lock a caution, an issue
// GitHub can't find any more an alarm, and the rest plain.
const standingTones: Readonly<Record<IssueStandingState, Tone>> = {
  gone: 'danger',
  closed: 'muted',
  locked: 'warning',
  fix_merged: 'success',
  fix_open: 'success',
  replied_last: 'muted',
  assigned: 'muted',
  open: 'muted',
}

// standingTone is the color of the sentence that says where an issue stands.
export function standingTone(state: IssueStandingState): Tone {
  return standingTones[state]
}

// selectedIssueAction is the action the sheet's button runs: the one the user chose, while
// the sheet still offers it enabled, else the sheet's primary. A close can be the button
// once chosen; it always asks first.
export function selectedIssueAction(issue: Issue, chosen: IssueAction): IssueAction {
  const offer = issue.offers.find((o) => o.action === chosen)

  return chosen !== '' && offer?.enabled === true ? chosen : issue.primary
}

// writeFirst holds a comment with nothing to say back.
export const writeFirst = 'Write something first.'

// hasText reports text that says something, as the backend reads it: anything left once
// the white space trim drops is gone.
function hasText(body: string): boolean {
  return body.trim() !== ''
}

// issueHeldBack says why the window holds an offer's action back, or '': a comment with
// nothing to say, or a close whose comment GitHub can't take now, until the text is
// cleared.
export function issueHeldBack(offer: IssueOffer | undefined, body: string): string {
  if (offer === undefined) {
    return ''
  }

  if (offer.action === 'comment' && !hasText(body)) {
    return writeFirst
  }

  return offer.commentsFirst && hasText(body) ? offer.commentBlocked : ''
}

// closeNote says what a close posts as a comment first, as its offer marks it, or '' when it
// posts nothing: clearing the text skips the comment. While GitHub can't take the comment,
// it says why instead, which holds the button back (issueHeldBack).
export function closeNote(offer: IssueOffer | undefined, body: string): string {
  if (offer?.commentsFirst !== true || !hasText(body)) {
    return ''
  }

  return offer.commentBlocked !== '' ? offer.commentBlocked : 'Your text is posted as a comment first.'
}

// issueTone is how an action's outcome reads in the inbox's toast: a close that didn't
// happen, or happened without the user, is a warning.
export function issueTone(result: IssueResult): 'success' | 'warning' {
  switch (result.outcome) {
    case 'commented_not_closed':
    case 'new_activity':
    case 'already_closed':
      return 'warning'
    case 'commented':
    case 'closed':
      return 'success'
  }
}

// keepsDraft reports an outcome that leaves the user's text in the sheet, body: a close
// stopped, before anything was sent, by a comment they haven't read, and a close someone made
// first, before the text went, which would otherwise be lost.
export function keepsDraft(result: IssueResult, body: string): boolean {
  return (
    result.outcome === 'new_activity' || (result.outcome === 'already_closed' && !result.commented && hasText(body))
  )
}

// ms reads an RFC 3339 time, or NaN for '' or one that doesn't read.
function ms(iso: string): number {
  return iso === '' ? Number.NaN : Date.parse(iso)
}

// seconds reads an RFC 3339 time to the whole second, as GitHub and the read keep times.
function seconds(iso: string): number {
  return Math.floor(ms(iso) / 1000)
}

// changedSince reports an issue that changed since the sheet read it, as its live inbox card
// says, at no cost: newer activity, to the second, or more comments than the read holds.
export function changedSince(issue: Issue, card: InboxItem | undefined): boolean {
  if (card === undefined || issue.fetchedAt === '') {
    return false
  }

  return seconds(card.updatedAt) > seconds(issue.updatedAt) || (card.details?.comments ?? 0) > issue.seen.comments
}

// withReply is the comment once a reply the user picked fills it in: the reply alone while
// the comment is empty, or still just the text the last pick filled in, else the user's
// own text with the reply after it, as a quote is added, so a pick never loses what they
// wrote.
export function withReply(draft: { body: string; filled: string }, text: string): string {
  if (draft.body.trim() === '' || draft.body === draft.filled) {
    return text
  }

  return `${draft.body.replace(/\s+$/, '')}\n\n${text}`
}
