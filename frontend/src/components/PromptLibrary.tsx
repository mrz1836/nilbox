import { useId, useState, type Ref } from 'react'

import { usagePlaces } from '../lib/launch'
import { replyGroups, replySlotBadges } from '../lib/replies'
import type { Prompt, PromptAppliesTo, PromptPurpose } from '../platform'
import { quietButtonClass, Segmented, type SegmentOption } from './controls'
import { MoreIcon, PlusIcon, SearchIcon, StarIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'

// Selection is the prompt open in the editor: a stored one by ID, or a new one.
export type Selection = { kind: 'prompt'; id: number } | { kind: 'new' }

export interface PromptLibraryProps {
  // mode is which part of the library shows: the agent prompts or the review replies, and
  // onModeChange asks to show the other; the page decides whether it may.
  mode: PromptPurpose
  onModeChange: (mode: PromptPurpose) => void
  // prompts are the part of the library the mode shows.
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

// Group is one titled part of the list, and which prompts it holds.
interface Group {
  key: string
  title: string
  holds: (p: Prompt) => boolean
}

// appliesGroup is the group of agent prompts for what they apply to.
function appliesGroup(appliesTo: PromptAppliesTo, title: string): Group {
  return { key: appliesTo, title, holds: (p) => p.appliesTo === appliesTo }
}

const promptGroups: readonly Group[] = [
  appliesGroup('pr', 'Pull requests'),
  appliesGroup('issue', 'Issues'),
  appliesGroup('any', 'Both'),
]

const replyListGroups: readonly Group[] = replyGroups.map(({ action, title }) => ({
  key: action,
  title,
  holds: (p) => p.replyAction === action,
}))

const modeOptions: readonly SegmentOption<PromptPurpose>[] = [
  { value: 'agent', label: 'Agent prompts', title: 'Prompts that launch a coding agent' },
  { value: 'reply', label: 'Review replies', title: 'The text a review starts with' },
]

// isDefault reports whether a prompt is an app default: an agent prompt's for a kind of
// item, or a reply's for a review.
function isDefault(p: Prompt): boolean {
  return p.usage.defaults.length > 0 || p.usage.replies.length > 0
}

// PromptLibrary is the Prompts page's sidebar: the agent prompts, grouped by what they are
// for, or the review replies, grouped by the review each is for, with a switch between the
// two. Either is searchable by title or nickname, with a button for a new one and a menu
// of what can be done with the whole library. A prompt that is a default somewhere says
// so, and the open one is marked, with a dot while it has unsaved changes.
export function PromptLibrary({
  mode,
  onModeChange,
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
  const replies = mode === 'reply'
  const groups = replies ? replyListGroups : promptGroups
  const noun = replies ? 'reply' : 'prompt'
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
            {replies ? 'New reply' : 'New'}
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
        <Segmented label="Kind of prompt" size="sm" value={mode} options={modeOptions} onChange={onModeChange} />
      </div>
      <div className="px-3 pb-2">
        <label htmlFor={`${id}-search`} className="sr-only">
          {replies ? 'Search replies' : 'Search prompts'}
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
      <nav aria-label={replies ? 'Review replies' : 'Prompts'} className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {selection?.kind === 'new' && (
          <div className="mb-1 rounded-md bg-raised px-2.5 py-2">
            <p className="text-sm font-medium italic">{newNickname.trim() === '' ? `New ${noun}` : newNickname}</p>
            <p className="text-xs text-muted">Not saved yet</p>
          </div>
        )}
        {filtered.length === 0 && q !== '' && (
          <p className="px-2.5 py-2 text-sm text-muted">
            No {noun} matches “{query}”.
          </p>
        )}
        {groups.map((g) => {
          const list = filtered.filter(g.holds)

          if (list.length === 0) {
            return null
          }

          return (
            <section key={g.key} aria-label={g.title} className="mt-2 first:mt-0">
              <h3 className="px-2.5 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase">
                {g.title}
              </h3>
              <ul>
                {list.map((p) => {
                  const current = selection?.kind === 'prompt' && selection.id === p.id
                  const places = p.usage.orgs.length + p.usage.repos.length
                  const badges = p.usage.replies.map((slot) => replySlotBadges[slot])

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
                        title={[p.nickname, p.name, ...usagePlaces(p.usage), ...badges].join('\n')}
                        className="flex w-full min-w-0 flex-col overflow-hidden rounded-md px-2.5 py-1.5 text-left transition-colors hover:bg-raised aria-[current=true]:bg-raised"
                      >
                        {/* Each line is held to the row's width, so a long nickname or name ends
                            in an ellipsis inside the row rather than running under the next
                            column; the row's tooltip holds both whole. */}
                        <span className="flex w-full max-w-full min-w-0 items-center gap-1.5">
                          <span className="min-w-0 truncate text-sm font-medium">{p.nickname}</span>
                          {isDefault(p) && <StarIcon className="size-3 shrink-0 text-accent" />}
                          {current && dirty && (
                            <span
                              aria-label="Unsaved changes"
                              className="ml-auto size-1.5 shrink-0 rounded-full bg-warning"
                            />
                          )}
                        </span>
                        <span className="block w-full max-w-full truncate text-xs text-muted">{p.name}</span>
                        {places > 0 && (
                          <span className="text-[0.6875rem] text-muted">
                            Default in {places} {places === 1 ? 'place' : 'places'}
                          </span>
                        )}
                        {/* A reply is for pull requests unless it says otherwise, as a comment
                            reply the issue sheet offers does. */}
                        {replies && p.appliesTo !== 'pr' && (
                          <span className="text-[0.6875rem] text-muted">
                            {p.appliesTo === 'issue' ? 'For issues' : 'For pull requests and issues'}
                          </span>
                        )}
                        {badges.length > 0 && (
                          <span className="mt-0.5 flex flex-wrap gap-1">
                            {badges.map((badge) => (
                              <span
                                key={badge}
                                className="rounded-full bg-accent/15 px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium text-text"
                              >
                                {badge}
                              </span>
                            ))}
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
