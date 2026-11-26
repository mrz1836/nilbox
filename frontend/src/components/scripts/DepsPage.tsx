import { useId, useState, type ReactNode } from 'react'

import { formatCount } from '../../lib/format'
import {
  depsGroups,
  groupsAt,
  hasKind,
  kindNames,
  kindTitle,
  leftOutText,
  matchClones,
  moduleKinds,
  noStaleText,
  pageText,
  readStep,
  sortClones,
  upgradeSessions,
  type DepsGroup,
} from '../../lib/manifests'
import type { SettingsSection } from '../../lib/settingsIndex'
import { formatAge, formatAgo } from '../../lib/time'
import type { ManifestsState } from '../../lib/useManifests'
import { useMutation } from '../../lib/useMutation'
import { useNowAs } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import { useSections, type OnSection, type StoredSections } from '../../lib/useSections'
import { useSessions } from '../../lib/useSessions'
import {
  focusSession,
  listAgents,
  type AgentInfo,
  type DepsSort,
  type ManifestKind,
  type ManifestsClone,
  type ScriptsSection,
} from '../../platform'
import { buttonClass, ErrorText, quietButtonClass, Segmented, type SegmentOption } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, RefreshIcon } from '../icons'
import { ProgressStatus } from '../ProgressStatus'
import { RelativeTime } from '../RelativeTime'
import { SearchBox } from '../SearchBox'
import { DepsLegend, DepsRow, depsListClass } from './DepsRow'
import { GroupHeading } from './GroupHeading'

// depsSortOptions are the orders a group can list its clones in, the pinned ones first in
// each.
const depsSortOptions: readonly SegmentOption<DepsSort>[] = [
  { value: 'oldest', label: 'Oldest', title: 'The dependencies that changed longest ago first' },
  { value: 'name', label: 'A–Z', title: 'By owner/name' },
  { value: 'modules', label: 'Modules', title: 'The most go.mod and package.json files first' },
]

// chipClass is a kind's chip: a pill with its name and its count, which turns the accent
// while it filters the rows, as a Worktrees chip does.
const chipClass =
  'inline-flex items-center gap-1.5 rounded-full border border-border bg-background px-2.25 text-xs leading-5 whitespace-nowrap text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:border-accent/60 aria-pressed:bg-accent/10 aria-pressed:text-text'

// stored names a group as the settings keep it: "deps.stale".
function stored(group: DepsGroup): ScriptsSection {
  return `deps.${group}`
}

// groupsOpen is which groups start open, as depsGroups says.
function groupsOpen(): Readonly<Partial<Record<ScriptsSection, boolean>>> {
  return Object.fromEntries(depsGroups.map((g) => [stored(g.id), g.open]))
}

// oldAs says how old a read is: "14m old".
function oldAs(now: Date, iso: string): string {
  return `${formatAge(now, iso)} old`
}

// toggled is set with key added, or taken out when it is there.
function toggled(set: ReadonlySet<string>, key: string): ReadonlySet<string> {
  const next = new Set(set)

  if (!next.delete(key)) {
    next.add(key)
  }

  return next
}

// noClones and noAgents are the lists until the clones and the agents are read.
const noClones: readonly ManifestsClone[] = []
const noAgents: readonly AgentInfo[] = []

export interface DepsPageProps {
  manifests: ManifestsState
  // where names the repository locations the clones are in, such as "under ~/projects"
  // (see whereClones).
  where: string
  // onUpgrade opens a clone's upgrade sheet.
  onUpgrade: (clone: ManifestsClone) => void
  onOpenSettings: (section: SettingsSection) => void
  // sections are the sections the user opened or folded, kept across starts, and
  // onSection stores another.
  sections?: StoredSections
  onSection?: OnSection
  // depsSort is the order each group lists its clones in, as the settings remember it,
  // and onDepsSort stores another.
  depsSort?: DepsSort
  onDepsSort?: (sort: DepsSort) => void
}

