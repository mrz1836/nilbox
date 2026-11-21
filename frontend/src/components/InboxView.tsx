import { lazy, Suspense, useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react'

import type { IgnoredWorkflows } from '../lib/ignoredWorkflows'
import { logFailure } from '../lib/call'
import { CardLinksContext, linkIndex, type CardLinks } from '../lib/cardLinks'
import { refText } from '../lib/format'
import type { InboxFilter } from '../lib/inboxFilter'
import { progressStep } from '../lib/inboxProgress'
import type { IssueDrafts } from '../lib/issueDrafts'
import { issueTone } from '../lib/issues'
import { itemKey, noWorktrees, worktreeOf } from '../lib/itemWorktrees'
import type { LaunchOptions } from '../lib/launch'
import { sheetCards, shownOrder } from '../lib/listOrder'
import { useCelebrations } from '../lib/celebrations'
import { nilChip } from '../lib/milestones'
import {
  flourishes,
  isInboxNil,
  listsShow,
  nextFlourish,
  partyCompact,
  plainFlourishes,
  type Flourish,
  type PartyFold,
} from '../lib/nil'
import type { ReviewDrafts } from '../lib/reviewDrafts'
import { ruleFor, withRepoRule } from '../lib/repoRules'
import { holdPlace } from '../lib/scroll'
import { noSessions, sessionsOf } from '../lib/sessions'
import {
  cameBy,
  movesOnTo,
  stepPast,
  useSheetVisits,
  type Came,
  type EndedItem,
  type SheetCommonProps,
} from '../lib/sheet'
import { formatAgo } from '../lib/time'
import {
  byList,
  findItem,
  inboxLists,
  itemsByRef,
  listTitles,
  tabOf,
  type TriagedInbox,
  type TriageTab,
} from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxOrderState } from '../lib/useInboxOrder'
import {
  offerReleaseAfterMerge,
  reachedNil,
  type AfterMerge,
  type AgentSession,
  type ConversationOrder,
  type InboxItem,
  type ItemKind,
  type ItemResolution,
  type ListKey,
  type NilMoment,
  type RepoRules,
  type Review,
  type ReviewResult,
  type ReviewTab,
  type Settings,
  type Worktree,
} from '../platform'
import { iconButtonClass } from './controls'
import { ErrorBanner } from './ErrorBanner'
import { HelpIcon, RefreshIcon } from './icons'
import { InboxFilling } from './InboxFilling'
import { InboxNil } from './InboxNil'
import { LinksContext, type Links } from './ItemLink'
import { ListSection, type Reveal } from './ListSection'
import { ProgressStatus } from './ProgressStatus'
import { ProjectsTip } from './ProjectsTip'
import { RelativeTime } from './RelativeTime'
import { defaultToastSeconds, Toast, type ToastMessage } from './Toast'
import { SheetTriage, type TriageActions } from './TriageControls'
import { useItemActions } from './useItemActions'

export type { Reveal } from './ListSection'

// The review and issue sheets load on their own: the diff, the conversation, and the
// markdown they show are not needed for the inbox's first paint, whose chunk stays under
// the build's size limit without them. The inbox starts loading both once it has painted
// (preloadSheets), so a card's first opening rarely waits for them.
const loadReviewSheet = () => import('./review/ReviewSheet')
const loadIssueSheet = () => import('./issue/IssueSheet')
const ReviewSheet = lazy(() => loadReviewSheet().then((m) => ({ default: m.ReviewSheet })))
const IssueSheet = lazy(() => loadIssueSheet().then((m) => ({ default: m.IssueSheet })))

