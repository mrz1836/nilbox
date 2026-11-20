import { use, useEffect, useEffectEvent, useId, useState, type ReactNode } from 'react'

import type { Result } from '../lib/call'
import { CardPrefsContext } from '../lib/cardPrefs'
import { withReply } from '../lib/issues'
import { quietWords } from '../lib/nudge'
import { replyMenuItems } from '../lib/replies'
import { snoozeLength } from '../lib/triage'
import { usePickReply } from '../lib/usePickReply'
import { useNowAs } from '../lib/useNow'
import { useQuery } from '../lib/useQuery'
import { fillReply, openExternal, reviewReplies, type InboxItem, type Nudged } from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { fieldClass, quietButtonClass } from './controls'
import { ChevronDownIcon } from './icons'
import { MenuButton } from './Menu'
import type { ToastMessage } from './Toast'

// Draft is the comment as the user left it: its text, and the text the last reply they
// picked filled in, so a pick replaces text they haven't touched (withReply). Until they
// write or pick, the dialog shows the default Nudge reply instead.
interface Draft {
  body: string
  filled: string
}

export interface NudgeDialogProps {
  item: InboxItem
  // snoozeDays is how long Then snooze snoozes the pull request, and snoozeFirst whether
  // its box starts ticked (Settings → Reviewing's Snooze after a nudge).
  snoozeDays: number
  snoozeFirst: boolean
  // onPost posts the comment, then snoozes the pull request when snooze is set, and
  // resolves with why it couldn't post it, or ''. Once it posts, the host closes the
  // dialog.
  onPost: (body: string, snooze: boolean) => Promise<string>
  onCancel: () => void
  // onManageReplies opens the replies in the Prompts tab, from the replies menu's foot.
  onManageReplies?: (() => void) | undefined
}

// NudgeDialog is where the user nudges the author of a pull request waiting on them: one
// comment on its conversation, asking about the changes they requested, which they read
// before it is posted. Its text starts from the library's default Nudge reply, filled in
// for the pull request on this Mac (fillReply), or empty without one; the replies menu
// offers the library's other comment replies for pull requests. Post comment (or ⌘↩)
// posts it with one REST request, and can snooze the pull request after, so it comes back
// once its author answers. A failure stays in the dialog, under the text.
export function NudgeDialog({ item, snoozeDays, snoozeFirst, onPost, onCancel, onManageReplies }: NudgeDialogProps) {
  const id = useId()
  const replies = useQuery(() => reviewReplies('comment'), [])
  const fallback = replies.data?.nudge ?? 0
  const started = useQuery(() => fillReply(item.id, fallback), [item.id, fallback], { enabled: fallback !== 0 })
  const start = fallback === 0 ? '' : (started.data ?? '')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [snooze, setSnooze] = useState(snoozeFirst)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const waited = useNowAs((now) => quietWords(item, now))
  const body = draft === null ? start : draft.body
  const said = draft ?? { body: start, filled: start }

  const picking = usePickReply(item.id, fillReply, (text) => {
    setDraft({ body: withReply(said, text), filled: text })
  })

  const post = () => {
    if (busy || body.trim() === '') {
      return
    }

    setBusy(true)
    setError('')
    void onPost(body, snooze).then((why) => {
      if (why !== '') {
        setBusy(false)
        setError(why)
      }
    })
  }

  // ⌘↩ posts from anywhere in the dialog, as the sheets' do.
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.key === 'Enter' && e.metaKey && !e.defaultPrevented) {
      e.preventDefault()
      post()
    }
  })

  useEffect(() => {
    const listen = (e: KeyboardEvent) => {
      onKey(e)
    }

    window.addEventListener('keydown', listen)

    return () => {
      window.removeEventListener('keydown', listen)
    }
  }, [])

  // replyItems are the library's comment replies for pull requests, the default Nudge reply
  // marked, or why there are none to pick.
  const replyItems = () =>
    replyMenuItems({
      replies: replies.data?.replies,
      error: replies.error,
      none: 'No comment replies yet',
      fallback,
      onPick: picking.pick,
      onManage: onManageReplies,
    })

  return (
    <ConfirmDialog
      title={`Nudge @${item.author}`}
      confirmLabel={busy ? 'Posting…' : 'Post comment'}
      tone="primary"
      size="wide"
      busy={busy}
      confirmDisabled={body.trim() === ''}
      onConfirm={post}
      onCancel={onCancel}
    >
      <div className="space-y-3">
        <p className="truncate">
          {`${item.repo} #${String(item.number)} · ${item.title}`}
          <span className="block text-xs">{waited}</span>
        </p>
        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <label htmlFor={id} className="text-xs font-medium text-muted">
              Your comment
            </label>
            <MenuButton label="Choose a reply" items={replyItems} className={quietButtonClass} align="right">
              Replies <ChevronDownIcon className="size-3" />
            </MenuButton>
          </div>
          <textarea
            id={id}
            rows={5}
            value={body}
            disabled={busy}
            placeholder="Ask the author about the changes you requested (markdown)"
            onChange={(e) => {
              const text = e.target.value

              picking.drop()
              setDraft({ body: text, filled: said.filled })
            }}
            className={`${fieldClass} block w-full font-mono text-xs text-text`}
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
        </div>
        <label className="flex items-start gap-2 text-xs">
          <input
            type="checkbox"
            checked={snooze}
            disabled={busy}
            onChange={(e) => {
              setSnooze(e.target.checked)
            }}
            className="checkbox mt-0.5"
          />
          <span>Then snooze for {snoozeLength(snoozeDays)}. It comes back early when the author pushes.</span>
        </label>
      </div>
    </ConfirmDialog>
  )
}

