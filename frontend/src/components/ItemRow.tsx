import { memo } from 'react'

import { counted } from '../lib/format'
import { agentName } from '../lib/launch'
import { runningSession } from '../lib/sessions'
import { useRowWork } from '../lib/rowWork'
import { formatAge, formatAgo, formatFull, formatWhen } from '../lib/time'
import { isBackFromSnooze, wakeSummary, type TriageTab } from '../lib/triage'
import { useNowAs } from '../lib/useNow'
import {
  openExternal,
  type AgentInfo,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type AgentSession,
  type Profile,
  type Prompt,
  type Worktree,
} from '../platform'
import { Highlight } from './Highlight'
import { ArchiveIcon, ClockIcon, ExternalIcon, IssueIcon, PullRequestIcon } from './icons'
import { ItemMenu, type EditActions } from './ItemMenu'
import { ItemSignals } from './ItemSignals'
import { LaunchControls } from './LaunchControls'
import { RelativeTime } from './RelativeTime'
import { SensitiveFiles } from './SensitiveFiles'
import { SessionTag } from './SessionTag'
import { TriageControls, type TriageActions } from './TriageControls'

// LaunchOptions are what every row's launch controls share: the coding agents, profiles,
// and prompts to choose from, and the saved launch defaults. Each row's own resolved
// settings and worktree come apart from them, so a change to one item's renders only its
// row.
export interface LaunchOptions {
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  launchDefaults: readonly LaunchDefaults[]
  onDefaultsSaved: () => Promise<void>
  onManagePrompts: () => void
}

export interface ItemRowProps {
  item: InboxItem
  // launch is null until the launch settings have loaded; the row has no launch button
  // until then.
  launch: LaunchOptions | null
  // resolution is the item's resolved launch settings, or null while they load.
  resolution?: ItemResolution | null
  // worktree is the item's worktree, as the Worktrees view last listed it, if it has one.
  worktree?: Worktree | undefined
  // sessions are the item's agent sessions, newest first, if it has any.
  sessions?: readonly AgentSession[] | undefined
  // tab is the list tab the row is in: the inbox, Snoozed, or Dismissed.
  tab?: TriageTab
  // triage is what the row's snooze, dismiss, and restore buttons do; without it the row
  // has none.
  triage?: TriageActions
  // edits is what the row's menu does beyond triage: copy the link and the edits on
  // GitHub. The menu shows with the triage buttons, when the row has both.
  edits?: EditActions | undefined
  // onFilesSettings opens Settings → Sensitive files from the sensitive-file panel.
  onFilesSettings?: (() => void) | undefined
  // highlighted marks the row the menu bar or a notification just asked to show.
  highlighted?: boolean
  // marks is what the inbox's filter looks for, which the title highlights; a plain
  // string, so the row renders again only when it changes.
  marks?: string
  // onFilterBy adds a filter to the inbox's query, or takes it away: the row's
  // repository, author, and labels offer it when clicked. Without it they are plain text.
  onFilterBy?: ((key: string, value: string) => void) | undefined
  // onReview opens a pull request in the review sheet, from the row's tab: its title does,
  // and ⌘-click or the ↗ beside it opens GitHub instead. Without it, and for an issue, the
  // title opens GitHub. It must keep its identity, so the row keeps its own.
  onReview?: ((id: number, tab: TriageTab) => void) | undefined
}

// tagClass is a status tag: weight and color mark it, with no pill around it.
const tagClass = 'shrink-0 text-[0.6875rem] leading-4 font-medium'

interface FilterTextProps {
  // filterKey and value are the filter the text adds, as in repo and its name; name
  // says what it is, as in "repository".
  filterKey: string
  value: string
  name: string
  onFilterBy: ((key: string, value: string) => void) | undefined
}

// FilterText is a card's repository or author: plain text, or a quiet inline button that
// filters the inbox by it.
function FilterText({ filterKey, value, name, onFilterBy }: FilterTextProps) {
  if (onFilterBy === undefined) {
    return value
  }

  return (
    <button
      type="button"
      aria-label={`Filter by ${name} ${value}`}
      title={`Filter by ${name} ${value}`}
      onClick={() => {
        onFilterBy(filterKey, value)
      }}
      className="rounded-sm hover:text-text hover:underline"
    >
      {value}
    </button>
  )
}

// KindIcon tells a pull request from an issue: green while open, muted for a draft.
function KindIcon({ item }: { item: InboxItem }) {
  const isPR = item.kind === 'pr'
  const label = isPR ? (item.isDraft ? 'Draft pull request' : 'Pull request') : 'Issue'
  const color = item.isDraft ? 'text-muted' : 'text-success'

  return (
    <span role="img" aria-label={label} title={label} className={`mt-0.5 ${color}`}>
      {isPR ? <PullRequestIcon className="size-4" /> : <IssueIcon className="size-4" />}
    </span>
  )
}

