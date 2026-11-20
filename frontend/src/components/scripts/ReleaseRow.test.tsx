import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { CommitChecks, ReleaseCandidate, ReleaseCloneState } from '../../platform'
import { at } from '../../test/clock'
import { releaseCandidate } from '../../test/fixtures'
import { GroupHeading } from './GroupHeading'
import { CandidateRow, MutedRow, ReadinessSummary, releaseListClass } from './ReleaseRow'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// cloneState is a clone on master with nothing in the way, as overrides change it.
function cloneState(overrides: Partial<ReleaseCloneState> = {}): ReleaseCloneState {
  return {
    branch: 'master',
    detached: false,
    dirty: 0,
    notPushed: 0,
    behind: false,
    localOnly: [],
    unreadable: false,
    ...overrides,
  }
}

// muted is acme-org/quiet as the Muted group lists it: its name and its clone, GitHub not
// asked about it.
const muted = releaseCandidate({
  repo: 'acme-org/quiet',
  dir: '~/projects/quiet',
  group: 'muted',
  version: '',
  ahead: 0,
  commits: [],
  headline: '',
  headAt: '',
  checks: '',
  clone: null,
})

// renderRows renders rows in a group's list, as the page lists them, the clock at
// FIXED_NOW.
function renderRows(rows: ReactNode) {
  return render(<ul className={releaseListClass}>{rows}</ul>, { wrapper: at() })
}

// renderCandidate renders one candidate's row and returns it, with its Release… handler.
function renderCandidate(candidate: ReleaseCandidate, props: { pinnedWith?: string; wave?: readonly string[] } = {}) {
  const onRelease = vi.fn<() => void>()

  renderRows(<CandidateRow candidate={candidate} onRelease={onRelease} {...props} />)

  return { row: screen.getByRole('listitem'), onRelease }
}

// edge is a row's readiness edge, its first cell.
function edge(row: HTMLElement): Element | null {
  return row.firstElementChild
}