// shortRef names a pull request as "example-repo #434": its repository without the org.
function shortRef(item: InboxItem): string {
  return `${item.repo.slice(item.repo.indexOf('/') + 1)} #${String(item.number)}`
}

export interface NudgeOptions {
  // nudge posts the comment on GitHub (the inbox's nudge), resolving with what GitHub
  // posting it left, or why it couldn't.
  nudge: (id: number, body: string) => Promise<Result<Nudged>>
  // snoozeDays is how long Then snooze snoozes the pull request, and snoozeIt snoozes it,
  // its toast's text leading with lead, and resolves with whether it did.
  snoozeDays: number
  snoozeIt: (item: InboxItem, days: number, lead?: string) => Promise<boolean>
  // markRead marks the pull request read: the comment is the user's own activity.
  markRead: (item: InboxItem) => void
  showToast: (message: Omit<ToastMessage, 'id'>) => void
  closeToast: () => void
  onManageReplies?: (() => void) | undefined
}

// Nudging is the Nudge dialog a list of cards holds: onNudge opens it for a pull request,
// and dialog is the dialog while it is open, which the list renders.
export interface Nudging {
  onNudge: (item: InboxItem) => void
  dialog: ReactNode
}

// useNudge holds the Nudge dialog for a list of cards. Once GitHub has posted a nudge, the
// dialog closes and a toast says so, with the comment's address (View comment), or, when
// the user ticked Then snooze, the snooze's own toast says both, with its Undo.
export function useNudge({
  nudge,
  snoozeDays,
  snoozeIt,
  markRead,
  showToast,
  closeToast,
  onManageReplies,
}: NudgeOptions): Nudging {
  const { nudgeSnooze } = use(CardPrefsContext)
  const [nudging, setNudging] = useState<InboxItem | null>(null)

  const post = async (item: InboxItem, body: string, snooze: boolean): Promise<string> => {
    const result = await nudge(item.id, body)

    if (!result.ok) {
      return result.error
    }

    setNudging(null)
    markRead(item)

    const who = `@${item.author}`

    if (snooze) {
      await snoozeIt(item, snoozeDays, `Nudged ${who} and snoozed`)

      return ''
    }

    const url = result.data.url

    showToast({
      text: `Nudged ${who} on ${shortRef(item)}`,
      detail: item.title,
      ...(url === ''
        ? {}
        : {
            action: {
              label: 'View comment',
              onClick: () => {
                closeToast()
                openExternal(url)
              },
            },
          }),
    })

    return ''
  }

  const dialog =
    nudging === null ? null : (
      <NudgeDialog
        key={nudging.id}
        item={nudging}
        snoozeDays={snoozeDays}
        snoozeFirst={nudgeSnooze}
        onPost={(body, snooze) => post(nudging, body, snooze)}
        onCancel={() => {
          setNudging(null)
        }}
        onManageReplies={
          onManageReplies === undefined
            ? undefined
            : () => {
                setNudging(null)
                onManageReplies()
              }
        }
      />
    )

  return {
    onNudge: (item) => {
      setNudging(item)
    },
    dialog,
  }
}
