import { useId, type RefObject } from 'react'

import { closeNote, issueHeldBack } from '../../lib/issues'
import type { IssueDraft } from '../../lib/issueDrafts'
import { runningSession } from '../../lib/sessions'
import type { AgentSession, InboxItem, Issue, IssueAction, ItemResolution } from '../../platform'
import { fieldClass } from '../controls'
import type { LaunchOptions } from '../ItemRow'
import { LaunchControls } from '../LaunchControls'
import type { MenuItem } from '../Menu'
import { ActionButton } from '../review/ActionButton'

export interface IssueComposerProps {
  issue: Issue
  item: InboxItem | undefined
  draft: IssueDraft
  onDraft: (change: (draft: IssueDraft) => IssueDraft) => void
  // selected is the action the button runs.
  selected: IssueAction
  onRun: (action: IssueAction) => void
  busy: boolean
  error: string
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  // onUnassign takes the user off the issue, from the menu's foot, or is undefined while
  // they aren't assigned to it.
  onUnassign?: (() => void) | undefined
  // textRef is the text box, which a quoted reply focuses.
  textRef?: RefObject<HTMLTextAreaElement | null>
}

// IssueComposer is where the user writes to the issue: the comment's text, the action
// button (Comment, and the closes, which post the text first), Unassign me, and a launch to
// hand the issue to a coding agent.
export function IssueComposer({
  issue,
  item,
  draft,
  onDraft,
  selected,
  onRun,
  busy,
  error,
  launch,
  resolution,
  sessions,
  onUnassign,
  textRef,
}: IssueComposerProps) {
  const id = useId()
  const offerOf = (action: IssueAction) => issue.offers.find((o) => o.action === action)
  const offer = offerOf(selected)
  const note = closeNote(offer, draft.body)
  // A note that says why the comment can't be posted holds the button back.
  const noteBlocks = note !== '' && offer !== undefined && offer.commentBlocked !== ''
  const extra: MenuItem[] = onUnassign === undefined ? [] : [{ label: 'Unassign me', onSelect: onUnassign }]

  return (
    <section aria-label="Your comment" className="space-y-2">
      <label htmlFor={id} className="text-xs font-medium text-muted">
        Your comment
      </label>
      <textarea
        ref={textRef}
        id={id}
        rows={5}
        value={draft.body}
        placeholder="Leave a comment (markdown)"
        onChange={(e) => {
          const text = e.target.value

          onDraft((d) => ({ ...d, body: text }))
        }}
        className={`${fieldClass} block w-full font-mono text-xs`}
      />
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <ActionButton
          offers={issue.offers}
          selected={selected}
          busy={busy}
          onSelect={(action) => {
            onDraft((d) => ({ ...d, action }))
          }}
          onRun={onRun}
          blocked={(action) => issueHeldBack(offerOf(action), draft.body)}
          menuLabel="More issue actions"
          extra={extra}
        />
        {launch !== null && item !== undefined && (
          <LaunchControls
            // The sheet steps through its list in place: each issue's launch starts from
            // its own settings, never the choices made for the last one.
            key={item.id}
            item={item}
            resolution={resolution}
            agents={launch.agents}
            profiles={launch.profiles}
            prompts={launch.prompts}
            launchDefaults={launch.launchDefaults}
            onDefaultsSaved={launch.onDefaultsSaved}
            onManagePrompts={launch.onManagePrompts}
            onSettingsSaved={launch.onSettingsSaved}
            running={runningSession(sessions)}
          />
        )}
      </div>
      {note !== '' && <p className={`text-xs ${noteBlocks ? 'text-warning' : 'text-muted'}`}>{note}</p>}
    </section>
  )
}
