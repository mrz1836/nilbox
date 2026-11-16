import { lazy, Suspense, use, useEffect, useEffectEvent, useRef, useState } from 'react'

import { issueTone } from '../../lib/issues'
import { worktreeOf } from '../../lib/itemWorktrees'
import { sessionsOf } from '../../lib/sessions'
import { isDismissed, isMuted, isSnoozed, type TriageTab } from '../../lib/triage'
import { useAutopilotChips, useOnAutopilot } from '../../lib/useAutopilot'
import type { InboxState } from '../../lib/useInbox'
import { useNowAs } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import {
  itemsByID,
  on,
  openExternal,
  resolveLaunches,
  routineWatching,
  type AgentSession,
  type ConversationOrder,
  type InboxItem,
  type ItemKind,
  type ItemResolution,
  type RepoRules,
  type ReviewTab,
  type Settings,
  type Worktree,
} from '../../platform'
import { resultTone, type SheetDrafts } from '../InboxView'
import { ItemRow, type LaunchOptions } from '../ItemRow'
import { defaultToastSeconds, Toast, type ToastMessage } from '../Toast'
import { SheetTriage } from '../TriageControls'
import { useItemActions } from '../useItemActions'
import { AutopilotRowsContext } from './AutopilotChip'

// The sheets are the inbox's own, loaded from the same chunks.
const ReviewSheet = lazy(() => import('../review/ReviewSheet').then((m) => ({ default: m.ReviewSheet })))
const IssueSheet = lazy(() => import('../issue/IssueSheet').then((m) => ({ default: m.IssueSheet })))

// RowsKit is what a routine's Watching list shares with the inbox's lists, so its rows are
// the inbox's own: the inbox (whose triage they make), the launch options, the sessions and
// worktrees, the sheets' drafts and choices, and the settings the rows open.
export interface RowsKit {
  inbox: InboxState
  launch: LaunchOptions | null
  sessions: ReadonlyMap<string, readonly AgentSession[]>
  worktrees: ReadonlyMap<string, Worktree>
  snoozeDays: number
  toastSeconds?: number
  repoRules: RepoRules | undefined
  drafts: SheetDrafts
  reviewTab: ReviewTab | undefined
  onReviewTab: (tab: ReviewTab) => void
  conversationOrder: ConversationOrder | undefined
  onConversationOrder: (order: ConversationOrder) => void
  onSnoozeSettings: () => void
  onFilesSettings: () => void
  onRepoRuleSettings: () => void
  onManageReplies: () => void
  onSettingsSaved: (settings: Settings) => void
  // onItemOpened hears each item opened in its sheet, as the inbox's do.
  onItemOpened: (itemId: number) => void
}

// noRules are the rules until the settings load.
const noRules: RepoRules = { pinned: [], muted: [] }

// tabFor is the tab of the inbox an item would be in: its triage decides its controls.
function tabFor(item: InboxItem, now: Date): TriageTab {
  if (isDismissed(item)) {
    return 'dismissed'
  }

  if (isSnoozed(item, now)) {
    return 'snoozed'
  }

  return isMuted(item) ? 'muted' : 'inbox'
}

// Viewing is the item a sheet shows, and its kind, which keeps the sheet as it is should
// the item leave the list.
interface Viewing {
  id: number
  kind: ItemKind
}

export interface WatchingListProps {
  routineId: number
  kit: RowsKit
  // highlight is the item Activity's Open asked to show, which the list marks and scrolls
  // into view; one the list doesn't hold opens on GitHub. seq numbers the asks.
  highlight: { id: number; url: string; seq: number } | null
}

