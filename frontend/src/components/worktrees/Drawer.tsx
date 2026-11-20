import type { ReactNode } from 'react'

import { counted } from '../../lib/format'
import { formatAgo } from '../../lib/time'
import { useNow } from '../../lib/useNow'
import { asReadNow, readable, type DetailsRead } from '../../lib/useWorktreeDetails'
import {
  agentLine,
  checksOf,
  clockOf,
  itemRef,
  lockInfo,
  madeBy,
  stateTones,
  stateWords,
  type AgentLine,
  type ItemFacts,
} from '../../lib/worktreeParts'
import { removeWhy, safety, verbOf } from '../../lib/worktrees'
import {
  openExternal,
  type AgentID,
  type AgentSession,
  type LinkedItem,
  type Worktree,
  type WorktreeLinkKind,
  type WorktreeRepo,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, CopyIcon, FolderIcon, LockIcon, ScissorsIcon, TerminalIcon, TrashIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { Changes, NotOnGitHub, waiting } from './ChangeLists'
import { CopyButton } from './CopyButton'
import {
  cleanUpButtonClass,
  columnHeadingClass,
  dangerVerbButtonClass,
  drawerClass,
  drawerColumnClass,
  drawerColumnsClass,
  drawerFooterClass,
  skeletonClass,
  toneText,
  verbButtonClass,
} from './layout'
import type { WorktreeRowActions } from './menus'
import { nameOf, programOf } from './RowCells'
import { SafetyBand } from './SafetyBand'

// The drawer under a row: what removing the worktree would cost, its changes, its commits
// not on GitHub with its branch, what's known about it, and its actions in words. It shows
// at once what the listing has, and the rest once Details is read (useWorktreeDetails).

// muted sets the quieter words of a line apart.
const muted = 'text-muted'

// review words a pull request's review decision, as the inbox read it.
const reviewWords: Readonly<Record<string, string>> = {
  APPROVED: 'approved',
  CHANGES_REQUESTED: 'changes requested',
}

// ItemLine is what a worktree is for: its pull request or issue, which opens on GitHub,
// its title, and where it stands, with its checks, review, and draft as the inbox read
// them while it lists it.
function ItemLine({ item, facts }: { item: LinkedItem; facts: ItemFacts | undefined }) {
  const extra =
    facts === undefined
      ? []
      : [
          checksOf(facts.checks)?.words.toLowerCase() ?? '',
          reviewWords[facts.review] ?? '',
          facts.draft ? 'draft' : '',
        ].filter((w) => w !== '')

  return (
    <>
      <button
        type="button"
        title="Open it on GitHub"
        onClick={() => {
          openExternal(item.url)
        }}
        className="text-accent hover:underline"
      >
        {itemRef(item)}
      </button>{' '}
      <span>{item.title}</span>{' '}
      <span className={muted}>
        {item.state}
        {item.state !== 'open' && item.closedAt !== '' && (
          <>
            {' '}
            <RelativeTime iso={item.closedAt} show={formatAgo} />
          </>
        )}
        {extra.length > 0 && ` · ${extra.join(' · ')}`}
      </span>
    </>
  )
}

// Started is when a session started, as a clock time and how long ago.
function Started({ iso }: { iso: string }) {
  if (iso === '') {
    return null
  }

  return (
    <>
      {` · started ${clockOf(iso)} (`}
      <RelativeTime iso={iso} show={formatAgo} />)
    </>
  )
}

