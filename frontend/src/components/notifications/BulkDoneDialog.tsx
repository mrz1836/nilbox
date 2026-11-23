import { useState } from 'react'

import { brand } from '../../generated/brand'
import { counted, formatCount } from '../../lib/format'
import {
  aboutText,
  bulkAges,
  bulkKeepsText,
  bulkMinutes,
  bulkRepos,
  doneURL,
  type BulkRepo,
} from '../../lib/githubThreads'
import { reposNamed } from '../../lib/notificationFilter'
import { useNow } from '../../lib/useNow'
import { openExternal, type GitHubBulkAge, type GitHubBulkRequest, type GitHubThread } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { linkClass, quietButtonClass, Segmented } from '../controls'
import { ExternalIcon } from '../icons'
import { useTicks, type Ticks } from '../worktrees/BranchChoices'

// ageOptions are the Older than control's options.
const ageOptions = bulkAges.map((a) => ({ value: a.value, label: a.label }))

export interface BulkDoneDialogProps {
  // threads are every notification the GitHub tab lists, which the dialog counts by
  // repository: it opens at once, asking nothing of the backend.
  threads: readonly GitHubThread[]
  // truncated is whether GitHub holds more than the newest the app reads.
  truncated: boolean
  // query is the toolbar's field: when it names repositories (repo:), only those start
  // ticked.
  query: string
  // perMinute and perHour are the bulk lane's pace, from which the dialog words how long
  // the marks take.
  perMinute: number
  perHour: number
  // busy is the start under way, and error why it failed, or '': the dialog stays open.
  busy: boolean
  error: string
  onConfirm: (req: GitHubBulkRequest) => void
  onCancel: () => void
}

interface RepoRowProps {
  row: BulkRepo
  checked: boolean
  onChange: (on: boolean) => void
}

// RepoRow is one repository the dialog offers: its box, its name, and how many of its
// notifications are that old, and unread.
function RepoRow({ row, checked, onChange }: RepoRowProps) {
  return (
    <li>
      <label className="flex cursor-pointer items-center gap-2.5 px-2 py-1.5">
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => {
            onChange(e.target.checked)
          }}
          className="checkbox"
        />
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-text">{row.repo}</span>
        <span className="shrink-0 text-xs whitespace-nowrap tabular-nums">
          {counted(row.count, 'notification', 'notifications')}
          {row.unread > 0 && ` · ${formatCount(row.unread)} unread`}
        </span>
      </label>
    </li>
  )
}

interface WhatHappensProps {
  count: number
  age: GitHubBulkAge
  truncated: boolean
  perMinute: number
  perHour: number
}

// WhatHappens says, before the user confirms, how marking them goes: in the background,
// for how long, what stays, and that there's no undo but GitHub's own.
function WhatHappens({ count, age, truncated, perMinute, perHour }: WhatHappensProps) {
  return (
    <div className="mt-3 rounded-md border border-border px-2.5 py-2 text-xs">
      <p className="font-medium text-text">What happens</p>
      <p className="mt-0.5">
        GitHub takes these one at a time, so this runs in the background:{' '}
        {aboutText(bulkMinutes(count, perMinute, perHour))}. Keep working, or quit: it picks up where it left off.{' '}
        {bulkKeepsText(age)}
      </p>
      {truncated && (
        <p className="mt-1">
          GitHub holds more than the newest 1,000 that {brand.name} shows. Older ones in the ticked repositories are
          marked done too, so the final count may be higher.
        </p>
      )}
      <p className="mt-1">
        There’s no undo here.{' '}
        <button
          type="button"
          onClick={() => {
            openExternal(doneURL)
          }}
          className={`inline-flex items-center gap-1 ${linkClass}`}
        >
          GitHub’s Done view
          <ExternalIcon className="size-3" />
        </button>{' '}
        can move any back.
      </p>
    </div>
  )
}

