import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Profiler, type ReactNode } from 'react'
import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ItemRowProps } from './components/ItemRow'
import { triageList } from './lib/triage'
import type { AutopilotMatch, AutopilotState, CardDensity, InboxItem, InboxSnapshot, ListKey } from './platform'
import { deferred, nextTask } from './test/async'
import { FIXED_NOW, fromNow } from './test/clock'
import {
  autopilotMatch,
  avatarDataURL,
  details,
  fileCheck,
  githubThreads,
  item,
  itemRef,
  list,
  notification,
  rate,
  settings,
  snapshot,
} from './test/fixtures'
import { emit, platform, renderApp } from './test/platform'

// The render profile: a 300-item inbox, and how much of it renders again for its authors'
// pictures landing, a minute tick, a background update that changes one item, a GitHub
// notifications update and an update of the app's own notifications, which the cards don't
// show, collapsing a list, leaving the inbox and coming back, and filtering it and clearing
// the filter; then, with Autopilot on, an Autopilot update that changes where a routine
// stands on one item. It runs twice, and each run in both of Settings → Cards' densities,
// comfortable and compact, with the same bounds. In the unit tests the components run as written,
// where the bounds below hold however much a parent renders again. `pnpm test:renders`
// (vitest.renders.config.ts) runs it through the React Compiler, as the build ships, where
// the tighter bounds hold, and prints what it measured.

// compiled reports a run through the React Compiler.
const compiled = import.meta.env.REACT_COMPILER === '1'

// counts are what the profile records: the rows rendered, the rows mounted, the commits,
// and the time React spent rendering.
const counts = vi.hoisted(() => ({ rows: 0, mounts: 0, commits: 0, ms: 0 }))

// ItemRow is wrapped, memo and all, so every render and every mount of a row is counted.
// The counted row calls the unwrapped row as a function, so the row's own hooks (a clock
// read through useNowAs, a context) belong to the counted component: a row that renders
// again from one of them is counted too, not only one its list renders again.
vi.mock('./components/ItemRow', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./components/ItemRow')>()
  const { memo, useState } = await import('react')
  const Row = (actual.ItemRow as unknown as { type: (props: ItemRowProps) => ReactNode }).type

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

    return Row(props)
  }

  return { ...actual, ItemRow: memo(CountedRow) }
})

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

