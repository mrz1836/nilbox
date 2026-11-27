import { lazy, Suspense, useRef, useState, type ReactNode } from 'react'

import type { IgnoredWorkflows } from '../../lib/ignoredWorkflows'
import { issueTone } from '../../lib/issues'
import { worktreeOf } from '../../lib/itemWorktrees'
import type { LaunchOptions } from '../../lib/launch'
import { sessionsOf } from '../../lib/sessions'
import { cameBy, stepPast, useSheetVisits, type Came, type EndedItem, type SheetCommonProps } from '../../lib/sheet'
import { isDismissed, isMuted, isSnoozed, type TriageTab } from '../../lib/triage'
import type { InboxState } from '../../lib/useInbox'
import { useNowAs } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import {
  resolveLaunches,
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
import type { EditActions } from '../ItemMenu'
import { defaultToastSeconds, Toast, type ToastMessage } from '../Toast'
import { SheetTriage, type TriageActions } from '../TriageControls'
import { useItemActions } from '../useItemActions'

// The sheets are the inbox's own, loaded from the same chunks.
const ReviewSheet = lazy(() => import('../review/ReviewSheet').then((m) => ({ default: m.ReviewSheet })))
const IssueSheet = lazy(() => import('../issue/IssueSheet').then((m) => ({ default: m.IssueSheet })))

// RowsKit is what the Autopilot tab's rows and sheets share with the inbox's lists, so
// they are the inbox's own: the inbox (whose triage they make), the launch options, the
// sessions and worktrees, the sheets' drafts and choices, and the settings the rows open.
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
  // ignoring is Settings → Reviewing's Ignored workflows, which the review sheet names and
  // adds to; without it, the sheet offers to ignore none.
  ignoring?: IgnoredWorkflows | null | undefined
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
// the item leave the list, and the step through the list the sheet came to it by, or null
// when it opened on it: an item found merged or closed as it shows is stepped past the way
// the sheet came (stepPast).
interface Viewing {
  id: number
  kind: ItemKind
  came: Came | null
}

// SheetHost is what a list of items does with them: the rows' triage and edits, each
// row's tab and launch settings, opening an item in its sheet, and what to render for the
// sheet and the list's toast.
export interface SheetHost {
  triage: TriageActions
  edits: EditActions
  tabOf: (item: InboxItem) => TriageTab
  resolutionOf: (id: number) => ItemResolution | null
  open: (item: InboxItem) => void
  node: ReactNode
}

export interface SheetHostOptions {
  kit: RowsKit
  // items are the list the sheet steps through, and listTitle its name in the sheet.
  items: readonly InboxItem[]
  listTitle: string
  // onClose hears the sheet close, by any way out.
  onClose?: () => void
}

// useSheetHost holds the inbox's own sheets over a list of items that aren't the inbox's
// lists, as a routine's Watching list and a single item Activity opens: the rows' triage
// and edits through the inbox (useItemActions), their launch settings (ResolveItems), the
// review or issue sheet, stepping through the list, and the toasts that say what was done.
export function useSheetHost({ kit, items, listTitle, onClose }: SheetHostOptions): SheetHost {
  const key = items.map((i) => i.id).join(',')
  const resolutions = useQuery(
    () =>
      resolveLaunches(key.split(',').map(Number)).then(
        (list) => new Map(list.map((r) => [r.itemId, r])) as ReadonlyMap<number, ItemResolution>,
      ),
    [key, kit.launch],
    { enabled: key !== '' && kit.launch !== null, log: 'could not resolve the items’ launches' },
  )
  // tabs are the rows' tabs, by the clock, so a snooze that ends moves its row's controls;
  // joined, so a tick renders the list again only when one changes.
  const tabs = useNowAs((now) => items.map((i) => tabFor(i, now)).join(','))
  const tabOf = (item: InboxItem): TriageTab => {
    const at = items.findIndex((i) => i.id === item.id)

    return (tabs.split(',')[at] as TriageTab | undefined) ?? 'inbox'
  }
  const [viewing, setViewing] = useState<Viewing | null>(null)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toasts = useRef(0)

  const showToast = (message: Omit<ToastMessage, 'id'>) => {
    toasts.current += 1
    setToast({ ...message, id: toasts.current })
  }

  const closeToast = () => {
    setToast(null)
  }

  const open = (item: InboxItem) => {
    setViewing({ id: item.id, kind: item.kind, came: null })
  }

  const close = () => {
    setViewing(null)
    onClose?.()
  }

  const { triage, edits, seen, ended, nudgeDialog, unassignOn, removeReviewOn } = useItemActions({
    inbox: kit.inbox,
    snoozeDays: kit.snoozeDays,
    onSnoozeSettings: kit.onSnoozeSettings,
    repoRules: kit.repoRules ?? noRules,
    onSettingsSaved: kit.onSettingsSaved,
    onRepoRuleSettings: kit.onRepoRuleSettings,
    showToast,
    closeToast,
    onOpen: open,
    listed: false,
    onManageReplies: kit.onManageReplies,
  })

  // An item opened in its sheet is heard once per opening, as the inbox's are, and marked
  // read, as is the one the sheet steps away from or closes on.
  useSheetVisits(viewing?.id ?? 0, seen, kit.onItemOpened)

  const viewed = viewing === null ? undefined : items.find((i) => i.id === viewing.id)
  const resolutionOf = (id: number) => resolutions.data?.get(id) ?? null
  const seconds = kit.toastSeconds ?? defaultToastSeconds
  const sheetToast = <Toast toast={toast} seconds={seconds} onClose={closeToast} />
  const sheetTriage = viewed === undefined ? null : <SheetTriage item={viewed} tab={tabOf(viewed)} actions={triage} />
  const kindOf = (id: number, was: ItemKind) => items.find((i) => i.id === id)?.kind ?? was
  const navigate = (id: number) => {
    setViewing((was) => ({
      id,
      kind: kindOf(id, was?.kind ?? 'pr'),
      came: was === null ? null : cameBy(items, was.id, id),
    }))
  }
  // endedHere moves the sheet on from an item a read of it found merged or closed since the
  // inbox last read it, as the inbox's sheets do: past it, the way the sheet stepped to it,
  // or, opened on it, the sheet closes; the toast says what became of it.
  const endedHere = (from: Viewing, gone: EndedItem) => {
    const past = stepPast(from.came, gone.id, (id) => items.some((i) => i.id === id))

    if (past === undefined) {
      close()
    } else {
      setViewing({ id: past.to, kind: kindOf(past.to, from.kind), came: past.came })
    }

    ended(gone)
  }
  const done = (message: string, title: string, tone: 'success' | 'warning') => {
    close()
    showToast({ text: message, detail: title, tone })
  }
  // closingOnce runs the user's taking themselves off an item from its sheet (take), which
  // closes the sheet once GitHub made it, the toast saying so, and resolves with why it
  // couldn't, which the sheet shows, or ''.
  const closingOnce =
    (take: (item: InboxItem) => Promise<string>) =>
    async (item: InboxItem): Promise<string> => {
      const error = await take(item)

      if (error === '') {
        close()
      }

      return error
    }
  // sheetProps are what both sheets take alike, for the item the sheet shows.
  const sheetProps: SheetCommonProps | null =
    viewing === null
      ? null
      : {
          id: viewing.id,
          item: viewed,
          siblings: items,
          listTitle,
          order: kit.conversationOrder ?? 'oldest',
          onOrder: kit.onConversationOrder,
          launch: kit.launch,
          resolution: resolutionOf(viewing.id),
          sessions: viewed === undefined ? undefined : sessionsOf(kit.sessions, viewed),
          worktree: viewed === undefined ? undefined : worktreeOf(kit.worktrees, viewed),
          onNavigate: navigate,
          onClose: close,
          onEnded: (gone) => {
            endedHere(viewing, gone)
          },
          onManageReplies: kit.onManageReplies,
          triage: sheetTriage,
          toast: sheetToast,
        }

  const node = (
    <>
      <Suspense fallback={null}>
        {sheetProps !== null && viewing?.kind === 'pr' && (
          <ReviewSheet
            {...sheetProps}
            drafts={kit.drafts.review}
            tab={kit.reviewTab ?? 'files'}
            onTab={kit.onReviewTab}
            onDone={(result, r) => {
              done(result.message, r.title, resultTone(result))
            }}
            onFilesSettings={kit.onFilesSettings}
            onToast={showToast}
            ignoring={kit.ignoring}
            onRemoveReview={closingOnce(removeReviewOn)}
            onUnassign={closingOnce(unassignOn)}
          />
        )}
        {sheetProps !== null && viewing?.kind === 'issue' && (
          <IssueSheet
            {...sheetProps}
            drafts={kit.drafts.issue}
            onDone={(result, issue) => {
              done(result.message, issue.title, issueTone(result))
            }}
            onUnassign={closingOnce(unassignOn)}
          />
        )}
      </Suspense>
      {nudgeDialog}
      <Toast toast={viewing === null ? toast : null} seconds={seconds} onClose={closeToast} />
    </>
  )

  return { triage, edits, tabOf, resolutionOf, open, node }
}