// TrustTag marks an item by who opened it: a trusted bot quietly, and an untrusted bot or
// an outside contributor as a warning, since their items launch in plan mode. Items from
// the repository's own people carry no tag.
export function TrustTag({ item }: { item: InboxItem }) {
  if (item.trust === 'bot') {
    return (
      <span
        title="A trusted bot: launches use the profile's permissions. Manage trusted bots in Settings → Trust."
        className={`${tagClass} text-muted`}
      >
        bot
      </span>
    )
  }

  if (item.trusted) {
    return null
  }

  if (item.authorIsBot) {
    return (
      <span
        title="A bot that isn't trusted, so launches start in plan or read-only mode. Add it to your trusted bots in Settings → Trust."
        className={`${tagClass} text-warning`}
      >
        untrusted bot
      </span>
    )
  }

  return (
    <span
      title="Launches in plan or read-only mode unless you lift the restriction"
      className={`${tagClass} text-warning`}
    >
      outside contributor
    </span>
  )
}

// mergeMethodWords names auto-merge's method on a card.
const mergeMethodWords: Readonly<Record<string, string>> = { SQUASH: 'squash', MERGE: 'merge commit', REBASE: 'rebase' }

// AutoMergeTag marks a pull request GitHub merges on its own once its requirements are
// met, with the method it uses.
function AutoMergeTag({ method }: { method: string }) {
  return (
    <span title="GitHub merges it once its requirements are met" className={`${tagClass} text-success`}>
      Auto-merge · {mergeMethodWords[method] ?? method.toLowerCase()}
    </span>
  )
}

// SnoozedTag says when a snoozed item comes back.
function SnoozedTag({ item, className }: { item: InboxItem; className: string }) {
  const back = useNowAs((now) => formatWhen(now, new Date(item.snoozedUntil)))

  return (
    <span title={`Snoozed until ${formatFull(item.snoozedUntil)}`} className={className}>
      <ClockIcon className="size-3" />
      Back {back}
    </span>
  )
}

// DismissedTag says how long ago an item was dismissed.
function DismissedTag({ item, className }: { item: InboxItem; className: string }) {
  const ago = useNowAs((now) => formatAgo(now, item.dismissedAt))

  return (
    <span title={`Dismissed ${formatFull(item.dismissedAt)}`} className={className}>
      <ArchiveIcon className="size-3" />
      Dismissed {ago}
    </span>
  )
}

// BackTag marks an item back from a snooze within the last day; a snooze that new
// activity ended early says what was new.
function BackTag({ item }: { item: InboxItem }) {
  const back = useNowAs((now) => isBackFromSnooze(item, now))

  if (!back) {
    return null
  }

  const woke = wakeSummary(item.wokeBy)

  return (
    <span
      title={
        woke === ''
          ? `Snoozed until ${formatFull(item.snoozedUntil)}`
          : `Back early, ${formatFull(item.snoozedUntil)}: ${woke} since you snoozed it`
      }
      className="inline-flex min-w-0 items-center gap-1 text-[0.6875rem] leading-4 font-medium text-info"
    >
      <ClockIcon className="size-3 shrink-0" />
      <span className="truncate">{woke === '' ? 'Back from snooze' : `Back early: ${woke}`}</span>
    </span>
  )
}

// TriageTag says where a snoozed or dismissed item stands, and marks an item just back
// from a snooze, so it is easy to spot among the rest. Each tag reads the clock itself,
// so a tick renders a tag only when its words change.
function TriageTag({ item, tab }: { item: InboxItem; tab: TriageTab }) {
  const quiet = `${tagClass} inline-flex items-center gap-1 text-muted`

  switch (tab) {
    case 'snoozed':
      return <SnoozedTag item={item} className={quiet} />
    case 'dismissed':
      return <DismissedTag item={item} className={quiet} />
    default:
      // An item never snoozed has nothing to mark, nor any reason to follow the clock.
      return item.snoozedUntil === '' ? null : <BackTag item={item} />
  }
}

// WorktreeTag marks an item that has a worktree, as the Worktrees view last listed it: its
// uncommitted files, and the agent and age of its latest session, as in "worktree · 2
// changes · Codex 3h ago".
function WorktreeTag({ wt, agents }: { wt: Worktree; agents: readonly AgentInfo[] }) {
  const parts = ['worktree']

  if (wt.dirtyCount > 0) {
    parts.push(counted(wt.dirtyCount, 'change', 'changes'))
  }

  const busy = wt.inUse.length > 0
  const last = !busy && wt.agent !== '' && wt.lastLaunchedAt !== ''

  return (
    <span title={wt.displayPath} className={`${tagClass} shrink-0 text-muted`}>
      {parts.join(' · ')}
      {busy && ' · in use'}
      {last && (
        <>
          {` · ${agentName(agents, wt.agent)} `}
          <RelativeTime iso={wt.lastLaunchedAt} show={formatAgo} />
        </>
      )}
    </span>
  )
}

