import type { ReactNode } from 'react'

import { refText } from '../../lib/format'
import { dayPassed, formatDay } from '../../lib/time'
import { useNowAs } from '../../lib/useNow'
import { openExternal, type Issue, type IssueFix, type IssueLinked, type IssuePerson } from '../../platform'
import { AssigneesSection, type AssigneesEditing } from '../Assignees'
import { PersonAvatar } from '../Avatar'
import { captionClass, inkLinkClass, quietButtonClass } from '../controls'
import { Fold } from '../Fold'
import { ItemLink } from '../ItemLink'
import { LabelChip, ProgressBar, ProjectLink } from '../ItemSignals'
import { RefMark } from '../RefMark'
import { LabelPicker, type OnEdit } from './IssuePickers'

export interface IssueSummaryProps {
  issue: Issue
  // onEdit changes the issue's labels, and assignees its assignees, which the details offer
  // to while the user may (Issue.canEdit); without them, they don't.
  onEdit?: OnEdit | undefined
  assignees?: AssigneesEditing | undefined
  // onShowSubIssues shows the sheet's Sub-issues tab; without it, the details don't offer to.
  onShowSubIssues?: (() => void) | undefined
}

// mentionsShownUpTo is how many mentions the details list open; past it, they fold away
// under their count until the user opens them.
const mentionsShownUpTo = 3

// PartProps are a titled part of the details: its title, how many it holds beside it (none
// for 0 or none given), and a control beside them, if any.
interface PartProps {
  title: string
  count?: number
  action?: ReactNode
  children: ReactNode
}

// Part is one titled part of the details.
function Part({ title, count = 0, action, children }: PartProps) {
  return (
    <section aria-label={title} className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <h3 className={`${captionClass} flex items-center gap-1.5`}>
          {title}
          {count > 0 && <span className="font-medium tabular-nums">{String(count)}</span>}
        </h3>
        {action}
      </div>
      {children}
    </section>
  )
}

// More says how many more of a list GitHub has than the sheet shows, or nothing.
function More({ count, url }: { count: number; url: string }) {
  if (count <= 0) {
    return null
  }

  return (
    <button
      type="button"
      onClick={() => {
        openExternal(url)
      }}
      className="text-xs text-muted hover:text-accent hover:underline"
    >
      and {String(count)} more on GitHub
    </button>
  )
}

// People lists assignees or participants, the user marked, or says there are none.
function People({
  people,
  more,
  url,
  none,
}: {
  people: readonly IssuePerson[]
  more: number
  url: string
  none: string
}) {
  if (people.length === 0) {
    return <p className="text-sm text-muted">{none}</p>
  }

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
      {people.map((p) => (
        <span key={p.login} className="inline-flex items-center gap-1.5 text-text">
          <PersonAvatar login={p.login} />
          <span>
            {p.login}
            {p.mine && <span className="text-muted"> (you)</span>}
          </span>
        </span>
      ))}
      <More count={more} url={url} />
    </div>
  )
}

// fixState says where a pull request that closes the issue stands, in words.
function fixState(fix: IssueFix): string {
  const parts: string[] = []

  switch (fix.pr.state) {
    case 'MERGED':
      parts.push(`merged into ${fix.base}`)
      break
    case 'CLOSED':
      parts.push('closed')
      break
    default:
      parts.push(fix.pr.draft ? 'draft' : 'open')
  }

  if (fix.pr.state === 'OPEN') {
    if (fix.checks !== '') {
      parts.push(`checks ${fix.checks}`)
    }

    if (fix.review !== '') {
      parts.push(fix.review.replaceAll('_', ' '))
    }
  }

  return parts.join(' · ')
}

// Linked is a linked item on its line: its state's mark, then its name and title.
function Linked({ linked, repo }: { linked: IssueLinked; repo: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <RefMark linked={linked} />
      <ItemLink linked={linked} from={repo} titled />
    </span>
  )
}

// Links lists linked items, each on its line, with its state's mark and its title.
function Links({ links, repo, label }: { links: readonly IssueLinked[]; repo: string; label?: string }) {
  return (
    <ul aria-label={label} className="space-y-1 text-sm">
      {links.map((l) => (
        <li key={refText(l)} className="min-w-0">
          <Linked linked={l} repo={repo} />
        </li>
      ))}
    </ul>
  )
}

