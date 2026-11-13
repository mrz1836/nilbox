import { useEffect, useLayoutEffect, useRef, useState } from 'react'

import { attempt } from '../lib/call'
import { refText } from '../lib/format'
import type { InboxFilter } from '../lib/inboxFilter'
import type { IssueDrafts } from '../lib/issueDrafts'
import { issueTone } from '../lib/issues'
import { itemKey, noWorktrees, worktreeOf } from '../lib/itemWorktrees'
import { sheetCards, shownOrder } from '../lib/listOrder'
import { isInboxNil } from '../lib/nil'
import type { ReviewDrafts } from '../lib/reviewDrafts'
import { ownRule, ruleFor, withRepoRule } from '../lib/repoRules'
import { noSessions, sessionsOf } from '../lib/sessions'
import { movesOnTo } from '../lib/sheet'
import { formatAgo, formatWhen } from '../lib/time'
import { findItem, isDismissed, itemsByRef, tabOf, type TriagedInbox, type TriageTab } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxOrderState } from '../lib/useInboxOrder'
import { useClock } from '../lib/useNow'
import {
  copyText,
  offerReleaseAfterMerge,
  setRepoRule,
  type AfterMerge,
  type AgentSession,
  type InboxItem,
  type ItemKind,
  type ItemResolution,
  type ListKey,
  type RepoRule,
  type RepoRules,
  type Review,
  type ReviewResult,
  type Settings,
  type Worktree,
} from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { InboxNil } from './InboxNil'
import { IssueSheet } from './issue/IssueSheet'
import { LinksContext, type Links } from './ItemLink'
import { teamsText, type EditActions } from './ItemMenu'
import type { LaunchOptions } from './ItemRow'
import { ListSection, type Reveal } from './ListSection'
import { ProjectsTip } from './ProjectsTip'
import { RelativeTime } from './RelativeTime'
import { ReviewSheet } from './review/ReviewSheet'
import { defaultToastSeconds, Toast, type ToastMessage } from './Toast'
import { SheetTriage, type TriageActions } from './TriageControls'

export type { LaunchOptions } from './ItemRow'
export type { Reveal } from './ListSection'

export interface InboxViewProps {
  // inbox is the window's shared inbox state (useInbox).
  inbox: InboxState
  // triaged is the inbox sorted into its tabs (useTriage), or null until it has loaded.
  triaged: TriagedInbox | null
  // launch is null until the settings and profiles have loaded; rows show no launch
  // controls until then.
  launch: LaunchOptions | null
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
  // worktrees holds each item's worktree, from the Worktrees view's latest listing (see
  // itemWorktrees); empty until the view has listed.
  worktrees?: ReadonlyMap<string, Worktree>
  // sessions holds each item's agent sessions, newest first, by itemKey (useSessions);
  // empty until they load, and while the app does not follow sessions.
  sessions?: ReadonlyMap<string, readonly AgentSession[]>
  // snoozeDays is how long a row's Snooze button hides an item.
  snoozeDays: number
  // toastSeconds is how long the inbox's toasts show, as set in Settings → Notifications.
  toastSeconds?: number
  // onSnoozeSettings opens the setting for the Snooze button's length.
  onSnoozeSettings: () => void
  // onFilesSettings opens Settings → Sensitive files.
  onFilesSettings?: () => void
  // collapsed holds the list names of the collapsed lists. It is null until the stored
  // state loads; the lists wait for it, so a collapsed list never shows open first.
  collapsed: ReadonlySet<ListKey> | null
  // onCollapse collapses or opens a list.
  onCollapse: (section: ListKey, collapsed: boolean) => void
  // reveal is the item the menu bar or a notification last asked to show, or null.
  reveal?: Reveal | null
  // showing reports whether the page is on screen; App keeps it mounted, hidden, while
  // another page shows.
  showing?: boolean
  // offerProjects reports whether the inbox offers to show issue projects (see
  // offersProjects), and onDismissProjects stores that the user put the offer away.
  offerProjects?: boolean
  onDismissProjects?: () => void
  // inboxOrder is how the lists are ordered (useInboxOrder). The lists wait for it to
  // load.
  inboxOrder: InboxOrderState
  // filter is the inbox as the toolbar's query narrows it (filterInbox), or null while
  // the query filters nothing.
  filter?: InboxFilter | null
  // onFilterBy adds a filter to the query, or takes it away, from a card's repository,
  // author, or label. It must keep its identity, so the cards keep theirs.
  onFilterBy?: (key: string, value: string) => void
  // onClearFilter empties the query.
  onClearFilter?: () => void
  // drafts are the sheets' drafts, which the window keeps for the session; with them, a card
  // opens in its sheet, and without, on GitHub.
  drafts?: SheetDrafts
  // onManageReplies opens the replies in the Prompts tab.
  onManageReplies?: () => void
  // onRelease opens a repository's release sheet, as a merge's toast offers when Release
  // can release it; without it, no merge offers a release. It must keep its identity.
  onRelease?: (offer: AfterMerge) => void
  // repoRules are the pinned and muted repositories and orgs, from the settings, which a
  // card's menu and its Unmute change; onSettingsSaved receives the settings as stored
  // after each change, and onRepoRuleSettings opens them in Settings → Repositories.
  repoRules?: RepoRules | undefined
  onSettingsSaved?: (settings: Settings) => void
  onRepoRuleSettings?: () => void
}