// SessionLine is what works in a worktree now: its program and process, the state of the
// session the app follows there and when it started, and the routine it runs for.
function SessionLine({ line, claude }: { line: Exclude<AgentLine, { kind: 'past' }>; claude: string }) {
  const pid = line.pid > 0 ? ` · pid ${String(line.pid)}` : ''

  if (line.kind === 'lock') {
    return (
      <>
        {line.name}, by its lock{' '}
        <span className={muted}>
          {pid}
          <Started iso={line.since} />
        </span>
      </>
    )
  }

  return (
    <>
      {line.version === '' ? line.name : programOf(line, claude)} <span className={muted}>{pid}</span>
      {line.state !== '' && (
        <>
          {' · '}
          <span className={toneText[stateTones[line.state]]}>{stateWords[line.state]}</span>
        </>
      )}
      <span className={muted}>
        <Started iso={line.since} />
        {line.kind === 'routine' && ` · for ⚡ ${line.by}, in the background`}
      </span>
    </>
  )
}

// LockLine says who holds a worktree's lock: someone by hand, which the app never undoes,
// or a Claude session, live or ended, with what releases an ended one's.
function LockLine({ worktree, claude }: { worktree: Worktree; claude: string }) {
  if (!worktree.agentLock) {
    return (
      <>
        {worktree.lockReason === '' ? 'By hand' : `By hand: “${worktree.lockReason}”`}{' '}
        <span className={muted}>· the app never unlocks it</span>
      </>
    )
  }

  const lock = lockInfo(worktree)
  const start = lock?.start ?? ''

  return (
    <>
      {`${claude} session`}
      {lock !== null && (
        <>
          {' '}
          <code className="font-mono text-[0.71875rem]">{lock.session}</code>
        </>
      )}{' '}
      <span className={muted}>
        {lock !== null && `· pid ${String(lock.pid)} `}
        {worktree.staleLock ? (
          <>
            {'· ended'}
            {start !== '' && (
              <>
                {', started '}
                <RelativeTime iso={start} show={formatAgo} />
              </>
            )}
            {` · ${worktree.canCleanUp ? 'Clean up' : 'Unlock'} releases it`}
          </>
        ) : (
          `· live${start === '' ? '' : `, started ${clockOf(start)}`}`
        )}
      </span>
    </>
  )
}

// RemovalLine says which branches a removal would offer to delete with the worktree, and
// why one that stays does.
function RemovalLine({ worktree }: { worktree: Worktree }) {
  if (worktree.branches.length === 0) {
    return (
      <span className={muted}>
        {worktree.detached || worktree.branch === ''
          ? 'It has no branch to delete.'
          : 'Its branch stays: it’s neither the branch made for it nor its pull request’s.'}
      </span>
    )
  }

  return worktree.branches.map((b) => (
    <span key={b.name} className={`block ${b.deletable ? '' : muted}`}>
      {b.deletable ? (
        <TrashIcon className="inline size-3 align-[-0.125rem]" />
      ) : (
        <LockIcon className="inline size-3 align-[-0.125rem]" />
      )}{' '}
      <code className="font-mono text-[0.71875rem]">{b.name}</code>{' '}
      {b.deletable ? <span className={muted}>can go with it</span> : `stays: ${b.reason}`}
    </span>
  ))
}

interface AboutProps {
  worktree: Worktree
  read: DetailsRead | undefined
  session: AgentSession | undefined
  facts: ItemFacts | undefined
  agentName: (id: AgentID) => string
}

// itemTerms head the line of a worktree's pull request or issue by how it is linked to it:
// made for it, on its branch, or with its commits copied onto it.
const itemTerms: Readonly<Record<WorktreeLinkKind, string>> = {
  '': 'For',
  name: 'For',
  branch: 'On the branch of',
  copy: 'Copied onto',
}

