import { useState } from 'react'

import type { BranchKind, BranchRules, Worktree, WorktreeBranch } from '../../platform'

// Ticks are the boxes of a confirmation: each shows its default, from the user's rules,
// until the user changes it.
export interface Ticks {
  // on reports whether the box under key is ticked, given its default.
  on: (key: string, byDefault: boolean) => boolean
  // set ticks or unticks every box under keys.
  set: (keys: readonly string[], on: boolean) => void
}

// useTicks keeps the boxes the user changed, by key. A box the user never touched shows
// its default, so a list that loads after the dialog opens starts from the rules too.
export function useTicks(): Ticks {
  const [changed, setChanged] = useState<ReadonlyMap<string, boolean>>(() => new Map())

  return {
    on: (key, byDefault) => changed.get(key) ?? byDefault,
    set: (keys, on) => {
      setChanged((prev) => {
        const next = new Map(prev)

        for (const key of keys) {
          next.set(key, on)
        }

        return next
      })
    },
  }
}

// ruleFor names the rule that says whether a branch of each kind starts ticked.
const ruleFor: Readonly<Record<BranchKind, 'own' | 'pullRequest' | 'merged'>> = {
  own: 'own',
  pullRequest: 'pullRequest',
  merged: 'merged',
}

// tickedByDefault reports whether a branch starts ticked: it may go, and the rules delete
// its kind.
export function tickedByDefault(branch: WorktreeBranch, rules: BranchRules): boolean {
  return branch.deletable && rules[ruleFor[branch.kind]]
}

// chosenBranches names the branches of wt that are ticked, with key giving each box's key.
export function chosenBranches(
  wt: Worktree,
  rules: BranchRules,
  ticks: Ticks,
  key: (name: string) => string,
): string[] {
  return wt.branches.filter((b) => b.deletable && ticks.on(key(b.name), tickedByDefault(b, rules))).map((b) => b.name)
}

// shortCommit is the abbreviated commit ID people read, as git shows it.
export function shortCommit(oid: string): string {
  return oid.slice(0, 7)
}

// kindText says what a branch is to its worktree.
const kindText: Readonly<Record<BranchKind, string>> = {
  own: 'the branch made for this worktree',
  pullRequest: 'the pull request’s branch, checked out here',
  merged: 'its pull request merged',
}

interface BranchBoxProps {
  branch: WorktreeBranch
  checked: boolean
  onChange: (on: boolean) => void
  // off holds the box, as while its worktree is unticked.
  off?: boolean
}

// BranchBox is a branch's box in a confirmation: what it is, the commit it points at,
// and, when it can't go, the reason it stays.
function BranchBox({ branch, checked, onChange, off = false }: BranchBoxProps) {
  const held = !branch.deletable

  return (
    <label
      className={`flex items-start gap-2.5 rounded-md px-2 py-1.5 ${held || off ? 'cursor-default' : 'cursor-pointer hover:bg-raised'} ${held ? 'opacity-75' : ''}`}
    >
      <input
        type="checkbox"
        checked={!held && checked}
        disabled={held || off}
        onChange={(e) => {
          onChange(e.target.checked)
        }}
        className="mt-1 accent-accent"
      />
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm text-text">
          Delete branch <code className="font-mono break-all">{branch.name}</code>
        </span>
        <span className="text-xs">
          {kindText[branch.kind]} · at <code className="font-mono">{shortCommit(branch.head)}</code>
        </span>
        {held && <span className="text-xs text-warning">Stays: {branch.reason}.</span>}
      </span>
    </label>
  )
}

// noBranchText says why removing wt deletes no branch: it has none, or its branch is not
// one the app deletes.
function noBranchText(wt: Worktree): string {
  if (wt.detached || wt.branch === '') {
    return 'It has no branch.'
  }

  return `Its branch ${wt.branch} stays: it is neither the branch made for this worktree nor its pull request’s.`
}

export interface WorktreeBranchesProps {
  worktree: Worktree
  rules: BranchRules
  ticks: Ticks
  // key gives the box key of each of the worktree's branches.
  boxKey: (name: string) => string
  // off holds every box, as while the worktree is unticked.
  off?: boolean
}

// WorktreeBranches lists a worktree's branches as boxes, each ticked as the rules say
// until the user changes it.
export function WorktreeBranches({ worktree: wt, rules, ticks, boxKey, off = false }: WorktreeBranchesProps) {
  if (wt.branches.length === 0) {
    return <p className="px-2 text-xs">{noBranchText(wt)}</p>
  }

  return (
    <ul aria-label={`Branches of ${wt.displayPath}`} className="flex flex-col">
      {wt.branches.map((b) => (
        <li key={b.name}>
          <BranchBox
            branch={b}
            checked={ticks.on(boxKey(b.name), tickedByDefault(b, rules))}
            off={off}
            onChange={(on) => {
              ticks.set([boxKey(b.name)], on)
            }}
          />
        </li>
      ))}
    </ul>
  )
}
