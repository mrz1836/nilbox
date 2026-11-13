import { refText } from '../lib/issues'
import { openExternal, type IssueLinked } from '../platform'
import { ExternalIcon } from './icons'

export interface ItemLinkProps {
  // linked is the pull request or issue the link names.
  linked: IssueLinked
  // from is the repository the sheet shows, as "owner/name": a linked item in it is named
  // by its number alone.
  from: string
  // titled shows the linked item's title after its name.
  titled?: boolean
}

// ItemLink names a pull request or an issue a sheet links to, as in "#14" or
// "acme-org/api#9", and opens it on GitHub, which the mark beside it says.
export function ItemLink({ linked, from, titled = false }: ItemLinkProps) {
  const name = linked.repo === from ? `#${String(linked.number)}` : refText(linked)

  return (
    <button
      type="button"
      title={`${linked.title}\nOpen it on GitHub`}
      onClick={() => {
        openExternal(linked.url)
      }}
      className="inline-flex min-w-0 items-center gap-1 rounded-sm text-left text-text hover:text-accent hover:underline"
    >
      <span className="shrink-0 font-medium">{name}</span>
      {titled && linked.title !== '' && <span className="min-w-0 truncate">{linked.title}</span>}
      <ExternalIcon className="size-3 shrink-0 text-muted" />
    </button>
  )
}
