import { useId, useState, type ReactNode } from 'react'

import { attempt } from '../../lib/call'
import { counted } from '../../lib/format'
import {
  intentOf,
  listProgressWords,
  matchCandidates,
  recentReleases,
  recentWords,
  releasable,
  releaseGroups,
  sharedHeadlines,
  sortCandidates,
} from '../../lib/release'
import { ruleFor } from '../../lib/repoRules'
import type { ReleaseCandidatesState } from '../../lib/useReleaseCandidates'
import { useSections, type OnSection, type StoredSections } from '../../lib/useSections'
import type { SettingsSection } from '../../lib/settingsIndex'
import { usable } from '../../lib/scriptsTab'
import { formatAge, formatAgo } from '../../lib/time'
import {
  setRepoRule,
  type IntegrationTool,
  type ReleaseCandidate,
  type ReleaseGroup,
  type ReleaseSort,
  type RepoRules,
  type ScriptRun,
  type ScriptsSection,
  type Settings,
} from '../../platform'
import { buttonClass, captionClass, quietButtonClass, Segmented, type SegmentOption } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, RefreshIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { GroupHeading } from './GroupHeading'
import { CandidateRow, MutedRow, ReadinessSummary, releaseListClass } from './ReleaseRow'
import { RepoSearch } from './RepoSearch'
import { RunStatus } from './RunStatus'
import { ToolProblem } from './ToolProblem'

// releaseSortOptions are the orders a group can list its repositories in, the pinned ones
// first in each.
const releaseSortOptions: readonly SegmentOption<ReleaseSort>[] = [
  { value: 'newest', label: 'Newest', title: 'The newest commit first' },
  { value: 'name', label: 'A–Z', title: 'By owner/name' },
  { value: 'commits', label: 'Commits', title: 'The most commits since the newest version first' },
]

// stored names a group as the settings keep it: "release.upToDate".
function stored(group: ReleaseGroup): ScriptsSection {
  return `release.${group}`
}

// groupsOpen is which groups start open, as releaseGroups says.
function groupsOpen(): Readonly<Partial<Record<ScriptsSection, boolean>>> {
  return Object.fromEntries(releaseGroups.map((g) => [stored(g.id), g.open]))
}

// oldAs says how old a list is: "14m old".
function oldAs(now: Date, iso: string): string {
  return `${formatAge(now, iso)} old`
}

// fromAgo says when a list kept from the last start was read: "From 2h ago".
function fromAgo(now: Date, iso: string): string {
  return `From ${formatAgo(now, iso)}`
}

// RecentRelease is a release in Recent: how it went, its version and repository, and
// when it started. It opens its run.
function RecentRelease({ run, onOpen }: { run: ScriptRun; onOpen: () => void }) {
  const version = intentOf(run).version

  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        className="flex w-full items-center gap-3 rounded-md px-3 py-1.5 text-left text-sm transition-colors hover:bg-raised"
      >
        <RunStatus status={run.status} />
        <span className="font-mono text-xs">{version === '' ? run.title : version}</span>
        <span className="min-w-0 truncate">{run.repo}</span>
        <span className="text-xs text-muted">{recentWords(run)}</span>
        <RelativeTime iso={run.startedAt} show={formatAgo} className="ml-auto shrink-0 text-xs text-muted" />
      </button>
    </li>
  )
}

export interface ReleasePageProps {
  candidates: ReleaseCandidatesState
  // tool is mage-x as the integrations list it, or undefined until listed.
  tool: IntegrationTool | undefined
  // runs are every run, newest first, for Recent; null until listed.
  runs: readonly ScriptRun[] | null
  // where names the repository locations the clones are in, such as "under ~/projects"
  // (see whereClones).
  where: string
  onRelease: (candidate: ReleaseCandidate) => void
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
  // sections are the sections the user opened or folded, kept across starts, and
  // onSection stores another.
  sections?: StoredSections
  onSection?: OnSection
  // repoRules are the pinned and muted repositories and orgs, from the settings, which a
  // muted row's Unmute changes; onSettingsSaved receives the settings as stored after it.
  repoRules?: RepoRules | undefined
  onSettingsSaved?: ((settings: Settings) => void) | undefined
  // releaseSort is the order each group lists its repositories in, as the settings
  // remember it, and onReleaseSort stores another.
  releaseSort?: ReleaseSort
  onReleaseSort?: (sort: ReleaseSort) => void
}

// noRepoRules are the rules until the settings have loaded.
const noRepoRules: RepoRules = { pinned: [], muted: [] }

