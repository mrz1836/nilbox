import type { RefObject } from 'react'

import { closeNote, issueHeldBack, withReply } from '../../lib/issues'
import type { IssueDraft } from '../../lib/issueDrafts'
import type { LaunchOptions } from '../../lib/launch'
import { replyMenuItems } from '../../lib/replies'
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
import { Composer } from '../conversation/Composer'
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

  // replyItems are the library's replies for issues, or why there are none to pick.
  const replyItems = () =>
    replyMenuItems({
      replies: replies.data,
      error: replies.error,
      none: 'No replies for issues yet',
      onPick: picking.pick,
      onManage: onManageReplies,
    })

  return (
    <Composer
      label="Your comment"
      replies={replyItems}
      picking={picking}
      text={draft.body}
      onText={(text) => {
        onDraft((d) => ({ ...d, body: text }))
      }}
      textRef={textRef}
      error={error}
      actions={
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
      }
      launch={launch}
      item={item}
      resolution={resolution}
      sessions={sessions}
      note={note}
      noteBlocks={noteBlocks}
    />
  )
}