// SheetDrafts are the drafts of both sheets over the inbox: the review sheet's and the issue
// sheet's.
export interface SheetDrafts {
  review: ReviewDrafts
  issue: IssueDrafts
}

// noRepoRules are the rules until the settings have loaded.
const noRepoRules: RepoRules = { pinned: [], muted: [] }

// ruleChangeWords is what the toast says once a target's rule changed from was to rule,
// and what that does.
function ruleChangeWords(target: string, was: RepoRule, rule: RepoRule): { text: string; detail: string } {
  switch (rule) {
    case 'pinned':
      return { text: `Pinned ${target}`, detail: 'Its items stay at the top of every list.' }
    case 'muted':
      return { text: `Muted ${target}`, detail: 'Its items wait in each list’s Muted tab.' }
    default:
      return was === 'muted'
        ? { text: `Unmuted ${target}`, detail: 'Its items are back in the lists.' }
        : { text: `Unpinned ${target}`, detail: 'Its items are back among the rest.' }
  }
}

// Viewing is the card a sheet shows, and the list and tab it was opened from, which the
// sheet steps through: the review sheet for a pull request, the issue sheet for an issue.
// It keeps the card's kind, so the sheet stays as it was should the card leave the list,
// and the header's step arrow it was stepped to with, if any, which keeps focus when the
// step opens the other kind's sheet in its place.
interface Viewing {
  id: number
  kind: ItemKind
  list: ListKey
  tab: TriageTab
  arrow: '' | 'prev' | 'next'
  // back is the sheet a link in it was followed from, which its back step returns to, or
  // null.
  back: Back | null
  // refocus counts the links followed and the steps back taken, each of which shows another
  // card in the sheet in place of the control it was taken with, so the sheet moves focus
  // into itself again.
  refocus: number
}

// Back is the sheet a link was followed from: where it was, with no step back of its own,
// so following another link replaces it rather than stacking, and its card's name and title.
interface Back {
  to: Viewing
  label: string
  title: string
}

// focusedArrow is the header's step arrow that has focus, or ''.
function focusedArrow(): Viewing['arrow'] {
  const step = document.activeElement?.closest('[data-step]')?.getAttribute('data-step')

  return step === 'prev' || step === 'next' ? step : ''
}

// resultTone is how a review's outcome reads in the toast: a review GitHub took, an
// approval or the comment a merge or a close posted first, and a merge or a close it
// didn't make is a warning.
function resultTone(result: ReviewResult): 'warning' | 'success' {
  return result.outcome === 'approved_not_merged' ||
    result.outcome === 'commented_not_merged' ||
    result.outcome === 'commented_not_closed'
    ? 'warning'
    : 'success'
}

const sections: readonly { key: ListKey; title: string }[] = [
  { key: 'reviewRequested', title: 'Review requested' },
  { key: 'authored', title: 'Authored' },
  { key: 'assigned', title: 'Assigned' },
]