// ReleasePage is the Scripts tab's Release page: every clone with a GitHub origin, one line
// each in its group's columns (ReleaseRow), the unreleased changes first, then the
// collapsed rest. Each row has Release… where there is something to release, the heading
// of such a group counts its rows by readiness, and a headline a wave of a group's rows
// share is badged on each of them. Above the groups sit a search over every group, and a
// sort of each (the newest commit first, by name, or the most commits first, the pinned
// repositories ahead in every one) remembered across starts; a way to read GitHub again,
// how far a read has got, and how old the list is when it was kept from the last start or
// the last read failed; and below them the recent releases, each opening its run. While
// magex can't be used, the page says why in place of its list. Each group stays open or
// folded as the user left it, across starts (useSections). The muted repositories sit in
// their own group, folded at first, each with Unmute.
export function ReleasePage({
  candidates,
  tool,
  runs,
  where,
  onRelease,
  onOpenRun,
  onOpenSettings,
  sections = {},
  onSection = () => undefined,
  repoRules = noRepoRules,
  onSettingsSaved,
  releaseSort = 'newest',
  onReleaseSort = () => undefined,
}: ReleasePageProps) {
  const id = useId()
  const [search, setSearch] = useState('')
  // picked is the sort chosen while the page shows, or null before one is, when the one
  // the settings remember applies; it holds the choice while the settings store it.
  const [picked, setPicked] = useState<ReleaseSort | null>(null)
  const sort = picked ?? releaseSort
  // unmuting is the target being unmuted, and unmuteError why the last unmute failed.
  const [unmuting, setUnmuting] = useState('')
  const [unmuteError, setUnmuteError] = useState('')
  const folds = useSections(sections, onSection, groupsOpen())
  const { list, error, refreshing, refreshError, progress, refresh } = candidates
  const searching = search.trim() !== ''
  const matched = list === null ? [] : matchCandidates(list.repos, search)
  const recent = recentReleases(runs)
  const openIntegrations = () => {
    onOpenSettings('integrations')
  }

  // unmute takes the mute off target. The settings, as stored, show it at once; the list
  // regroups once the service has read the rules ("releases:updated").
  const unmute = async (target: string) => {
    setUnmuting(target)
    setUnmuteError('')

    const result = await attempt(setRepoRule(target, ''))

    setUnmuting('')

    if (result.ok) {
      onSettingsSaved?.(result.data)
    } else {
      setUnmuteError(`Couldn’t unmute ${target}: ${result.error}`)
    }
  }

  // mutedBy is the target whose rule mutes a repository, or '' when none does any more.
  const mutedBy = (repo: string) => {
    const applied = ruleFor(repoRules, repo)

    return applied.rule === 'muted' ? applied.target : ''
  }

  // pinnedBy is the target whose rule pins a repository, or '' when none does.
  const pinnedBy = (repo: string) => {
    const applied = ruleFor(repoRules, repo)

    return applied.rule === 'pinned' ? applied.target : ''
  }

  let body: ReactNode

  if (!usable(tool)) {
    body = <ToolProblem tool={tool} binary="magex" name="mage-x" page="Release" onOpenSettings={openIntegrations} />
  } else if (list === null) {
    body =
      error === '' ? (
        <p role="status" className="flex items-center gap-2 px-1 text-sm text-muted">
          <Spinner />
          {progress === null ? 'Listing your repositories…' : listProgressWords(progress)}
        </p>
      ) : (
        <ErrorBanner>Couldn’t list the repositories: {error}</ErrorBanner>
      )
  } else if (list.repos.length === 0 && list.withoutGitHub === 0) {
    body = (
      <div className="flex flex-col items-start gap-2 px-1">
        <p className="text-sm text-muted">
          No clones {where} yet. Release lists the repositories you have cloned there.
        </p>
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
        {searching && matched.length === 0 && (
          <p className="px-1 text-sm text-muted">No repository matching “{search.trim()}”.</p>
        )}
        {releaseGroups.map((group) => {
          const rows = sortCandidates(
            matched.filter((r) => r.group === group.id),
            sort,
            (repo) => ruleFor(repoRules, repo).rule === 'pinned',
          )
          const shown = searching || folds.isOpen(stored(group.id))

          if (rows.length === 0 && (searching || group.id !== 'unreleased')) {
            return null
          }

          // waves are the repositories each headline is the newest commit of, among the
          // rows listed here: a search that leaves fewer drops the badge.
          const waves = sharedHeadlines(rows)

          return (
            <section key={group.id} aria-labelledby={`${id}-${group.id}`} className="flex flex-col gap-1">
              <GroupHeading
                id={`${id}-${group.id}`}
                title={group.title}
                count={rows.length}
                open={shown}
                disabled={searching}
                onToggle={() => {
                  folds.toggle(stored(group.id))
                }}
                note={releasable(group.id) && rows.length > 0 ? <ReadinessSummary rows={rows} /> : undefined}
              />
              {shown &&
                (rows.length === 0 ? (
                  <p className="px-3 text-sm text-muted">No repository has unreleased changes.</p>
                ) : (
                  <ul aria-label={group.title} className={releaseListClass}>
                    {rows.map((candidate) =>
                      candidate.group === 'muted' ? (
                        <MutedRow
                          key={`${candidate.repo} ${candidate.dir}`}
                          candidate={candidate}
                          target={mutedBy(candidate.repo)}
                          busy={unmuting !== ''}
                          onUnmute={() => {
                            void unmute(mutedBy(candidate.repo))
                          }}
                        />
                      ) : (
                        <CandidateRow
                          key={`${candidate.repo} ${candidate.dir}`}
                          candidate={candidate}
                          pinnedWith={pinnedBy(candidate.repo)}
                          wave={waves.get(candidate.headline) ?? []}
                          onRelease={() => {
                            onRelease(candidate)
                          }}
                        />
                      ),
                    )}
                  </ul>
                ))}
            </section>
          )
        })}
        {list.withoutGitHub > 0 && (
          <p className="px-1 text-xs text-muted">
            {counted(list.withoutGitHub, 'clone', 'clones')} without a GitHub origin{' '}
            {list.withoutGitHub === 1 ? 'isn’t' : 'aren’t'} listed.
          </p>
        )}
      </>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-2 px-4 pt-4 pb-2">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold">Release</h2>
          <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted">
            {usable(tool) && tool !== undefined && (
              <span>
                {tool.binary} {tool.copy.version.raw}
              </span>
            )}
            {list !== null && list.checkedAt !== '' && !list.kept && (
              <span>
                · Read <RelativeTime iso={list.checkedAt} show={formatAgo} />
              </span>
            )}
          </span>
          <button
            type="button"
            disabled={refreshing || !usable(tool)}
            onClick={() => {
              void refresh()
            }}
            className={`ml-auto ${quietButtonClass}`}
          >
            <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
            {refreshing ? 'Reading GitHub…' : 'Read GitHub again'}
          </button>
        </div>
        <ErrorText error={refreshError === '' ? '' : `Couldn’t read GitHub again: ${refreshError}`} />
        <ErrorText error={unmuteError} />
        {list !== null && list.error !== '' && (
          <p role="note" className="flex items-center gap-1.5 text-xs text-warning" title={list.error}>
            <AlertIcon className="size-3.5 shrink-0" />
            Couldn’t reach GitHub · <RelativeTime iso={list.checkedAt} show={oldAs} />
            <span className="sr-only">: {list.error}</span>
          </p>
        )}
        {list !== null && list.error === '' && (list.kept || progress !== null) && (
          // The list kept from the last start, with its age, and how far the read of
          // GitHub under way has got. The space before the progress keeps the two apart
          // when read as text; the gap keeps them apart on screen.
          <p role="status" className="flex flex-wrap items-center gap-x-1.5 text-xs text-muted">
            {(list.refreshing || progress !== null) && <Spinner className="size-3.5" />}
            {list.kept && (
              <span>
                <RelativeTime iso={list.checkedAt} show={fromAgo} />
                {list.refreshing && ' · checking again…'}
              </span>
            )}
            {progress !== null && <span> {listProgressWords(progress)}</span>}
          </p>
        )}
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <RepoSearch value={search} onChange={setSearch} />
          </div>
          <Segmented<ReleaseSort>
            label="Sort the repositories"
            size="sm"
            value={sort}
            options={releaseSortOptions}
            onChange={(next) => {
              setPicked(next)
              onReleaseSort(next)
            }}
          />
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-3 pb-4">
        {body}
        {recent.length > 0 && (
          <section aria-labelledby={`${id}-recent`} className="flex flex-col gap-1">
            <h3 id={`${id}-recent`} className={`${captionClass} px-1`}>
              Recent
            </h3>
            <ul aria-label="Recent releases" className="flex flex-col">
              {recent.map((run) => (
                <RecentRelease
                  key={run.id}
                  run={run}
                  onOpen={() => {
                    onOpenRun(run.id)
                  }}
                />
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  )
}