// ItemRow is one inbox card, three lines high: the title with its draft and conflict
// tags; where it is, who opened it, when it last moved, and, for a pull request from
// outside the repository, the sensitive files it changes; and its signals (checks,
// reviews, comments, size, labels). The triage buttons, the menu of everything else (see
// ItemMenu), and the launch button sit on the right, and a launch's outcome spans the
// card below. The row does one thing with GitHub at a time, which its buttons and its
// menu share (useRowWork). Long titles are cut short with an
// ellipsis, so every card keeps the same shape. A card off screen is not rendered until
// it scrolls near (item-row in index.css).
//
// The row is wrapped in memo: the inbox renders its rows in a loop, where the React
// Compiler can't keep each row's element from one render to the next, so without memo a
// change to one item would render every row in its list again.
export const ItemRow = memo(function ItemRow({
  item,
  launch,
  resolution = null,
  worktree,
  sessions,
  tab = 'inbox',
  triage,
  edits,
  onFilesSettings,
  highlighted = false,
  marks = '',
  onFilterBy,
  onReview,
}: ItemRowProps) {
  const conflicting = item.details?.mergeable === 'CONFLICTING'
  const autoMerge = item.details?.autoMerge ?? ''
  const reviews = item.kind === 'pr' && onReview !== undefined
  const [work, start] = useRowWork()

  return (
    <li
      data-item-id={item.id}
      data-highlighted={highlighted || undefined}
      className="item-row group grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-start gap-x-3 border-b border-border px-4 py-3 transition-colors last:border-b-0 hover:bg-raised/40 data-highlighted:bg-accent/10"
    >
      <KindIcon item={item} />
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <button
            type="button"
            title={reviews ? `${item.title}\nReview it here; ⌘-click opens GitHub` : item.title}
            onClick={(e) => {
              if (reviews && !e.metaKey) {
                onReview(item.id, tab)
              } else {
                openExternal(item.url)
              }
            }}
            className="min-w-0 truncate text-left text-sm font-medium text-text hover:text-accent hover:underline"
          >
            <Highlight text={item.title} marks={marks} />
          </button>
          {reviews && (
            <button
              type="button"
              aria-label="Open on GitHub"
              title="Open on GitHub"
              onClick={() => {
                openExternal(item.url)
              }}
              className="shrink-0 rounded-sm text-muted/70 opacity-0 transition-opacity group-hover:opacity-100 hover:text-accent focus-visible:opacity-100"
            >
              <ExternalIcon className="size-3" />
            </button>
          )}
          {item.isDraft && <span className={`${tagClass} text-muted`}>Draft</span>}
          {autoMerge !== '' && <AutoMergeTag method={autoMerge} />}
          {conflicting && (
            <span title="Has merge conflicts with its base branch" className={`${tagClass} text-danger`}>
              Conflicts
            </span>
          )}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs text-muted">
          <span className="truncate">
            <FilterText filterKey="repo" value={item.repo} name="repository" onFilterBy={onFilterBy} /> #{item.number}
            {' · '}
            <FilterText filterKey="author" value={item.author} name="author" onFilterBy={onFilterBy} />
            {' · '}
            <RelativeTime iso={item.updatedAt} show={formatAge} />
          </span>
          <TrustTag item={item} />
          {item.files !== null && <SensitiveFiles itemId={item.id} check={item.files} onSettings={onFilesSettings} />}
          <TriageTag item={item} tab={tab} />
          {worktree !== undefined && <WorktreeTag wt={worktree} agents={launch?.agents ?? []} />}
          {sessions !== undefined && <SessionTag sessions={sessions} agents={launch?.agents ?? []} />}
        </div>
        <ItemSignals item={item} onFilterBy={onFilterBy} />
      </div>
      {triage !== undefined && (
        <div className="flex items-center gap-0.5 self-center">
          <TriageControls item={item} tab={tab} actions={triage} work={work} start={start} />
          {edits !== undefined && (
            <ItemMenu item={item} tab={tab} triage={triage} edits={edits} work={work} start={start} />
          )}
        </div>
      )}
      {launch !== null && (
        <LaunchControls
          item={item}
          resolution={resolution}
          agents={launch.agents}
          profiles={launch.profiles}
          prompts={launch.prompts}
          launchDefaults={launch.launchDefaults}
          onDefaultsSaved={launch.onDefaultsSaved}
          onManagePrompts={launch.onManagePrompts}
          running={runningSession(sessions)}
        />
      )}
    </li>
  )
})
