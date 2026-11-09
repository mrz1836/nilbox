import { useId, useState, type Ref } from 'react'

import { usagePlaces } from '../lib/launch'
import type { Prompt, PromptAppliesTo } from '../platform'
import { quietButtonClass } from './controls'
import { MoreIcon, PlusIcon, SearchIcon, StarIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'

// Selection is the prompt open in the editor: a stored one by ID, or a new one.
export type Selection = { kind: 'prompt'; id: number } | { kind: 'new' }

export interface PromptLibraryProps {
  prompts: readonly Prompt[]
  // selection is the prompt open in the editor, or null when none is.
  selection: Selection | null
  // newNickname is the nickname typed for a new prompt, while one is open.
  newNickname: string
  // dirty marks the open prompt as having unsaved changes.
  dirty: boolean
  // onAsk asks to open a prompt, or a new one; the page decides whether it may.
  onAsk: (next: Selection) => void
  // actions are what can be done with the whole library, such as importing and
  // exporting it, offered in a menu beside New.
  actions: readonly MenuItem[]
  // searchRef reaches the search field, for ⌘F to focus it.
  searchRef?: Ref<HTMLInputElement>
}

const groups: readonly { appliesTo: PromptAppliesTo; title: string }[] = [
  { appliesTo: 'pr', title: 'Pull requests' },
  { appliesTo: 'issue', title: 'Issues' },
  { appliesTo: 'any', title: 'Both' },
]

// isAppDefault reports whether a prompt is the app's default for a kind.
function isAppDefault(p: Prompt): boolean {
  return p.usage.defaults.length > 0
}

// PromptLibrary is the Prompts page's sidebar: every prompt, grouped by what it is for and
// searchable by title or nickname, with a button for a new one and a menu of what can be
// done with the whole library. A prompt that is a default somewhere says so, and the open
// one is marked, with a dot while it has unsaved changes.
export function PromptLibrary({
  prompts,
  selection,
  newNickname,
  dirty,
  onAsk,
  actions,
  searchRef,
}: PromptLibraryProps) {
  const id = useId()
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()
  const filtered =
    q === '' ? prompts : prompts.filter((p) => p.nickname.toLowerCase().includes(q) || p.name.toLowerCase().includes(q))

  return (
    <aside aria-label="Prompt library" className="flex w-60 shrink-0 flex-col border-r border-border">
      <div className="flex items-center justify-between gap-2 px-4 pt-4 pb-2">
        <h2 className="text-sm font-semibold">Prompts</h2>
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            onClick={() => {
              onAsk({ kind: 'new' })
            }}
            className={`${quietButtonClass} text-text`}
          >
            <PlusIcon className="size-3.5" />
            New
          </button>
          <MenuButton
            label="Import and export prompts"
            title="Import and export"
            items={actions}
            className={`${quietButtonClass} py-1 aria-expanded:bg-raised aria-expanded:text-text`}
          >
            <MoreIcon className="size-3.5" />
          </MenuButton>
        </div>
      </div>
      <div className="px-3 pb-2">
        <label htmlFor={`${id}-search`} className="sr-only">
          Search prompts
        </label>
        <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60">
          <SearchIcon className="size-3.5 shrink-0 text-muted" />
          <input
            ref={searchRef}
            id={`${id}-search`}
            type="search"
            value={query}
            placeholder="Search"
            onChange={(e) => {
              setQuery(e.target.value)
            }}
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
          />
        </div>
      </div>
      <nav aria-label="Prompts" className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {selection?.kind === 'new' && (
          <div className="mb-1 rounded-md bg-raised px-2.5 py-2">
            <p className="text-sm font-medium italic">{newNickname.trim() === '' ? 'New prompt' : newNickname}</p>
            <p className="text-xs text-muted">Not saved yet</p>
          </div>
        )}
        {filtered.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">No prompt matches “{query}”.</p>}
        {groups.map((g) => {
          const list = filtered.filter((p) => p.appliesTo === g.appliesTo)

          if (list.length === 0) {
            return null
          }

          return (
            <section key={g.appliesTo} aria-label={g.title} className="mt-2 first:mt-0">
              <h3 className="px-2.5 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase">
                {g.title}
              </h3>
              <ul>
                {list.map((p) => {
                  const current = selection?.kind === 'prompt' && selection.id === p.id
                  const places = p.usage.orgs.length + p.usage.repos.length

                  return (
                    <li key={p.id}>
                      <button
                        type="button"
                        aria-current={current ? 'true' : undefined}
                        onClick={() => {
                          if (!current) {
                            onAsk({ kind: 'prompt', id: p.id })
                          }
                        }}
                        title={usagePlaces(p.usage).join('\n') || undefined}
                        className="flex w-full flex-col rounded-md px-2.5 py-1.5 text-left transition-colors hover:bg-raised aria-[current=true]:bg-raised"
                      >
                        <span className="flex w-full items-center gap-1.5">
                          <span className="truncate text-sm font-medium">{p.nickname}</span>
                          {isAppDefault(p) && <StarIcon className="size-3 shrink-0 text-accent" />}
                          {current && dirty && (
                            <span
                              aria-label="Unsaved changes"
                              className="ml-auto size-1.5 shrink-0 rounded-full bg-warning"
                            />
                          )}
                        </span>
                        <span className="truncate text-xs text-muted">{p.name}</span>
                        {places > 0 && (
                          <span className="text-[0.6875rem] text-muted">
                            Default in {places} {places === 1 ? 'place' : 'places'}
                          </span>
                        )}
                      </button>
                    </li>
                  )
                })}
              </ul>
            </section>
          )
        })}
      </nav>
    </aside>
  )
}
