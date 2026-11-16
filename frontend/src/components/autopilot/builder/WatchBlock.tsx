import { useId, useState } from 'react'

import {
  authorLabel,
  authorValue,
  omit,
  placeLabel,
  placeValue,
  problemsAt,
  problemsExactly,
} from '../../../lib/routineBuilder'
import type {
  AutopilotInvolving,
  AutopilotListSource,
  AutopilotSearch,
  AutopilotSearchState,
  AutopilotSource,
  AutopilotTestSearch,
} from '../../../platform'
import { fieldClass, quietButtonClass } from '../../controls'
import { ChevronDownIcon, ChevronRightIcon, PlusIcon, TrashIcon } from '../../icons'
import { Block, Notes, type BuilderKit } from './parts'
import { TokenField } from './TokenField'

// maxSearches is the most searches one routine runs.
const maxSearches = 5

// kindChoices are what a search can look for.
const kindChoices = [
  { value: 'pr', label: 'Pull requests' },
  { value: 'issue', label: 'Issues' },
  { value: '', label: 'Pull requests and issues' },
] as const

// involvingLabels say how the items involve you.
const involvingLabels: Readonly<Record<AutopilotInvolving, string>> = {
  assigned: 'Assigned to you',
  reviewRequested: 'Asking for your review',
  mentioned: 'Mentioning you',
}

// stateLabels say each state a search can ask for; only a pull request has one.
const stateLabels: Readonly<Record<AutopilotSearchState, string>> = {
  ready: 'Not a draft',
  draft: 'A draft',
  failing: 'Checks failing',
  passing: 'Checks passing',
  pending: 'Checks pending',
  changesRequested: 'Changes requested',
  approved: 'Approved',
  reviewRequired: 'Review required',
  unreviewed: 'Not reviewed yet',
}

export interface WatchBlockProps {
  kit: BuilderKit
  sources: readonly AutopilotSource[]
  onChange: (sources: AutopilotSource[]) => void
  // cost is the line under the block, what its searches add to each refresh; '' before a
  // test says.
  cost: string
  // tested are the searches as the last test read them, in their order, whose errors and
  // warnings show here.
  tested: readonly AutopilotTestSearch[]
}

// WatchBlock is a routine's Watch: the inbox lists it watches and its searches, each built
// from simple fields (what kind of item, by whom, where, and More ▸), with the cost line
// under it.
export function WatchBlock({ kit, sources, onChange, cost, tested }: WatchBlockProps) {
  const searches = sources.filter((s) => s.search !== undefined && s.search !== null).length
  const hasLists = sources.some((s) => s.lists !== undefined && s.lists !== null)
  const set = (i: number, src: AutopilotSource) => {
    onChange(sources.map((s, j) => (j === i ? src : s)))
  }
  const testNotes = tested.flatMap((t, i) => {
    const prefix = tested.length > 1 ? `Search ${String(i + 1)}: ` : ''

    return [...(t.error === '' ? [] : [prefix + t.error]), ...t.warnings.map((w) => prefix + w)]
  })

  return (
    <Block title="Watch" hint="Which items">
      {sources.map((src, i) => {
        const key = `source-${String(i)}`
        const remove =
          sources.length > 1
            ? () => {
                onChange(sources.filter((_, j) => j !== i))
              }
            : undefined

        return src.search !== undefined && src.search !== null ? (
          <SearchSource
            key={key}
            index={i}
            kit={kit}
            search={src.search}
            onChange={(search) => {
              set(i, { search })
            }}
            onRemove={remove}
          />
        ) : (
          <ListsSource
            key={key}
            index={i}
            kit={kit}
            lists={src.lists ?? []}
            onChange={(lists) => {
              set(i, { lists })
            }}
            onRemove={remove}
          />
        )
      })}
      <Notes
        problems={[...problemsExactly(kit.problems, 'watch', ''), ...problemsExactly(kit.problems, 'watch', 'sources')]}
        extra={testNotes}
      />
      <div className="flex flex-wrap items-center gap-2">
        {searches < maxSearches && (
          <button
            type="button"
            onClick={() => {
              onChange([...sources, { search: { kind: 'pr', where: ['@me'] } }])
            }}
            className={quietButtonClass}
          >
            <PlusIcon className="size-3.5" />
            Add a search
          </button>
        )}
        {!hasLists && (
          <button
            type="button"
            onClick={() => {
              onChange([...sources, { lists: ['reviewRequested'] }])
            }}
            className={quietButtonClass}
          >
            <PlusIcon className="size-3.5" />
            Watch inbox lists
          </button>
        )}
        {cost !== '' && <span className="ml-auto text-xs text-muted">{cost}</span>}
      </div>
    </Block>
  )
}

