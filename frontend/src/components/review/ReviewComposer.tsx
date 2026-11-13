import { useId } from 'react'

import { counted } from '../../lib/format'
import type { ReviewDraft } from '../../lib/reviewDrafts'
import { runningSession } from '../../lib/sessions'
import { useQuery } from '../../lib/useQuery'
import {
  fillReply,
  reviewReplies,
  type AgentSession,
  type InboxItem,
  type ItemResolution,
  type ReplyAction,
  type Review,
  type ReviewAction,
  type ReviewOffer,
  type ReviewReplyList,
} from '../../platform'
import { fieldClass, quietButtonClass } from '../controls'
import { ChevronDownIcon } from '../icons'
import type { LaunchOptions } from '../ItemRow'
import { LaunchControls } from '../LaunchControls'
import { MenuButton, type MenuItem } from '../Menu'
import { ActionButton, menuOnly } from './ActionButton'

export interface ReviewComposerProps {
  review: Review
  item: InboxItem | undefined
  draft: ReviewDraft
  onDraft: (change: (draft: ReviewDraft) => ReviewDraft) => void
  // reply is the review's text and the replies to start it from (useReplyBody).
  reply: ReplyBody
  // selected is the action the button runs.
  selected: ReviewAction
  onRun: (action: ReviewAction) => void
  busy: boolean
  error: string
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  onManageReplies?: (() => void) | undefined
}

// replyActionOf is the review an action's text goes with, which decides the replies the
// composer offers.
export function replyActionOf(action: ReviewAction): ReplyAction {
  switch (action) {
    case 'request_changes':
      return 'request_changes'
    case 'comment':
      return 'comment'
    default:
      return 'approve'
  }
}

// sentText is the text an action sends, from the composer's body: all of it, but for a
// close, which is never the button, and so never the action the composer's default reply
// was filled in for: it sends only what the user wrote or picked, never that default.
export function sentText(action: ReviewAction, draft: ReviewDraft, body: string): string {
  return action !== 'close' || draft.edited ? body : ''
}

// needsText says why an action is held back for want of text: a request for changes or a
// comment says something, in its text or in a line comment.
export function needsText(action: ReviewAction, body: string, comments: number): string {
  if ((action === 'request_changes' || action === 'comment') && body.trim() === '' && comments === 0) {
    return 'Write something, or add a line comment, first.'
  }

  return ''
}

// sendsText reports an offer whose action sends the text and line comments, as the backend
// marks it: one that submits a review of its own, or a merge or a close that posts them as
// a comment first.
export function sendsText(offer: ReviewOffer | undefined): boolean {
  return offer !== undefined && (offer.reviews || offer.commentsFirst)
}

// commentFirst says what a merge with no review of its own, or a close, posts as a comment
// first, as the backend marks its offer, or '' when it posts nothing: clearing the text,
// and the line comments, skips the comment. While GitHub can't take the comment, it says
// why instead, and how to go ahead without it, which holds the button back too (heldBack).
export function commentFirst(offer: ReviewOffer | undefined, body: string, comments: number): string {
  const text = body.trim() !== ''

  if (offer?.commentsFirst !== true || (!text && comments === 0)) {
    return ''
  }

  if (offer.commentBlocked !== '') {
    return offer.commentBlocked
  }

  return text ? 'Your text is posted as a comment first.' : 'Your line comments are posted as a comment first.'
}

// heldBack says why the window holds an offer's action back, or '': a request for changes
// or a comment with nothing to say, or a merge or a close whose comment GitHub can't take
// now.
export function heldBack(offer: ReviewOffer | undefined, body: string, comments: number): string {
  if (offer === undefined) {
    return ''
  }

  const reason = needsText(offer.action, body, comments)

  if (reason !== '' || offer.commentBlocked === '') {
    return reason
  }

  return commentFirst(offer, body, comments)
}

// ReplyBody is a review's text as the composer shows it, and the replies it offers.
export interface ReplyBody {
  // list is the replies for the review the selected action sends, with the defaults, or
  // undefined while they load.
  list: ReviewReplyList | undefined
  // fallback is the default reply for the pull request, a bot's or a person's, or 0.
  fallback: number
  // body is the text: the draft's once the user wrote any, else the default reply filled
  // in for the pull request.
  body: string
}

