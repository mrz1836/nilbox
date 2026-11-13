import { useId, useState } from 'react'

import { counted } from '../../lib/format'
import { shortCommit } from '../../lib/git'
import { formatAgo } from '../../lib/time'
import { useQuery } from '../../lib/useQuery'
import { mergedBranches, type BranchPick, type BranchRules, type MergedBranch } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass } from '../controls'
import { AlertIcon, ChevronDownIcon } from '../icons'
import { Spinner } from '../Spinner'
import { useTicks, type Ticks } from './BranchChoices'

export interface MergedBranchesDialogProps {
  rules: BranchRules
  // now is what the merge ages are measured against.
  now: Date
  onConfirm: (picks: BranchPick[]) => void
  onCancel: () => void
}

// boxKey is a merged branch's box key: its clone and name, which a NUL keeps apart.
function boxKey(m: MergedBranch): string {
  return `${m.repoPath}\u0000${m.branch.name}`
}

// unpushedHeld reports a branch kept only for its commits not on GitHub, which the user may
// delete anyway once they ask, and confirm (MergedBranch.unpushed).
function unpushedHeld(m: MergedBranch): boolean {
  return !m.branch.deletable && m.unpushed !== 0
}

// unpushedWords counts a branch's commits not on GitHub, or says "commits" when they
// couldn't be counted.
function unpushedWords(n: number): string {
  return n > 0 ? counted(n, 'commit', 'commits') : 'commits'
}

// Choice is which branches the user may tick, and which start ticked: those that may go,
// as the rules say, and, once the user asks, those kept only for commits not on GitHub,
// never ticked to start with.
interface Choice {
  tickable: (m: MergedBranch) => boolean
  byDefault: (m: MergedBranch) => boolean
}

// CloneGroup is the merged branches of one clone.
interface CloneGroup {
  repoPath: string
  displayRepo: string
  branches: MergedBranch[]
}

// byClone groups the merged branches by clone, keeping their order: the backend sorts
// them by clone, then name.
function byClone(found: readonly MergedBranch[]): CloneGroup[] {
  const groups: CloneGroup[] = []

  for (const m of found) {
    const last = groups.at(-1)

    if (last?.repoPath === m.repoPath) {
      last.branches.push(m)
    } else {
      groups.push({ repoPath: m.repoPath, displayRepo: m.displayRepo, branches: [m] })
    }
  }

  return groups
}

// Warning is the small warning sign by a clone or branch that keeps a branch, with what
// it means as its name and tooltip.
function Warning({ label }: { label: string }) {
  return (
    <span role="img" aria-label={label} title={label} className="shrink-0 text-warning">
      <AlertIcon className="size-3.5" />
    </span>
  )
}

interface MergedRowProps {
  merged: MergedBranch
  // tickable is whether the user may tick it.
  tickable: boolean
  checked: boolean
  now: Date
  onChange: (on: boolean) => void
}

// MergedRow is one merged branch: its box, name, when its pull request merged and the
// commit it points at, the pull request, and, when it can't go, why, or, offered anyway,
// what deleting it loses.
function MergedRow({ merged: m, tickable, checked, now, onChange }: MergedRowProps) {
  const pr = m.pullRequest
  const held = !tickable

  return (
    <li>
      <label
        className={`flex items-start gap-2.5 rounded-md px-2 py-1 ${held ? 'cursor-default' : 'cursor-pointer hover:bg-raised'}`}
      >
        <input
          type="checkbox"
          checked={!held && checked}
          disabled={held}
          onChange={(e) => {
            onChange(e.target.checked)
          }}
          className="checkbox mt-0.5"
        />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex items-center gap-2">
            <code className={`min-w-0 truncate font-mono text-[0.8125rem] ${held ? 'text-muted' : 'text-text'}`}>
              {m.branch.name}
            </code>
            {held && <Warning label="Can’t be deleted" />}
            <span className="ml-auto shrink-0 text-xs whitespace-nowrap">
              merged <time dateTime={pr.mergedAt}>{formatAgo(now, pr.mergedAt)}</time> · at{' '}
              <code className="font-mono">{shortCommit(m.branch.head)}</code>
            </span>
          </span>
          <span className="truncate text-xs" title={`${pr.repo}#${String(pr.number)}: ${pr.title}`}>
            #{pr.number} {pr.title}
          </span>
          {held && <span className="text-xs text-warning">Stays: {m.branch.reason}.</span>}
          {!held && unpushedHeld(m) && (
            <span className="text-xs text-warning">
              Has {unpushedWords(m.unpushed)} not on GitHub, which deleting it leaves only in this clone.
            </span>
          )}
        </span>
      </label>
    </li>
  )
}

interface CloneRowProps {
  group: CloneGroup
  choice: Choice
  ticks: Ticks
  now: Date
  open: boolean
  onToggle: () => void
}