// The window's clock reads a faked time, starting at the fixtures' FIXED_NOW, so the
// minute tick can be stepped. renderApp makes the clock as the window renders, once the
// timers are faked. Each run fakes them afresh, since the setup file puts the real timers
// back after every test, and so starts at FIXED_NOW, however far the run before it
// stepped the clock.
beforeEach(() => {
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

// inbox is 300 items, 100 review requests and 200 of the user's own, with change made to
// each.
function inbox(change: (item: InboxItem) => InboxItem = (i) => i): InboxSnapshot {
  const ids = (from: number, length: number) => Array.from({ length }, (_, i) => change(row(from + i)))

  return snapshot({
    reviewRequested: list(ids(1, 100)),
    yours: list(ids(101, 200)),
    rate: { graphql: rate(), core: rate(), search: rate() },
    lastRefreshedAt: ago(5),
  })
}

// shown counts the rows a list shows in its own tab.
function shown(key: ListKey): number {
  return triageList(inbox()[key], FIXED_NOW).inbox.length
}

// mountedRows counts the inbox rows in the document, showing or hidden.
function mountedRows(): number {
  return document.querySelectorAll('[data-item-id]').length
}

// settle lets every answer, and every render it causes, finish.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await nextTask()
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

// answerThreads answers the window's reads of the GitHub notifications with count of them,
// all unread, each read a fresh copy, as a bound call returns freshly decoded JSON.
function answerThreads(count: number) {
  platform.githubThreadsSnapshot.mockImplementation(() =>
    Promise.resolve(structuredClone(githubThreads({ status: { count, unread: count } }))),
  )
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

// densities are Settings → Cards' densities, each profiled with the same bounds.
const densities: readonly CardDensity[] = ['comfortable', 'compact']

// cards answers the window's reads of the settings with the cards in density, and
// whatever else change sets.
function cards(density: CardDensity, change: Parameters<typeof settings>[0] = {}) {
  platform.getSettings.mockResolvedValue(settings({ cardDensity: density, ...change }))
}

// cardsAre reports whether the inbox's cards show in density.
function cardsAre(density: CardDensity): boolean {
  const row = document.querySelector('[data-item-id]')

  return density === 'compact' ? row?.getAttribute('data-density') === 'compact' : !row?.hasAttribute('data-density')
}

describe.each(densities)('App rendering, %s cards', (density) => {
  // The steps find things by label and text rather than by role: working out the
  // accessibility tree of hundreds of rows takes longer than everything measured.
  it('renders a large inbox again only where it changed', { timeout: 60_000 }, async () => {
    const answer = setUp()
    cards(density)
    answerThreads(3)
    const rows = shown('reviewRequested') + shown('yours')
    // The authors' pictures wait until the test lands them.
    let land: (pictures: Record<string, string>) => void = () => undefined
    platform.cardPictures.mockImplementation(() => {
      const request = deferred<Record<string, string>>()
      land = request.resolve

      return request.promise
    })

    const mounted = await measure(async () => {
      await renderApp({ now: () => new Date(), wrapper: Profiled })
      await screen.findByText('Item 1', {}, wait)
    })

    expect(mountedRows()).toBe(rows)
    expect(cardsAre(density)).toBe(true)

    // The authors' pictures landing renders the avatars that show them, and no row. A
    // compact card shows no avatar, so none is asked for.
    await waitFor(() => {
      expect(platform.cardPictures).toHaveBeenCalledTimes(density === 'comfortable' ? 1 : 0)
    }, wait)
    const pictures = await measure(async () => {
      act(() => {
        land({ 'octo-user': avatarDataURL })
      })
      await waitFor(() => {
        expect(document.querySelector('[data-item-id] img')?.getAttribute('src') ?? '').toBe(
          density === 'comfortable' ? avatarDataURL : '',
        )
      }, wait)
    })

    expect(pictures.mounts).toBe(0)

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

    // A GitHub notifications update, which only the toolbar's button shows: no card has a
    // part in it.
    answerThreads(4)
    const threads = await measure(async () => {
      act(() => {
        emit('ghthreads:updated')
      })
      await waitFor(() => {
        expect(screen.getByTestId('github-threads-count')).toHaveTextContent('4')
      }, wait)
    })

    expect(threads.mounts).toBe(0)

    // An update of the app's own notifications, which only the bell and the Notifications
    // tab show: the inbox's rows have no part in it, though the inbox hears of it (the
    // sheet's opening marks an item's notifications read).
    platform.listNotifications.mockImplementation(() =>
      Promise.resolve({ items: [notification({ id: 1, itemId: 1 }), notification({ id: 2, itemId: 2 })], unread: 2 }),
    )
    const notifications = await measure(async () => {
      act(() => {
        emit('notifications:updated')
      })
      await waitFor(() => {
        expect(screen.getByTestId('notifications-unread')).toHaveTextContent('2')
      }, wait)
    })

    expect(notifications.mounts).toBe(0)

    // Collapsing a list takes its rows out of the document.
    const collapse = await measure(async () => {
      await userEvent.click(screen.getByLabelText('Collapse Yours'))
    })

    expect(mountedRows()).toBe(rows - shown('yours'))
    expect(collapse.mounts).toBe(0)

    // Leaving the inbox keeps it, hidden, so coming back mounts nothing.
    const views = within(screen.getByLabelText('Views'))
    const away = await measure(async () => {
      await userEvent.click(views.getByLabelText('Worktrees'))
      await screen.findByText(/^\d+ worktrees? in \d+ clones? · checked/, {}, wait)
    })
    const back = await measure(async () => {
      await userEvent.click(views.getByLabelText('Inbox'))
    })

    expect(mountedRows()).toBe(rows - shown('yours'))
    expect(back.mounts).toBe(0)

    // Filtering by a phrase only Review requested's items have renders their rows again,
    // to mark their titles, and mounts none: the lists it hides drop their rows, and the
    // collapsed Yours, open while filtering, has no match to show. It is pasted in one
    // event, as the field's own steps are measured in its tests.
    const unfiltered = mountedRows()
    const matches = inbox()
      .reviewRequested.items.concat(inbox().yours.items)
      .filter((i) => /item 5/i.test(i.title)).length
    const filter = await measure(async () => {
      await userEvent.click(screen.getByLabelText('Filter inbox'))
      await userEvent.paste('"Item 5"')
    })

    expect(document.querySelector('header [role="status"]')?.textContent).toBe(`${String(matches)} matches`)
    expect(filter.mounts).toBe(0)

    // Clearing it mounts back only the rows it hid, and the inbox is as it was.
    const hidden = unfiltered - mountedRows()
    const clear = await measure(async () => {
      await userEvent.click(screen.getByLabelText('Clear the search'))
    })

    expect(clear.mounts).toBeLessThanOrEqual(hidden)
    expect(mountedRows()).toBe(unfiltered)

    // Opening the card guide from the inbox's button mounts the guide's two sample cards
    // alone (the card it numbers, and the one that offers a nudge), and closing it leaves
    // the inbox as it was.
    const guide = await measure(async () => {
      await userEvent.click(screen.getByLabelText('How to read a card'))
      await screen.findByText('feat(client): accept a caller-supplied connection gater', {}, wait)
    })

    expect(guide.mounts).toBe(2)
    await userEvent.keyboard('{Escape}')
    expect(mountedRows()).toBe(unfiltered)

    // Through the compiler, only the changed row renders again, and nothing else does (the
    // pictures render only the avatars); filtering renders only the rows it marks, and the
    // card guide only its own two samples. As written, every render of a list renders its rows
    // again, a couple of times a step.
    const most = compiled
      ? {
          pictures: 0,
          update: 1,
          threads: 0,
          notifications: 0,
          collapse: 0,
          away: 0,
          back: 0,
          filter: matches,
          guide: 2,
        }
      : {
          pictures: 2 * rows,
          update: 2 * rows,
          threads: 2 * rows,
          notifications: 2 * rows,
          collapse: rows,
          away: 2 * rows,
          back: rows,
          filter: 2 * rows,
          guide: 2 * rows + 2,
        }

    expect(pictures.rows).toBeLessThanOrEqual(most.pictures)
    expect(update.rows).toBeLessThanOrEqual(most.update)
    expect(threads.rows).toBeLessThanOrEqual(most.threads)
    expect(notifications.rows).toBeLessThanOrEqual(most.notifications)
    expect(collapse.rows).toBeLessThanOrEqual(most.collapse)
    expect(away.rows).toBeLessThanOrEqual(most.away)
    expect(back.rows).toBeLessThanOrEqual(most.back)
    expect(filter.rows).toBeLessThanOrEqual(most.filter)
    expect(guide.rows).toBeLessThanOrEqual(most.guide)

    if (compiled) {
      const steps = Object.entries({
        mounted,
        pictures,
        tick,
        update,
        threads,
        notifications,
        collapse,
        away,
        back,
        filter,
        clear,
        guide,
      }).map(
        ([step, m]) => `  ${step}: ${String(m.rows)} / ${String(m.mounts)} / ${String(m.commits)} / ${String(m.ms)}`,
      )

      console.warn(
        [`render profile, ${density} cards (rows rendered / rows mounted / commits / ms rendering):`, ...steps].join(
          '\n',
        ),
      )
    }
  })

  // match is a routine's newest match on item id, in state.
  function match(id: number, state: AutopilotState): AutopilotMatch {
    return autopilotMatch({ id, item: itemRef({ id }), state, reason: '', steps: [], effects: [] })
  }

  it(
    'renders again, for an Autopilot update, only the cards whose routine’s standing changed',
    { timeout: 60_000 },
    async () => {
      setUp()
      answerThreads(0)
      cards(density, { autopilot: { ...settings().autopilot, enabled: true } })
      let matches = [match(1, 'waiting'), match(2, 'done')]
      platform.latestAutopilotMatches.mockImplementation(() => Promise.resolve(structuredClone(matches)))
      const rows = shown('reviewRequested') + shown('yours')
      // rule is the rule item id's card shows.
      const rule = (id: number) => document.querySelector(`[data-item-id="${String(id)}"]`)?.getAttribute('data-rule')

      await renderApp({ now: () => new Date(), wrapper: Profiled })
      await screen.findByText('Item 1', {}, wait)
      await waitFor(() => {
        expect(rule(1)).toBe('autopilot_needs_you')
      }, wait)
      expect(cardsAre(density)).toBe(true)

      // Item 2's chip changes, but not where its routine stands; item 3's routine starts work.
      matches = [match(1, 'waiting'), match(2, 'closed'), match(3, 'running')]
      const update = await measure(async () => {
        act(() => {
          emit('autopilot:updated')
        })
        await waitFor(() => {
          expect(rule(3)).toBe('agent_working')
        }, wait)
      })

      expect(rule(2)).toBe('waiting')
      expect(update.mounts).toBe(0)
      expect(update.rows).toBeLessThanOrEqual(compiled ? 1 : 2 * rows)

      if (compiled) {
        console.warn(
          `render profile, Autopilot update, ${density} cards (rows rendered / rows mounted / commits / ms rendering): ${String(update.rows)} / ${String(update.mounts)} / ${String(update.commits)} / ${String(update.ms)}`,
        )
      }
    },
  )
})
