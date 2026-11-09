import { counted } from '../../lib/format'
import { formatAgo } from '../../lib/time'
import { useQuery } from '../../lib/useQuery'
import { mergedBranches, type BranchPick, type BranchRules, type MergedBranch } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass } from '../controls'
import { BranchBox, useTicks } from './BranchChoices'

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

// RepoGroup is the merged branches of one clone.
interface RepoGroup {
  repoPath: string
  displayRepo: string
  branches: MergedBranch[]
}

// byRepo groups the merged branches by clone, keeping their order: the backend sorts them
// by clone, then name.
function byRepo(found: readonly MergedBranch[]): RepoGroup[] {
  const groups: RepoGroup[] = []

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

// MergedBranchesDialog asks which of the clones' local branches to delete whose pull
// requests merged. It looks them up itself when it opens: the backend lists each clone's
// branches and asks GitHub about them in batches of fifty names. Each box starts
// ticked as the user's rules say, and a branch that can't go is shown, held, with why.
export function MergedBranchesDialog({ rules, now, onConfirm, onCancel }: MergedBranchesDialogProps) {
  const found = useQuery(() => mergedBranches(), [])
  const ticks = useTicks()
  const branches = found.data ?? []
  const deletable = branches.filter((m) => m.branch.deletable)
  const chosen = deletable.filter((m) => ticks.on(boxKey(m), rules.merged))

  return (
    <ConfirmDialog
      title="Delete merged branches?"
      confirmLabel={`Delete ${counted(chosen.length, 'branch', 'branches')}`}
      confirmDisabled={chosen.length === 0}
      wide
      onConfirm={() => {
        onConfirm(chosen.map((m) => ({ repoPath: m.repoPath, name: m.branch.name, head: m.branch.head })))
      }}
      onCancel={onCancel}
    >
      {found.error !== '' ? (
        <p role="alert">Couldn’t check the branches: {found.error}</p>
      ) : found.data === undefined ? (
        <p role="status">Checking your clones’ branches against GitHub…</p>
      ) : branches.length === 0 ? (
        <p>
          No local branch in your clones belongs to a merged pull request. A protected branch, and each repository’s
          default branch, is never offered.
        </p>
      ) : (
        <>
          <p>
            These local branches belong to pull requests that merged. Each is deleted only while it still points at the
            commit shown, and the results list each commit, so a branch can be brought back.
          </p>
          {deletable.length > 1 && (
            <div className="mt-2 flex gap-1">
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
            </div>
          )}
          <div className="mt-2 flex max-h-80 flex-col gap-3 overflow-y-auto">
            {byRepo(branches).map((group) => (
              <section key={group.repoPath} aria-label={group.displayRepo}>
                <h3 className="px-2 text-xs font-medium text-text">{group.displayRepo}</h3>
                <ul className="flex flex-col">
                  {group.branches.map((m) => {
                    const pr = m.pullRequest

                    return (
                      <li key={m.branch.name}>
                        <BranchBox
                          branch={m.branch}
                          checked={ticks.on(boxKey(m), rules.merged)}
                          onChange={(on) => {
                            ticks.set([boxKey(m)], on)
                          }}
                          detail={
                            <span title={`${pr.repo}#${String(pr.number)}: ${pr.title}`}>
                              #{pr.number} {pr.title} · merged{' '}
                              <time dateTime={pr.mergedAt}>{formatAgo(now, pr.mergedAt)}</time>
                            </span>
                          }
                        />
                      </li>
                    )
                  })}
                </ul>
              </section>
            ))}
          </div>
        </>
      )}
    </ConfirmDialog>
  )
}