// CloneRow is one clone's merged branches: a box that ticks them all, its name, which
// opens the list, how many it has and how many are ticked, a warning when some can't go,
// and, open, a row for each branch.
function CloneRow({ group, choice, ticks, now, open, onToggle }: CloneRowProps) {
  const id = useId()
  const deletable = group.branches.filter(choice.tickable)
  const ticked = deletable.filter((m) => ticks.on(boxKey(m), choice.byDefault(m))).length
  const held = group.branches.length - deletable.length

  return (
    <li>
      <div className="flex items-center gap-2.5 px-2 py-1.5">
        <input
          type="checkbox"
          aria-label={`Every branch in ${group.displayRepo}`}
          checked={ticked > 0 && ticked === deletable.length}
          disabled={deletable.length === 0}
          ref={(box) => {
            if (box !== null) {
              box.indeterminate = ticked > 0 && ticked < deletable.length
            }
          }}
          onChange={(e) => {
            ticks.set(deletable.map(boxKey), e.target.checked)
          }}
          className="checkbox"
        />
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`${id}-branches`}
          onClick={onToggle}
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md text-left text-sm font-medium text-text hover:underline"
        >
          <ChevronDownIcon
            className={`size-3.5 shrink-0 text-muted transition-transform ${open ? '' : '-rotate-90'}`}
          />
          <span className="truncate">{group.displayRepo}</span>
        </button>
        {held > 0 && <Warning label={`${counted(held, 'branch', 'branches')} can’t be deleted`} />}
        <span className="shrink-0 text-xs whitespace-nowrap tabular-nums">
          {counted(group.branches.length, 'branch', 'branches')} · {String(ticked)} ticked
        </span>
      </div>
      {open && (
        <ul id={`${id}-branches`} aria-label={`Branches in ${group.displayRepo}`} className="pb-1.5 pl-6">
          {group.branches.map((m) => (
            <MergedRow
              key={m.branch.name}
              merged={m}
              tickable={choice.tickable(m)}
              checked={ticks.on(boxKey(m), choice.byDefault(m))}
              now={now}
              onChange={(on) => {
                ticks.set([boxKey(m)], on)
              }}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

// skeletonRows are the widths of the placeholder clone rows shown while the branches
// load, varied so the list reads as a list.
const skeletonRows = ['w-48', 'w-36', 'w-56', 'w-40', 'w-44'] as const

// Checking stands in for the list while the backend reads every clone and asks GitHub: a
// spinner with what is happening, over placeholder rows shaped like the list to come, a
// highlight sweeping down them one after another.
function Checking() {
  return (
    <>
      <div role="status" className="flex items-start gap-2.5">
        <Spinner className="mt-0.5 size-4" />
        <div>
          <p className="text-text">Checking your clones’ branches against GitHub…</p>
          <p className="mt-0.5 text-xs">
            Each clone’s local branches are listed, then GitHub is asked which of their pull requests merged.
          </p>
        </div>
      </div>
      <ul aria-hidden="true" className="mt-3 divide-y divide-border rounded-md border border-border">
        {skeletonRows.map((width, i) => (
          <li key={width} className="flex items-center gap-2.5 px-2 py-2.5">
            <span className="skeleton size-4" style={{ animationDelay: `${String(i * 120)}ms` }} />
            <span className={`skeleton h-3 ${width}`} style={{ animationDelay: `${String(i * 120)}ms` }} />
            <span className="skeleton ml-auto h-3 w-24" style={{ animationDelay: `${String(i * 120)}ms` }} />
          </li>
        ))}
      </ul>
    </>
  )
}

// MergedBranchesDialog asks which of the clones' local branches to delete whose pull
// requests merged. It looks them up itself when it opens: the backend lists each clone's
// branches and asks GitHub about them in batches of fifty names. The branches are grouped
// by clone, each group closed to start with and ticked as a whole or branch by branch,
// every box starting as the user's rules say. A branch that can't go is shown, held, with
// why, and its clone carries a warning.
//
// A branch kept only for its commits not on GitHub can be deleted anyway: the user asks
// for those branches to be offered, ticks them, none ticked to start with, and confirms
// once more in a second dialog that names each and what it holds. A branch checked out,
// protected, or a remote's default never can.
export function MergedBranchesDialog({ rules, now, onConfirm, onCancel }: MergedBranchesDialogProps) {
  const found = useQuery(() => mergedBranches(), [])
  const ticks = useTicks()
  // open holds the clones whose branches are shown; every clone starts closed.
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  // anyway offers the branches kept only for commits not on GitHub, and confirming asks
  // once more before deleting any.
  const [anyway, setAnyway] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const branches = found.data ?? []
  const choice: Choice = {
    tickable: (m) => m.branch.deletable || (anyway && unpushedHeld(m)),
    byDefault: (m) => m.branch.deletable && rules.merged,
  }
  const deletable = branches.filter(choice.tickable)
  const chosen = deletable.filter((m) => ticks.on(boxKey(m), choice.byDefault(m)))
  const risky = chosen.filter(unpushedHeld)
  const unpushed = branches.filter(unpushedHeld)
  const groups = byClone(branches)
  const held = branches.length - deletable.length
  const picks = chosen.map((m) => ({ repoPath: m.repoPath, name: m.branch.name, head: m.branch.head }))

  return (
    <>
      <ConfirmDialog
        title="Delete merged branches?"
        confirmLabel={
          found.data === undefined ? 'Delete branches' : `Delete ${counted(chosen.length, 'branch', 'branches')}`
        }
        confirmDisabled={chosen.length === 0}
        size="large"
        onConfirm={() => {
          if (risky.length > 0) {
            setConfirming(true)
          } else {
            onConfirm(picks)
          }
        }}
        onCancel={onCancel}
      >
        {found.error !== '' ? (
          <p role="alert">Couldn’t check the branches: {found.error}</p>
        ) : found.data === undefined ? (
          <Checking />
        ) : branches.length === 0 ? (
          <p>
            No local branch in your clones belongs to a merged pull request. A protected branch, and each repository’s
            default branch, is never offered.
          </p>
        ) : (
          <>
            <p>
              These local branches belong to pull requests that merged. Each is deleted only while it still points at
              the commit shown, and the results list each commit, so a branch can be brought back.
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="text-xs text-text">
                {counted(branches.length, 'branch', 'branches')} in {counted(groups.length, 'clone', 'clones')} ·{' '}
                {String(chosen.length)} ticked
                {held > 0 && ` · ${String(held)} can’t be deleted`}
              </span>
              <span className="ml-auto flex gap-1">
                <button
                  type="button"
                  className={quietButtonClass}
                  onClick={() => {
                    ticks.set(deletable.map(boxKey), true)
                  }}
                >
                  Select all
                </button>
                <button
                  type="button"
                  className={quietButtonClass}
                  onClick={() => {
                    ticks.set(deletable.map(boxKey), false)
                  }}
                >
                  Select none
                </button>
              </span>
            </div>
            {unpushed.length > 0 && (
              <label className="mt-2 flex cursor-pointer items-start gap-2.5 rounded-md border border-warning/40 px-2.5 py-2 text-xs">
                <input
                  type="checkbox"
                  checked={anyway}
                  onChange={(e) => {
                    setAnyway(e.target.checked)
                    ticks.set(unpushed.map(boxKey), false)
                  }}
                  className="checkbox mt-px"
                />
                <span>
                  <span className="font-medium text-text">
                    Also offer the {counted(unpushed.length, 'branch', 'branches')} with commits not on GitHub
                  </span>{' '}
                  Deleting one leaves its commits only in its clone, until git cleans them away. You’re asked again
                  before any is deleted.
                </span>
              </label>
            )}
            <ul
              aria-label="Clones"
              className="mt-2 max-h-[55vh] divide-y divide-border overflow-y-auto rounded-md border border-border"
            >
              {groups.map((group) => (
                <CloneRow
                  key={group.repoPath}
                  group={group}
                  choice={choice}
                  ticks={ticks}
                  now={now}
                  open={open.has(group.repoPath)}
                  onToggle={() => {
                    setOpen((was) => {
                      const next = new Set(was)

                      if (!next.delete(group.repoPath)) {
                        next.add(group.repoPath)
                      }

                      return next
                    })
                  }}
                />
              ))}
            </ul>
          </>
        )}
      </ConfirmDialog>
      {confirming && (
        <ConfirmDialog
          title={`Delete ${counted(risky.length, 'branch', 'branches')} with commits not on GitHub?`}
          confirmLabel={`Delete ${counted(chosen.length, 'branch', 'branches')}`}
          cancelLabel="Go back"
          size="wide"
          onConfirm={() => {
            setConfirming(false)
            onConfirm(picks)
          }}
          onCancel={() => {
            setConfirming(false)
          }}
        >
          <p>
            Their commits aren’t in their pull requests or anywhere on GitHub. Once a branch is deleted, its commits are
            left only in its clone, until git cleans them away. Each is deleted only while it still points at the commit
            shown, and the results list that commit, which brings the branch back while git still has it.
          </p>
          <ul aria-label="Branches with commits not on GitHub" className="mt-2 max-h-48 space-y-0.5 overflow-y-auto">
            {risky.map((m) => (
              <li key={boxKey(m)} className="flex items-baseline gap-2 text-xs">
                <span className="truncate">{m.displayRepo}</span>
                <code className="truncate font-mono text-text">{m.branch.name}</code>
                <span className="ml-auto shrink-0 whitespace-nowrap text-warning">
                  {unpushedWords(m.unpushed)} not on GitHub
                </span>
              </li>
            ))}
          </ul>
        </ConfirmDialog>
      )}
    </>
  )
}
