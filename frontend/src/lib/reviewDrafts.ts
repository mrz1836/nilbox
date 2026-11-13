// The review sheet's drafts: what the user has written for each pull request, kept for
// the session, so closing the sheet, or stepping to another pull request and back, loses
// nothing. A draft is cleared once its action succeeds.

import type { ReviewAction } from '../platform'
import type { DiffSide } from './diff'
import { useDrafts, type Drafts } from './drafts'

// PendingComment is a line comment waiting to go with the review.
export interface PendingComment {
  // id tells pending comments apart while they are edited.
  id: number
  path: string
  side: DiffSide
  // line is the comment's last line, and startLine a range's first, or 0 for one line.
  line: number
  startLine: number
  body: string
}

// ReviewDraft is one pull request's draft.
export interface ReviewDraft {
  // body is the review's text, and edited whether the user wrote any of it, so a default
  // reply never replaces what they wrote.
  body: string
  edited: boolean
  // replyId is the reply the body was filled from, or 0.
  replyId: number
  // action is the action the user chose in the action button, or '' for the sheet's
  // primary.
  action: ReviewAction
  comments: PendingComment[]
  // viewed are the paths the user marked as viewed.
  viewed: string[]
}

// emptyDraft is a pull request's draft before the user does anything.
export const emptyDraft: ReviewDraft = Object.freeze({
  body: '',
  edited: false,
  replyId: 0,
  action: '',
  comments: [],
  viewed: [],
})

// ReviewDrafts is every pull request's draft, by inbox item ID.
export type ReviewDrafts = Drafts<ReviewDraft>

// useReviewDrafts keeps the drafts for as long as the component that holds them lives:
// the window, for the session.
export function useReviewDrafts(): ReviewDrafts {
  return useDrafts(emptyDraft)
}

// nextCommentId is a new pending comment's ID: one past the draft's largest.
export function nextCommentId(draft: ReviewDraft): number {
  return draft.comments.reduce((max, c) => Math.max(max, c.id), 0) + 1
}