describe('CandidateRow', () => {
  it('lays an unreleased repository out on one line, and Release… opens its sheet', async () => {
    const { row, onRelease } = renderCandidate(releaseCandidate())
    const r = within(row)

    expect(row.parentElement).toHaveClass('@container', 'grid')
    expect(row).toHaveClass('group/row', 'grid-cols-subgrid', 'col-span-full')
    expect(r.getByTitle('acme-org/sdk')).toHaveTextContent('acme-org/sdk')
    expect(r.getByText('v1.4.2')).toHaveClass('font-mono')
    expect(r.getByText('+4')).toHaveAttribute('title', '4 commits since v1.4.2')
    expect(r.getByText('Fix retries on 502 (#45)')).toHaveAttribute('title', 'Fix retries on 502 (#45)')
    expect(row).toHaveTextContent('Your clone: on feature-x')
    expect(r.getByText('12m ago')).toHaveAttribute('dateTime', '2026-06-15T11:48:00Z')
    expect(row.querySelector('[data-checks-running]')).not.toBeInTheDocument()

    const release = r.getByRole('button', { name: 'Release acme-org/sdk…' })

    expect(release).toHaveTextContent('Release…')
    expect(release).toHaveClass('group-hover/row:bg-surface', 'group-focus-within/row:bg-surface')

    await userEvent.click(release)

    expect(onRelease).toHaveBeenCalledOnce()
  })

  it.each<{ name: string; checks: CommitChecks; clone: ReleaseCloneState; want: string; bar: string[] }>([
    { name: 'failing checks', checks: 'FAILURE', clone: cloneState(), want: 'failing', bar: ['bg-danger'] },
    { name: 'checks that errored', checks: 'ERROR', clone: cloneState(), want: 'failing', bar: ['bg-danger'] },
    {
      name: 'failing checks over a clone that blocks',
      checks: 'FAILURE',
      clone: cloneState({ dirty: 2 }),
      want: 'failing',
      bar: ['bg-danger'],
    },
    {
      name: 'a clone that blocks',
      checks: 'SUCCESS',
      clone: cloneState({ dirty: 2 }),
      want: 'blocked',
      bar: ['bg-warning'],
    },
    {
      name: 'running checks',
      checks: 'PENDING',
      clone: cloneState(),
      want: 'running',
      bar: ['bg-muted/60', 'motion-safe:animate-pulse'],
    },
    {
      name: 'expected checks',
      checks: 'EXPECTED',
      clone: cloneState(),
      want: 'running',
      bar: ['bg-muted/60', 'motion-safe:animate-pulse'],
    },
    { name: 'passed checks', checks: 'SUCCESS', clone: cloneState(), want: 'ready', bar: ['bg-success'] },
    {
      name: 'passed checks, behind and on a branch',
      checks: 'SUCCESS',
      clone: cloneState({ behind: true, branch: 'feature-x' }),
      want: 'ready',
      bar: ['bg-success'],
    },
    { name: 'no checks, never green', checks: '', clone: cloneState(), want: 'none', bar: ['bg-muted/30'] },
  ])('colors the edge by its readiness: $name', ({ checks, clone, want, bar }) => {
    const { row } = renderCandidate(releaseCandidate({ checks, clone }))

    expect(edge(row)).toHaveAttribute('data-readiness', want)
    expect(edge(row)).toHaveAttribute('aria-hidden', 'true')
    expect(edge(row)).toHaveClass('col-start-1', '@max-4xl:row-end-3', ...bar)
  })

  it.each<{ checks: CommitChecks; words: string }>([
    { checks: 'SUCCESS', words: 'checks passed' },
    { checks: 'PENDING', words: 'checks running' },
    { checks: 'EXPECTED', words: 'checks running' },
    { checks: 'FAILURE', words: 'checks failing' },
    { checks: 'ERROR', words: 'checks failing' },
    { checks: '', words: 'no checks' },
  ])('says the checks $checks as words, for a screen reader and in the tooltip', ({ checks, words }) => {
    const { row } = renderCandidate(releaseCandidate({ checks }))
    const text = within(row).getByText(words)

    expect(text).toHaveClass('sr-only')
    expect(text.parentElement).toHaveAttribute('title', words)
    expect(text.parentElement).toHaveClass('col-start-2')
  })

  it('marks running checks with the working dot', () => {
    const { row } = renderCandidate(releaseCandidate({ checks: 'PENDING' }))

    expect(row.querySelector('[data-checks-running]')).toBeInTheDocument()
  })

  it('splits the repository into a muted owner and its name', () => {
    const { row } = renderCandidate(releaseCandidate())
    const [owner, name] = within(row).getByTitle('acme-org/sdk').children

    expect(owner).toHaveTextContent(/^acme-org\/$/)
    expect(owner).toHaveClass('text-muted')
    expect(name).toHaveTextContent(/^sdk$/)
    expect(name).toHaveClass('font-medium')
  })

  it('reads a repository without an owner as all name', () => {
    const { row } = renderCandidate(releaseCandidate({ repo: 'sdk' }))
    const [owner, name] = within(row).getByTitle('sdk').children

    expect(owner).toBeEmptyDOMElement()
    expect(name).toHaveTextContent(/^sdk$/)
  })

  it('marks a pinned repository after its name, saying where to unpin it', () => {
    const { row } = renderCandidate(releaseCandidate(), { pinnedWith: 'acme-org' })
    const pin = within(row).getByRole('img', { name: 'Pinned with acme-org' })

    expect(pin).toHaveAttribute('title', 'Pinned with acme-org: unpin it in Settings → Repositories')
    expect(pin.previousElementSibling).toHaveAttribute('title', 'acme-org/sdk')
  })

  it('shows no pin for a repository no rule pins', () => {
    const { row } = renderCandidate(releaseCandidate())

    expect(within(row).queryByRole('img')).not.toBeInTheDocument()
  })

  it('counts one commit since the version, and groups a big count’s digits', () => {
    renderRows(
      <>
        <CandidateRow candidate={releaseCandidate({ ahead: 1 })} onRelease={vi.fn()} />
        <CandidateRow candidate={releaseCandidate({ repo: 'acme-org/docs', ahead: 13708 })} onRelease={vi.fn()} />
      </>,
    )

    expect(screen.getByText('+1')).toHaveAttribute('title', '1 commit since v1.4.2')
    expect(screen.getByText('+13,708')).toHaveAttribute('title', '13,708 commits since v1.4.2')
  })

  it('says +? when GitHub couldn’t compare the version with the branch', () => {
    renderRows(
      <>
        <CandidateRow candidate={releaseCandidate({ ahead: 0, commits: [] })} onRelease={vi.fn()} />
        <CandidateRow
          candidate={releaseCandidate({ repo: 'acme-org/docs', ahead: 0, commits: [], defaultBranch: '' })}
          onRelease={vi.fn()}
        />
      </>,
    )

    const [sdk, docs] = screen.getAllByText('+?')

    expect(sdk).toHaveAttribute(
      'title',
      'GitHub couldn’t compare v1.4.2 with master, so how many commits came since isn’t known',
    )
    expect(docs).toHaveAttribute(
      'title',
      'GitHub couldn’t compare v1.4.2 with the default branch, so how many commits came since isn’t known',
    )
  })

  it('marks commits that suggest a minor release with a feat chip', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        commits: [
          { oid: 'a1', headline: 'feat(client): retry on 429', at: '2026-06-15T11:48:00Z' },
          { oid: 'b2', headline: 'Tidy the tests', at: '2026-06-14T11:48:00Z' },
        ],
      }),
    )
    const chip = within(row).getByText('feat')

    expect(chip).toHaveAttribute('title', '1 commit starts with feat:, which usually means a minor release.')
    expect(chip).toHaveClass('text-info', 'border-info/40', 'bg-info/10', 'rounded-full')
    expect(within(row).queryByText('breaking')).not.toBeInTheDocument()
  })

  it('marks commits that suggest a major release with a breaking chip', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        commits: [
          { oid: 'a1', headline: 'feat!: drop the v1 config format', at: '2026-06-15T11:48:00Z' },
          { oid: 'b2', headline: 'feat: read the v2 config', at: '2026-06-14T11:48:00Z' },
        ],
      }),
    )
    const chip = within(row).getByText('breaking')

    expect(chip).toHaveAttribute('title', '1 commit is marked breaking (feat!:), which usually means a major release.')
    expect(chip).toHaveClass('text-caution', 'border-caution/40', 'bg-caution/10')
    expect(within(row).queryByText('feat')).not.toBeInTheDocument()
  })

  it('shows no chip when no commit suggests a bump', () => {
    const { row } = renderCandidate(releaseCandidate())

    expect(within(row).queryByText('feat')).not.toBeInTheDocument()
    expect(within(row).queryByText('breaking')).not.toBeInTheDocument()
  })

  it('badges a headline the newest commit of three listed repositories shares', () => {
    const wave = ['acme-org/sdk', 'acme-org/web', 'acme-org/tools']
    const { row } = renderCandidate(releaseCandidate(), { wave })
    const badge = within(row).getByTitle(
      '3 of the listed repositories have this as their newest commit: acme-org/sdk, acme-org/web, acme-org/tools',
    )

    expect(badge).toHaveTextContent('×3, the newest commit of 3 listed repositories')
    expect(within(badge).getByText('×3')).toHaveAttribute('aria-hidden', 'true')
    expect(within(badge).getByText(', the newest commit of 3 listed repositories')).toHaveClass('sr-only')
    expect(within(row).getByText('Fix retries on 502 (#45)')).toBeInTheDocument()
  })

  it('leaves a headline only two listed repositories share unbadged', () => {
    const { row } = renderCandidate(releaseCandidate(), { wave: ['acme-org/sdk', 'acme-org/web'] })

    expect(within(row).queryByText(/^×/)).not.toBeInTheDocument()
  })

  it('shows only the blockers of a clone that has any, in amber', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        clone: cloneState({ dirty: 1, behind: true, branch: 'feat/cancel', localOnly: ['v1.4.3'] }),
      }),
    )
    const r = within(row)
    const dirty = r.getByText('1 uncommitted').parentElement
    const local = r.getByText('v1.4.3 only on this Mac').parentElement

    expect(row).toHaveTextContent('Your clone: 1 uncommitted, v1.4.3 only on this Mac')
    expect(dirty).toHaveAttribute('title', '1 file has uncommitted changes. Commit or stash them first.')
    expect(dirty).toHaveClass('text-warning', 'font-medium')
    expect(local).toHaveAttribute(
      'title',
      'The tag v1.4.3 is in this clone but not on GitHub. Push it or delete it first.',
    )
    expect(row).not.toHaveTextContent('behind')
    expect(row).not.toHaveTextContent('feat/cancel')
    expect(edge(row)).toHaveAttribute('data-readiness', 'blocked')
  })

  it('shows the quiet notes muted: the commits the clone lacks, then its branch', () => {
    const { row } = renderCandidate(releaseCandidate({ clone: cloneState({ behind: true, branch: 'feature-x' }) }))
    const behind = within(row).getByTitle('origin/master has commits your clone doesn’t. magex pulls them first.')
    const branch = within(row).getByTitle('You’re on feature-x. magex switches to master, then back.')

    expect(row).toHaveTextContent('Your clone: behind, on feature-x')
    expect(behind).toHaveTextContent(/^behind$/)
    expect(behind).toHaveClass('text-muted')
    expect(branch).toHaveTextContent(/^on feature-x$/)
    expect(branch).toHaveClass('text-muted')
    expect(within(branch).getByText('feature-x')).toHaveClass('truncate')
    expect(behind.compareDocumentPosition(branch) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('says nothing about a clone with nothing in the way', () => {
    const { row } = renderCandidate(releaseCandidate({ clone: cloneState() }))

    expect(row).not.toHaveTextContent('Your clone')
  })

  it('shows no age for a newest commit without a time', () => {
    const { row } = renderCandidate(releaseCandidate({ headAt: '' }))

    expect(row.querySelector('time')).not.toBeInTheDocument()
  })

  it('offers Release… for a repository with no releases yet, its edge from its checks alone', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        repo: 'acme-org/tools',
        group: 'noReleases',
        version: '',
        ahead: 0,
        commits: [],
        headline: 'First commit',
        checks: 'FAILURE',
        clone: null,
      }),
    )
    const r = within(row)

    expect(r.getByText('no version yet')).toBeInTheDocument()
    expect(r.getByText('First commit')).toBeInTheDocument()
    expect(r.getByRole('button', { name: 'Release acme-org/tools…' })).toBeInTheDocument()
    expect(row).not.toHaveTextContent('+')
    expect(edge(row)).toHaveAttribute('data-readiness', 'failing')
  })

  it('offers no Release… and no edge for a repository up to date', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        repo: 'acme-org/web',
        group: 'upToDate',
        version: 'v0.9.1',
        ahead: 0,
        commits: [],
        headline: 'Release v0.9.1',
        clone: null,
      }),
    )
    const r = within(row)

    expect(r.queryByRole('button')).not.toBeInTheDocument()
    expect(r.getByText('checks passed')).toBeInTheDocument()
    expect(r.getByText('v0.9.1')).toBeInTheDocument()
    expect(row).not.toHaveTextContent('+')
    expect(edge(row)).not.toHaveAttribute('data-readiness')
    expect(edge(row)).toHaveClass('bg-transparent')
  })

  it('says why a repository can’t be released here, in its headline’s place, without checks', () => {
    const { row } = renderCandidate(
      releaseCandidate({
        repo: 'acme-org/old',
        group: 'cantRelease',
        reason: 'Archived on GitHub',
        version: 'v2.0.0',
        ahead: 0,
        commits: [],
        headline: 'Archive',
        clone: null,
      }),
    )
    const r = within(row)

    expect(r.getByText('Archived on GitHub')).toHaveClass('text-text')
    expect(r.queryByText('Archive')).not.toBeInTheDocument()
    expect(r.queryByText('checks passed')).not.toBeInTheDocument()
    expect(r.queryByRole('button')).not.toBeInTheDocument()
    expect(edge(row)).toHaveClass('bg-transparent')
  })
})