// preloadSheets starts loading both sheets' code. A load that fails is left to the sheet's
// own load on its opening, which tries again.
function preloadSheets() {
  for (const load of [loadReviewSheet, loadIssueSheet]) {
    load().catch(() => undefined)
  }
}

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
  // onReveal shows another card in the inbox, as a card's links ask (LinkChip); without
  // it, an issue's card links its fixes to GitHub, and a pull request's links nothing.
  onReveal?: (itemId: number) => void
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
  // reviewTab is the review sheet's tab it opens on, the one the user last chose, and
  // onReviewTab hears each choice, for the window to remember.
  reviewTab?: ReviewTab | undefined
  onReviewTab?: ((tab: ReviewTab) => void) | undefined
  // conversationOrder is the order both sheets list a conversation in, the one the user
  // last chose, and onConversationOrder hears each choice, for the window to remember.
  conversationOrder?: ConversationOrder | undefined
  onConversationOrder?: ((order: ConversationOrder) => void) | undefined
  // onItemOpened hears each item opened in its sheet, once per opening, however it was
  // opened: the window marks the item's notifications read.
  onItemOpened?: ((itemId: number) => void) | undefined
  // onShowCardGuide opens the card guide, from the "How to read a card" button beside the
  // last-refreshed time; without it the inbox offers no way to.
  onShowCardGuide?: (() => void) | undefined
  // ignoring is Settings → Reviewing's Ignored workflows, which the review sheet names and
  // adds to; without it, the sheet offers to ignore none.
  ignoring?: IgnoredWorkflows | null | undefined
}

// SheetDrafts are the drafts of both sheets over the inbox: the review sheet's and the issue
// sheet's.
export interface SheetDrafts {
  review: ReviewDrafts
  issue: IssueDrafts
}

// noRepoRules are the rules until the settings have loaded.
const noRepoRules: RepoRules = { pinned: [], muted: [] }

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
  // came is the step through the list's cards the sheet came to the card by, or null when
  // it opened on it, as from a card, its menu, or a link: a card found merged or closed as
  // it shows is stepped past the way the sheet came (stepPast).
  came: Came | null
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
export function resultTone(result: ReviewResult): 'warning' | 'success' {
  return result.outcome === 'approved_not_merged' ||
    result.outcome === 'commented_not_merged' ||
    result.outcome === 'commented_not_closed'
    ? 'warning'
    : 'success'
}

// sections are the inbox's lists as its sections show them, in order.
const sections: readonly { key: ListKey; title: string }[] = inboxLists.map((key) => ({ key, title: listTitles[key] }))

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

