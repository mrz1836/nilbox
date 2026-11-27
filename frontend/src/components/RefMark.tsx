import { refLook, type RefMarkKind } from '../lib/issues'
import type { IssueLinked } from '../platform'
import {
  IssueClosedIcon,
  IssueIcon,
  IssueNotPlannedIcon,
  MergeIcon,
  PullRequestClosedIcon,
  PullRequestIcon,
} from './icons'

// markIcons is the icon of each mark.
const markIcons: Readonly<Record<RefMarkKind, typeof IssueIcon>> = {
  issue: IssueIcon,
  'issue-done': IssueClosedIcon,
  'issue-skipped': IssueNotPlannedIcon,
  pr: PullRequestIcon,
  'pr-merged': MergeIcon,
  'pr-closed': PullRequestClosedIcon,
}

// RefMark is a linked pull request's or issue's kind and state as a small mark in its
// color (refLook), which says them to a screen reader, as in "Closed as not planned".
export function RefMark({ linked }: { linked: IssueLinked }) {
  const look = refLook(linked)
  const Icon = markIcons[look.mark]

  return (
    <span role="img" aria-label={look.words} title={look.words} className="inline-flex shrink-0">
      <Icon className={`size-3.5 ${look.color}`} />
    </span>
  )
}
