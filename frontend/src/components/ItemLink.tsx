import { createContext, use } from 'react'

import { refText } from '../lib/format'
import { openExternal } from '../platform'
import { inkLinkClass } from './controls'
import { ExternalIcon } from './icons'

// Linked is a pull request or an issue a sheet links to: its repository as "owner/name", its
// number, title, and page on GitHub.
export interface Linked {
  repo: string
  number: number
  title: string
  url: string
}

// Links is how a sheet opens an item it links to: holds reports that the inbox holds it,
// and follow opens it in its sheet, with a step back to the sheet the link was in.
export interface Links {
  holds: (repo: string, number: number) => boolean
  follow: (repo: string, number: number) => void
}

// LinksContext is the inbox's Links, around its sheets; without it, every link opens
// GitHub.
export const LinksContext = createContext<Links | null>(null)

export interface ItemLinkProps {
  linked: Linked
  // from is the repository the sheet shows, as "owner/name": a linked item in it is named
  // by its number alone.
  from: string
  // titled shows the linked item's title after its name, and muted sets it in the muted ink,
  // as for one that is done.
  titled?: boolean
  muted?: boolean
  // stacked shows the title under the name, wrapping to three lines, rather than cut short
  // beside it, for a narrow column; a long repository's name is cut short there, never its
  // number.
  stacked?: boolean
}

// mutedLinkClass is inkLinkClass in the muted ink.
const mutedLinkClass = 'text-muted hover:text-accent hover:underline'

// StackedName is a linked item's name as a stacked link shows it: its repository, cut short
// when it is long, then its number, always whole; its number alone in the sheet's own
// repository.
function StackedName({ linked, from }: { linked: Linked; from: string }) {
  return (
    <span className="flex min-w-0 font-medium">
      {linked.repo !== from && <span className="min-w-0 truncate">{linked.repo}</span>}
      <span className="shrink-0">#{String(linked.number)}</span>
    </span>
  )
}

// ItemLink names a pull request or an issue a sheet links to, as in "#14" or
// "acme-org/api#9", and opens it: in its sheet when the inbox holds it, else on GitHub,
// which the mark beside it says.
export function ItemLink({ linked, from, titled = false, muted = false, stacked = false }: ItemLinkProps) {
  const links = use(LinksContext)
  const here = links?.holds(linked.repo, linked.number) === true
  const away = !here && <ExternalIcon className="size-3 shrink-0 text-muted" />
  // A space keeps the name and the title apart when read aloud; the flex layout drops it.
  const title = titled && linked.title !== '' && (
    <>
      {' '}
      <span className={stacked ? 'line-clamp-3 break-words' : 'min-w-0 truncate'}>{linked.title}</span>
    </>
  )

  return (
    <button
      type="button"
      title={`${linked.title}\n${here ? 'Open it here' : 'Open it on GitHub'}`}
      onClick={() => {
        if (here) {
          links.follow(linked.repo, linked.number)
        } else {
          openExternal(linked.url)
        }
      }}
      className={`${muted ? mutedLinkClass : inkLinkClass} ${stacked ? 'flex flex-col items-start' : 'inline-flex items-center gap-1'} min-w-0 rounded-sm text-left`}
    >
      {stacked ? (
        <>
          <span className="flex max-w-full min-w-0 items-center gap-1">
            <StackedName linked={linked} from={from} />
            {away}
          </span>
          {title}
        </>
      ) : (
        <>
          <span className="shrink-0 font-medium">
            {linked.repo === from ? `#${String(linked.number)}` : refText(linked)}
          </span>
          {title}
          {away}
        </>
      )}
    </button>
  )
}
