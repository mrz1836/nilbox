import { useId, type RefObject } from 'react'

import { closeNote, issueHeldBack, withReply } from '../../lib/issues'
import type { IssueDraft } from '../../lib/issueDrafts'
import { runningSession } from '../../lib/sessions'
import { usePickReply } from '../../lib/usePickReply'
import { useQuery } from '../../lib/useQuery'
import {
  fillIssueReply,
  issueReplies,
  type AgentSession,
  type InboxItem,
  type Issue,
  type IssueAction,
  type ItemResolution,
} from '../../platform'
import { fieldClass, quietButtonClass } from '../controls'
import { ChevronDownIcon } from '../icons'
import type { LaunchOptions } from '../ItemRow'
import { LaunchControls } from '../LaunchControls'
import { MenuButton, type MenuItem } from '../Menu'
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
  // onManageReplies opens the replies in the Prompts tab, from the Replies menu's foot.
  onManageReplies?: (() => void) | undefined
}

// IssueComposer is where the user writes to the issue: a reply from the library to fill the
// comment in with (the library's comment replies for issues, never one picked for the
// user, so ⌘↩ never posts text they didn't write or choose, and a pick never loses text
// they wrote: see withReply), the comment's text, the action button (Comment, and the
// closes, which post the text first), Unassign me, and a launch to hand the issue to a
// coding agent.
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
  onManageReplies,
}: IssueComposerProps) {
  const id = useId()
  const replies = useQuery(() => issueReplies(), [])
  const offerOf = (action: IssueAction) => issue.offers.find((o) => o.action === action)
  const offer = offerOf(selected)
  const note = closeNote(offer, draft.body)
  // A note that says why the comment can't be posted holds the button back.
  const noteBlocks = note !== '' && offer !== undefined && offer.commentBlocked !== ''
  const extra: MenuItem[] = onUnassign === undefined ? [] : [{ label: 'Unassign me', onSelect: onUnassign }]

  const picking = usePickReply(issue.id, fillIssueReply, (text) => {
    onDraft((d) => ({ ...d, body: withReply(d, text), filled: text }))
  })

  // replyChoices are the library's replies for issues, or why there are none to pick.
  const replyChoices = (): MenuItem[] => {
    const list = replies.data ?? []

    if (list.length > 0) {
      return list.map((r) => ({
        label: r.nickname,
        title: r.name,
        onSelect: () => {
          picking.pick(r.id)
        },
      }))
    }

    if (replies.data === undefined && replies.error !== '') {
      return [{ label: 'Couldn’t load the replies', title: replies.error, disabled: true }]
    }

    return [{ label: replies.data === undefined ? 'Loading replies…' : 'No replies for issues yet', disabled: true }]
  }

  const replyItems = (): MenuItem[] => [
    ...replyChoices(),
    ...(onManageReplies === undefined ? [] : [{ label: 'Manage replies…', divided: true, onSelect: onManageReplies }]),
  ]

  return (
    <section aria-label="Your comment" className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={id} className="text-xs font-medium text-muted">
          Your comment
        </label>
        <MenuButton label="Choose a reply" items={replyItems} className={quietButtonClass} align="right">
          Replies <ChevronDownIcon className="size-3" />
        </MenuButton>
      </div>
      <textarea
        ref={textRef}
        id={id}
        rows={5}
        value={draft.body}
        placeholder="Leave a comment (markdown)"
        onChange={(e) => {
          const text = e.target.value

          picking.drop()
          onDraft((d) => ({ ...d, body: text }))
        }}
        className={`${fieldClass} block w-full font-mono text-xs`}
      />
      {picking.error !== '' && (
        <p role="alert" className="text-xs text-danger">
          Couldn’t fill in the reply: {picking.error}
        </p>
      )}
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
