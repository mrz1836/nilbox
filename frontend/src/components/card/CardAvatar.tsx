import type { InboxItem } from '../../platform'
import { BotIcon, IssueIcon, PullRequestIcon } from '../icons'

// faces are the backings an author's initials can sit on, each from the theme's tokens,
// spelled out in full so Tailwind finds them. A login always picks the same one.
const faces: readonly string[] = [
  'bg-accent/22 text-accent',
  'bg-success/22 text-success',
  'bg-warning/22 text-warning',
  'bg-info/22 text-info',
  'bg-danger/18 text-danger',
  'bg-muted/25 text-text',
]

// faceOf picks the backing for a login: a stable hash of it into faces.
export function faceOf(login: string): string {
  let hash = 0

  for (const char of login) {
    hash = (hash * 31 + char.charCodeAt(0)) % 1_000_003
  }

  return faces[hash % faces.length] ?? ''
}

// initialsOf is what stands for a login on its avatar: the first letters of its first
// two words, as in "CA" for contributor-a, or its first two letters, as in "OC" for octocat.
export function initialsOf(login: string): string {
  const words = login.split(/[-_.]+/).filter((word) => word !== '')
  const [first = '', second] = words
  const initials = second === undefined ? first.slice(0, 2) : `${first.slice(0, 1)}${second.slice(0, 1)}`

  return initials.toUpperCase()
}

// KindBadge tells a pull request from an issue at the avatar's corner: green while open,
// muted for a draft.
function KindBadge({ item }: { item: InboxItem }) {
  const isPR = item.kind === 'pr'
  const label = isPR ? (item.isDraft ? 'Draft pull request' : 'Pull request') : 'Issue'
  const color = isPR && item.isDraft ? 'text-muted' : 'text-success'

  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={`absolute -right-1 -bottom-1 z-10 grid size-4 place-items-center rounded-full bg-surface ${color}`}
    >
      {isPR ? <PullRequestIcon className="size-3" /> : <IssueIcon className="size-3" />}
    </span>
  )
}

// CardAvatar is who opened a card's item, with what kind of item it is: a circle with the
// author's initials on a color their login picks, or a rounded square with the bot mark
// for a bot, and a kind badge at its corner. The user's own items get an accent ring. It
// is not a button: the byline names the author.
export function CardAvatar({ item }: { item: InboxItem }) {
  const ring = item.authored ? 'ring-2 ring-accent' : ''

  return (
    <span className="relative inline-flex size-7 shrink-0">
      {item.authorIsBot ? (
        <span
          aria-hidden="true"
          data-avatar="bot"
          className={`grid size-7 place-items-center rounded-lg bg-info/18 text-info ${ring}`}
        >
          <BotIcon className="size-4" />
        </span>
      ) : (
        <span
          aria-hidden="true"
          data-avatar="initials"
          className={`grid size-7 place-items-center rounded-full text-[0.6875rem] font-semibold ${faceOf(item.author)} ${ring}`}
        >
          {initialsOf(item.author)}
        </span>
      )}
      <KindBadge item={item} />
    </span>
  )
}