// DepsPage is the Scripts tab's Deps page: every clone whose origin is on GitHub, muted
// ones left out, one line each in its group's columns (DepsRow), by how long ago its
// dependencies last changed as the clone's own git says (never a package registry): Stale
// first and open, then Aging and Fresh, folded, each heading's legend saying what its
// rows' edges mean. A row unfolds to the clone's go.mod and package.json files, several at
// once, and stays unfolded across reads; Upgrade… opens its upgrade sheet, and a row
// shows the upgrade an agent works on there, whose tag brings its terminal tab forward (one
// that can't be shown says why in the head). A row's mark, or its drawer's, marks the
// repository up to date, which moves it to Fresh at once, or unmarks it (a mark that fails
// says why in the head). Its head says how many clones and modules it
// lists and when it read them, or, in that place, how far the read under way has got, so
// the rows never move; a way to read every clone again; which clones it leaves out; and
// how old the page is when the last read failed. Above the groups sit a search over every
// group, which opens them all, a chip for each kind of module that keeps only the clones
// with one, and a sort of each group (the dependencies that changed longest ago first, by
// name, or the most modules first, the pinned repositories ahead in every one) remembered
// across starts. Each group stays open or folded as the user left it, across starts
// (useSections).
export function DepsPage({
  manifests,
  where,
  onUpgrade,
  onOpenSettings,
  sections = {},
  onSection = () => undefined,
  depsSort = 'oldest',
  onDepsSort = () => undefined,
}: DepsPageProps) {
  const id = useId()
  const [search, setSearch] = useState('')
  // kind is the kind of module a chip keeps the rows to, or null for every clone.
  const [kind, setKind] = useState<ManifestKind | null>(null)
  // picked is the sort chosen while the page shows, or null before one is, when the one
  // the settings remember applies; it holds the choice while the settings store it.
  const [picked, setPicked] = useState<DepsSort | null>(null)
  const sort = picked ?? depsSort
  // open are the clones whose drawers are unfolded, by folder: they outlast a read.
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const folds = useSections(sections, onSection, groupsOpen())
  const { byItem } = useSessions()
  const agents = useQuery(() => listAgents(), [], { log: 'could not list the agents for Deps' }).data ?? noAgents
  // shower brings an upgrade's terminal tab forward from its row's tag, and its error says
  // in the head why it couldn't, until the next try.
  const shower = useMutation((sessionId: string) => focusSession(sessionId))
  const { page, error, refreshing, refreshError, progress, refresh, mark, marking, markError } = manifests
  const clones = page?.clones ?? noClones
  // placed is each clone's group as the clock stands, in the clones' order (groupsAt): the
  // page renders again for the clock only once a clone moves to another group.
  const placed = useNowAs((now) => groupsAt(now, clones)).split(' ')
  const groupOf = new Map(clones.map((clone, i) => [clone, placed[i]]))
  const searching = search.trim() !== ''
  const searched = matchClones(clones, search)
  const shown = kind === null ? searched : searched.filter((c) => hasKind(c, kind))
  // A read is under way while the first one runs, one the page asked for does, or one says
  // how far it has got.
  const reading = (page === null && error === '') || refreshing || progress !== null
  const leftOut = page === null ? '' : leftOutText(page)

  let body: ReactNode

  if (page === null) {
    body = error !== '' && <ErrorBanner>Couldn’t read the clones: {error}</ErrorBanner>
  } else if (page.clones.length === 0 && leftOut === '') {
    body = (
      <div className="flex flex-col items-start gap-2 px-1">
        <p className="text-sm text-muted">No clones {where} yet. Deps lists the repositories you have cloned there.</p>
        <button
          type="button"
          onClick={() => {
            onOpenSettings('repositories')
          }}
          className={buttonClass}
        >
          Change the repository locations
        </button>
      </div>
    )
  } else {
    body = (
      <>
        {searching && shown.length === 0 && (
          <p className="px-1 text-sm text-muted">No repository matching “{search.trim()}”.</p>
        )}
        {depsGroups.map((group) => {
          const rows = sortClones(
            shown.filter((c) => groupOf.get(c) === group.id),
            sort,
          )
          const isOpen = searching || folds.isOpen(stored(group.id))

          if (rows.length === 0 && (searching || group.id !== 'stale')) {
            return null
          }

          return (
            <section key={group.id} aria-labelledby={`${id}-${group.id}`} className="flex flex-col gap-1">
              <GroupHeading
                id={`${id}-${group.id}`}
                title={group.title}
                count={rows.length}
                open={isOpen}
                disabled={searching}
                onToggle={() => {
                  folds.toggle(stored(group.id))
                }}
                note={rows.length > 0 ? <DepsLegend group={group.id} rows={rows} /> : undefined}
              />
              {isOpen &&
                (rows.length === 0 ? (
                  <p className="px-3 text-sm text-muted">{noStaleText(kind)}</p>
                ) : (
                  <ul aria-label={group.title} className={depsListClass}>
                    {rows.map((clone) => (
                      <DepsRow
                        key={`${clone.repo} ${clone.dir}`}
                        clone={clone}
                        open={open.has(clone.dir)}
                        onToggle={() => {
                          setOpen((was) => toggled(was, clone.dir))
                        }}
                        sessions={upgradeSessions(byItem, clone.repo)}
                        agents={agents}
                        onShowTerminal={(sessionId) => {
                          void shower.run(sessionId)
                        }}
                        onUpgrade={() => {
                          onUpgrade(clone)
                        }}
                        onMark={(checked) => {
                          void mark(clone.repo, checked)
                        }}
                        marking={marking}
                      />
                    ))}
                  </ul>
                ))}
            </section>
          )
        })}
      </>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-2 px-4 pt-4 pb-2">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="text-sm font-semibold">Deps</h2>
          <span className="flex min-w-0 items-center overflow-hidden text-xs whitespace-nowrap text-muted">
            {reading ? (
              <ProgressStatus step={readStep(progress)} />
            ) : (
              page !== null && (
                <span className="truncate">
                  {pageText(page)}
                  {page.checkedAt !== '' && (
                    <>
                      {' · read '}
                      <RelativeTime iso={page.checkedAt} show={formatAgo} />
                    </>
                  )}
                </span>
              )
            )}
          </span>
          <button
            type="button"
            disabled={refreshing}
            onClick={() => {
              void refresh()
            }}
            className={`ml-auto shrink-0 ${quietButtonClass}`}
          >
            <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
            {refreshing ? 'Reading…' : 'Read again'}
          </button>
        </div>
        {leftOut !== '' && <p className="text-xs text-muted">{leftOut}</p>}
        <ErrorText error={refreshError === '' ? '' : `Couldn’t read the clones again: ${refreshError}`} />
        <ErrorText error={markError} />
        <ErrorText error={shower.error} />
        {page !== null && page.error !== '' && (
          <p role="note" className="flex items-center gap-1.5 text-xs text-warning" title={page.error}>
            <AlertIcon className="size-3.5 shrink-0" />
            Couldn’t read the clones · <RelativeTime iso={page.checkedAt} show={oldAs} />
            <span className="sr-only">: {page.error}</span>
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <div className="min-w-48 flex-1">
            <SearchBox value={search} onChange={setSearch} />
          </div>
          {clones.length > 0 && (
            <div role="group" aria-label="Filter by kind" className="flex shrink-0 items-center gap-1.5">
              {moduleKinds.map((k) => {
                // n counts the clones the search leaves with a module of the kind, which the
                // chip shows; a kind none has gets no chip, unless it is the one filtering.
                const n = searched.filter((c) => hasKind(c, k)).length
                const on = kind === k

                if (n === 0 && !on) {
                  return null
                }

                return (
                  <button
                    key={k}
                    type="button"
                    aria-pressed={on}
                    title={kindTitle(k, n, on)}
                    onClick={() => {
                      setKind(on ? null : k)
                    }}
                    className={chipClass}
                  >
                    {kindNames[k]} <b className="font-semibold text-text tabular-nums">{formatCount(n)}</b>
                  </button>
                )
              })}
            </div>
          )}
          <Segmented<DepsSort>
            label="Sort the clones"
            size="sm"
            value={sort}
            options={depsSortOptions}
            onChange={(next) => {
              setPicked(next)
              onDepsSort(next)
            }}
          />
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-3 pb-4">{body}</div>
    </div>
  )
}