interface SourceProps<T> {
  index: number
  kit: BuilderKit
  onChange: (value: T) => void
  onRemove: (() => void) | undefined
}

// RemoveSource is a source's 🗑.
function RemoveSource({ index, onRemove }: { index: number; onRemove: (() => void) | undefined }) {
  if (onRemove === undefined) {
    return null
  }

  return (
    <button
      type="button"
      aria-label={`Remove source ${String(index + 1)}`}
      title="Remove"
      onClick={onRemove}
      className="ml-auto rounded-md p-1 text-muted hover:bg-raised hover:text-danger"
    >
      <TrashIcon className="size-3.5" />
    </button>
  )
}

// ListsSource is a source of inbox lists, which the inbox reads on every refresh already.
function ListsSource({
  index,
  kit,
  lists,
  onChange,
  onRemove,
}: SourceProps<AutopilotListSource[]> & { lists: readonly AutopilotListSource[] }) {
  const id = useId()
  const problems = problemsAt(kit.problems, 'watch', `sources[${String(index)}]`)

  return (
    <div className="flex flex-col gap-1 rounded-md border border-border p-2">
      <fieldset className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <legend className="mb-1 text-xs font-medium text-muted">Inbox lists</legend>
        {kit.catalog.lists.map((l) => (
          <label key={l.id} htmlFor={`${id}-${l.id}`} className="inline-flex items-center gap-1.5 text-sm">
            <input
              id={`${id}-${l.id}`}
              type="checkbox"
              className="checkbox"
              checked={lists.includes(l.id)}
              onChange={(e) => {
                onChange(e.target.checked ? [...lists, l.id] : lists.filter((x) => x !== l.id))
              }}
            />
            {l.label}
          </label>
        ))}
        <RemoveSource index={index} onRemove={onRemove} />
      </fieldset>
      <Notes problems={problems} />
    </div>
  )
}

