import { use } from 'react'

import { agentRunOf, sheetLine } from '../lib/runView'
import type { AgentInfo, AgentSession, Worktree } from '../platform'
import { AutopilotRowsContext } from './autopilot/AutopilotChip'
import { AutopilotIcon } from './icons'
import { WorktreeTag } from './ItemRow'
import { SessionTag } from './SessionTag'

export interface WorkLineProps {
  // worktree is the item's worktree, as the Worktrees view last listed it, if it has one.
  worktree: Worktree | undefined
  // sessions are the item's agent sessions, newest first.
  sessions: readonly AgentSession[] | undefined
  agents: readonly AgentInfo[]
  // itemId is the item the sheet shows, whose newest run of a routine's agent the line
  // under it names; 0 names none.
  itemId?: number
}

// AutopilotLine is a sheet's one line of Autopilot, under its work line: what the newest
// background run a routine made on the item came to ("⚡ Fix failing checks pushed
// 4f2a9c1 · checks pass"), linking to the run's view in the Autopilot tab. The run's
// summary is its session's last words, which the sheet's agent verdict shows. Without the
// rows' Autopilot, as while Autopilot (beta) is off, or with no such run, it shows nothing.
function AutopilotLine({ itemId }: { itemId: number }) {
  const rows = use(AutopilotRowsContext)
  const match = itemId > 0 ? rows?.chips.get(itemId) : undefined

  if (rows === null || match === undefined) {
    return null
  }

  const run = agentRunOf(match)
  const line = sheetLine(match)

  if (run === undefined || line === '') {
    return null
  }

  const { onShow } = rows

  return (
    <p className="flex min-w-0 items-center gap-1.5 text-xs text-text">
      <AutopilotIcon className="size-3.5 shrink-0 text-accent" />
      {onShow === undefined ? (
        <span className="min-w-0 truncate">{line}</span>
      ) : (
        <button
          type="button"
          onClick={() => {
            onShow({
              routineId: match.routineId,
              matchId: match.id,
              runId: run.id,
              createdAt: match.createdAt,
              activity: false,
            })
          }}
          title="Show the run in the Autopilot tab"
          className="min-w-0 truncate text-left hover:underline"
        >
          {line}
        </button>
      )}
    </p>
  )
}

// WorkLine is a sheet's work line, under where its item stands: the item's worktree tag
// and its agent session's tag, which opens the item's sessions, and under them the line
// Autopilot says of the item's newest background run. The tag drops its own worktree mark
// beside the worktree tag, and its panel opens from the right, since both sheets show the
// line in their right column. A sheet puts it outside its scroller, so it stays in view and
// its panel isn't cut off. An item with neither shows nothing.
export function WorkLine({ worktree, sessions, agents, itemId = 0 }: WorkLineProps) {
  const tags = worktree !== undefined || (sessions !== undefined && sessions.length > 0)

  return (
    <>
      {tags && (
        <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-muted">
          {worktree !== undefined && <WorktreeTag wt={worktree} agents={agents} />}
          {sessions !== undefined && (
            <SessionTag sessions={sessions} agents={agents} markWorktree={worktree === undefined} align="right" />
          )}
        </div>
      )}
      <AutopilotLine itemId={itemId} />
    </>
  )
}