// useReplyBody works out the review's text for the pull request with id, opened by a bot
// or not, with the draft, for the action selected.
export function useReplyBody(id: number, isBot: boolean, draft: ReviewDraft, action: ReviewAction): ReplyBody {
  const replyAction = replyActionOf(action)
  const replies = useQuery(() => reviewReplies(replyAction), [replyAction])
  const list = replies.data
  const fallback = list === undefined ? 0 : isBot ? list.defaultBots : list.default
  const filled = useQuery(() => fillReply(id, fallback), [id, fallback], { enabled: !draft.edited && fallback !== 0 })
  const body = draft.edited ? draft.body : fallback === 0 ? '' : (filled.data ?? '')

  return { list, fallback, body }
}

// ReviewComposer is where the review is written and sent: a reply from the library to start
// from (the default one for the review, and for a bot's or a person's pull request, fills
// the text until the user writes their own), the text, the line comments waiting to go,
// the action button, and a launch to ask a coding agent for its own review.
export function ReviewComposer({
  review,
  item,
  draft,
  onDraft,
  reply,
  selected,
  onRun,
  busy,
  error,
  launch,
  resolution,
  sessions,
  onManageReplies,
}: ReviewComposerProps) {
  const id = useId()
  const { list, fallback, body } = reply
  const offerOf = (action: ReviewAction) => review.offers.find((o) => o.action === action)
  const offer = offerOf(selected)
  const firstNote = commentFirst(offer, body, draft.comments.length)
  // A note that says why the comment can't be posted holds the button back.
  const noteBlocks = firstNote !== '' && offer !== undefined && offer.commentBlocked !== ''

  const pick = (replyId: number) => {
    void fillReply(review.id, replyId).then(
      (text) => {
        onDraft((d) => ({ ...d, body: text, edited: true, replyId }))
      },
      () => undefined,
    )
  }

  const replyItems = (): MenuItem[] => [
    ...(list?.replies ?? []).map((r) => ({
      label: r.nickname,
      hint: r.id === fallback ? 'default' : undefined,
      title: r.name,
      onSelect: () => {
        pick(r.id)
      },
    })),
    {
      label: 'No reply',
      divided: true,
      onSelect: () => {
        onDraft((d) => ({ ...d, body: '', edited: true, replyId: 0 }))
      },
    },
    ...(onManageReplies === undefined ? [] : [{ label: 'Manage replies…', onSelect: onManageReplies }]),
  ]

  return (
    <section aria-label="Your review" className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={id} className="text-xs font-medium text-muted">
          Your review
        </label>
        <MenuButton label="Choose a reply" items={replyItems} className={quietButtonClass} align="right">
          Replies <ChevronDownIcon className="size-3" />
        </MenuButton>
      </div>
      <textarea
        id={id}
        rows={5}
        value={body}
        placeholder="Leave a comment (markdown)"
        onChange={(e) => {
          const text = e.target.value

          onDraft((d) => ({ ...d, body: text, edited: true }))
        }}
        className={`${fieldClass} block w-full font-mono text-xs`}
      />
      {draft.comments.length > 0 && (
        <p className="flex items-center gap-2 text-xs text-muted">
          {counted(draft.comments.length, 'line comment', 'line comments')} go with the review.
          <button
            type="button"
            onClick={() => {
              onDraft((d) => ({ ...d, comments: [] }))
            }}
            className={quietButtonClass}
          >
            Discard
          </button>
        </p>
      )}
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <ActionButton
          offers={review.offers}
          selected={selected}
          busy={busy}
          onSelect={(action) => {
            onDraft((d) => ({ ...d, action }))
          }}
          onRun={onRun}
          blocked={(action) => heldBack(offerOf(action), sentText(action, draft, body), draft.comments.length)}
          menuOnly={menuOnly}
        />
        {launch !== null && item !== undefined && (
          <LaunchControls
            // The sheet steps through its list in place: each pull request's launch starts
            // from its own settings, never the choices made for the last one.
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
      {firstNote !== '' && <p className={`text-xs ${noteBlocks ? 'text-warning' : 'text-muted'}`}>{firstNote}</p>}
    </section>
  )
}
