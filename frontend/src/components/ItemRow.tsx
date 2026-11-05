import { formatAge } from '../lib/age'
import {
  openInBrowser,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type Profile,
  type Prompt,
} from '../platform'
import { IssueIcon, PullRequestIcon } from './icons'
import { ItemSignals } from './ItemSignals'
import { LaunchControls } from './LaunchControls'

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
}

const tagClass = 'shrink-0 rounded-full border px-1.5 py-px text-[11px] leading-4 font-medium'

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
        className={`${tagClass} border-border text-muted`}
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
        className={`${tagClass} border-warning text-warning`}
      >
        untrusted bot
      </span>
    )
  }

  return (
    <span
      title="Launches in plan mode unless you lift the restriction"
      className={`${tagClass} border-warning text-warning`}
    >
      outside contributor
    </span>
  )
}

// ItemRow is one inbox card, three lines high: the title with its draft and conflict
// tags; where it is, who opened it, and when it last moved; and its signals (checks,
// reviews, comments, size, labels). The launch button sits on the right, and a launch's
// outcome spans the card below. Long titles are cut short with an ellipsis, so every card
// keeps the same shape.
export function ItemRow({ item, now, launch }: ItemRowProps) {
  const conflicting = item.details?.mergeable === 'CONFLICTING'

  return (
    <li className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-3 border-b border-border px-4 py-3 transition-colors last:border-b-0 hover:bg-raised/40">
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
          {item.isDraft && <span className={`${tagClass} border-border text-muted`}>Draft</span>}
          {conflicting && (
            <span title="Has merge conflicts with its base branch" className={`${tagClass} border-danger text-danger`}>
              Conflicts
            </span>
          )}
        </div>
        <p className="mt-0.5 flex min-w-0 items-center gap-2 text-xs text-muted">
          <span className="truncate">
            {item.repo} #{item.number} · {item.author} ·{' '}
            <time dateTime={item.updatedAt}>{formatAge(now, item.updatedAt)}</time>
          </span>
          <TrustTag item={item} />
        </p>
        <ItemSignals item={item} />
      </div>
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
