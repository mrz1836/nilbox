import { formatAge, formatAgo } from '../lib/age'
import { formatFull, formatWhen, isBackFromSnooze, wakeSummary, type TriageTab } from '../lib/triage'
import {
  openInBrowser,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type Profile,
  type Prompt,
} from '../platform'
import { ArchiveIcon, ClockIcon, IssueIcon, PullRequestIcon } from './icons'
import { ItemSignals } from './ItemSignals'
import { LaunchControls } from './LaunchControls'
import { SensitiveFiles } from './SensitiveFiles'
import { TriageControls, type TriageActions } from './TriageControls'

// LaunchOptions are what each row's launch controls need: the profiles and prompts to
// choose from, each item's resolved launch settings, and the saved launch defaults.
export interface LaunchOptions {
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  launchDefaults: readonly LaunchDefaults[]
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
  onDefaultsSaved: () => Promise<void>
  onManagePrompts: () => void
}

export interface ItemRowProps {
  item: InboxItem
  now: Date
  // launch is null until the launch settings have loaded; the row has no launch button
  // until then.
  launch: LaunchOptions | null
  // tab is the list tab the row is in: the inbox, Snoozed, or Dismissed.
  tab?: TriageTab
  // triage is what the row's snooze, dismiss, and restore buttons do; without it the row
  // has none.
  triage?: TriageActions
  // onFilesSettings opens Settings → Sensitive files from the sensitive-file panel.
  onFilesSettings?: (() => void) | undefined
  // highlighted marks the row the menu bar or a notification just asked to show.
  highlighted?: boolean
}

// tagClass is a status tag: weight and color mark it, with no pill around it.
const tagClass = 'shrink-0 text-[11px] leading-4 font-medium'

function openItem(url: string) {
  openInBrowser(url).catch((err: unknown) => {
    console.error('could not open the item in the browser', err)
  })
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
function TrustTag({ item }: { item: InboxItem }) {
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
        title="A bot that isn't trusted, so launches start in plan mode. Add it to your trusted bots in Settings → Trust."
        className={`${tagClass} text-warning`}
      >
        untrusted bot
      </span>
    )
  }

  return (
    <span title="Launches in plan mode unless you lift the restriction" className={`${tagClass} text-warning`}>
      outside contributor
    </span>
  )
}

// TriageTag says where a snoozed or dismissed item stands, and marks an item just back
// from a snooze, so it is easy to spot among the rest.
function TriageTag({ item, tab, now }: { item: InboxItem; tab: TriageTab; now: Date }) {
  const quiet = `${tagClass} inline-flex items-center gap-1 text-muted`

  if (tab === 'snoozed') {
    return (
      <span title={`Snoozed until ${formatFull(item.snoozedUntil)}`} className={quiet}>
        <ClockIcon className="size-3" />
        Back {formatWhen(now, new Date(item.snoozedUntil))}
      </span>
    )
  }

  if (tab === 'dismissed') {
    return (
      <span title={`Dismissed ${formatFull(item.dismissedAt)}`} className={quiet}>
        <ArchiveIcon className="size-3" />
        Dismissed {formatAgo(now, item.dismissedAt)}
      </span>
    )
  }

  if (isBackFromSnooze(item, now)) {
    // A snooze that new activity ended early says what was new.
    const woke = wakeSummary(item.wokeBy)

    return (
      <span
        title={
          woke === ''
            ? `Snoozed until ${formatFull(item.snoozedUntil)}`
            : `Back early, ${formatFull(item.snoozedUntil)}: ${woke} since you snoozed it`
        }
        className="inline-flex min-w-0 items-center gap-1 text-[11px] leading-4 font-medium text-info"
      >
        <ClockIcon className="size-3 shrink-0" />
        <span className="truncate">{woke === '' ? 'Back from snooze' : `Back early: ${woke}`}</span>
      </span>
    )
  }

  return null
}

// ItemRow is one inbox card, three lines high: the title with its draft and conflict
// tags; where it is, who opened it, when it last moved, and, for a pull request from
// outside the repository, the sensitive files it changes; and its signals (checks,
// reviews, comments, size, labels). The triage buttons and the launch button sit on the
// right, and a launch's outcome spans the card below. Long titles are cut short with an
// ellipsis, so every card keeps the same shape.
export function ItemRow({
  item,
  now,
  launch,
  tab = 'inbox',
  triage,
  onFilesSettings,
  highlighted = false,
}: ItemRowProps) {
  const conflicting = item.details?.mergeable === 'CONFLICTING'

  return (
    <li
      data-item-id={item.id}
      data-highlighted={highlighted || undefined}
      className="group grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-start gap-x-3 border-b border-border px-4 py-3 transition-colors last:border-b-0 hover:bg-raised/40 data-highlighted:bg-accent/10"
    >
      <KindIcon item={item} />
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <button
            type="button"
            title={item.title}
            onClick={() => {
              openItem(item.url)
            }}
            className="min-w-0 truncate text-left text-sm font-medium text-text hover:text-accent hover:underline"
          >
            {item.title}
          </button>
          {item.isDraft && <span className={`${tagClass} text-muted`}>Draft</span>}
          {conflicting && (
            <span title="Has merge conflicts with its base branch" className={`${tagClass} text-danger`}>
              Conflicts
            </span>
          )}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs text-muted">
          <span className="truncate">
            {item.repo} #{item.number} · {item.author} ·{' '}
            <time dateTime={item.updatedAt}>{formatAge(now, item.updatedAt)}</time>
          </span>
          <TrustTag item={item} />
          {item.files !== null && (
            <SensitiveFiles itemId={item.id} check={item.files} now={now} onSettings={onFilesSettings} />
          )}
          <TriageTag item={item} tab={tab} now={now} />
        </div>
        <ItemSignals item={item} />
      </div>
      {triage !== undefined && <TriageControls item={item} tab={tab} now={now} actions={triage} />}
      {launch !== null && (
        <LaunchControls
          item={item}
          resolution={launch.resolutions.get(item.id) ?? null}
          profiles={launch.profiles}
          prompts={launch.prompts}
          launchDefaults={launch.launchDefaults}
          onDefaultsSaved={launch.onDefaultsSaved}
          onManagePrompts={launch.onManagePrompts}
        />
      )}
    </li>
  )
}
