import { useId, useState, type ReactNode } from 'react'

import { counted, formatCount } from '../../lib/format'
import {
  cloneNotes,
  commitChecksText,
  intentOf,
  listProgressWords,
  matchCandidates,
  recentReleases,
  recentWords,
  releasable,
  releaseGroups,
} from '../../lib/release'
import type { ReleaseCandidatesState } from '../../lib/useReleaseCandidates'
import { useSections, type OnSection, type StoredSections } from '../../lib/useSections'
import type { SettingsSection } from '../../lib/settingsIndex'
import { usable } from '../../lib/scriptsTab'
import { formatAge, formatAgo } from '../../lib/time'
import type {
  CommitChecks,
  IntegrationTool,
  ReleaseCandidate,
  ReleaseGroup,
  ScriptRun,
  ScriptsSection,
} from '../../platform'
import { buttonClass, captionClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, CheckIcon, DotIcon, RefreshIcon, RingIcon, XIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { GroupHeading } from './GroupHeading'
import { RepoSearch } from './RepoSearch'
import { RunStatus } from './RunStatus'
import { ToolProblem } from './ToolProblem'

// checksIcons mark the checks on a commit beside their words: passed, running, failing,
// or none at all.
const checksIcons: Readonly<Record<CommitChecks, ReactNode>> = {
  SUCCESS: <CheckIcon className="size-3.5 shrink-0 text-success" />,
  PENDING: <DotIcon className="size-3.5 shrink-0 text-warning" />,
  EXPECTED: <DotIcon className="size-3.5 shrink-0 text-warning" />,
  FAILURE: <XIcon className="size-3.5 shrink-0 text-danger" />,
  ERROR: <XIcon className="size-3.5 shrink-0 text-danger" />,
  '': <RingIcon className="size-3.5 shrink-0 text-muted" />,
}

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

// CandidateRow is one repository: its newest version and how many commits came since,
// the checks on its newest commit and its age, that commit's headline, what matters about
// its clone, and Release… for a repository with something to release (or, for one that
// can't be released here, why).
function CandidateRow({ candidate, onRelease }: { candidate: ReleaseCandidate; onRelease: () => void }) {
  const notes = cloneNotes(candidate.clone, candidate.defaultBranch)

  return (
    <li className="flex items-start gap-3 rounded-md px-3 py-2 hover:bg-raised/60">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">
          <span className="font-medium">{candidate.repo}</span>
          <span className="font-mono text-xs text-muted">
            {candidate.version === '' ? 'no version yet' : candidate.version}
          </span>
          {candidate.ahead > 0 && (
            <span className="text-xs text-text" title={counted(candidate.ahead, 'commit since', 'commits since')}>
              +{formatCount(candidate.ahead)}
            </span>
          )}
          <span className="flex items-center gap-1 text-xs text-muted">
            {checksIcons[candidate.checks]}
            {commitChecksText(candidate.checks)}
          </span>
          {candidate.headAt !== '' && (
            <RelativeTime iso={candidate.headAt} show={formatAgo} className="text-xs text-muted" />
          )}
        </p>
        {candidate.headline !== '' && <p className="truncate text-xs text-muted">{candidate.headline}</p>}
        {candidate.reason !== '' && <p className="text-xs text-text">{candidate.reason}</p>}
        {notes.length > 0 && (
          <p className="flex items-center gap-1 text-xs text-warning">
            <AlertIcon className="size-3.5 shrink-0" />
            <span className="sr-only">Your clone: </span>
            {notes.join(' · ')}
          </p>
        )}
      </div>
      {releasable(candidate.group) && (
        <button type="button" aria-label={`Release ${candidate.repo}…`} onClick={onRelease} className={buttonClass}>
          Release…
        </button>
      )}
    </li>
  )
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
  // cloneRoot is the folder the clones are under, with ~.
  cloneRoot: string
  onRelease: (candidate: ReleaseCandidate) => void
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
  // sections are the sections the user opened or folded, kept across starts, and
  // onSection stores another.
  sections?: StoredSections
  onSection?: OnSection
}

// ReleasePage is the Scripts tab's Release page: every clone with a GitHub origin, in its
// group (unreleased changes first, newest commit first, then the collapsed rest), each
// with Release… where there is something to release; a search over every group; a way
// to read GitHub again, how far a read has got, and how old the list is when it was kept
// from the last start or the last read failed; and the recent releases, each opening its
// run. While magex can't be used, the page says why in place of its list. Each group
// stays open or folded as the user left it, across starts (useSections).
export function ReleasePage({
  candidates,
  tool,
  runs,
  cloneRoot,
  onRelease,
  onOpenRun,
  onOpenSettings,
  sections = {},
  onSection = () => undefined,
}: ReleasePageProps) {
  const id = useId()
  const [search, setSearch] = useState('')
  const folds = useSections(sections, onSection, groupsOpen())
  const { list, error, refreshing, refreshError, progress, refresh } = candidates
  const searching = search.trim() !== ''
  const matched = list === null ? [] : matchCandidates(list.repos, search)
  const recent = recentReleases(runs)
  const openIntegrations = () => {
    onOpenSettings('integrations')
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
          No clones under {cloneRoot} yet. Release lists the repositories you have cloned there.
        </p>
        <button
          type="button"
          onClick={() => {
            onOpenSettings('repositories')
          }}
          className={buttonClass}
        >
          Change the clone root
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
          const rows = matched.filter((r) => r.group === group.id)
          const shown = searching || folds.isOpen(stored(group.id))

          if (rows.length === 0 && (searching || group.id !== 'unreleased')) {
            return null
          }

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
              />
              {shown &&
                (rows.length === 0 ? (
                  <p className="px-3 text-sm text-muted">No repository has unreleased changes.</p>
                ) : (
                  <ul aria-label={group.title} className="flex flex-col">
                    {rows.map((candidate) => (
                      <CandidateRow
                        key={`${candidate.repo} ${candidate.dir}`}
                        candidate={candidate}
                        onRelease={() => {
                          onRelease(candidate)
                        }}
                      />
                    ))}
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
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Release</h2>
          <div className="flex items-center gap-2 text-xs text-muted">
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
            <button
              type="button"
              disabled={refreshing || !usable(tool)}
              onClick={() => {
                void refresh()
              }}
              className={quietButtonClass}
            >
              <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
              {refreshing ? 'Reading GitHub…' : 'Read GitHub again'}
            </button>
          </div>
        </div>
        <ErrorText error={refreshError === '' ? '' : `Couldn’t read GitHub again: ${refreshError}`} />
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
        <RepoSearch value={search} onChange={setSearch} />
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
