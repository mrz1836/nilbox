import { createContext, use } from 'react'

import { refText } from '../lib/format'
import { openExternal } from '../platform'
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
  // titled shows the linked item's title after its name.
  titled?: boolean
}

// ItemLink names a pull request or an issue a sheet links to, as in "#14" or
// "acme-org/api#9", and opens it: in its sheet when the inbox holds it, else on GitHub,
// which the mark beside it says.
export function ItemLink({ linked, from, titled = false }: ItemLinkProps) {
  const links = use(LinksContext)
  const here = links?.holds(linked.repo, linked.number) === true
  const name = linked.repo === from ? `#${String(linked.number)}` : refText(linked)

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
      className="inline-flex min-w-0 items-center gap-1 rounded-sm text-left text-text hover:text-accent hover:underline"
    >
      <span className="shrink-0 font-medium">{name}</span>
      {/* A space keeps the name and the title apart when read aloud; the flex layout drops it. */}
      {titled && linked.title !== '' && (
        <>
          {' '}
          <span className="min-w-0 truncate">{linked.title}</span>
        </>
      )}
      {!here && <ExternalIcon className="size-3 shrink-0 text-muted" />}
    </button>
  )
}
