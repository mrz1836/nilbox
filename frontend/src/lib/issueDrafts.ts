// The issue sheet's drafts: what the user has written for each issue, and the action they
// chose, kept for the session, so closing the sheet, or stepping to another card and back,
// loses nothing. A draft is cleared once its action succeeds.

import type { IssueAction } from '../platform'
import { useDrafts, type Drafts } from './drafts'

// IssueDraft is one issue's draft.
export interface IssueDraft {
  // body is the comment's text.
  body: string
  // action is the action the user chose in the action button, or '' for the sheet's
  // primary.
  action: IssueAction
}

// emptyIssueDraft is an issue's draft before the user does anything.
export const emptyIssueDraft: IssueDraft = Object.freeze({ body: '', action: '' })

// IssueDrafts is every issue's draft, by inbox item ID.
export type IssueDrafts = Drafts<IssueDraft>

// useIssueDrafts keeps the drafts for as long as the component that holds them lives: the
// window, for the session.
export function useIssueDrafts(): IssueDrafts {
  return useDrafts(emptyIssueDraft)
}