// WatchingList is a routine's Watching list: the items its watch finds now (those in the
// inbox lists it names and those its searches return, as the last refresh read them),
// newest activity first, as the inbox's own rows, with their signals, launch button, ⚡
// chip, and ⋯ menu. A row's sheet opens on a click and steps through the list. This is
// where an item that is in no inbox list shows up.
export function WatchingList({ routineId, kit, highlight }: WatchingListProps) {
  const rows = use(AutopilotRowsContext)
  const ids = useQuery(() => routineWatching(routineId), [routineId])
  const key = (ids.data ?? []).join(',')
  const numbers = key === '' ? [] : key.split(',').map(Number)
  const items = useQuery(() => itemsByID(numbers), [key], { enabled: ids.data !== undefined })
  const resolutions = useQuery(
    () =>
      resolveLaunches(numbers).then(
        (list) => new Map(list.map((r) => [r.itemId, r])) as ReadonlyMap<number, ItemResolution>,
      ),
    [key, kit.launch],
    { enabled: key !== '' && kit.launch !== null, log: 'could not resolve the watched items’ launches' },
  )
  const chips = useAutopilotChips(key)
  const list = items.data ?? []
  // tabs are the rows' tabs, by the clock, so a snooze that ends moves its row's controls;
  // joined, so a tick renders the list again only when one changes.
  const tabs = useNowAs((now) => list.map((i) => tabFor(i, now)).join(','))
  const tabOf = (item: InboxItem): TriageTab => {
    const at = list.findIndex((i) => i.id === item.id)

    return (tabs.split(',')[at] as TriageTab | undefined) ?? 'inbox'
  }
  const [viewing, setViewing] = useState<Viewing | null>(null)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toasts = useRef(0)
  const listRef = useRef<HTMLUListElement>(null)

  useOnAutopilot(() => {
    void ids.refetch()
    void items.refetch()
  })

  // The rows' triage changes the stored items, which the inbox says it changed.
  const readItems = useEffectEvent(() => {
    void items.refetch()
  })

  useEffect(
    () =>
      on('inbox:updated', () => {
        readItems()
      }),
    [],
  )

  const showToast = (message: Omit<ToastMessage, 'id'>) => {
    toasts.current += 1
    setToast({ ...message, id: toasts.current })
  }

  const closeToast = () => {
    setToast(null)
  }

  const { triage, edits } = useItemActions({
    inbox: kit.inbox,
    snoozeDays: kit.snoozeDays,
    onSnoozeSettings: kit.onSnoozeSettings,
    repoRules: kit.repoRules ?? noRules,
    onSettingsSaved: kit.onSettingsSaved,
    onRepoRuleSettings: kit.onRepoRuleSettings,
    showToast,
    closeToast,
    onOpen: (item) => {
      setViewing({ id: item.id, kind: item.kind })
    },
    listed: false,
  })

  const viewed = viewing === null ? undefined : list.find((i) => i.id === viewing.id)
  const shownId = highlight !== null && list.some((i) => i.id === highlight.id) ? highlight.id : 0
  const listed = items.data !== undefined

  // Activity's Open scrolls to the row it asked for, or opens the item on GitHub when the
  // routine no longer watches it.
  useEffect(() => {
    if (highlight === null || !listed) {
      return
    }

    if (shownId === 0) {
      if (highlight.url !== '') {
        openExternal(highlight.url)
      }

      return
    }

    const row = [...(listRef.current?.querySelectorAll('[data-item-id]') ?? [])].find(
      (el) => el.getAttribute('data-item-id') === String(shownId),
    )

    row?.scrollIntoView({ block: 'center' })
  }, [highlight, shownId, listed])

  // An item opened in its sheet is heard once per opening, as the inbox's are.
  const opened = useRef(0)
  const itemOpened = useEffectEvent((id: number) => {
    kit.onItemOpened(id)
  })
  const viewingId = viewing?.id ?? 0

  useEffect(() => {
    if (viewingId !== opened.current) {
      opened.current = viewingId

      if (viewingId > 0) {
        itemOpened(viewingId)
      }
    }
  }, [viewingId])

  const sheetToast = <Toast toast={toast} seconds={kit.toastSeconds ?? defaultToastSeconds} onClose={closeToast} />
  const sheetTriage = viewed === undefined ? null : <SheetTriage item={viewed} tab={tabOf(viewed)} actions={triage} />
  const done = (message: string, title: string, tone: 'success' | 'warning') => {
    setViewing(null)
    showToast({ text: message, detail: title, tone })
  }

  if (ids.error !== '' || items.error !== '') {
    return (
      <p role="alert" className="px-4 py-3 text-sm text-danger">
        {ids.error !== '' ? ids.error : items.error}
      </p>
    )
  }

  if (!listed) {
    return (
      <p role="status" className="px-4 py-3 text-sm text-muted">
        Reading what it watches…
      </p>
    )
  }

  return (
    <>
      {list.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">
          Nothing to watch right now. Its lists and searches find nothing open, as the last refresh read them.
        </p>
      ) : (
        <AutopilotRowsContext value={{ names: rows?.names ?? new Map(), chips, onShow: rows?.onShow }}>
          <ul ref={listRef} aria-label="Watching" className="rounded-lg border border-border bg-surface">
            {list.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                launch={kit.launch}
                resolution={resolutions.data?.get(item.id) ?? null}
                worktree={worktreeOf(kit.worktrees, item)}
                sessions={sessionsOf(kit.sessions, item)}
                tab={tabOf(item)}
                triage={triage}
                edits={edits}
                onFilesSettings={kit.onFilesSettings}
                highlighted={item.id === shownId}
                onOpen={(it) => {
                  setViewing({ id: it.id, kind: it.kind })
                }}
              />
            ))}
          </ul>
        </AutopilotRowsContext>
      )}
      <Suspense fallback={null}>
        {viewing !== null && viewing.kind === 'pr' && (
          <ReviewSheet
            id={viewing.id}
            item={viewed}
            siblings={list}
            listTitle="Watching"
            drafts={kit.drafts.review}
            tab={kit.reviewTab ?? 'files'}
            onTab={kit.onReviewTab}
            order={kit.conversationOrder ?? 'oldest'}
            onOrder={kit.onConversationOrder}
            launch={kit.launch}
            resolution={resolutions.data?.get(viewing.id) ?? null}
            sessions={viewed === undefined ? undefined : sessionsOf(kit.sessions, viewed)}
            worktree={viewed === undefined ? undefined : worktreeOf(kit.worktrees, viewed)}
            onNavigate={(id) => {
              setViewing({ id, kind: list.find((i) => i.id === id)?.kind ?? viewing.kind })
            }}
            onClose={() => {
              setViewing(null)
            }}
            onDone={(result, r) => {
              done(result.message, r.title, resultTone(result))
            }}
            onFilesSettings={kit.onFilesSettings}
            onManageReplies={kit.onManageReplies}
            triage={sheetTriage}
            toast={sheetToast}
          />
        )}
        {viewing !== null && viewing.kind === 'issue' && (
          <IssueSheet
            id={viewing.id}
            item={viewed}
            siblings={list}
            listTitle="Watching"
            drafts={kit.drafts.issue}
            order={kit.conversationOrder ?? 'oldest'}
            onOrder={kit.onConversationOrder}
            launch={kit.launch}
            resolution={resolutions.data?.get(viewing.id) ?? null}
            sessions={viewed === undefined ? undefined : sessionsOf(kit.sessions, viewed)}
            worktree={viewed === undefined ? undefined : worktreeOf(kit.worktrees, viewed)}
            onNavigate={(id) => {
              setViewing({ id, kind: list.find((i) => i.id === id)?.kind ?? viewing.kind })
            }}
            onClose={() => {
              setViewing(null)
            }}
            onDone={(result, issue) => {
              done(result.message, issue.title, issueTone(result))
            }}
            onManageReplies={kit.onManageReplies}
            triage={sheetTriage}
            toast={sheetToast}
          />
        )}
      </Suspense>
      <Toast
        toast={viewing === null ? toast : null}
        seconds={kit.toastSeconds ?? defaultToastSeconds}
        onClose={closeToast}
      />
    </>
  )
}
