import { counted, joinList, refText } from '../../lib/format'
import { closedWords, doneShownUpTo, subIssueGroups } from '../../lib/issues'
import { formatAgo } from '../../lib/time'
import { openExternal, type Issue, type IssuePerson, type IssueSubIssue } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { SubIssueRing } from '../card/IssueState'
import { linkClass } from '../controls'
import { Fold } from '../Fold'
import { ItemLink } from '../ItemLink'
import { ProgressBar } from '../ItemSignals'
import { RefMark } from '../RefMark'
import { RelativeTime } from '../RelativeTime'

// assignedWords says who a sub-issue is assigned to, as in "Assigned to you, octo-bob, and 3
// more".
function assignedWords(people: readonly IssuePerson[], more: number): string {
  const names = people.map((p) => (p.mine ? 'you' : p.login))

  if (more > 0) {
    names.push(`${String(more)} more`)
  }

  return `Assigned to ${joinList(names)}`
}

// Assigned is a sub-issue's first assignees, as their pictures, and how many more it has,
// which its tooltip names; nothing for a sub-issue assigned to no one.
function Assigned({ people, more }: { people: readonly IssuePerson[]; more: number }) {
  if (people.length === 0) {
    return null
  }

  const words = assignedWords(people, more)

  return (
    <span role="img" aria-label={words} title={words} className="flex shrink-0 items-center gap-0.5">
      {people.map((p) => (
        <PersonAvatar key={p.login} login={p.login} />
      ))}
      {more > 0 && <span className="pl-0.5 text-caption text-muted tabular-nums">+{String(more)}</span>}
    </span>
  )
}

// Closed says how a done sub-issue was closed, when that was not as completed, and when.
function Closed({ sub }: { sub: IssueSubIssue }) {
  const words = closedWords(sub.issue.stateReason)

  if (words === '' && sub.closedAt === '') {
    return null
  }

  return (
    <span className="shrink-0 text-xs text-muted">
      {words}
      {words !== '' && sub.closedAt !== '' && ' · '}
      {sub.closedAt !== '' && (
        <>
          closed <RelativeTime iso={sub.closedAt} show={formatAgo} />
        </>
      )}
    </span>
  )
}

// SubIssueRow is one sub-issue on a line of its own: its state, its name and title, which
// open it in the sheet when the inbox holds it (with a step back here) and else on GitHub,
// how it was closed and when once it is done, its own sub-issues' progress, and who it is
// assigned to.
function SubIssueRow({ sub, from }: { sub: IssueSubIssue; from: string }) {
  const done = sub.issue.state !== 'OPEN'

  return (
    <li className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-raised">
      <RefMark linked={sub.issue} />
      <span className="flex min-w-0 flex-1">
        <ItemLink linked={sub.issue} from={from} titled muted={done} />
      </span>
      {done && <Closed sub={sub} />}
      {sub.subIssuesTotal > 0 && (
        <span className="shrink-0 text-caption">
          <SubIssueRing subIssues={{ total: sub.subIssuesTotal, completed: sub.subIssuesDone }} />
        </span>
      )}
      <Assigned people={sub.assignees} more={sub.moreAssignees} />
    </li>
  )
}

// OnGitHub is a button that opens the issue's page, where GitHub lists all its sub-issues.
function OnGitHub({ url, children }: { url: string; children: string }) {
  return (
    <button
      type="button"
      onClick={() => {
        openExternal(url)
      }}
      className={linkClass}
    >
      {children}
    </button>
  )
}

// SubIssues is the issue sheet's Sub-issues tab: how far along they are, then those still
// open and those done, each under a fold with its count. Open always starts open, so what is
// left leads; Done starts open while it holds a few (doneShownUpTo), and folded past that,
// until the user opens or folds either. It says when GitHub has more than the sheet read, and
// when it lists sub-issues this account can't read.
export function SubIssues({ issue }: { issue: Issue }) {
  const total = issue.subIssuesTotal
  const shown = issue.subIssues.length
  const { open, done } = subIssueGroups(issue.subIssues)

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <div className="flex items-center gap-3 text-sm text-muted">
        <ProgressBar done={issue.subIssuesDone} total={total} className="w-48" />
        <span>
          {String(issue.subIssuesDone)} of {String(total)} done
        </span>
      </div>
      {shown === 0 ? (
        <p className="text-sm text-muted">
          {issue.fetchedAt === '' ? (
            'Not read from GitHub yet.'
          ) : (
            <>
              GitHub lists {counted(total, 'sub-issue', 'sub-issues')} this account can’t read ·{' '}
              <OnGitHub url={issue.url}>Open on GitHub</OnGitHub>
            </>
          )}
        </p>
      ) : (
        total > shown && (
          <p className="text-xs text-muted">
            Showing {String(shown)} of {String(total)} · <OnGitHub url={issue.url}>Open all on GitHub</OnGitHub>
          </p>
        )
      )}
      {open.length > 0 && (
        <Fold title="Open" count={open.length} auto>
          <ul aria-label="Open sub-issues">
            {open.map((sub) => (
              <SubIssueRow key={refText(sub.issue)} sub={sub} from={issue.repo} />
            ))}
          </ul>
        </Fold>
      )}
      {done.length > 0 && (
        <Fold title="Done" count={done.length} auto={done.length <= doneShownUpTo}>
          <ul aria-label="Done sub-issues">
            {done.map((sub) => (
              <SubIssueRow key={refText(sub.issue)} sub={sub} from={issue.repo} />
            ))}
          </ul>
        </Fold>
      )}
    </div>
  )
}
