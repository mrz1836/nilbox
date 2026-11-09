import type { Choice, Confirmation, Target } from '../../lib/useWorktrees'
import type { BranchRules, LinkedItem, Worktree } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { chosenBranches, useTicks, WorktreeBranches } from './BranchChoices'
import { MergedBranchesDialog } from './MergedBranchesDialog'
import { plural } from './shared'

export interface ConfirmationDialogProps {
  confirmation: Confirmation
  // rules are the user's branch rules, which tick each branch's box to start with.
  rules: BranchRules
  // now is what ages are measured against.
  now: Date
  onConfirm: (choice: Choice) => void
  onCancel: () => void
}

// itemText names the item a worktree was made for and where it stands, as in "pull
// request #7, merged".
function itemText(item: LinkedItem): string {
  return `${item.kind === 'pr' ? 'pull request' : 'issue'} #${String(item.number)}, ${item.state}`
}

type Single = Extract<Confirmation, { repoPath: string }>

// singleWords are the title, button, and tone of each confirmation for one worktree.
const singleWords: Readonly<Record<Single['kind'], { title: string; label: string; tone: 'danger' | 'primary' }>> = {
  remove: { title: 'Remove this worktree?', label: 'Remove', tone: 'danger' },
  force: { title: 'Force remove this worktree?', label: 'Force remove', tone: 'danger' },
  cleanUp: { title: 'Clean up this worktree?', label: 'Clean up', tone: 'primary' },
}

// folderText says what the action does to the worktree's folder.
function folderText(kind: Single['kind'], wt: Worktree): string {
  switch (kind) {
    case 'remove':
      return `This deletes the folder ${wt.displayPath}, which has no uncommitted changes.`
    case 'force':
      return `${wt.displayPath} has ${plural(wt.dirtyCount, 'uncommitted file')}, counting untracked files. Force removing it discards them for good.`
    default:
      return `This deletes the folder ${wt.displayPath}, whose work is finished: it has no uncommitted changes, and every commit on it is on GitHub.`
  }
}

// SingleConfirmation confirms removing, force removing, or cleaning up one worktree, with
// a box for each of its branches.
function SingleConfirmation({
  confirmation,
  rules,
  onConfirm,
  onCancel,
}: Omit<ConfirmationDialogProps, 'confirmation' | 'now'> & { confirmation: Single }) {
  const ticks = useTicks()
  const wt = confirmation.worktree
  const words = singleWords[confirmation.kind]
  const boxKey = (name: string) => name

  return (
    <ConfirmDialog
      title={words.title}
      confirmLabel={words.label}
      tone={words.tone}
      onConfirm={() => {
        onConfirm({ branches: chosenBranches(wt, rules, ticks, boxKey) })
      }}
      onCancel={onCancel}
    >
      <p>{folderText(confirmation.kind, wt)}</p>
      {wt.item !== null && <p className="mt-1">Made for {itemText(wt.item)}.</p>}
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
      'The pull request or issue of each of these is finished, and each has no uncommitted changes and every commit on GitHub. Choose the worktrees to clean up, and the branches to delete with each. Each is checked again first.',
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
// with, and boxes for each one's branches under it.
function BulkConfirmation({
  confirmation,
  rules,
  onConfirm,
  onCancel,
}: Omit<ConfirmationDialogProps, 'confirmation' | 'now'> & { confirmation: Bulk }) {
  const ticks = useTicks()
  const words = bulkWords[confirmation.kind]
  const chosen = confirmation.targets.filter((t) => ticks.on(t.worktree.path, true))

  return (
    <ConfirmDialog
      title={words.title}
      confirmLabel={`${words.verb} ${plural(chosen.length, 'worktree')}`}
      confirmDisabled={chosen.length === 0}
      tone={words.tone}
      wide
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
        {confirmation.targets.map(({ worktree: wt }) => {
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
                  className="mt-1 accent-accent"
                />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-sm font-medium break-all text-text">{wt.displayPath}</span>
                  {wt.item !== null && <span className="text-xs">{itemText(wt.item)}</span>}
                </span>
              </label>
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

// ConfirmationDialog asks before each destructive action, showing what it does and a box
// for each worktree and branch it would take, ticked as the user's rules say. Unticking a
// box leaves that worktree or branch alone; a branch that can't go is shown, held, with
// why.
export function ConfirmationDialog({ confirmation, rules, now, onConfirm, onCancel }: ConfirmationDialogProps) {
  switch (confirmation.kind) {
    case 'merged':
      return (
        <MergedBranchesDialog
          rules={rules}
          now={now}
          onConfirm={(branchPicks) => {
            onConfirm({ branchPicks })
          }}
          onCancel={onCancel}
        />
      )
    case 'all':
    case 'finished':
      return <BulkConfirmation confirmation={confirmation} rules={rules} onConfirm={onConfirm} onCancel={onCancel} />
    default:
      return <SingleConfirmation confirmation={confirmation} rules={rules} onConfirm={onConfirm} onCancel={onCancel} />
  }
}
