import { useEffect, useEffectEvent } from 'react'

import { counted, formatCount } from '../../lib/format'
import { asReadNow, changesOf, notOnGitHub, type WorktreeDetailsState } from '../../lib/useWorktreeDetails'
import type { Choice, Confirmation, Target } from '../../lib/useWorktrees'
import { copiedOf, linkWords, unpushedKnown, unpushedUnknown } from '../../lib/worktreeParts'
import { safety, type Safety } from '../../lib/worktrees'
import type {
  AgentID,
  AgentSession,
  BranchRules,
  LinkedItem,
  Worktree,
  WorktreeCommit,
  WorktreeLinkKind,
  WorktreeRepo,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { Spinner } from '../Spinner'
import { chosenBranches, useTicks, WorktreeBranches } from './BranchChoices'
import { CommitLine, FileLine, fileKey, RestInTerminal, waiting } from './ChangeLists'
import { MergedBranchesDialog } from './MergedBranchesDialog'
import { detachedOf } from './RowCells'
import { SafetyBand, SafetyLine } from './SafetyBand'

export interface ConfirmationDialogProps {
  confirmation: Confirmation
  // rules are the user's branch rules, which tick each branch's box to start with.
  rules: BranchRules
  // now is what ages are measured against.
  now: Date
  // repos are the listed clones, details the worktrees' details read on demand
  // (useWorktreeDetails), which Force remove reads as it opens, sessions the sessions the
  // window follows, by the folder they work in (liveSessions), and agentName how an agent
  // is named: the dialogs word what removing each worktree would cost from them.
  repos: readonly WorktreeRepo[]
  details: WorktreeDetailsState
  sessions: ReadonlyMap<string, AgentSession>
  agentName: (id: AgentID) => string
  onConfirm: (choice: Choice) => void
  onCancel: () => void
}

// Cost is what the confirmations need to say what removing a worktree would cost.
type Cost = Pick<ConfirmationDialogProps, 'repos' | 'now' | 'sessions' | 'agentName'>

// costOf says what removing fresh, a worktree of the clone at repoPath counted as its
// details last counted it (asReadNow), would cost, as its drawer says it (lib/worktrees'
// safety).
function costOf(fresh: Worktree, repoPath: string, { repos, now, sessions, agentName }: Cost): Safety {
  return safety(fresh, repos.find((r) => r.path === repoPath) ?? null, now, sessions.get(fresh.displayPath), agentName)
}

// itemText names the item a worktree is linked to, how (linkedBy), and where it stands, as
// in "Made for pull request #7, merged", "On the branch of pull request #42, open", or "Its
// commits were copied onto pull request #42, merged".
function itemText(linkedBy: WorktreeLinkKind, item: LinkedItem): string {
  return `${linkWords[linkedBy]} ${item.kind === 'pr' ? 'pull request' : 'issue'} #${String(item.number)}, ${item.state}`
}

type Single = Extract<Confirmation, { repoPath: string }>

// singleWords are the title, button, and tone of each confirmation for one worktree.
const singleWords: Readonly<Record<Single['kind'], { title: string; label: string; tone: 'danger' | 'primary' }>> = {
  remove: { title: 'Remove this worktree?', label: 'Remove', tone: 'danger' },
  force: { title: 'Force remove this worktree?', label: 'Force remove', tone: 'danger' },
  cleanUp: { title: 'Clean up this worktree?', label: 'Clean up', tone: 'primary' },
}

// folderText says what removing or cleaning up a worktree does to its folder, with fresh,
// the worktree counted as its details last counted it (asReadNow), as the sentence over it
// is. Clean up checks the worktree again first, and leaves one whose work isn't finished
// any more, as when its copies on GitHub are gone.
function folderText(kind: Exclude<Single['kind'], 'force'>, fresh: Worktree): string {
  const path = fresh.displayPath

  if (kind === 'remove') {
    return `This deletes the folder ${path}, which has no uncommitted changes.`
  }

  if (fresh.dirtyCount > 0 || unpushedKnown(fresh) > 0 || unpushedUnknown(fresh)) {
    return `Clean up checks it again first, and deletes the folder ${path} only if its work is still finished, which, as last read, it isn’t.`
  }

  const every =
    copiedOf(fresh) === null ? 'every commit on it is on GitHub' : 'the change of every commit on it is on GitHub'

  return `This deletes the folder ${path}, whose work is finished: it has no uncommitted changes, and ${every}.`
}

// listBoxClass frames each list Force remove shows, and listHeadingClass heads it, in
// capitals, its count in the text's color.
const listBoxClass = 'mt-2 rounded-lg border border-border/70 bg-background px-2.5 py-1.5'

const listHeadingClass =
  'mb-1 flex flex-wrap items-center gap-x-1.5 text-caption font-semibold tracking-wide text-muted uppercase'

const countClass = 'tracking-normal text-text tabular-nums'

// Reading says the rest of a list is being read.
function Reading({ text }: { text: string }) {
  return (
    <p role="status" className="mt-1 flex items-center gap-1.5 text-xs text-muted">
      <Spinner className="size-2.25" />
      {text}
    </p>
  )
}

interface ForceListsProps {
  repoPath: string
  worktree: Worktree
  details: WorktreeDetailsState
}

// ForceLists are what Force remove takes and what it leaves: the uncommitted files it
// discards, those its details read (at most 200), else the listing's first 12, and the
// commits that aren't on GitHub (notOnGitHub), which stay on its branch, or, on a detached
// worktree, go with it, each saying where the rest are past the first 200. It reads the
// worktree's details as it opens, which asks again after a read that failed, and again
// after a listing that lands while it's open, once a listing.
function ForceLists({ repoPath, worktree: wt, details }: ForceListsProps) {
  const read = details.of(wt.path)
  const { files, count, current } = changesOf(wt, read)
  const reading = waiting(wt, read)
  const rest = count - files.length
  const commits: readonly WorktreeCommit[] = read?.state === 'read' ? read.details.commits : []
  const unpushed = notOnGitHub(wt, read)
  const detached = detachedOf(wt)
  // unread reports nothing read for this listing: as the dialog opens, unless its drawer
  // has read it, and again once a listing lands while it's open, which drops what was read.
  const unread = read === undefined
  const readAgain = useEffectEvent(() => {
    details.read(repoPath, wt)
  })

  // It reads as it opens, and again each time unread changes: one that turns true is a new
  // listing's, and one that turns false is the read starting, which asking again leaves be.
  useEffect(() => {
    readAgain()
  }, [unread])

  return (
    <>
      <section aria-label="Files it discards" className={listBoxClass}>
        <h3 className={listHeadingClass}>
          Discards <span className={countClass}>{formatCount(count)}</span> {count === 1 ? 'file' : 'files'}
        </h3>
        <ul className="flex max-h-40 flex-col overflow-y-auto text-xs text-text">
          {files.map((file) => (
            <FileLine key={fileKey(file)} file={file} />
          ))}
        </ul>
        {rest > 0 &&
          (!current && reading ? (
            <Reading text={`reading the other ${formatCount(rest)}…`} />
          ) : (
            <p className="mt-1 text-xs text-muted">
              <RestInTerminal listed={files.length} count={count} command="git status" />
            </p>
          ))}
      </section>
      {unpushed > 0 && (
        <section aria-label="Commits not on GitHub" className={listBoxClass}>
          <h3 className={listHeadingClass}>
            {detached ? 'Loses' : 'Keeps'} <span className={countClass}>{formatCount(unpushed)}</span> not on GitHub
            {detached ? (
              ', which no branch holds'
            ) : (
              <>
                , on <code className="font-mono font-medium tracking-normal normal-case">{wt.branch}</code>
              </>
            )}
          </h3>
          {commits.length > 0 && (
            <ul className="flex flex-col text-xs text-text">
              {commits.map((commit) => (
                <CommitLine key={commit.oid} commit={commit} />
              ))}
            </ul>
          )}
          {commits.length > 0 && unpushed > commits.length && (
            <p className="mt-1 text-xs text-muted">
              <RestInTerminal listed={commits.length} count={unpushed} command="git log" />
            </p>
          )}
          {reading && <Reading text="reading them…" />}
        </section>
      )}
    </>
  )
}

// SingleConfirmation confirms removing, force removing, or cleaning up one worktree: on
// top, what it would cost, as the worktree's drawer says it; then, for Force remove, the
// files it discards and the commits it keeps, else what it does to the folder; and a box
// for each of its branches.
function SingleConfirmation({
  confirmation,
  rules,
  details,
  onConfirm,
  onCancel,
  ...cost
}: Omit<ConfirmationDialogProps, 'confirmation'> & { confirmation: Single }) {
  const ticks = useTicks()
  const { kind, repoPath, worktree: wt } = confirmation
  const words = singleWords[kind]
  const boxKey = (name: string) => name
  // fresh is the worktree counted as its details last counted it, which the sentence on
  // top and what it does to the folder both go by.
  const fresh = asReadNow(wt, details.of(wt.path))

  return (
    <ConfirmDialog
      title={words.title}
      confirmLabel={words.label}
      tone={words.tone}
      size="wide"
      onConfirm={() => {
        onConfirm({ branches: chosenBranches(wt, rules, ticks, boxKey) })
      }}
      onCancel={onCancel}
    >
      <SafetyBand
        safety={costOf(fresh, repoPath, cost)}
        className="mb-2.5 rounded-lg border border-border/70 text-text"
      />
      {kind === 'force' ? (
        <ForceLists repoPath={repoPath} worktree={wt} details={details} />
      ) : (
        <p>{folderText(kind, fresh)}</p>
      )}
      {wt.item !== null && <p className="mt-1.5">{itemText(wt.linkedBy, wt.item)}.</p>}
      <div className="-mx-2 mt-3">
        <WorktreeBranches worktree={wt} rules={rules} ticks={ticks} boxKey={boxKey} />
      </div>
    </ConfirmDialog>
  )
}

type Bulk = Extract<Confirmation, { targets: Target[] }>

// bulkWords are the title, intro, verb, and tone of each bulk confirmation.
const bulkWords: Readonly<
  Record<Bulk['kind'], { title: string; intro: string; verb: string; tone: 'danger' | 'primary' }>
> = {
  all: {
    title: 'Remove clean worktrees?',
    intro:
      'Each of these has no uncommitted changes and is unlocked. Choose the worktrees to remove, and the branches to delete with each.',
    verb: 'Remove',
    tone: 'danger',
  },
  finished: {
    title: 'Clean up finished worktrees?',
    intro:
      'The pull request or issue of each of these is finished, and each has no uncommitted changes, and every commit on it, or its change, is on GitHub. Choose the worktrees to clean up, and the branches to delete with each. Each is checked again first.',
    verb: 'Clean up',
    tone: 'primary',
  },
}

// branchKey is the box key of a worktree's branch: the worktree and the branch, which a
// NUL keeps apart. A worktree's own box is keyed by its path alone.
function branchKey(path: string): (name: string) => string {
  return (name) => `${path}\u0000${name}`
}

// BulkConfirmation confirms a bulk action, with a box for each worktree, ticked to start
// with, what removing it would cost in a line under it, and boxes for its branches.
function BulkConfirmation({
  confirmation,
  rules,
  onConfirm,
  onCancel,
  ...cost
}: Omit<ConfirmationDialogProps, 'confirmation'> & { confirmation: Bulk }) {
  const ticks = useTicks()
  const words = bulkWords[confirmation.kind]
  const chosen = confirmation.targets.filter((t) => ticks.on(t.worktree.path, true))

  return (
    <ConfirmDialog
      title={words.title}
      confirmLabel={`${words.verb} ${counted(chosen.length, 'worktree', 'worktrees')}`}
      confirmDisabled={chosen.length === 0}
      tone={words.tone}
      size="wide"
      onConfirm={() => {
        onConfirm({
          picks: chosen.map(({ repoPath, worktree: wt }) => ({
            repoPath,
            path: wt.path,
            branches: chosenBranches(wt, rules, ticks, branchKey(wt.path)),
          })),
        })
      }}
      onCancel={onCancel}
    >
      <p>{words.intro}</p>
      <ul aria-label="Worktrees" className="mt-3 flex max-h-80 flex-col gap-2 overflow-y-auto">
        {confirmation.targets.map(({ repoPath, worktree: wt }) => {
          const on = ticks.on(wt.path, true)

          return (
            <li key={wt.path} className="rounded-md border border-border px-1 py-1">
              <label className="flex cursor-pointer items-start gap-2.5 rounded-md px-2 py-1.5 hover:bg-raised">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) => {
                    ticks.set([wt.path], e.target.checked)
                  }}
                  className="checkbox mt-0.5"
                />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-sm font-medium break-all text-text">{wt.displayPath}</span>
                  {wt.item !== null && <span className="text-xs">{itemText(wt.linkedBy, wt.item)}</span>}
                </span>
              </label>
              <div className="pr-2 pb-1 pl-8.5">
                <SafetyLine safety={costOf(asReadNow(wt, cost.details.of(wt.path)), repoPath, cost)} />
              </div>
              <div className="ml-6">
                <WorktreeBranches worktree={wt} rules={rules} ticks={ticks} boxKey={branchKey(wt.path)} off={!on} />
              </div>
            </li>
          )
        })}
      </ul>
    </ConfirmDialog>
  )
}

// ConfirmationDialog asks before each destructive action, showing what it would cost and
// what it does, and a box for each worktree and branch it would take, ticked as the user's
// rules say. Unticking a box leaves that worktree or branch alone; a branch that can't go
// is shown, held, with why.
export function ConfirmationDialog({ confirmation, details, ...props }: ConfirmationDialogProps) {
  switch (confirmation.kind) {
    case 'merged':
      return (
        <MergedBranchesDialog
          rules={props.rules}
          now={props.now}
          onConfirm={(branchPicks) => {
            props.onConfirm({ branchPicks })
          }}
          onCancel={props.onCancel}
        />
      )
    case 'all':
    case 'finished':
      return <BulkConfirmation confirmation={confirmation} details={details} {...props} />
    default:
      return <SingleConfirmation confirmation={confirmation} details={details} {...props} />
  }
}
