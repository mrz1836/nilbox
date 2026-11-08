import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Profiler, type ComponentType, type ReactNode } from 'react'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'

import type { ItemRowProps } from './components/ItemRow'
import { triageList } from './lib/triage'
import type { InboxItem, InboxSnapshot } from './platform'
import { FIXED_NOW, fromNow } from './test/clock'
import { details, fileCheck, item, list, rate, snapshot } from './test/fixtures'
import { emit, platform, renderApp } from './test/platform'

// The render profile: a 300-item inbox, and how much of it renders again for a minute
// tick, a background update that changes one item, collapsing a list, and leaving the
// inbox and coming back. It runs twice. In the unit tests the components run as written,
// where the bounds below hold however much a parent renders again. `pnpm test:renders`
// (vitest.renders.config.ts) runs it through the React Compiler, as the build ships, where
// the tighter bounds hold, and prints what it measured.

// compiled reports a run through the React Compiler.
const compiled = import.meta.env.REACT_COMPILER === '1'

// counts are what the profile records: the rows rendered, the rows mounted, the commits,
// and the time React spent rendering.
const counts = vi.hoisted(() => ({ rows: 0, mounts: 0, commits: 0, ms: 0 }))

// ItemRow is wrapped, memo and all, so every render and every mount of a row is counted.
vi.mock('./components/ItemRow', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./components/ItemRow')>()
  const { createElement, memo, useState } = await import('react')
  const Row = (actual.ItemRow as unknown as { type: ComponentType<ItemRowProps> }).type

  // counted records a render of a row. The count is kept outside React on purpose: it is
  // how often React renders the row.
  const counted = () => {
    counts.rows += 1
  }

  function CountedRow(props: ItemRowProps) {
    useState(() => {
      counts.mounts += 1
    })
    counted()

    return createElement(Row, props)
  }

  return { ...actual, ItemRow: memo(CountedRow) }
})

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

// The window's clock reads a faked time, starting at the fixtures' FIXED_NOW, so the
// minute tick can be stepped. renderApp makes the clock as the window renders, once the
// timers are faked.
beforeAll(() => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'], now: FIXED_NOW })
})

afterAll(() => {
  vi.useRealTimers()
})

const minute = 60_000

// ago is a time minutes before FIXED_NOW.
function ago(minutes: number): string {
  return fromNow(-minutes * minute).toISOString()
}

// row is one of the inbox's items. About one in ten moved within the hour, so a minute
// tick changes its age; the rest are hours to days old. A few are snoozed, back from a
// snooze, dismissed, or pinned, and every seventh carries a sensitive-file check.
function row(id: number): InboxItem {
  const minutes = id % 10 === 0 ? id % 50 : id * 47

  return item({
    id,
    number: id,
    title: `Item ${String(id)}`,
    url: `https://github.com/acme-org/example-repo/pull/${String(id)}`,
    updatedAt: ago(minutes),
    details: details(),
    files: id % 7 === 0 ? fileCheck() : null,
    snoozedUntil: id % 13 === 0 ? ago(-3 * 24 * 60) : id % 17 === 0 ? ago(120) : '',
    dismissedAt: id % 19 === 0 ? ago(600) : '',
    pinnedAt: id % 23 === 0 ? ago(30) : '',
  })
}

// inbox is 300 items, 100 in each list, with change made to each.
function inbox(change: (item: InboxItem) => InboxItem = (i) => i): InboxSnapshot {
  const ids = (from: number) => Array.from({ length: 100 }, (_, i) => change(row(from + i)))

  return snapshot({
    reviewRequested: list(ids(1)),
    authored: list(ids(101)),
    assigned: list(ids(201)),
    rate: { graphql: rate(), core: rate(), search: rate() },
    lastRefreshedAt: ago(5),
  })
}

// shown counts the rows a list shows in its own tab.
function shown(key: 'reviewRequested' | 'authored' | 'assigned'): number {
  return triageList(inbox()[key], FIXED_NOW).inbox.length
}

// mountedRows counts the inbox rows in the document, showing or hidden.
function mountedRows(): number {
  return document.querySelectorAll('[data-item-id]').length
}

// settle lets every answer, and every render it causes, finish.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((resolve) => {
        setTimeout(resolve, 0)
      })
    })
  }
}

