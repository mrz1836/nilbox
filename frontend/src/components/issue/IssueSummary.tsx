import type { ReactNode } from 'react'

import { counted, refText } from '../../lib/format'
import { dayPassed, formatDay } from '../../lib/time'
import { useNowAs } from '../../lib/useNow'
import { openExternal, type Issue, type IssueFix, type IssueLinked, type IssuePerson } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { captionClass } from '../controls'
import { ItemLink } from '../ItemLink'
import { LabelChip, ProgressBar, ProjectLink } from '../ItemSignals'
import { AssigneePicker, LabelPicker, type OnEdit } from './IssuePickers'

export interface IssueSummaryProps {
  issue: Issue
  // onEdit changes the issue's labels and assignees, which the details offer to while the
  // user may (Issue.canEdit); without it, they don't.
  onEdit?: OnEdit | undefined
}

// Part is one titled part of the details, with a control beside its title, if any.
function Part({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={title} className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <h3 className={captionClass}>{title}</h3>
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

// Links lists linked items, each on its line, with their titles.
function Links({ links, repo }: { links: readonly IssueLinked[]; repo: string }) {
  return (
    <ul className="space-y-1 text-sm">
      {links.map((l) => (
        <li key={refText(l)} className="flex min-w-0">
          <ItemLink linked={l} from={repo} titled />
        </li>
      ))}
    </ul>
  )
}

// Development is the pull requests that close the issue once merged, where each stands,
// and the pull requests and issues that mentioned it.
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
              <ItemLink linked={fix.pr} from={issue.repo} titled />
              <p className="text-xs text-muted">{fixState(fix)}</p>
            </li>
          ))}
        </ul>
      )}
      <More count={issue.moreFixes} url={issue.url} />
      {issue.mentioned.length > 0 && (
        <>
          <p className="text-xs text-muted">Mentioned in</p>
          <Links links={issue.mentioned} repo={issue.repo} />
        </>
      )}
    </>
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
        className="text-left text-sm text-text hover:text-accent hover:underline"
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

// IssueSummary is the details beside the conversation: the pull requests that fix the
// issue, who it is assigned to, its labels and projects, its milestone, its sub-issues and
// the issue it is part of, and who took part.
export function IssueSummary({ issue, onEdit }: IssueSummaryProps) {
  const editing = issue.canEdit && onEdit !== undefined

  return (
    <div className="space-y-4">
      <Part title="Development">
        <Development issue={issue} />
      </Part>
      <Part title="Assignees" action={editing && <AssigneePicker issue={issue} onEdit={onEdit} />}>
        <People people={issue.assignees} more={issue.moreAssignees} url={issue.url} none="No one" />
      </Part>
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
      {issue.subIssuesTotal > 0 && (
        <Part title="Sub-issues">
          <div className="flex items-center gap-2 text-xs text-muted">
            <ProgressBar done={issue.subIssuesDone} total={issue.subIssuesTotal} />
            <span>
              {String(issue.subIssuesDone)} of {counted(issue.subIssuesTotal, 'sub-issue', 'sub-issues')} done
            </span>
          </div>
          <Links links={issue.subIssues} repo={issue.repo} />
          <More count={issue.subIssuesTotal - issue.subIssues.length} url={issue.url} />
        </Part>
      )}
      {issue.parent !== null && (
        <Part title="Part of">
          <Links links={[issue.parent]} repo={issue.repo} />
        </Part>
      )}
      {issue.duplicateOf !== null && (
        <Part title="Duplicate of">
          <Links links={[issue.duplicateOf]} repo={issue.repo} />
        </Part>
      )}
      <Part title="Participants">
        <People people={issue.participants} more={issue.moreParticipants} url={issue.url} none="No one yet" />
      </Part>
    </div>
  )
}
