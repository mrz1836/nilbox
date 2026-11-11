import { useEffect, useLayoutEffect, useRef, useState } from 'react'

import { attempt } from '../lib/call'
import type { InboxFilter } from '../lib/inboxFilter'
import { noWorktrees } from '../lib/itemWorktrees'
import { isInboxNil } from '../lib/nil'
import type { ReviewDrafts } from '../lib/reviewDrafts'
import { noSessions, sessionsOf } from '../lib/sessions'
import { formatAgo, formatWhen } from '../lib/time'
import { findItem, isDismissed, type TriagedInbox, type TriageTab } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxOrderState } from '../lib/useInboxOrder'
import { useClock } from '../lib/useNow'
import {
  copyText,
  offerReleaseAfterMerge,
  type AfterMerge,
  type AgentSession,
  type InboxItem,
  type ItemResolution,
  type ListKey,
  type Review,
  type ReviewResult,
  type Worktree,
} from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { InboxNil } from './InboxNil'
import { teamsText, type EditActions } from './ItemMenu'
import type { LaunchOptions } from './ItemRow'
import { ListSection, type Reveal } from './ListSection'
import { ProjectsTip } from './ProjectsTip'
import { RelativeTime } from './RelativeTime'
import { ReviewSheet } from './review/ReviewSheet'
import { Toast, type ToastMessage } from './Toast'
import type { TriageActions } from './TriageControls'

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
  // drafts are the review sheet's drafts, which the window keeps for the session; without
  // them pull requests open on GitHub.
  drafts?: ReviewDrafts
  // onManageReplies opens the review replies in the Prompts tab.
  onManageReplies?: () => void
  // onRelease opens a repository's release sheet, as a merge's toast offers when Release
  // can release it; without it, no merge offers a release. It must keep its identity.
  onRelease?: (offer: AfterMerge) => void
}

// Reviewing is the pull request the review sheet shows, and the list and tab it was
// opened from, which it steps through.
interface Reviewing {
  id: number
  list: ListKey
  tab: TriageTab
}

// resultTone is how a review's outcome reads in the toast: a review GitHub took, an
// approval or the comment a merge posted first, and a merge it didn't make is a warning.
function resultTone(result: ReviewResult): 'warning' | 'success' {
  return result.outcome === 'approved_not_merged' || result.outcome === 'commented_not_merged' ? 'warning' : 'success'
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
  const { order, setStatusSort, setStatusOrder } = inboxOrder
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

  const [reviewing, setReviewing] = useState<Reviewing | null>(null)
  const reviews = drafts !== undefined
  // shown is the snapshot the lists show, for the menu's review to find an item's list in
  // when it is chosen, without a new action for every snapshot.
  const shown = useRef(snapshot)

  useLayoutEffect(() => {
    shown.current = snapshot
  }, [snapshot])

  // reviewIn opens a pull request in the sheet from each list, and reviewFromMenu from a
  // card's menu, in the first list that holds it; each is made once, so the cards, which
  // take them, keep their identity.
  const [reviewIn] = useState(() => {
    const open = (list: ListKey) => (id: number, tab: TriageTab) => {
      setReviewing({ id, list, tab })
    }

    return { reviewRequested: open('reviewRequested'), authored: open('authored'), assigned: open('assigned') }
  })
  const [reviewFromMenu] = useState(() => (item: InboxItem, tab: TriageTab) => {
    const list = sections.find(({ key }) => shown.current?.[key].items.some((i) => i.id === item.id))?.key

    setReviewing({ id: item.id, list: list ?? 'reviewRequested', tab })
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

  // triage is what every row's triage buttons do. It reads no time while rendering (the
  // toast after a snooze reads the clock when it shows), so the rows keep the same
  // actions from one tick to the next.
  const triage: TriageActions = {
    snoozeDays,
    onDismiss: (item) => {
      void dismiss(item.id).then((result) => {
        if (result.ok) {
          showToast({ text: 'Dismissed', detail: item.title, undo: undo(item) })
        } else {
          failed('dismiss', item, result.error)
        }
      })
    },
    onSnooze: (item, days) => {
      void snooze(item.id, days).then((result) => {
        if (!result.ok) {
          failed('snooze', item, result.error)

          return
        }

        const until = findItem(result.data, item.id)?.snoozedUntil ?? ''
        const text = until === '' ? 'Snoozed' : `Snoozed until ${formatWhen(clock.getSnapshot(), new Date(until))}`

        showToast({ text, detail: item.title, undo: undo(item) })
      })
    },
    onRestore: (item) => {
      void restore(item.id).then((result) => {
        if (result.ok) {
          showToast({ text: isDismissed(item) ? 'Restored' : 'Unsnoozed', detail: item.title })
        } else {
          failed('restore', item, result.error)
        }
      })
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
      const result = await unassign(item.id)

      if (result.ok) {
        showToast({ text: 'Unassigned you on GitHub', detail: item.title })
      } else {
        failed('unassign you from', item, result.error)
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
    onReview: reviews ? reviewFromMenu : undefined,
  }

  const reviewed = reviewing === null || snapshot === null ? undefined : findItem(snapshot, reviewing.id)
  const siblings =
    reviewing === null || triaged === null
      ? []
      : triaged.lists[reviewing.list][reviewing.tab].filter((i) => i.kind === 'pr')

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
                byStatus: order.statusSorted.includes(key),
                stored: order.statusOrder,
                onSortChange: (on) => {
                  setStatusSort(key, on)
                },
                onOrderChange: setStatusOrder,
              }}
              matched={filter?.lists[key]}
              marks={filter?.marks ?? ''}
              onFilterBy={onFilterBy}
              onReview={reviews ? reviewIn[key] : undefined}
            />
          ))}
      {reviewing !== null && drafts !== undefined && (
        <ReviewSheet
          id={reviewing.id}
          item={reviewed}
          siblings={siblings}
          listTitle={sections.find(({ key }) => key === reviewing.list)?.title ?? ''}
          drafts={drafts}
          launch={launch}
          resolution={resolutions.get(reviewing.id) ?? null}
          sessions={reviewed === undefined ? undefined : sessionsOf(sessions, reviewed)}
          onNavigate={(id) => {
            setReviewing({ ...reviewing, id })
          }}
          onClose={() => {
            setReviewing(null)
          }}
          onDone={(result, r) => {
            setReviewing(null)
            showToast({ text: result.message, detail: r.title, tone: resultTone(result) })

            if (result.outcome === 'merged') {
              offerRelease(r, toastCount.current)
            }
          }}
          onFilesSettings={onFilesSettings}
          onManageReplies={onManageReplies}
        />
      )}
      <Toast toast={toast} onClose={closeToast} />
    </div>
  )
}
