import type { InboxItem } from '../../platform'
import { BotIcon } from '../icons'
import { tagClass } from './CardParts'

// mergeBotOf is the listed auto-merge bot whose latest review approved a pull request
// (Settings → Reviewing → Auto-merge bots), or '' when the setting is off, none did, or the
// details are not read yet.
export function mergeBotOf(item: InboxItem): string {
  return item.details?.mergeBot ?? ''
}

// mergeBotTitle says what a bot's approval means: the pull request merges by itself.
export function mergeBotTitle(bot: string): string {
  return `${bot} approved this pull request and merges it once its checks pass`
}

// MergeBotChip marks a card whose pull request a listed auto-merge bot approved, so it
// merges by itself once its checks pass, with the bot in its title. A narrow list keeps
// the icon alone.
export function MergeBotChip({ bot }: { bot: string }) {
  return (
    <span
      title={mergeBotTitle(bot)}
      className="inline-flex shrink-0 items-center gap-1 rounded-full border border-success/45 bg-success/10 px-1.5 py-px text-caption leading-4 font-medium text-success"
    >
      <BotIcon className="size-3 shrink-0" />
      <span className="@max-3xl:sr-only">Auto-merge</span>
    </span>
  )
}

// MergeBotIcon is MergeBotChip for a compact card's title line: the icon alone, named for
// a screen reader, with the bot in its title.
export function MergeBotIcon({ item }: { item: InboxItem }) {
  const bot = mergeBotOf(item)

  if (bot === '') {
    return null
  }

  return (
    <span
      role="img"
      aria-label="Auto-merge"
      title={mergeBotTitle(bot)}
      className="relative z-10 grid shrink-0 place-items-center text-success"
    >
      <BotIcon className="size-3" />
    </span>
  )
}

// MergeBotTag marks the review sheet's pull request that a listed auto-merge bot approved,
// beside its trust tag.
export function MergeBotTag({ item }: { item: InboxItem }) {
  const bot = mergeBotOf(item)

  if (bot === '') {
    return null
  }

  return (
    <span title={mergeBotTitle(bot)} className={`${tagClass} inline-flex items-center gap-1 text-success`}>
      <BotIcon className="size-3 shrink-0" />
      auto-merge
    </span>
  )
}