// About is the drawer's third column: what a worktree is for, what works in it, its
// launches, who made it and when, its path, its lock, and what a removal would delete.
function About({ worktree, read, session, facts, agentName }: AboutProps) {
  const line = agentLine(worktree, session, agentName)
  const claude = agentName('claude')
  const created = read?.state === 'read' ? read.details.createdAt : ''
  const rows: { term: string; value: ReactNode }[] = [
    {
      term: itemTerms[worktree.linkedBy],
      value:
        worktree.item === null ? (
          <span className={muted}>no pull request or issue</span>
        ) : (
          <ItemLine item={worktree.item} facts={facts} />
        ),
    },
  ]

  if (line !== null && line.kind !== 'past') {
    rows.push({ term: 'Session', value: <SessionLine line={line} claude={claude} /> })
  }

  if (worktree.launches > 0) {
    rows.push({
      term: 'Launches',
      value: (
        <>
          {counted(worktree.launches, 'launch', 'launches')}
          {worktree.agent !== '' && worktree.lastLaunchedAt !== '' && (
            <span className={muted}>
              {` · last ${agentName(worktree.agent)}, `}
              <RelativeTime iso={worktree.lastLaunchedAt} show={formatAgo} />
            </span>
          )}
        </>
      ),
    })
  }

  rows.push(
    {
      term: 'Made by',
      value: (
        <>
          {madeBy(worktree)}
          {waiting(worktree, read) ? (
            <>
              {' '}
              <span role="img" aria-label="Reading when it was made" className={`${skeletonClass} w-11`} />
            </>
          ) : (
            created !== '' && (
              <span className={muted}>
                {' · '}
                <RelativeTime iso={created} show={formatAgo} />
              </span>
            )
          )}
        </>
      ),
    },
    {
      term: 'Path',
      value: (
        <>
          <span className="font-mono text-[0.6875rem] [overflow-wrap:anywhere]">{worktree.displayPath}</span>{' '}
          <CopyButton
            text={worktree.path}
            what="worktree path"
            ariaLabel="Copy the path"
            title="Copy the path"
            className={`${quietButtonClass} align-[-0.125rem]`}
          />
        </>
      ),
    },
  )

  if (worktree.locked) {
    rows.push({ term: 'Lock', value: <LockLine worktree={worktree} claude={claude} /> })
  }

  if (!worktree.prunable) {
    rows.push({ term: 'Removal', value: <RemovalLine worktree={worktree} /> })
  }

  return (
    <div className={drawerColumnClass}>
      <h4 className={columnHeadingClass}>About</h4>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1 text-xs">
        {rows.map(({ term, value }) => (
          <div key={term} className="contents">
            <dt className="whitespace-nowrap text-muted">{term}</dt>
            <dd className="min-w-0 leading-normal">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

// errorsOf are the sentences that say what of a worktree's details couldn't be read: the
// whole call's failure, or each read's that failed.
function errorsOf(read: DetailsRead | undefined): readonly string[] {
  switch (read?.state) {
    case 'failed':
      return [read.error]
    case 'read':
      return read.details.errors
    default:
      return []
  }
}

export interface DrawerProps extends WorktreeRowActions {
  // id is the drawer's, which its row's chevron controls.
  id: string
  repo: WorktreeRepo
  worktree: Worktree
  // read is what the window has read of the worktree's details for this listing, or
  // undefined before it asks.
  read: DetailsRead | undefined
  // session is the session the app follows in the worktree, if any.
  session: AgentSession | undefined
  // facts are what the inbox read of the worktree's item, while it lists it.
  facts: ItemFacts | undefined
  agentName: (id: AgentID) => string
  // disabled holds every change while one, or a listing, runs; working reports a change
  // taking this worktree, whose verb waits for the list to be read again.
  disabled: boolean
  working: boolean
  // opening reports Terminal or Finder being opened on the worktree.
  opening: boolean
}

// Drawer is a row's drawer: on top, what removing the worktree would cost (SafetyBand),
// counted as its details last counted it, as the lists under it are (asReadNow); then its
// changes, its commits not on GitHub with its branch, and what's known about it, side by
// side, or one above another when narrow; then what of its details couldn't be read; and
// its actions in words: Open in Terminal, Reveal in Finder, and Copy path (or Prune, for a
// folder that's gone), then Clean up or Unlock, and Remove… for a clean one or Force
// remove… for one with uncommitted files, each saying why when it can't run. Which removal
// it offers, and why one can't run, are counted as the sentence over them is, so they never
// contradict it; whether one may run at all stays the backend's (its can* flags), which
// checks again as it removes, so a read can only hold a removal back, never allow one the
// listing refused.
export function Drawer({
  id,
  repo,
  worktree,
  read,
  session,
  facts,
  agentName,
  disabled,
  working,
  opening,
  onTerminal,
  onReveal,
  onCleanUp,
  onUnlock,
  onRemove,
  onForceRemove,
  onPrune,
}: DrawerProps) {
  const now = useNow()
  // fresh is the worktree counted as its details last counted it, which the sentence on
  // top and the removal in the footer both go by.
  const fresh = asReadNow(worktree, read)
  const verb = verbOf(fresh, false)
  const errors = errorsOf(read)
  const force = fresh.dirtyCount > 0
  const removeReason = removeWhy(fresh, force, agentName)
  const removeTitle =
    removeReason !== ''
      ? removeReason
      : force
        ? `Discards ${counted(fresh.dirtyCount, 'uncommitted file', 'uncommitted files')}; lists them and asks first`
        : 'Removes the folder; asks which branches go with it'

  return (
    <li id={id} aria-label={`About ${nameOf(worktree)}`} className={drawerClass}>
      <SafetyBand safety={safety(fresh, repo, now, session, agentName)} />
      <div className={drawerColumnsClass}>
        <Changes worktree={worktree} read={read} />
        <NotOnGitHub worktree={worktree} read={read} />
        <About worktree={worktree} read={read} session={session} facts={facts} agentName={agentName} />
      </div>
      {errors.length > 0 && (
        <ul className="flex flex-col gap-0.5 border-t border-border/70 px-3 py-1.5 text-xs text-warning">
          {errors.map((error) => (
            <li key={error} className="flex items-start gap-1.5">
              <AlertIcon className="mt-0.5 size-3 shrink-0" />
              {error}
            </li>
          ))}
        </ul>
      )}
      <div className={drawerFooterClass}>
        {worktree.prunable ? (
          <button
            type="button"
            disabled={disabled}
            title="Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch"
            onClick={() => {
              onPrune(repo)
            }}
            className={quietButtonClass}
          >
            <ScissorsIcon className="size-3" />
            {`Prune ${repo.name}`}
          </button>
        ) : (
          <>
            {readable(worktree) && (
              <>
                <button type="button" disabled={opening} onClick={onTerminal} className={quietButtonClass}>
                  <TerminalIcon className="size-3.5" />
                  Open in Terminal
                </button>
                <button type="button" disabled={opening} onClick={onReveal} className={quietButtonClass}>
                  <FolderIcon className="size-3.5" />
                  Reveal in Finder
                </button>
              </>
            )}
            <CopyButton
              text={worktree.path}
              what="worktree path"
              label="Copy path"
              icon={<CopyIcon className="size-3.5" />}
            />
          </>
        )}
        <span className="flex-1" />
        {verb !== null && verb.act !== 'launch' && !working && (
          <button
            type="button"
            disabled={disabled}
            title={verb.title}
            onClick={() => {
              if (verb.act === 'cleanUp') {
                onCleanUp(repo, worktree)
              } else {
                onUnlock(repo, worktree)
              }
            }}
            className={`ml-1 ${verb.act === 'cleanUp' ? cleanUpButtonClass : verbButtonClass}`}
          >
            {verb.label}
          </button>
        )}
        {!worktree.prunable && (
          <button
            type="button"
            disabled={disabled || removeReason !== ''}
            title={removeTitle}
            onClick={() => {
              if (force) {
                onForceRemove(repo, worktree)
              } else {
                onRemove(repo, worktree)
              }
            }}
            className={`ml-1 ${force ? dangerVerbButtonClass : verbButtonClass}`}
          >
            {force ? 'Force remove…' : 'Remove…'}
          </button>
        )}
      </div>
    </li>
  )
}