// InboxView shows the inbox lists across the full width, with a refresh control, the
// time of the last refresh, and the card guide's button beside it; only a list that holds
// something, in any of its tabs, shows.
// Each list has tabs for what was snoozed or dismissed from it and collapses from its
// header, and each triage action is confirmed by a toast with an Undo. When every list is
// empty, the inbox throws a party in their place (InboxNil), above any list that still
// holds what was snoozed, dismissed, or muted. Above such a list the party is a strip until
// its run opens it in full, and with celebrations off it is always the strip
// (partyCompact); turning a list to what was set aside folds it into the strip, keeping
// that list where it stood. Each party reports to the milestones (reachedNil), which say
// what it was worth and whether confetti flies. The account and the API budget live in
// the toolbar.
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
  onReveal,
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
  reviewTab = 'files',
  onReviewTab,
  conversationOrder = 'oldest',
  onConversationOrder,
  onItemOpened,
  onShowCardGuide,
  ignoring = null,
}: InboxViewProps) {
  const { snapshot, loadError, refreshError, refreshing, progress, caughtUp, refresh } = inbox
  const { order, setStatusSort, setStatusOrder, setListKind } = inboxOrder
  // chosenOrder is the conversations' order the user chose here, shared by both sheets at
  // once, until the stored setting they remember it in comes back the same.
  const [chosenOrder, setChosenOrder] = useState<ConversationOrder | null>(null)
  const conversation = chosenOrder ?? conversationOrder

  const chooseConversation = (next: ConversationOrder) => {
    setChosenOrder(next)
    onConversationOrder?.(next)
  }
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
  // filling is the inbox's first refresh under way, which nothing stored stands in for yet.
  const filling = refreshing && snapshot !== null && snapshot.lastRefreshedAt === ''
  // An empty inbox throws no party until the window has seen no refresh under way, so a
  // stored empty inbox doesn't celebrate while the app's start is still reading GitHub.
  const nil =
    caughtUp &&
    snapshot !== null &&
    triaged !== null &&
    collapsed !== null &&
    error === '' &&
    isInboxNil(snapshot, triaged)
  // mode is how the window celebrates (Settings → Appearance).
  const mode = useCelebrations()
  // busy records that the inbox has held something while the page was showing, since its
  // last party, so emptying it from here earns the party, while coming to the page at nil,
  // or the party showing again with nothing come back (a filter cleared), is finding it
  // empty. The page stays mounted while another shows, so hiding it forgets busy: an inbox
  // emptied meanwhile is found at nil.
  const [busy, setBusy] = useState(false)
  const [visit, setVisit] = useState({ showing, n: 0 })
  // peak is the most the inbox has held on this visit since its last party, which the next
  // party reports as what the user cleared on the way.
  const [peak, setPeak] = useState(0)
  // fold is how the user left the party on this visit (partyCompact): turning a list to
  // what was set aside, or the party's collapse button, folds it into its strip, and the
  // strip's run opens it again. Each visit starts as it comes.
  const [fold, setFold] = useState<PartyFold>('auto')
  const party = nil && filter === null
  // thrown numbers each party, one each time the page reaches nil while it shows or shows at
  // nil, with what it reports to the milestones: whether the user emptied the inbox, and the
  // most it held on the way.
  const [thrown, setThrown] = useState<{ n: number; earned: boolean; cleared: number } | null>(null)
  const live = party && showing
  const [wasLive, setWasLive] = useState(false)
  // reached is what the latest party was worth, once the milestones answer, by its number,
  // with the flourish it plays; its moment is null when they could not answer.
  const [reached, setReached] = useState<{ n: number; moment: NilMoment | null; flourish: Flourish } | null>(null)
  // lastFlourish is the flourish the last party that played one played, so the next plays
  // another. It outlives the visits, as the page stays mounted.
  const lastFlourish = useRef<Flourish | null>(null)
  // setAside says a list still shows below the party, which it can fold away for.
  const setAside = triaged !== null && listsShow(triaged)
  const compact = partyCompact(fold, setAside, mode)
  // held is the list a tab was chosen in as it folded the party, and where it stood in
  // the window then; the fold keeps it there rather than pulling the tab just chosen up
  // from under the pointer.
  const held = useRef<{ el: Element; top: number } | null>(null)
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
    setPeak(0)
    setFold('auto')
  } else if (live !== wasLive) {
    // A party starts once the visit has settled, so it reports this visit's busy and peak,
    // and the next counts afresh from there.
    setWasLive(live)

    if (live) {
      setThrown({ n: (thrown?.n ?? 0) + 1, earned: busy, cleared: peak })
      setBusy(false)
      setPeak(0)
    }
  } else if (showing && count > 0 && (!busy || count > peak)) {
    setBusy(true)
    setPeak(Math.max(peak, count))
  }

  // Each party reports to the milestones once, and shows once they answer: a local write,
  // a few milliseconds. Its flourish is picked then, never the last one played, and only a
  // party that plays one moves lastFlourish on.
  const settle = useEffectEvent((n: number, moment: NilMoment | null) => {
    const flourish = nextFlourish(
      lastFlourish.current,
      Math.random,
      nilChip(moment) === null ? plainFlourishes : flourishes,
    )

    if (mode !== 'off' && !(mode === 'milestones' && moment?.confetti === true)) {
      lastFlourish.current = flourish
    }

    setReached({ n, moment, flourish })
  })
  const report = useEffectEvent((n: number, earned: boolean, cleared: number) => {
    reachedNil({ earned, cleared, loud: mode === 'milestones' }).then(
      (moment) => {
        settle(n, moment)
      },
      () => {
        settle(n, null)
      },
    )
  })
  // reported is the last party reported, so a remount in development reports none twice.
  const reported = useRef(0)

  useEffect(() => {
    if (thrown === null || reported.current === thrown.n) {
      return
    }

    reported.current = thrown.n
    report(thrown.n, thrown.earned, thrown.cleared)
  }, [thrown])

  // chooseTab folds the party once the user turns a list to what was set aside while it
  // shows: it has had its moment, and the page is theirs to work in. It stays folded for
  // the visit, so dismissing an item again after restoring it, which empties the inbox
  // anew, keeps the party to its strip.
  const chooseTab = (tab: TriageTab, section: Element | null) => {
    if (tab === 'inbox' || !party) {
      return
    }

    if (!compact && section !== null) {
      held.current = { el: section, top: section.getBoundingClientRect().top }
    }

    setFold('folded')
  }

  useLayoutEffect(() => {
    const at = held.current

    held.current = null

    if (at !== null) {
      holdPlace(at.el, at.top)
    }
  }, [fold])

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

  useEffect(() => {
    if (sheets) {
      preloadSheets()
    }
  }, [sheets])
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
      setViewing({ id: item.id, kind: item.kind, list, tab, arrow: '', back: null, refocus: 0, came: null })
    }

    return byList(open)
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
      came: null,
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

    logFailure(
      offerReleaseAfterMerge(r.repo.slice(0, slash), r.repo.slice(slash + 1), r.baseRef),
      'could not ask whether the merge offers a release:',
      (offer) => {
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
      },
    )
  }

  // What every row's controls and menu do, and the triage a sheet's header makes, each said
  // in a toast.
  const { triage, edits, dismissIt, snoozeIt, restoreIt, changeRule, unassignOn, seen, ended, nudgeDialog } =
    useItemActions({
      inbox,
      snoozeDays,
      onSnoozeSettings,
      repoRules,
      onSettingsSaved,
      onRepoRuleSettings,
      showToast,
      closeToast,
      onOpen: sheets ? openFromMenu : undefined,
      onManageReplies,
    })

  // Each opening of a sheet is heard once, and marks read the item it opened on and the one
  // it moved from.
  const viewingId = viewing === null || !sheets ? 0 : viewing.id

  useSheetVisits(viewingId, seen, onItemOpened)

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
    came: cameBy(siblings, from.id, id),
  })

  // step moves a sheet to the card with id.
  const step = (id: number) => {
    if (viewing !== null) {
      setViewing(toCard(viewing, id))
    }
  }
  const initialFocus = viewing === null || viewing.arrow === '' ? undefined : `[data-step="${viewing.arrow}"]`

  // cardLinks are the links between the lists' cards, an issue and the pull requests that
  // close it, which show the linked card here (LinkChip).
  const cardLinks: CardLinks | null =
    snapshot === null || onReveal === undefined ? null : { index: linkIndex(snapshot), reveal: onReveal }

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
        came: null,
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

  // endedHere moves the sheet on from a card a read of it found merged or closed since the
  // inbox's last refresh, which that read took out of the inbox already (the item's own
  // read, which stores what it found and tells the window): past it, the way the sheet
  // stepped to it, else back to the sheet a link to it was followed from, else, opened on
  // it, the sheet closes. Each marks it read as any move does, and the toast says what
  // became of it.
  const endedHere = (from: Viewing, gone: EndedItem) => {
    const past = stepPast(from.came, gone.id, (id) => siblings.some((s) => s.id === id))

    if (past !== undefined) {
      setViewing({ ...toCard(from, past.to), came: past.came })
    } else if (from.back !== null) {
      setViewing({ ...from.back.to, refocus: from.refocus + 1 })
    } else {
      setViewing(null)
    }

    ended(gone)
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
  // sheetProps are what both sheets take alike, for the card the sheet shows.
  const sheetProps: SheetCommonProps | null =
    viewing === null
      ? null
      : {
          id: viewing.id,
          item: viewed,
          siblings,
          listTitle,
          order: conversation,
          onOrder: chooseConversation,
          launch,
          resolution: resolutions.get(viewing.id) ?? null,
          sessions: viewed === undefined ? undefined : sessionsOf(sessions, viewed),
          worktree: viewed === undefined ? undefined : worktreeOf(worktrees, viewed),
          onNavigate: step,
          onClose: () => {
            setViewing(null)
          },
          onEnded: (gone) => {
            endedHere(viewing, gone)
          },
          onManageReplies,
          initialFocus,
          refocus: viewing.refocus,
          back,
          triage: sheetTriageButtons,
          toast: sheetToast,
        }

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="relative flex items-center gap-3 text-sm">
        <button
          type="button"
          disabled={refreshing}
          onClick={refresh}
          className="flex items-center gap-1.5 rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised disabled:opacity-disabled"
        >
          <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
          {refreshing ? 'Refreshing…' : 'Refresh'}
        </button>
        {refreshing ? (
          <ProgressStatus step={progressStep(progress)} />
        ) : (
          <span className="text-muted">
            {snapshot === null || snapshot.lastRefreshedAt === '' ? (
              'Not refreshed yet'
            ) : (
              <>
                Last refreshed <RelativeTime iso={snapshot.lastRefreshedAt} show={formatAgo} />
              </>
            )}
          </span>
        )}
        {onShowCardGuide !== undefined && (
          <button
            type="button"
            aria-label="How to read a card"
            title="How to read a card"
            onClick={onShowCardGuide}
            className={`${iconButtonClass} border border-border`}
          >
            <HelpIcon className="size-3.5" />
          </button>
        )}
        {refreshing && (
          <div
            aria-hidden="true"
            data-testid="inbox-loading-bar"
            className="loading-bar absolute inset-x-0 -bottom-2.25"
          />
        )}
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
      {party && thrown !== null && reached?.n === thrown.n && (
        <InboxNil
          key={thrown.n}
          stats={triaged}
          earned={thrown.earned}
          moment={reached.moment}
          flourish={reached.flourish}
          mode={mode}
          compact={compact}
          onOpen={() => {
            setFold('open')
          }}
          onCollapse={
            setAside
              ? () => {
                  setFold('folded')
                }
              : undefined
          }
        />
      )}
      {filling ? (
        <InboxFilling />
      ) : triaged === null || collapsed === null || order === null ? (
        loadError === '' && <p className="text-sm text-muted">Loading the inbox…</p>
      ) : (
        <CardLinksContext value={cardLinks}>
          {sections.map(({ key, title }) => (
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
              onTabChange={chooseTab}
            />
          ))}
        </CardLinksContext>
      )}
      <LinksContext value={links}>
        <Suspense fallback={null}>
          {sheetProps !== null && sheets && viewing?.kind === 'pr' && (
            <ReviewSheet
              {...sheetProps}
              drafts={drafts.review}
              tab={reviewTab}
              onTab={(tab) => {
                onReviewTab?.(tab)
              }}
              onDone={(result, r) => {
                setViewing(null)
                showToast({ text: result.message, detail: r.title, tone: resultTone(result) })

                if (result.outcome === 'merged') {
                  offerRelease(r, toastCount.current)
                }
              }}
              onFilesSettings={onFilesSettings}
              onToast={showToast}
              ignoring={ignoring}
            />
          )}
          {sheetProps !== null && sheets && viewing?.kind === 'issue' && (
            <IssueSheet
              {...sheetProps}
              drafts={drafts.issue}
              onDone={(result, is) => {
                setViewing(null)
                showToast({ text: result.message, detail: is.title, tone: issueTone(result) })
              }}
              onUnassign={unassignHere}
            />
          )}
        </Suspense>
      </LinksContext>
      {nudgeDialog}
      <Toast toast={sheetOpen ? null : toast} seconds={toastSeconds} onClose={closeToast} />
    </div>
  )
}
