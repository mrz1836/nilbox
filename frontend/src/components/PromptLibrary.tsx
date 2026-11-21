import { useId, useState, type Ref } from 'react'

import { formatCount } from '../lib/format'
import { routinePlaces, routinesUsing, usagePlaces } from '../lib/launch'
import { replyGroups, replySlotBadges } from '../lib/replies'
import type { Prompt, PromptAppliesTo, PromptPurpose } from '../platform'
import { quietButtonClass, Segmented, type SegmentOption } from './controls'
import { ChevronDownIcon, MoreIcon, PlusIcon, StarIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'
import { SearchBox } from './SearchBox'

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
  // collapsed holds the groups the user folded away, by groupKey, and onCollapse folds one
  // away or opens it again. The page keeps them, so they stay as they were left.
  collapsed: ReadonlySet<string>
  onCollapse: (key: string, collapsed: boolean) => void
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

// promptGroups are the agent prompts' groups: one for each kind of item, both, and then
// repositories, whose prompts the Deps page's upgrades launch rather than an item's.
const promptGroups: readonly Group[] = [
  appliesGroup('pr', 'Pull requests'),
  appliesGroup('issue', 'Issues'),
  appliesGroup('any', 'Both'),
  appliesGroup('repo', 'Repositories'),
]

const replyListGroups: readonly Group[] = replyGroups.map(({ action, title }) => ({
  key: action,
  title,
  holds: (p) => p.replyAction === action,
}))

// groupsOf are the groups a part of the library is listed in, in the order it lists them.
function groupsOf(mode: PromptPurpose): readonly Group[] {
  return mode === 'reply' ? replyListGroups : promptGroups
}

// collapseKey names a group as the collapsed groups are kept, by the part of the library
// it is in, so the two parts' groups never share a key.
function collapseKey(mode: PromptPurpose, group: Group): string {
  return `${mode}:${group.key}`
}

// groupKey names the group a prompt is listed in (see collapseKey), or '' for none.
export function groupKey(p: Prompt): string {
  const group = groupsOf(p.purpose).find((g) => g.holds(p))

  return group === undefined ? '' : collapseKey(p.purpose, group)
}

// firstShown is the prompt of a part of the library that it lists first in a group left
// open, or else the first it lists at all, or undefined when the part is empty. The page
// opens it when it picks a prompt on its own, so what it opens is in sight.
export function firstShown(
  prompts: readonly Prompt[],
  mode: PromptPurpose,
  collapsed: ReadonlySet<string>,
): Prompt | undefined {
  const listed = groupsOf(mode).flatMap((g) => prompts.filter(g.holds).map((p) => ({ p, key: collapseKey(mode, g) })))

  return (listed.find((entry) => !collapsed.has(entry.key)) ?? listed[0])?.p ?? prompts[0]
}

const modeOptions: readonly SegmentOption<PromptPurpose>[] = [
  { value: 'agent', label: 'Agent prompts', title: 'Prompts that launch a coding agent' },
  { value: 'reply', label: 'Review replies', title: 'The text a review starts with' },
]

// isDefault reports whether a prompt is an app default: an agent prompt's for a kind of
// item or a pull request's situation, or a reply's for a review.
function isDefault(p: Prompt): boolean {
  return p.usage.defaults.length > 0 || p.usage.slots.length > 0 || p.usage.replies.length > 0
}

interface GroupHeadingProps {
  title: string
  // count is how many prompts the group lists, and noun and nouns name one and several of
  // them, for a screen reader.
  count: number
  noun: string
  nouns: string
  // open says whether the group shows its prompts, and onToggle folds it away or opens it.
  open: boolean
  onToggle: () => void
  // listId is the group's list, which the toggle controls.
  listId: string
  // searching says the library is searched: count then counts the matches, and the group
  // shows open with nothing to toggle.
  searching: boolean
  // unsaved says the group holds the open prompt, with unsaved changes, out of sight.
  unsaved: boolean
}

// headingTextClass is a group heading's text: small capitals in the muted color.
const headingTextClass = 'text-caption font-semibold tracking-wide text-muted uppercase'

// GroupHeading is a group's title, underlined, with a badge of how many prompts it lists.
// The whole heading is a toggle that folds the group away and opens it again, its chevron
// turned to say which. A folded group that holds the open prompt shows the dot of its
// unsaved changes, so they never go out of sight. While the library is searched, every
// group with a match shows open, so its heading toggles nothing and its badge, drawn as
// the inbox draws a filtered count, counts the matches.
function GroupHeading({ title, count, noun, nouns, open, onToggle, listId, searching, unsaved }: GroupHeadingProps) {
  const spoken = `${title}, ${formatCount(count)} ${searching ? 'matching' : count === 1 ? noun : nouns}`
  const badge = (
    <span
      data-filtered={searching || undefined}
      className={`shrink-0 rounded-full px-1.5 leading-[1.125rem] font-medium tracking-normal tabular-nums ${searching ? 'text-text outline-1 -outline-offset-1 outline-accent/60 outline-dashed' : 'bg-raised text-muted'}`}
    >
      {formatCount(count)}
    </span>
  )

  if (searching) {
    return (
      <h3
        aria-label={spoken}
        className={`mx-2.5 mb-1 flex items-center gap-1.5 border-b border-border pb-1.5 ${headingTextClass}`}
      >
        <span className="min-w-0 truncate">{title}</span>
        {badge}
      </h3>
    )
  }

  return (
    <h3 className="mx-2.5 mb-1 border-b border-border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={unsaved ? `${spoken}, unsaved changes` : spoken}
        title={open ? 'Collapse' : 'Expand'}
        onClick={onToggle}
        className={`flex w-full items-center gap-1.5 pb-1.5 text-left transition-colors hover:text-text ${headingTextClass}`}
      >
        <span className="min-w-0 truncate">{title}</span>
        {badge}
        {unsaved && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-warning" />}
        <ChevronDownIcon className={`ml-auto size-3.5 shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} />
      </button>
    </h3>
  )
}

// PromptLibrary is the Prompts page's sidebar: the agent prompts, grouped by what they are
// for (pull requests, issues, both, or repositories), or the review replies, grouped by
// the review each is for, with a switch between the two. Either is searchable by title or
// nickname, with a button for a new one and a menu of what can be done with the whole
// library. Each group's heading counts its prompts and folds the group away
// (GroupHeading). A prompt that is a default somewhere says so, and the open one is
// marked, with a dot while it has unsaved changes.
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
  collapsed,
  onCollapse,
}: PromptLibraryProps) {
  const id = useId()
  const [query, setQuery] = useState('')
  const replies = mode === 'reply'
  const groups = groupsOf(mode)
  const noun = replies ? 'reply' : 'prompt'
  const nouns = replies ? 'replies' : 'prompts'
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
          {/* The menu opens from the button's left edge, over the editor: lined up with its
              right edge, it is wider than the sidebar and runs off the window. */}
          <MenuButton
            label="Import and export prompts"
            title="Import and export"
            items={actions}
            align="left"
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
        <SearchBox
          value={query}
          onChange={setQuery}
          label={replies ? 'Search replies' : 'Search prompts'}
          placeholder="Search"
          inputRef={searchRef}
          dense={false}
        />
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

          const key = collapseKey(mode, g)
          const searching = q !== ''
          // A search shows every group with a match open, whatever the user folded away.
          const open = searching || !collapsed.has(key)
          const listId = `${id}-${g.key}`

          return (
            <section key={g.key} aria-label={g.title} className="mt-3 first:mt-1">
              <GroupHeading
                title={g.title}
                count={list.length}
                noun={noun}
                nouns={nouns}
                open={open}
                onToggle={() => {
                  onCollapse(key, open)
                }}
                listId={listId}
                searching={searching}
                unsaved={!open && dirty && list.some((p) => selection?.kind === 'prompt' && selection.id === p.id)}
              />
              <ul id={listId} hidden={!open}>
                {open &&
                  list.map((p) => {
                    const current = selection?.kind === 'prompt' && selection.id === p.id
                    const places = p.usage.orgs.length + p.usage.repos.length + p.usage.slotPlaces.length
                    const badges = p.usage.replies.map((slot) => replySlotBadges[slot])
                    const used = [
                      places > 0 ? `Default in ${String(places)} ${places === 1 ? 'place' : 'places'}` : '',
                      routinesUsing(p.usage),
                    ].filter((part) => part !== '')

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
                          title={[
                            p.nickname,
                            p.name,
                            ...usagePlaces(p.usage),
                            ...routinePlaces(p.usage),
                            ...badges,
                          ].join('\n')}
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
                          {used.length > 0 && (
                            <span className="block w-full max-w-full truncate text-caption text-muted">
                              {used.join(' · ')}
                            </span>
                          )}
                          {/* A reply is for pull requests unless it says otherwise, as a comment
                            reply the issue sheet offers does. */}
                          {replies && p.appliesTo !== 'pr' && (
                            <span className="text-caption text-muted">
                              {p.appliesTo === 'issue' ? 'For issues' : 'For pull requests and issues'}
                            </span>
                          )}
                          {badges.length > 0 && (
                            <span className="mt-0.5 flex flex-wrap gap-1">
                              {badges.map((badge) => (
                                <span
                                  key={badge}
                                  className="rounded-full bg-accent/14 px-1.5 text-caption leading-[1.125rem] font-medium text-text"
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