// SearchSource is a GitHub search written from simple fields: the kind of item, who opened
// them, where they are, and More ▸ for the rest.
function SearchSource({
  index,
  kit,
  search,
  onChange,
  onRemove,
}: SourceProps<AutopilotSearch> & { search: AutopilotSearch }) {
  const id = useId()
  const at = `sources[${String(index)}].search`
  const problems = problemsAt(kit.problems, 'watch', at)
  // More ▸ starts open on a search that sets something under it.
  const more =
    (search.involving?.length ?? 0) > 0 ||
    (search.labels?.length ?? 0) > 0 ||
    (search.notLabels?.length ?? 0) > 0 ||
    (search.base ?? '') !== '' ||
    (search.headPrefix ?? '') !== '' ||
    (search.state?.length ?? 0) > 0 ||
    (search.more ?? '') !== ''
  const [open, setOpen] = useState(more)
  const authors = [
    { value: '@me', label: 'You' },
    ...kit.suggestions.bots.map((b) => ({
      value: `app/${b.replace(/\[bot\]$/, '')}`,
      label: authorLabel(`app/${b.replace(/\[bot\]$/, '')}`),
    })),
    ...kit.suggestions.authors.map((a) => ({ value: a, label: a })),
  ]
  const places = [
    { value: '@me', label: 'Your account' },
    ...kit.suggestions.owners
      .filter((o) => o.toLowerCase() !== kit.viewer.toLowerCase())
      .map((o) => ({ value: `org:${o}`, label: o })),
  ]
  const labels = kit.suggestions.labels.map((l) => ({ value: l, label: l }))
  const patch = (change: Partial<AutopilotSearch>) => {
    onChange({ ...search, ...change })
  }
  const whereProblems = problemsAt(kit.problems, 'watch', `${at}.where`)

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-2">
      <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5">
        <label htmlFor={`${id}-kind`} className="sr-only">
          Look for
        </label>
        <select
          id={`${id}-kind`}
          value={search.kind ?? ''}
          onChange={(e) => {
            const kind = e.target.value

            onChange(kind === 'pr' || kind === 'issue' ? { ...search, kind } : omit(search, 'kind'))
          }}
          className={`${fieldClass} col-span-2 justify-self-start py-1`}
        >
          {kindChoices.map((k) => (
            <option key={k.value} value={k.value}>
              {k.label}
            </option>
          ))}
        </select>
        <span className="text-sm text-muted">by</span>
        <TokenField
          id={`${id}-authors`}
          label="Opened by"
          placeholder="Anyone"
          tokens={search.authors ?? []}
          onChange={(authorsList) => {
            patch({ authors: authorsList })
          }}
          tokenLabel={authorLabel}
          suggestions={authors}
          toValue={(t) => authorValue(t, kit.suggestions.bots)}
        />
        <span className="text-sm text-muted">in</span>
        <TokenField
          id={`${id}-where`}
          label="Where"
          placeholder="Your account, an org, or owner/repo"
          tokens={search.where ?? []}
          onChange={(where) => {
            patch({ where })
          }}
          tokenLabel={placeLabel}
          suggestions={places}
          toValue={(t) => placeValue(t, kit.viewer)}
          invalid={whereProblems.length > 0}
        />
      </div>
      <div className="flex items-center gap-2">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => {
            setOpen(!open)
          }}
          className={quietButtonClass}
        >
          {open ? <ChevronDownIcon className="size-3.5" /> : <ChevronRightIcon className="size-3.5" />}
          More
        </button>
        <RemoveSource index={index} onRemove={onRemove} />
      </div>
      {open && (
        <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5 border-t border-border pt-2">
          <span className="text-xs text-muted">Involving you</span>
          <fieldset className="flex flex-wrap gap-x-3 gap-y-1">
            <legend className="sr-only">Involving you</legend>
            {(Object.keys(involvingLabels) as AutopilotInvolving[]).map((v) => (
              <label key={v} htmlFor={`${id}-inv-${v}`} className="inline-flex items-center gap-1.5 text-sm">
                <input
                  id={`${id}-inv-${v}`}
                  type="checkbox"
                  className="checkbox"
                  checked={search.involving?.includes(v) ?? false}
                  onChange={(e) => {
                    const was = search.involving ?? []

                    patch({ involving: e.target.checked ? [...was, v] : was.filter((x) => x !== v) })
                  }}
                />
                {involvingLabels[v]}
              </label>
            ))}
          </fieldset>
          <span className="text-xs text-muted">Labeled</span>
          <TokenField
            id={`${id}-labels`}
            label="Labeled any of"
            placeholder="Any label"
            tokens={search.labels ?? []}
            onChange={(l) => {
              patch({ labels: l })
            }}
            suggestions={labels}
          />
          <span className="text-xs text-muted">Not labeled</span>
          <TokenField
            id={`${id}-not-labels`}
            label="Not labeled"
            placeholder="None left out"
            tokens={search.notLabels ?? []}
            onChange={(l) => {
              patch({ notLabels: l })
            }}
            suggestions={labels}
          />
          {search.kind !== 'issue' && (
            <>
              <label htmlFor={`${id}-base`} className="text-xs text-muted">
                Into branch
              </label>
              <input
                id={`${id}-base`}
                value={search.base ?? ''}
                placeholder="Any base branch"
                spellCheck={false}
                onChange={(e) => {
                  patch({ base: e.target.value })
                }}
                className={`${fieldClass} py-1`}
              />
              <label htmlFor={`${id}-head`} className="text-xs text-muted">
                From a branch starting
              </label>
              <input
                id={`${id}-head`}
                value={search.headPrefix ?? ''}
                placeholder="dependabot/"
                spellCheck={false}
                onChange={(e) => {
                  patch({ headPrefix: e.target.value })
                }}
                className={`${fieldClass} py-1`}
              />
              <span className="text-xs text-muted">State</span>
              <fieldset className="flex flex-wrap gap-x-3 gap-y-1">
                <legend className="sr-only">State</legend>
                {(Object.keys(stateLabels) as AutopilotSearchState[]).map((v) => (
                  <label key={v} htmlFor={`${id}-state-${v}`} className="inline-flex items-center gap-1.5 text-sm">
                    <input
                      id={`${id}-state-${v}`}
                      type="checkbox"
                      className="checkbox"
                      checked={search.state?.includes(v) ?? false}
                      onChange={(e) => {
                        const was = search.state ?? []

                        patch({ state: e.target.checked ? [...was, v] : was.filter((x) => x !== v) })
                      }}
                    />
                    {stateLabels[v]}
                  </label>
                ))}
              </fieldset>
            </>
          )}
          <label htmlFor={`${id}-more`} className="text-xs text-muted">
            More qualifiers
          </label>
          <input
            id={`${id}-more`}
            value={search.more ?? ''}
            placeholder="language:go"
            spellCheck={false}
            onChange={(e) => {
              patch({ more: e.target.value })
            }}
            className={`${fieldClass} py-1 font-mono text-[0.8125rem]`}
          />
        </div>
      )}
      <Notes problems={problems} />
    </div>
  )
}
