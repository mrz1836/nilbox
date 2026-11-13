import type { AgentInfo, AgentSession, Worktree } from '../platform'
import { WorktreeTag } from './ItemRow'
import { SessionTag } from './SessionTag'

export interface WorkLineProps {
  // worktree is the item's worktree, as the Worktrees view last listed it, if it has one.
  worktree: Worktree | undefined
  // sessions are the item's agent sessions, newest first.
  sessions: readonly AgentSession[] | undefined
  agents: readonly AgentInfo[]
}

// WorkLine is a sheet's work line, under where its item stands: the item's worktree tag
// and its agent session's tag, which opens the item's sessions. The tag drops its own
// worktree mark beside the worktree tag, and its panel opens from the right, since both
// sheets show the line in their right column. A sheet puts it outside its scroller, so it
// stays in view and its panel isn't cut off. An item with neither shows nothing.
export function WorkLine({ worktree, sessions, agents }: WorkLineProps) {
  if (worktree === undefined && (sessions === undefined || sessions.length === 0)) {
    return null
  }

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-muted">
      {worktree !== undefined && <WorktreeTag wt={worktree} agents={agents} />}
      {sessions !== undefined && (
        <SessionTag sessions={sessions} agents={agents} markWorktree={worktree === undefined} align="right" />
      )}
    </div>
  )
}