// NoMatches says that nothing in the inbox matches the query, what the filter looks at,
// and offers to clear it. It sits at the top, where the eye already is.
function NoMatches({ query, onClear }: { query: string; onClear: (() => void) | undefined }) {
  return (
    <div className="flex items-start gap-4 rounded-lg border border-border bg-surface px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium">No items match “{query.trim()}”</p>
        <p className="mt-0.5 text-xs text-muted">
          The filter looks at the items in your inbox, snoozed and dismissed ones too: their titles, repositories,
          numbers, branches, authors, labels, and projects. It doesn&apos;t search descriptions or comments, which
          aren&apos;t kept here.
        </p>
      </div>
      {onClear !== undefined && (
        <button
          type="button"
          onClick={onClear}
          className="shrink-0 rounded-md border border-border bg-surface px-3 py-1 text-sm font-medium hover:bg-raised"
        >
          Clear filter
        </button>
      )}
    </div>
  )
}

// firstMessage returns the first non-empty message.
function firstMessage(...messages: string[]): string {
  return messages.find((m) => m !== '') ?? ''
}

// InboxView shows the three inbox lists across the full width, with a refresh control and
// the time of the last refresh. Each list has tabs for what was snoozed or dismissed from
// it and collapses from its header, and each triage action is confirmed by a toast with an
// Undo. When every list is empty, the inbox throws a party above them (InboxNil). The
// account and the API budget live in the toolbar.
//
// While the toolbar's query filters the inbox, each list shows only its matching items,
// open whatever its stored collapse state, and a list with no match hides; when nothing
// matches at all, the inbox says so at the top instead of throwing its party.
export function InboxView({
  inbox,
  triaged,
  launch,
  resolutions,
  worktrees = noWorktrees,
  sessions = noSessions,
  snoozeDays,
  toastSeconds = defaultToastSeconds,
  onSnoozeSettings,
  onFilesSettings,
  collapsed,
  onCollapse,
  reveal = null,
  showing = true,
  offerProjects = false,
  onDismissProjects,
  inboxOrder,
  filter = null,
  onFilterBy,
  onClearFilter,
  drafts,
  onManageReplies,
  onRelease,
  repoRules = noRepoRules,
  onSettingsSaved,
  onRepoRuleSettings,
}: InboxViewProps) {
  const {
    snapshot,
    loadError,
    refreshError,
    refreshing,
    refresh,
    dismiss,
    snooze,
    restore,
    pin,
    unpin,
    refreshItem,
    unassign,
    removeReviewRequest,
  } = inbox
  const { order, setStatusSort, setStatusOrder, setListKind } = inboxOrder
  const clock = useClock()
  const lastError = snapshot?.lastError ?? ''
  const error = firstMessage(refreshError, lastError, loadError)
  // closedError is the refresh error the user closed. It stays closed while it is still
  // the error, through a refresh they start, and shows again once that refresh fails or
  // once it clears and comes back. An inbox that could not load can't be closed: the page
  // has nothing else to show.
  const [closedError, setClosedError] = useState('')
  const [wasRefreshing, setWasRefreshing] = useState(refreshing)

  if (refreshing !== wasRefreshing) {
    setWasRefreshing(refreshing)

    if (!refreshing) {
      setClosedError('')
    }
  }

  if (closedError !== '' && refreshError !== closedError && lastError !== closedError) {
    setClosedError('')
  }

  // refreshFailure is the refresh error to show, past the one the user closed.
  const refreshFailure = [refreshError, lastError].find((e) => e !== '' && e !== closedError) ?? ''

  const [toast, setToast] = useState<ToastMessage | null>(null)
  const count = triaged?.count ?? 0
  const nil =
    snapshot !== null && triaged !== null && collapsed !== null && error === '' && isInboxNil(snapshot, triaged)
  // busy records that the inbox has held something while the page was showing, so emptying
  // it from here throws the full party, while coming to the page at nil only sprinkles.
  // The page stays mounted while another shows, so hiding it forgets busy, and each visit
  // numbers the party anew: an inbox emptied meanwhile is found at nil, and sprinkles.
  const [busy, setBusy] = useState(false)
  const [visit, setVisit] = useState({ showing, n: 0 })
  const toastCount = useRef(0)
  // offeredProjects records that the projects tip showed during this run, so a refresh
  // that then finds access says the projects are on; projectsClosed, that the user
  // closed it.
  const [offeredProjects, setOfferedProjects] = useState(false)
  const [projectsClosed, setProjectsClosed] = useState(false)

  if (offerProjects && !offeredProjects) {
    setOfferedProjects(true)
  }

  const projectsOn = offeredProjects && snapshot?.projectAccess === 'on'
  const projectsTip = !projectsClosed && (offerProjects || projectsOn)

  if (visit.showing !== showing) {
    setVisit({ showing, n: showing ? visit.n + 1 : visit.n })
    setBusy(false)
  } else if (showing && !busy && count > 0) {
    setBusy(true)
  }

  // revealIn is the first list that holds the item to show, the one that shows it.
  const revealIn =
    reveal === null || snapshot === null
      ? null
      : (sections.find(({ key }) => snapshot[key].items.some((item) => item.id === reveal.id))?.key ?? null)
  // opened is the last reveal request whose list was opened, so a list the user collapses
  // afterwards stays collapsed.
  const opened = useRef(0)

  useEffect(() => {
    if (reveal === null || revealIn === null || collapsed === null || opened.current === reveal.seq) {
      return
    }

    opened.current = reveal.seq

    if (collapsed.has(revealIn)) {
      onCollapse(revealIn, false)
    }
  }, [reveal, revealIn, collapsed, onCollapse])

  const [viewing, setViewing] = useState<Viewing | null>(null)
  const sheets = drafts !== undefined
  // shown is the snapshot the lists show, for the menu's review to find an item's list in
  // when it is chosen, without a new action for every snapshot.
  const shown = useRef(snapshot)

  useLayoutEffect(() => {
    shown.current = snapshot
  }, [snapshot])

  // openIn opens a card in its sheet from each list, and openFromMenu from a card's menu,
  // in the first list that holds it; each is made once, so the cards, which take them,
  // keep their identity.
  const [openIn] = useState(() => {
    const open = (list: ListKey) => (item: InboxItem, tab: TriageTab) => {
      setViewing({ id: item.id, kind: item.kind, list, tab, arrow: '', back: null, refocus: 0 })
    }

    return { reviewRequested: open('reviewRequested'), authored: open('authored'), assigned: open('assigned') }
  })
  const [openFromMenu] = useState(() => (item: InboxItem, tab: TriageTab) => {
    const list = sections.find(({ key }) => shown.current?.[key].items.some((i) => i.id === item.id))?.key

    setViewing({
      id: item.id,
      kind: item.kind,
      list: list ?? 'reviewRequested',
      tab,
      arrow: '',
      back: null,
      refocus: 0,
    })
  })

  const showToast = (message: Omit<ToastMessage, 'id'>) => {
    toastCount.current += 1
    setToast({ ...message, id: toastCount.current })
  }

  const closeToast = () => {
    setToast(null)
  }

  // offerRelease asks, once a pull request merged, whether its repository can be released
  // now, and, when it can, adds Release <repo>… to the toast that says it merged, while
  // that toast still shows. Asking reads only what the app keeps, never GitHub.
  const offerRelease = (r: Review, toastId: number) => {
    if (onRelease === undefined) {
      return
    }

    const slash = r.repo.indexOf('/')

    void attempt(offerReleaseAfterMerge(r.repo.slice(0, slash), r.repo.slice(slash + 1), r.baseRef)).then((answer) => {
      if (!answer.ok) {
        console.error('could not ask whether the merge offers a release:', answer.error)

        return
      }

      const offer = answer.data

      if (!offer.offer) {
        return
      }

      setToast((shown) =>
        shown?.id === toastId
          ? {
              ...shown,
              action: {
                label: `Release ${offer.repo}…`,
                onClick: () => {
                  setToast(null)
                  onRelease(offer)
                },
              },
            }
          : shown,
      )
    })
  }

  // failed reports a triage action the backend refused; the item is back as it was.
  const failed = (what: string, item: InboxItem, error: string) => {
    showToast({ text: `Could not ${what} “${item.title}”`, detail: error, tone: 'error' })
  }

  // undo puts an item back the way it was before it was snoozed or dismissed.
  const undo = (item: InboxItem) => () => {
    closeToast()
    void restore(item.id).then((result) => {
      if (!result.ok) {
        failed('undo the change to', item, result.error)
      }
    })
  }

  // setRule sets the rule of a target and records the settings as stored, or says why it
  // couldn't. It resolves with whether it did.
  const setRule = async (target: string, rule: RepoRule): Promise<boolean> => {
    const result = await attempt(setRepoRule(target, rule))

    if (!result.ok) {
      showToast({ text: `Could not change the rule for ${target}`, detail: result.error, tone: 'error' })

      return false
    }

    onSettingsSaved?.(result.data)

    return true
  }

  // changeRule pins, mutes, or takes the rule off a repository or an org, as a card asked,
  // and says so in a toast whose Undo puts back the rule the target had. The inbox shows the
  // change once the backend has rebuilt it under the new rules ("inbox:updated"). It
  // resolves with whether the rule changed.
  const changeRule = async (target: string, rule: RepoRule): Promise<boolean> => {
    const was = ownRule(repoRules, target)
    const done = await setRule(target, rule)

    if (done) {
      showToast({
        ...ruleChangeWords(target, was, rule),
        undo: () => {
          closeToast()
          void setRule(target, was)
        },
      })
    }

    return done
  }

  // dismissIt, snoozeIt, and restoreIt set an item aside or bring it back, and say so in a
  // toast, or why the backend refused; each resolves with whether it took the change.
  const dismissIt = async (item: InboxItem): Promise<boolean> => {
    const result = await dismiss(item.id)

    if (result.ok) {
      showToast({ text: 'Dismissed', detail: item.title, undo: undo(item) })
    } else {
      failed('dismiss', item, result.error)
    }

    return result.ok
  }

  const snoozeIt = async (item: InboxItem, days: number): Promise<boolean> => {
    const result = await snooze(item.id, days)

    if (!result.ok) {
      failed('snooze', item, result.error)

      return false
    }

    const until = findItem(result.data, item.id)?.snoozedUntil ?? ''
    const text = until === '' ? 'Snoozed' : `Snoozed until ${formatWhen(clock.getSnapshot(), new Date(until))}`

    showToast({ text, detail: item.title, undo: undo(item) })

    return true
  }

  const restoreIt = async (item: InboxItem): Promise<boolean> => {
    const result = await restore(item.id)

    if (result.ok) {
      showToast({ text: isDismissed(item) ? 'Restored' : 'Unsnoozed', detail: item.title })
    } else {
      failed('restore', item, result.error)
    }

    return result.ok
  }

  // triage is what every row's triage buttons do. It reads no time while rendering (the
  // toast after a snooze reads the clock when it shows), so the rows keep the same
  // actions from one tick to the next.
  const triage: TriageActions = {
    snoozeDays,
    onDismiss: (item) => {
      void dismissIt(item)
    },
    onSnooze: (item, days) => {
      void snoozeIt(item, days)
    },
    onRestore: (item) => {
      void restoreIt(item)
    },
    // A pin shows at once, as the item moves to the top, so it needs no toast.
    onPin: (item) => {
      void pin(item.id).then((result) => {
        if (!result.ok) {
          failed('pin', item, result.error)
        }
      })
    },
    onUnpin: (item) => {
      void unpin(item.id).then((result) => {
        if (!result.ok) {
          failed('unpin', item, result.error)
        }
      })
    },
    // A refreshed item that is still open updates in place; one that is not leaves the
    // inbox, which the toast explains.
    onRefresh: async (item) => {
      const result = await refreshItem(item.id)

      if (!result.ok) {
        failed('refresh', item, result.error)
      } else if (findItem(result.data, item.id) === undefined) {
        showToast({ text: 'No longer open on GitHub', detail: item.title })
      }
    },
    onChangeDefault: onSnoozeSettings,
    repoRules,
    onRepoRule: (target, rule) => {
      void changeRule(target, rule)
    },
    onRepoRuleSettings: onRepoRuleSettings ?? (() => undefined),
  }

  // unassignOn takes the user off the item on GitHub, from its card's menu or its sheet, and
  // says so once GitHub made it; it resolves with why it couldn't, or ''.
  const unassignOn = async (item: InboxItem): Promise<string> => {
    const result = await unassign(item.id)

    if (!result.ok) {
      return result.error
    }

    showToast({ text: 'Unassigned you on GitHub', detail: item.title })

    return ''
  }

  // edits is what every row's menu does beyond triage. An edit shows once GitHub has
  // made it, as the item leaves the list, which the toast confirms; a pull request that
  // still asks a team for a review stays, and the toast says which.
  const edits: EditActions = {
    onCopyLink: (item) => {
      void attempt(copyText(item.url)).then((result) => {
        showToast(
          result.ok
            ? { text: 'Link copied', detail: item.url }
            : { text: 'Could not copy the link', detail: result.error, tone: 'error' },
        )
      })
    },
    onUnassign: async (item) => {
      const error = await unassignOn(item)

      if (error !== '') {
        failed('unassign you from', item, error)
      }
    },
    onRemoveReview: async (item) => {
      const result = await removeReviewRequest(item.id)

      if (!result.ok) {
        failed('remove you as a reviewer of', item, result.error)

        return
      }

      const after = findItem(result.data, item.id)

      showToast({
        text: 'Removed you as a reviewer on GitHub',
        detail: after?.reviewRequest === 'team' ? `It still asks ${teamsText(after.reviewTeams)}` : item.title,
      })
    },
    onOpen: sheets ? openFromMenu : undefined,
  }

  const viewed = viewing === null || snapshot === null ? undefined : findItem(snapshot, viewing.id)
  // A sheet steps through the cards of its list and tab as the list shows them, pull
  // requests and issues alike: the filter's matches, sorted by project status when the list
  // is, and the card it shows, kept in its place should the filter stop matching it.
  const siblings =
    viewing === null || triaged === null
      ? []
      : sheetCards(
          triaged.lists[viewing.list],
          viewing.tab,
          filter?.lists[viewing.list],
          shownOrder(order, viewing.list),
          viewing.id,
        )
  const listTitle = viewing === null ? '' : (sections.find(({ key }) => key === viewing.list)?.title ?? '')

  // toCard is the sheet from moved to the card with id among its cards, which switches the
  // sheet when its kind differs.
  const toCard = (from: Viewing, id: number): Viewing => ({
    ...from,
    id,
    kind: siblings.find((s) => s.id === id)?.kind ?? from.kind,
    arrow: focusedArrow(),
  })

  // step moves a sheet to the card with id.
  const step = (id: number) => {
    if (viewing !== null) {
      setViewing(toCard(viewing, id))
    }
  }
  const initialFocus = viewing === null || viewing.arrow === '' ? undefined : `[data-step="${viewing.arrow}"]`

  // links open a card a sheet links to in its own sheet, at its place in the first list
  // that holds it, with one step back to the sheet the link was in, while its card is
  // still in the inbox to name it.
  const listed = snapshot === null ? null : itemsByRef(snapshot)
  const links: Links = {
    holds: (repo, number) => listed?.has(itemKey(repo, number)) === true,
    follow: (repo, number) => {
      const target = listed?.get(itemKey(repo, number))

      if (viewing === null || target === undefined || triaged === null) {
        return
      }

      setViewing({
        id: target.item.id,
        kind: target.item.kind,
        list: target.list,
        tab: tabOf(triaged.lists[target.list], target.item.id) ?? 'inbox',
        arrow: '',
        back:
          viewed === undefined
            ? null
            : { to: { ...viewing, arrow: '', back: null }, label: refText(viewed), title: viewed.title },
        refocus: viewing.refocus + 1,
      })
    },
  }
  const back =
    viewing?.back == null
      ? null
      : {
          label: viewing.back.label,
          title: viewing.back.title,
          onBack: () => {
            if (viewing.back !== null) {
              setViewing({ ...viewing.back.to, refocus: viewing.refocus + 1 })
            }
          },
        }

  // unassignHere takes the user off an issue from its sheet: the sheet closes and the toast
  // says so once GitHub made it, and it hears why when it couldn't.
  const unassignHere = async (it: InboxItem): Promise<string> => {
    const error = await unassignOn(it)

    if (error === '') {
      setViewing(null)
    }

    return error
  }

  // leaving runs act, a triage action from a sheet that takes its card out of the tab the
  // sheet steps through, along with every card leaves says goes too. The sheet moves on at
  // once, as the inbox shows the action at once: to the nearest card after it that stays,
  // else the nearest before it, or it closes, worked out before the action changes the
  // cards. Should the backend refuse, a sheet still on the card it moved to goes back. A
  // card the tab no longer holds, as one whose snooze ended while it was open, has no place
  // to move on from, and keeps the sheet.
  const leaving = (act: () => Promise<boolean>, leaves: (card: InboxItem) => boolean = () => false) => {
    const from = viewing

    if (from === null || !siblings.some((s) => s.id === from.id)) {
      void act()

      return
    }

    const after = movesOnTo(siblings, from.id, leaves)
    const moved = after === undefined ? null : toCard(from, after.id)

    setViewing(moved)
    void act().then((done) => {
      if (!done && moved !== null) {
        setViewing((now) => (now === moved ? from : now))
      }
    })
  }
  // sheetTriage is what the triage buttons in a sheet's header do: what a row's do, with a
  // snooze, a dismissal, and a restore moving the sheet on (leaving).
  const sheetTriage: TriageActions = {
    ...triage,
    onSnooze: (item, days) => {
      leaving(() => snoozeIt(item, days))
    },
    onDismiss: (item) => {
      leaving(() => dismissIt(item))
    },
    onRestore: (item) => {
      leaving(() => restoreIt(item))
    },
    // Unmute is the one rule a sheet changes. The card leaves the Muted tab only when no
    // rule mutes it once the change is made, as an org's still would once its repository's
    // own is taken off, and every card the change unmutes leaves with it.
    onRepoRule: (target, rule) => {
      const changed = withRepoRule(repoRules, target, rule)
      const unmuted = (card: InboxItem) =>
        ruleFor(repoRules, card.repo).rule === 'muted' && ruleFor(changed, card.repo).rule !== 'muted'

      if (viewed !== undefined && unmuted(viewed)) {
        leaving(() => changeRule(target, rule), unmuted)
      } else {
        void changeRule(target, rule)
      }
    },
  }
  // The card's own tab decides its triage buttons, which may differ from the tab the sheet
  // steps through, as when a snooze ended while the sheet was open.
  const cardTab = viewing === null || triaged === null ? null : tabOf(triaged.lists[viewing.list], viewing.id)
  const sheetTriageButtons =
    viewed === undefined || cardTab === null ? null : <SheetTriage item={viewed} tab={cardTab} actions={sheetTriage} />
  // While a sheet is open, the toast shows in it: the page behind it is inert, so its Undo
  // couldn't be reached there.
  const sheetOpen = viewing !== null && sheets
  const sheetToast = <Toast toast={toast} seconds={toastSeconds} onClose={closeToast} />

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex items-center gap-3 text-sm">
        <button
          type="button"
          disabled={refreshing}
          onClick={refresh}
          className="rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised disabled:opacity-60"
        >
          {refreshing ? 'Refreshing…' : 'Refresh'}
        </button>
        <span className="text-muted">
          {snapshot === null || snapshot.lastRefreshedAt === '' ? (
            'Not refreshed yet'
          ) : (
            <>
              Last refreshed <RelativeTime iso={snapshot.lastRefreshedAt} show={formatAgo} />
            </>
          )}
        </span>
      </div>
      {refreshFailure !== '' ? (
        <ErrorBanner
          onClose={() => {
            setClosedError(refreshFailure)
          }}
        >
          {refreshFailure}
        </ErrorBanner>
      ) : (
        loadError !== '' && <ErrorBanner>{loadError}</ErrorBanner>
      )}
      {snapshot !== null && snapshot.scopeWarning !== '' && (
        <p className="rounded-md border border-warning px-3 py-2 text-sm text-warning">{snapshot.scopeWarning}</p>
      )}
      {projectsTip && (
        <ProjectsTip
          on={projectsOn}
          refreshing={refreshing}
          onCheck={refresh}
          onClose={() => {
            setProjectsClosed(true)

            if (!projectsOn) {
              onDismissProjects?.()
            }
          }}
        />
      )}
      {filter !== null && filter.matches === 0 && <NoMatches query={filter.query} onClear={onClearFilter} />}
      {nil && filter === null && <InboxNil key={visit.n} stats={triaged} earned={busy} />}
      {triaged === null || collapsed === null || order === null
        ? loadError === '' && <p className="text-sm text-muted">Loading the inbox…</p>
        : sections.map(({ key, title }) => (
            <ListSection
              key={key}
              title={title}
              triaged={triaged.lists[key]}
              launch={launch}
              resolutions={resolutions}
              worktrees={worktrees}
              sessions={sessions}
              triage={triage}
              edits={edits}
              collapsed={filter === null && collapsed.has(key)}
              onCollapse={(on) => {
                onCollapse(key, on)
              }}
              onFilesSettings={onFilesSettings}
              reveal={key === revealIn ? reveal : null}
              order={{
                ...shownOrder(order, key),
                onSortChange: (on) => {
                  setStatusSort(key, on)
                },
                onOrderChange: setStatusOrder,
                onKindChange: (kind) => {
                  setListKind(key, kind)
                },
              }}
              matched={filter?.lists[key]}
              marks={filter?.marks ?? ''}
              onFilterBy={onFilterBy}
              onOpen={sheets ? openIn[key] : undefined}
            />
          ))}
      <LinksContext value={links}>
        {viewing !== null && sheets && viewing.kind === 'pr' && (
          <ReviewSheet
            id={viewing.id}
            item={viewed}
            siblings={siblings}
            listTitle={listTitle}
            drafts={drafts.review}
            launch={launch}
            resolution={resolutions.get(viewing.id) ?? null}
            sessions={viewed === undefined ? undefined : sessionsOf(sessions, viewed)}
            worktree={viewed === undefined ? undefined : worktreeOf(worktrees, viewed)}
            onNavigate={step}
            onClose={() => {
              setViewing(null)
            }}
            onDone={(result, r) => {
              setViewing(null)
              showToast({ text: result.message, detail: r.title, tone: resultTone(result) })

              if (result.outcome === 'merged') {
                offerRelease(r, toastCount.current)
              }
            }}
            onFilesSettings={onFilesSettings}
            onManageReplies={onManageReplies}
            initialFocus={initialFocus}
            refocus={viewing.refocus}
            back={back}
            triage={sheetTriageButtons}
            toast={sheetToast}
          />
        )}
        {viewing !== null && sheets && viewing.kind === 'issue' && (
          <IssueSheet
            id={viewing.id}
            item={viewed}
            siblings={siblings}
            listTitle={listTitle}
            drafts={drafts.issue}
            launch={launch}
            resolution={resolutions.get(viewing.id) ?? null}
            sessions={viewed === undefined ? undefined : sessionsOf(sessions, viewed)}
            worktree={viewed === undefined ? undefined : worktreeOf(worktrees, viewed)}
            onNavigate={step}
            onClose={() => {
              setViewing(null)
            }}
            onDone={(result, is) => {
              setViewing(null)
              showToast({ text: result.message, detail: is.title, tone: issueTone(result) })
            }}
            onUnassign={unassignHere}
            onManageReplies={onManageReplies}
            initialFocus={initialFocus}
            refocus={viewing.refocus}
            back={back}
            triage={sheetTriageButtons}
            toast={sheetToast}
          />
        )}
      </LinksContext>
      <Toast toast={sheetOpen ? null : toast} seconds={toastSeconds} onClose={closeToast} />
    </div>
  )
}
