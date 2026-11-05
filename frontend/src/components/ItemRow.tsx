import { formatAge } from '../lib/age'
import { openInBrowser, type InboxItem, type Profile } from '../platform'
import { IssueIcon, PullRequestIcon } from './icons'
import { ItemSignals } from './ItemSignals'
import { LaunchControls } from './LaunchControls'

// LaunchOptions are what each row's launch controls need from the settings and profiles.
export interface LaunchOptions {
  profiles: Profile[]
  defaultProfile: string
  worktreeDefault: boolean
}

export interface ItemRowProps {
  item: InboxItem
  now: Date
  // launch is null until the settings and profiles have loaded; the row has no launch
  // button until then.
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
          {!item.trusted && (
            <span
              title="Launches in plan mode unless you lift the restriction"
              className={`${tagClass} border-warning text-warning`}
            >
              outside contributor
            </span>
          )}
        </p>
        <ItemSignals item={item} />
      </div>
      {launch !== null && (
        <LaunchControls
          item={item}
          profiles={launch.profiles}
          defaultProfile={launch.defaultProfile}
          worktreeDefault={launch.worktreeDefault}
        />
      )}
    </li>
  )
}
