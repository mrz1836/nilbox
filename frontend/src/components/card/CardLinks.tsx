import { use } from 'react'

import { cardLinks, CardLinksContext, linkName, linkWords, type CardLink } from '../../lib/cardLinks'
import { openExternal, type InboxItem } from '../../platform'
import { ExternalIcon, IssueIcon, MergeIcon, PullRequestIcon } from '../icons'

// useCardLinks reads the links of an item's card (cardLinks) from the inbox around it, and
// open, which shows a linked card in the inbox, highlighted, or opens a link the inbox
// doesn't hold on GitHub.
export function useCardLinks(item: InboxItem): { links: CardLink[]; open: (link: CardLink) => void } {
  const inbox = use(CardLinksContext)
  const links = cardLinks(item, inbox?.index ?? null)

  const open = (link: CardLink) => {
    if (inbox !== null && link.id > 0) {
      inbox.reveal(link.id)
    } else {
      openExternal(link.url)
    }
  }

  return { links, open }
}

// markColor is a link's mark's color: green while it is open, as a card's kind badge is,
// muted for a draft, and the done color once merged.
function markColor(link: CardLink): string {
  if (link.merged) {
    return 'text-info'
  }

  return link.draft ? 'text-muted' : 'text-success'
}

// LinkMark is a link's kind and state as a mark: a pull request's, or its merge's once
// merged, or an issue's.
function LinkMark({ link }: { link: CardLink }) {
  const Icon = link.kind === 'issue' ? IssueIcon : link.merged ? MergeIcon : PullRequestIcon

  return <Icon className={`size-3 shrink-0 ${markColor(link)}`} />
}

// actionWords say what clicking a link does.
function actionWords(link: CardLink): string {
  return link.id > 0 ? 'Show it in the inbox' : 'Open it on GitHub'
}

// LinkChip connects a card to the items GitHub links it to: on an issue's card, the pull
// requests that close it once they merge; on a pull request's, the listed issues it closes.
// It names the first with its mark, and "+N" for the rest, which its tooltip names with the
// first; a click shows the first's card in the inbox, highlighted, or opens it on GitHub when
// the inbox doesn't hold it, as the mark after its name then says. The peek lists them all
// (LinkList). A card with no links has no chip. compact drops the chip's outline, for a
// compact card's title line.
export function LinkChip({ item, compact = false }: { item: InboxItem; compact?: boolean }) {
  const { links, open } = useCardLinks(item)
  const [first] = links

  if (first === undefined) {
    return null
  }

  const rest = links.length - 1
  const more = rest > 0 ? `, and ${String(rest)} more` : ''

  return (
    <button
      type="button"
      data-card-link
      title={`${links.map((link) => linkWords(link, item.repo)).join('\n')}\n${actionWords(first)}`}
      aria-label={`${linkWords(first, item.repo)}${more}. ${actionWords(first)}`}
      onClick={() => {
        open(first)
      }}
      className={`relative z-10 inline-flex shrink-0 items-center gap-1 text-[0.6875rem] leading-4 font-medium text-muted tabular-nums transition-colors hover:text-accent ${compact ? 'rounded-sm' : 'rounded-full border border-border px-1.5 py-px hover:border-accent/50'}`}
    >
      <LinkMark link={first} />
      {linkName(first, item.repo)}
      {rest > 0 && <span className="opacity-70">+{rest}</span>}
      {first.id === 0 && <ExternalIcon className="size-2.5 shrink-0 opacity-70" />}
    </button>
  )
}

// standWords say where a linked pull request stands, after its title in the peek: merged,
// a draft, and from outside the project when the issue's card doesn't take its word.
function standWords(link: CardLink): string {
  const words = [link.merged ? 'merged' : link.draft ? 'draft' : '', link.trusted ? '' : 'from outside the project']

  return words.filter((word) => word !== '').join(' · ')
}

// LinkList lists a card's links in its peek, each on its line: its mark, its name, and its
// title, then, for a pull request, where it stands. Each opens as the chip does.
export function LinkList({
  item,
  links,
  open,
}: {
  item: InboxItem
  links: readonly CardLink[]
  open: (link: CardLink) => void
}) {
  return (
    <ul className="flex flex-col gap-1">
      {links.map((link) => {
        const stands = standWords(link)

        return (
          <li key={`${link.repo}#${String(link.number)}`} className="min-w-0">
            <button
              type="button"
              title={`${linkWords(link, item.repo)}\n${actionWords(link)}`}
              onClick={() => {
                open(link)
              }}
              className="flex max-w-full min-w-0 items-center gap-1 rounded-sm text-left text-text hover:text-accent hover:underline"
            >
              <LinkMark link={link} />
              <span className="shrink-0 font-medium">{linkName(link, item.repo)}</span>{' '}
              <span className="min-w-0 truncate">{link.title}</span>
              {link.id === 0 && <ExternalIcon className="size-3 shrink-0 text-muted" />}
            </button>
            {stands !== '' && (
              <span className={`block pl-4 ${link.trusted ? 'text-muted' : 'text-warning'}`}>{stands}</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}