// Development is the pull requests that close the issue once merged, where each stands,
// and the pull requests and issues that mentioned it, folded away under their count past a
// few (mentionsShownUpTo).
function Development({ issue }: { issue: Issue }) {
  if (issue.fixes.length === 0 && issue.mentioned.length === 0) {
    return <p className="text-sm text-muted">No pull request is linked to it.</p>
  }

  return (
    <>
      {issue.fixes.length > 0 && (
        <ul className="space-y-1.5 text-sm">
          {issue.fixes.map((fix) => (
            <li key={refText(fix.pr)} className="min-w-0">
              <Linked linked={fix.pr} repo={issue.repo} />
              <p className="pl-5 text-xs text-muted">{fixState(fix)}</p>
            </li>
          ))}
        </ul>
      )}
      <More count={issue.moreFixes} url={issue.url} />
      {issue.mentioned.length > 0 && (
        <Fold title="Mentioned in" count={issue.mentioned.length} auto={issue.mentioned.length <= mentionsShownUpTo}>
          <Links links={issue.mentioned} repo={issue.repo} label="Mentioned in" />
        </Fold>
      )}
    </>
  )
}

// SubIssuesLine is how far along the issue's sub-issues are, in one line, under a title
// whose Show button shows them in the Sub-issues tab.
function SubIssuesLine({ issue, onShow }: { issue: Issue; onShow: (() => void) | undefined }) {
  return (
    <Part
      title="Sub-issues"
      action={
        onShow !== undefined && (
          <button type="button" onClick={onShow} className={quietButtonClass}>
            Show
          </button>
        )
      }
    >
      <div className="flex items-center gap-2 text-xs text-muted">
        <ProgressBar done={issue.subIssuesDone} total={issue.subIssuesTotal} className="flex-1" />
        <span className="shrink-0">
          {String(issue.subIssuesDone)} of {String(issue.subIssuesTotal)} done
        </span>
      </div>
    </Part>
  )
}

// Milestone is the issue's milestone: its name, when it is due, in the color of a warning
// once that day has passed while it is open, and how far along it is.
function Milestone({ issue }: { issue: Issue }) {
  const m = issue.milestone
  const due = m?.dueOn ?? ''
  const overdue = useNowAs((now) => dayPassed(now, due))

  if (m === null) {
    return null
  }

  return (
    <Part title="Milestone">
      <button
        type="button"
        onClick={() => {
          openExternal(m.url)
        }}
        className={`${inkLinkClass} text-left text-sm`}
      >
        {m.title}
      </button>
      <div className="flex items-center gap-2 text-xs text-muted">
        {due !== '' && (
          <span className={overdue && m.state === 'OPEN' ? 'text-warning' : ''}>
            {overdue && m.state === 'OPEN' ? 'Overdue since' : 'Due'} {formatDay(due)}
          </span>
        )}
        <ProgressBar done={m.progress} total={100} />
        <span>{String(Math.round(m.progress))}% done</span>
      </div>
    </Part>
  )
}

// IssueSummary is the details beside the conversation: where the issue sits among others
// first, its sub-issues' progress and the issue it is part of, then the pull requests that
// fix it and what mentioned it, who it is assigned to (AssigneesSection, as the review sheet
// shows a pull request's), its labels and projects, its milestone, the issue it duplicates,
// and who took part.
export function IssueSummary({ issue, onEdit, assignees, onShowSubIssues }: IssueSummaryProps) {
  const editing = issue.canEdit && onEdit !== undefined

  return (
    <div className="space-y-4">
      {issue.subIssuesTotal > 0 && <SubIssuesLine issue={issue} onShow={onShowSubIssues} />}
      {issue.parent !== null && (
        <Part title="Part of">
          <Links links={[issue.parent]} repo={issue.repo} />
        </Part>
      )}
      <Part title="Development" count={issue.fixes.length + issue.moreFixes}>
        <Development issue={issue} />
      </Part>
      <AssigneesSection
        look="details"
        itemId={issue.id}
        repo={issue.repo}
        noun="issue"
        url={issue.url}
        assignees={issue.assignees}
        more={issue.moreAssignees}
        editing={issue.canEdit ? assignees : undefined}
      />
      <Part title="Labels" action={editing && <LabelPicker issue={issue} onEdit={onEdit} />}>
        {issue.labels.length === 0 ? (
          <p className="text-sm text-muted">None</p>
        ) : (
          <div className="flex flex-wrap gap-1">
            {issue.labels.map((l) => (
              <LabelChip key={l.name} label={l} />
            ))}
          </div>
        )}
      </Part>
      {issue.projects.length > 0 && (
        <Part title="Projects">
          <ul className="space-y-1">
            {issue.projects.map((p) => (
              <li key={p.url}>
                <ProjectLink project={p} />
              </li>
            ))}
          </ul>
        </Part>
      )}
      <Milestone issue={issue} />
      {issue.duplicateOf !== null && (
        <Part title="Duplicate of">
          <Links links={[issue.duplicateOf]} repo={issue.repo} />
        </Part>
      )}
      <Part title="Participants" count={issue.participants.length + issue.moreParticipants}>
        <People people={issue.participants} more={issue.moreParticipants} url={issue.url} none="No one yet" />
      </Part>
    </div>
  )
}