interface Measure {
  rows: number
  mounts: number
  commits: number
  ms: number
}

// measure counts what run renders, mounts, and commits.
async function measure(run: () => Promise<void> | void): Promise<Measure> {
  counts.rows = 0
  counts.mounts = 0
  counts.commits = 0
  counts.ms = 0
  await run()
  await settle()

  return { rows: counts.rows, mounts: counts.mounts, commits: counts.commits, ms: Math.round(counts.ms) }
}

// setUp answers the window's reads of the inbox with a 300-item inbox, and returns a way
// to change the inbox the next read returns. The platform fake answers everything else.
function setUp() {
  // Each answer is a fresh copy, as a bound call returns freshly decoded JSON.
  let current = inbox()
  platform.getInboxSnapshot.mockImplementation(() => Promise.resolve(structuredClone(current)))

  return (next: InboxSnapshot) => {
    current = next
  }
}

// Profiled records each commit of the window, and the time React spent rendering it.
function Profiled({ children }: { children: ReactNode }) {
  return (
    <Profiler
      id="app"
      onRender={(_id, _phase, duration) => {
        counts.commits += 1
        counts.ms += duration
      }}
    >
      {children}
    </Profiler>
  )
}

// wait is how long a step may take: rendering hundreds of rows is slow in jsdom, more so
// beside the other test files.
const wait = { timeout: 15_000 }

describe('App rendering', () => {
  // The steps find things by label and text rather than by role: working out the
  // accessibility tree of hundreds of rows takes longer than everything measured.
  it('renders a large inbox again only where it changed', { timeout: 60_000 }, async () => {
    const answer = setUp()
    const rows = shown('reviewRequested') + shown('authored') + shown('assigned')

    const mounted = await measure(async () => {
      await renderApp({ now: () => new Date(), wrapper: Profiled })
      await screen.findByText('Item 1', {}, wait)
    })

    expect(mountedRows()).toBe(rows)

    // The minute tick renders the times it changes, and no row.
    const tick = await measure(() => {
      act(() => {
        vi.advanceTimersByTime(minute)
      })
    })

    expect(screen.getByText('6m ago')).toBeInTheDocument()
    expect(tick.rows).toBe(0)

    // A background update that changes one item.
    answer(inbox((i) => (i.id === 50 ? { ...i, title: 'Item 50, renamed' } : i)))
    const update = await measure(async () => {
      act(() => {
        emit('inbox:updated')
      })
      await screen.findByText('Item 50, renamed', {}, wait)
    })

    expect(update.mounts).toBe(0)

    // Collapsing a list takes its rows out of the document.
    const collapse = await measure(async () => {
      await userEvent.click(screen.getByLabelText('Collapse Authored'))
    })

    expect(mountedRows()).toBe(rows - shown('authored'))
    expect(collapse.mounts).toBe(0)

    // Leaving the inbox keeps it, hidden, so coming back mounts nothing.
    const views = within(screen.getByLabelText('Views'))
    const away = await measure(async () => {
      await userEvent.click(views.getByLabelText('Worktrees'))
      await screen.findByText(/^Worktrees of the clones under/, {}, wait)
    })
    const back = await measure(async () => {
      await userEvent.click(views.getByLabelText('Inbox'))
    })

    expect(mountedRows()).toBe(rows - shown('authored'))
    expect(back.mounts).toBe(0)

    // Through the compiler, only the changed row renders again, and nothing else does. As
    // written, every render of a list renders its rows again, a couple of times a step.
    const most = compiled
      ? { update: 1, collapse: 0, away: 0, back: 0 }
      : { update: 2 * rows, collapse: rows, away: 2 * rows, back: rows }

    expect(update.rows).toBeLessThanOrEqual(most.update)
    expect(collapse.rows).toBeLessThanOrEqual(most.collapse)
    expect(away.rows).toBeLessThanOrEqual(most.away)
    expect(back.rows).toBeLessThanOrEqual(most.back)

    if (compiled) {
      const steps = Object.entries({ mounted, tick, update, collapse, away, back }).map(
        ([step, m]) => `  ${step}: ${String(m.rows)} / ${String(m.mounts)} / ${String(m.commits)} / ${String(m.ms)}`,
      )

      console.warn(['render profile (rows rendered / rows mounted / commits / ms rendering):', ...steps].join('\n'))
    }
  })
})