describe('MutedRow', () => {
  it('shows the folder in the headline’s place, and Unmute unmutes its rule', async () => {
    const onUnmute = vi.fn<() => void>()

    renderRows(<MutedRow candidate={muted} target="acme-org" busy={false} onUnmute={onUnmute} />)

    const row = screen.getByRole('listitem')
    const unmute = within(row).getByRole('button', { name: 'Unmute acme-org' })

    expect(within(row).getByTitle('acme-org/quiet')).toHaveTextContent('acme-org/quiet')
    expect(within(row).getByText('~/projects/quiet')).toHaveClass('font-mono')
    expect(row).not.toHaveTextContent('checks')
    expect(row).not.toHaveTextContent('no version yet')
    expect(row.querySelector('[data-readiness]')).not.toBeInTheDocument()
    expect(unmute).toHaveTextContent('Unmute')
    expect(unmute).toHaveAttribute(
      'title',
      'Unmute acme-org: GitHub is asked about it again, and its items go back to the inbox’s lists',
    )
    expect(unmute).toBeEnabled()

    await userEvent.click(unmute)

    expect(onUnmute).toHaveBeenCalledOnce()
  })

  it('holds Unmute while another unmute is under way', () => {
    renderRows(<MutedRow candidate={muted} target="acme-org/quiet" busy onUnmute={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Unmute acme-org/quiet' })).toBeDisabled()
  })

  it('says a repository unmuted already is being read again', () => {
    renderRows(<MutedRow candidate={muted} target="" busy={false} onUnmute={vi.fn()} />)

    expect(screen.getByText('Unmuted · reading it again…')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

// withChecks is the default candidate with other checks and clone, named repo.
function withChecks(repo: string, checks: CommitChecks, clone: ReleaseCloneState = cloneState()): ReleaseCandidate {
  return releaseCandidate({ repo, checks, clone })
}

describe('ReadinessSummary', () => {
  it('counts the rows by readiness, what needs a look first, each with its edge’s color', () => {
    const { container } = render(
      <ReadinessSummary
        rows={[
          withChecks('acme-org/a', 'SUCCESS'),
          withChecks('acme-org/b', 'FAILURE'),
          withChecks('acme-org/c', ''),
          withChecks('acme-org/d', 'FAILURE'),
          withChecks('acme-org/e', 'SUCCESS', cloneState({ notPushed: 1 })),
          withChecks('acme-org/f', 'PENDING'),
        ]}
      />,
    )

    expect(container).toHaveTextContent(/^: 2 failing, 1 blocked, 1 running, 1 ready, 1 without checks$/)

    const items = [...container.querySelectorAll('.inline-flex')]

    expect(items.map((i) => i.textContent)).toEqual([
      '2 failing',
      '1 blocked',
      '1 running',
      '1 ready',
      '1 without checks',
    ])
    const dots = items.map((i) => i.firstElementChild)
    const colors = ['bg-danger', 'bg-warning', 'bg-muted/60', 'bg-success', 'bg-muted/30']

    colors.forEach((color, i) => {
      expect(dots[i]).toHaveClass('size-1.5', 'rounded-full', color)
      expect(dots[i]).toHaveAttribute('aria-hidden', 'true')
    })
  })
})

describe('GroupHeading', () => {
  it('names its button with the readiness summary as its note', async () => {
    const onToggle = vi.fn<() => void>()
    const { container } = render(
      <GroupHeading
        id="unreleased"
        title="Unreleased changes"
        count={3}
        open
        disabled={false}
        onToggle={onToggle}
        note={
          <ReadinessSummary
            rows={[
              withChecks('acme-org/a', 'FAILURE'),
              withChecks('acme-org/b', 'SUCCESS'),
              withChecks('acme-org/c', 'ERROR'),
            ]}
          />
        }
      />,
    )
    const button = screen.getByRole('button', { name: 'Unreleased changes · 3: 2 failing, 1 ready' })

    expect(button).toHaveAttribute('aria-expanded', 'true')
    expect(button.parentElement).toHaveAttribute('id', 'unreleased')
    expect(screen.getByText('2 failing').closest('.flex-1')).toHaveClass('ml-2', 'min-w-0', 'normal-case')
    expect(container.firstElementChild).toHaveClass('sticky', 'top-0', 'z-10', 'bg-background')

    await userEvent.click(button)

    expect(onToggle).toHaveBeenCalledOnce()
  })

  it('cuts a string note short, and shows the group folded', () => {
    render(
      <GroupHeading
        id="hooks"
        title="Without a hooks config"
        count={2}
        open={false}
        disabled
        onToggle={vi.fn()}
        note="no .github/env/ or .github/.env.base"
      >
        <button type="button">Set up</button>
      </GroupHeading>,
    )
    const button = screen.getByRole('button', { name: /^Without a hooks config · 2/ })

    expect(button).toHaveAttribute('aria-expanded', 'false')
    expect(button).toBeDisabled()
    expect(screen.getByText('no .github/env/ or .github/.env.base')).toHaveClass('truncate', 'ml-2')
    expect(screen.getByRole('button', { name: 'Set up' })).toBeInTheDocument()
  })

  it('shows only its title and count without a note', () => {
    render(<GroupHeading id="yours" title="Yours" count={1} open disabled={false} onToggle={vi.fn()} />)

    const button = screen.getByRole('button', { name: 'Yours · 1' })

    expect(button.querySelectorAll(':scope > span')).toHaveLength(1)
  })
})
