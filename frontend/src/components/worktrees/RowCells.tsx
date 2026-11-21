import type { ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { shortCommit } from '../../lib/git'
import { capitalize, formatAge, formatAgo, formatFull } from '../../lib/time'
import { useNow } from '../../lib/useNow'
import type { RowWork } from '../../lib/useWorktrees'
import {
  agentLine,
  checksOf,
  clockOf,
  copiedTag,
  folderOf,
  itemRef,
  kindOf,
  splitBranch,
  stateTones,
  stateWords,
  type AgentLine,
  type ItemFacts,
  type KindIcon,
} from '../../lib/worktreeParts'
import { edgeTitle, laneOf, lanes, stateOf, workNotes, type StateGlyph, type WorkNote } from '../../lib/worktrees'
import { openExternal, type AgentID, type AgentSession, type LinkedItem, type Worktree } from '../../platform'
import {
  AlertIcon,
  ArrowUpIcon,
  CheckIcon,
  CopyIcon,
  GhostIcon,
  IssueClosedIcon,
  IssueIcon,
  LockIcon,
  MergeIcon,
  PullRequestClosedIcon,
  PullRequestIcon,
  RingIcon,
  SparkIcon,
} from '../icons'
import { RelativeTime } from '../RelativeTime'
import { Spinner } from '../Spinner'
import { StatusDot } from '../StatusDot'
import {
  ageCellClass,
  agentCellClass,
  branchCellClass,
  edgeClass,
  itemCellClass,
  stateCellClass,
  tagClass,
  toneBg,
  toneText,
  workCellClass,
  workMarkClass,
} from './layout'

// The cells of a Worktrees row, each placed in the page's grid by its own classes
// (layout.ts): the edge, the state, the branch, the item, the work, the agent, the age, and
// what a change taking the row is doing. Each says what it shows in words in its tooltip,
// and to a screen reader.

// detachedOf reports a worktree on no branch.
export function detachedOf(wt: Worktree): boolean {
  return wt.detached || wt.branch === ''
}

// nameOf names a worktree for its controls: its branch, or its folder when it has none.
export function nameOf(wt: Worktree): string {
  return detachedOf(wt) ? folderOf(wt) : wt.branch
}

// Edge is the bar down a row's left side, in its lane's color, which says why it stands
// there in its tooltip; an open row has none.
export function Edge({ worktree }: { worktree: Worktree }) {
  const lane = laneOf(worktree)
  const { tone } = lanes[lane]

  if (tone === '') {
    return null
  }

  return (
    <span
      aria-hidden="true"
      data-lane={lane}
      title={edgeTitle(worktree)}
      className={`${edgeClass} ${toneBg[tone]} ${lane === 'running' ? 'motion-safe:animate-pulse' : ''}`}
    />
  )
}

// Glyph is the state's one mark.
function Glyph({ glyph, tone }: { glyph: StateGlyph; tone: keyof typeof toneBg }) {
  const icon = 'size-3.5'

  switch (glyph) {
    case 'ping':
      return <StatusDot color={toneBg[tone]} ping />
    case 'dot':
      return <StatusDot color={toneBg[tone]} />
    case 'alert':
      return <AlertIcon className={icon} />
    case 'ghost':
      return <GhostIcon className={icon} />
    case 'lock':
      return <LockIcon className={icon} />
    case 'up':
      return <ArrowUpIcon className={icon} />
    case 'ring':
      return <RingIcon className={icon} />
    case 'check':
      return <CheckIcon className={icon} />
  }
}

// StateCell is a row's state as one mark, its words in its tooltip and for a screen reader.
export function StateCell({ worktree }: { worktree: Worktree }) {
  const state = stateOf(worktree)

  return (
    <span className={stateCellClass}>
      <span
        title={capitalize(state.words)}
        className={`inline-flex size-3.5 cursor-help items-center justify-center ${toneText[state.tone]}`}
      >
        <Glyph glyph={state.glyph} tone={state.tone} />
        <span className="sr-only">{state.words}</span>
      </span>
    </span>
  )
}

// BranchCell is a row's branch: a muted prefix that gives way first and the name in medium
// weight, the whole in its tooltip; or the commit a detached worktree is at.
export function BranchCell({ worktree }: { worktree: Worktree }) {
  if (detachedOf(worktree)) {
    return (
      <span title={`Detached HEAD at ${worktree.head}`} className={branchCellClass}>
        <span className="truncate text-muted italic">
          detached at <code className="font-mono text-xs text-text not-italic">{shortCommit(worktree.head)}</code>
        </span>
      </span>
    )
  }

  const [prefix, name] = splitBranch(worktree.branch)

  return (
    <span title={worktree.branch} className={branchCellClass}>
      {prefix !== '' && <span className="min-w-0 shrink truncate text-muted">{prefix}</span>}
      <span className="max-w-full shrink-0 truncate font-medium">{name}</span>
    </span>
  )
}

// kindIcons draw each mark of a pull request or issue.
const kindIcons: Readonly<Record<KindIcon, (className: string) => ReactNode>> = {
  pr: (className) => <PullRequestIcon className={className} />,
  prMerged: (className) => <MergeIcon className={className} />,
  prClosed: (className) => <PullRequestClosedIcon className={className} />,
  issue: (className) => <IssueIcon className={className} />,
  issueClosed: (className) => <IssueClosedIcon className={className} />,
}

// KindMark marks a pull request or issue as it stands, its words in its tooltip.
function KindMark({ icon, className }: { icon: KindIcon; className: string }) {
  return kindIcons[icon](className)
}

interface ItemCellProps {
  worktree: Worktree
  // facts are what the inbox read of the worktree's item, while it lists it.
  facts: ItemFacts | undefined
}

interface LinkedCellProps {
  item: LinkedItem
  facts: ItemFacts | undefined
  // copied is the tooltip of the tag on a pull request the worktree is linked to by its
  // commits' copies on its branch (copiedTag), or '' for one linked otherwise, with no tag.
  copied: string
}

// LinkedCell is the pull request or issue a worktree is for: its mark in its state's color,
// its number, which opens it on GitHub, and its title; then a copied tag when the worktree
// is linked to it by its commits' copies, and its draft tag and its checks' dot, as the
// inbox read them, while it lists it.
function LinkedCell({ item, facts, copied }: LinkedCellProps) {
  const now = useNow()
  const draft = facts?.draft ?? false
  const kind = kindOf(item, draft, now)
  const checks = facts === undefined ? null : checksOf(facts.checks)

  return (
    <span className={itemCellClass}>
      <span title={kind.words} className={`inline-flex shrink-0 cursor-help ${toneText[kind.tone]}`}>
        <KindMark icon={kind.icon} className="size-3.25" />
        <span className="sr-only">{kind.words}</span>
      </span>
      <button
        type="button"
        title={`Open ${itemRef(item)} on GitHub`}
        onClick={() => {
          openExternal(item.url)
        }}
        className="shrink-0 text-muted tabular-nums hover:text-text hover:underline"
      >
        #{String(item.number)}
      </button>
      <span title={`${item.owner}/${item.name}#${String(item.number)}: ${item.title}`} className="min-w-0 truncate">
        {item.title}
      </span>
      {copied !== '' && (
        <span title={copied} className={tagClass}>
          copied
        </span>
      )}
      {draft && (
        <span title="A draft, as the inbox read it" className={`${tagClass} border-dashed`}>
          draft
        </span>
      )}
      {checks !== null && (
        <span
          title={`${checks.words}, as the inbox read them`}
          className="inline-flex shrink-0 cursor-help items-center px-1.5"
        >
          <StatusDot color={toneBg[checks.tone]} ping={checks.pending} />
          <span className="sr-only">{checks.words.toLowerCase()}</span>
        </span>
      )}
    </span>
  )
}

// ItemCell is what a worktree is for: its pull request or issue, or with none its folder's
// name, muted, and a Claude Code tag on one Claude Code made.
export function ItemCell({ worktree, facts }: ItemCellProps) {
  if (worktree.item !== null) {
    return <LinkedCell item={worktree.item} facts={facts} copied={copiedTag(worktree)} />
  }

  return (
    <span className={itemCellClass}>
      <span title={worktree.displayPath} className="min-w-0 truncate font-mono text-[0.71875rem] text-muted">
        {folderOf(worktree)}
      </span>
      {worktree.legacy && (
        <span
          title={`Claude Code made it in the clone’s .claude/worktrees, before ${brand.name} made its own`}
          className={tagClass}
        >
          Claude Code
        </span>
      )}
    </span>
  )
}

// NoteMark is a work note's mark: ⧉ for commits on GitHub only as copies, ↑ for commits
// that aren't, or couldn't be counted; none for uncommitted files.
function NoteMark({ kind }: { kind: WorkNote['kind'] }) {
  switch (kind) {
    case 'dirty':
      return null
    case 'copied':
      return <CopyIcon className="size-2.75" />
    default:
      return <ArrowUpIcon className="size-2.75" />
  }
}

// WorkCell is the work a worktree holds that isn't on GitHub: its uncommitted files, in
// amber, and its commits not on GitHub, ↑2, or ↑? when they couldn't be counted; or,
// muted, its commits on GitHub only as copies, ⧉ 10 on #42. It is empty when there is
// none: the state's ✓ says so already.
export function WorkCell({ worktree }: { worktree: Worktree }) {
  return (
    <span className={workCellClass}>
      {workNotes(worktree).map((note) => (
        <span
          key={note.kind}
          title={note.title}
          className={`inline-flex shrink-0 cursor-help items-center gap-0.75 ${toneText[note.tone]} ${note.tone === 'muted' ? '' : 'font-medium'}`}
        >
          <NoteMark kind={note.kind} />
          <span className="sr-only">{note.sr}</span>
          {note.text}
        </span>
      ))}
    </span>
  )
}

// programOf names the program a session runs: Claude Code, with its version when its
// process gives it, or the agent's own name.
export function programOf(line: { name: string; version: string }, claude: string): string {
  const name = line.name === claude ? 'Claude Code' : line.name

  return line.version === '' ? name : `${name} ${line.version}`
}

// Since is how long a session has run, after a dot, when it's known.
function Since({ iso }: { iso: string }) {
  if (iso === '') {
    return null
  }

  return (
    <>
      {' · '}
      <RelativeTime iso={iso} show={formatAge} />
    </>
  )
}

interface AgentCellProps {
  worktree: Worktree
  // session is the session the app follows in the worktree, if any.
  session: AgentSession | undefined
  agentName: (id: AgentID) => string
}

// lineTitle is the agent cell's tooltip for what works in a worktree.
function lineTitle(line: AgentLine, claude: string): string {
  switch (line.kind) {
    case 'routine':
      return `⚡ ${line.by} runs ${programOf(line, claude)} here in the background (pid ${String(line.pid)})`
    case 'live': {
      const state = line.state === '' ? '' : `, ${stateWords[line.state]}`
      const since = line.since === '' ? '' : `, since ${clockOf(line.since)}`

      return `${programOf(line, claude)}, pid ${String(line.pid)}${state}${since}`
    }
    case 'lock':
      return `No process is listed, but a live ${line.name} session holds it by its lock (pid ${String(line.pid)}${line.since === '' ? '' : `, since ${clockOf(line.since)}`})`
    case 'past':
      return `The last session the app launched here: ${line.name}, ${formatFull(line.at)}`
  }
}

// AgentWords are what the agent cell says of what works in a worktree.
function AgentWords({ line }: { line: AgentLine }) {
  switch (line.kind) {
    case 'routine':
      return (
        <>
          <StatusDot color="bg-info" ping />
          <span className="min-w-0 truncate">
            <SparkIcon className="inline size-3 align-[-0.125rem]" />{' '}
            <span className="font-medium text-text">{line.by}</span> · in the background
          </span>
        </>
      )
    case 'live': {
      const tone = line.state === '' ? 'info' : stateTones[line.state]
      const ping = line.state === '' || line.state === 'working' || line.state === 'starting'

      return (
        <>
          <StatusDot color={toneBg[tone]} ping={ping} />
          <span className="min-w-0 truncate">
            <span className="font-medium text-text">{line.name}</span>
            {line.version !== '' && <span className="font-mono text-[0.6875rem] @max-xl:hidden"> {line.version}</span>}
            {' · '}
            <span className={toneText[tone]}>
              {line.state === '' ? `pid ${String(line.pid)}` : stateWords[line.state]}
            </span>
            <Since iso={line.since} />
          </span>
        </>
      )
    }
    case 'lock':
      return (
        <>
          <StatusDot color="bg-info" ping />
          <span className="min-w-0 truncate">
            <span className="font-medium text-text">{line.name}</span> · by its lock
            <Since iso={line.since} />
          </span>
        </>
      )
    case 'past':
      return (
        <span className="min-w-0 truncate">
          {line.name} · <RelativeTime iso={line.at} show={formatAgo} />
        </span>
      )
  }
}

// AgentCell is what works in a worktree and how it stands (an agent, a routine's agent in
// the background, another program, or a live lock), or else the last session the app
// launched there, muted; empty when there was none.
export function AgentCell({ worktree, session, agentName }: AgentCellProps) {
  const line = agentLine(worktree, session, agentName)

  if (line === null) {
    return null
  }

  return (
    <span title={lineTitle(line, agentName('claude'))} className={agentCellClass}>
      <AgentWords line={line} />
    </span>
  )
}

// AgeCell is how long ago a worktree's last commit was, when it's known.
export function AgeCell({ worktree }: { worktree: Worktree }) {
  if (worktree.lastCommitUnix <= 0) {
    return null
  }

  const iso = new Date(worktree.lastCommitUnix * 1000).toISOString()

  return (
    <span title={`Last commit: ${formatFull(iso)}`} className={ageCellClass}>
      <RelativeTime iso={iso} show={formatAgo} />
    </span>
  )
}

// workTones color what a change taking a row says.
const workTones: Readonly<Record<RowWork['state'], string>> = {
  doing: 'text-text',
  done: 'text-success',
  left: 'text-warning',
}

// WorkMark says, in place of a row's verb and tools, what a change taking it is doing, or
// what became of it, until the list is read again.
export function WorkMark({ work }: { work: RowWork }) {
  return (
    <span role="status" className={`${workMarkClass} ${workTones[work.state]}`}>
      {work.state === 'doing' && <Spinner className="size-3" />}
      {work.state === 'done' && <CheckIcon className="size-3.5" />}
      {work.state === 'left' && <AlertIcon className="size-3.5" />}
      {work.text}
    </span>
  )
}