// tickedOf is the rows ticked, each box starting as starts says.
function tickedOf(rows: readonly BulkRepo[], ticks: Ticks, starts: (repo: string) => boolean): BulkRepo[] {
  return rows.filter((r) => ticks.on(r.repo, starts(r.repo)))
}

// BulkDoneDialog asks which of the user's GitHub notifications to mark done in bulk: those
// older than an age, in the repositories ticked. It counts them from the list the tab
// holds, by repository, the noisiest first, every one ticked to start with, or those the
// toolbar's field names. A repository with none at the age chosen leaves the list, its box
// remembered for when it comes back. Ticking every repository marks every notification
// that old, those past the newest the app reads included; otherwise, those of the
// repositories ticked.
export function BulkDoneDialog({
  threads,
  truncated,
  query,
  perMinute,
  perHour,
  busy,
  error,
  onConfirm,
  onCancel,
}: BulkDoneDialogProps) {
  const now = useNow()
  const [age, setAge] = useState<GitHubBulkAge>('any')
  const ticks = useTicks()
  // everyRepo is every repository the tab lists, of any age: ticking all of them marks
  // everything.
  const everyRepo = bulkRepos(threads, 'any', now)
  const named = reposNamed(
    query,
    everyRepo.map((r) => r.repo),
  )
  const starts = (repo: string) => named === null || named.has(repo)
  const rows = bulkRepos(threads, age, now)
  const ticked = tickedOf(rows, ticks, starts)
  const total = rows.reduce((n, r) => n + r.count, 0)
  const chosen = ticked.reduce((n, r) => n + r.count, 0)
  const all = tickedOf(everyRepo, ticks, starts).length === everyRepo.length

  const confirm = () => {
    onConfirm(all ? { age, repos: [], all: true } : { age, repos: ticked.map((r) => r.repo), all: false })
  }

  return (
    <ConfirmDialog
      title="Mark notifications done on GitHub?"
      confirmLabel={`Mark ${formatCount(chosen)} done`}
      confirmDisabled={chosen === 0}
      busy={busy}
      size="large"
      onConfirm={confirm}
      onCancel={onCancel}
    >
      <p>
        Marking one done takes it out of your GitHub inbox, as GitHub’s own Mark as done does. Any with new activity
        come back on their own.
      </p>
      <div className="mt-3 flex items-center gap-2">
        <span className="text-xs text-text">Older than</span>
        <Segmented label="Older than" size="sm" value={age} options={ageOptions} onChange={setAge} />
      </div>
      {rows.length === 0 ? (
        <p className="mt-3">None of your notifications are that old.</p>
      ) : (
        <>
          <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="text-xs text-text tabular-nums">
              {counted(total, 'notification', 'notifications')} in {counted(rows.length, 'repository', 'repositories')}{' '}
              · {formatCount(chosen)} ticked
            </span>
            <span className="ml-auto flex gap-1">
              <button
                type="button"
                className={quietButtonClass}
                onClick={() => {
                  ticks.set(
                    rows.map((r) => r.repo),
                    true,
                  )
                }}
              >
                Select all
              </button>
              <button
                type="button"
                className={quietButtonClass}
                onClick={() => {
                  ticks.set(
                    rows.map((r) => r.repo),
                    false,
                  )
                }}
              >
                Select none
              </button>
            </span>
          </div>
          <ul
            aria-label="Repositories"
            className="mt-2 max-h-[55vh] divide-y divide-border overflow-y-auto rounded-md border border-border"
          >
            {rows.map((row) => (
              <RepoRow
                key={row.repo}
                row={row}
                checked={ticks.on(row.repo, starts(row.repo))}
                onChange={(on) => {
                  ticks.set([row.repo], on)
                }}
              />
            ))}
          </ul>
        </>
      )}
      <WhatHappens count={chosen} age={age} truncated={truncated} perMinute={perMinute} perHour={perHour} />
      {error !== '' && (
        <p role="alert" className="mt-2 text-danger">
          Couldn’t start marking them done: {error}
        </p>
      )}
    </ConfirmDialog>
  )
}
