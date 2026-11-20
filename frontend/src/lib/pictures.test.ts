import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { InboxItem, PictureBot } from '../platform'
import { issue, issueComment, issueEvent, issueTimeline, item, review } from '../test/fixtures'
import { platform } from '../test/platform'
import {
  cardAuthors,
  cardBatches,
  cardPictureKey,
  pictureLogins,
  reviewPictureLogins,
  useCardPictures,
  type Pictures,
} from './pictures'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('pictureLogins', () => {
  it('names who opened it, its people, and who wrote each comment, each once, sorted', () => {
    expect(pictureLogins(issue())).toEqual(['octo-other', 'octo-user'])

    expect(
      pictureLogins(
        issue({
          author: 'zed-user',
          assignees: [{ login: 'amy-user', mine: false }],
          participants: [],
          timeline: issueTimeline({
            items: [
              issueComment({ id: 'IC_1', actor: 'kim-user' }),
              issueComment({ id: 'IC_2', actor: 'kim-user' }),
              issueComment({ id: 'IC_3', actor: 'dependabot', actorIsBot: true }),
              issueComment({ id: 'IC_4', actor: 'hidden-user', minimized: true }),
              issueComment({ id: 'IC_5', actor: '' }),
              issueEvent({ id: 'LE_1', actor: 'event-user' }),
            ],
          }),
        }),
      ),
      'a bot, a hidden comment, a deleted account, and an event show no picture',
    ).toEqual(['amy-user', 'kim-user', 'zed-user'])
  })

  it('leaves out a bot that opened it', () => {
    expect(pictureLogins(issue({ author: 'dependabot', isBot: true, assignees: [], participants: [] }))).toEqual([
      'octo-other',
      'octo-user',
    ])
  })
})

describe('reviewPictureLogins', () => {
  it('names who the pull request is assigned to, beside who opened it', () => {
    expect(
      reviewPictureLogins(
        review({
          assignees: [
            { login: 'zed-user', mine: false },
            { login: 'octo-other', mine: false },
          ],
        }),
      ),
      'each once, sorted',
    ).toEqual(['octo-other', 'zed-user'])
  })
})

// botURL is a bot's picture on the avatar host, as GitHub gives it.
function botURL(app: number): string {
  return `https://avatars.githubusercontent.com/in/${String(app)}?v=4`
}

// person is an item opened by login, and bot one opened by a bot with its picture's URL.
function person(login: string): InboxItem {
  return item({ author: login })
}

function bot(login: string, url = botURL(1)): InboxItem {
  return item({ author: login, authorIsBot: true, authorAvatarUrl: url })
}

// picture is a stand-in data: URL for what.
function picture(what: string): string {
  return `data:image/png;base64,${btoa(what)}`
}

// Request is a request for the cards' pictures the test answers when it says.
interface Request {
  people: string[]
  bots: PictureBot[]
  answer: (got: Pictures) => void
  fail: (err: unknown) => void
  cancel: ReturnType<typeof vi.fn>
}

// held makes every request for the cards' pictures wait for the test, and lists them.
function held(): Request[] {
  const requests: Request[] = []

  platform.cardPictures.mockImplementation((people, bots) => {
    const cancel = vi.fn()
    let answer: (got: Pictures) => void = () => undefined
    let fail: (err: unknown) => void = () => undefined
    const promise = new Promise<Pictures>((resolve, reject) => {
      answer = resolve
      fail = reject
    })

    requests.push({ people: people ?? [], bots: bots ?? [], answer, fail, cancel })

    return Object.assign(promise, { cancel })
  })

  return requests
}

// many are count people, login-00 and on, each the author of an item.
function many(count: number, login = 'person'): InboxItem[] {
  return Array.from({ length: count }, (_, i) => person(`${login}-${String(i).padStart(2, '0')}`))
}

describe('cardAuthors', () => {
  it('names each person and each bot once, sorted, leaving out a bot with no URL and a deleted account', () => {
    expect(
      cardAuthors([
        person('zed-user'),
        bot('renovate', botURL(2)),
        person('amy-user'),
        person('zed-user'),
        bot('dependabot'),
        bot('github-actions', ''),
        person(''),
        bot('', botURL(3)),
      ]),
    ).toEqual({
      people: ['amy-user', 'zed-user'],
      bots: [
        { login: 'dependabot', url: botURL(1) },
        { login: 'renovate', url: botURL(2) },
      ],
    })
  })
})

describe('cardBatches', () => {
  it('asks for at most 25 authors a request, the people first', () => {
    const authors = cardAuthors([...many(58), bot('dependabot'), bot('renovate', botURL(2))])
    const batches = cardBatches(authors)

    expect(batches.map((b) => [b.people.length, b.bots.length])).toEqual([
      [25, 0],
      [25, 0],
      [8, 2],
    ])
    expect(batches.flatMap((b) => b.people)).toEqual(authors.people)
    expect(batches.flatMap((b) => b.bots)).toEqual(authors.bots)
    expect(cardBatches({ people: [], bots: [] })).toEqual([])
  })
})

describe('cardPictureKey', () => {
  it('keys a person by login, and a bot by its login with [bot] after it', () => {
    expect(cardPictureKey('dependabot', false)).toBe('dependabot')
    expect(cardPictureKey('dependabot', true)).toBe('dependabot[bot]')
  })
})

describe('useCardPictures', () => {
  it('asks for 60 authors in three requests at once, and shows each one’s pictures as it lands', async () => {
    const requests = held()
    const items = [...many(58), bot('dependabot'), bot('renovate', botURL(2))]
    const { result } = renderHook(() => useCardPictures(items, true))

    expect(requests.map((r) => r.people.length + r.bots.length)).toEqual([25, 25, 10])
    expect(result.current).toEqual({})

    // The last request lands first: a slow one holds back only its own pictures.
    await act(async () => {
      requests[2]?.answer({ 'person-57': picture('57'), 'dependabot[bot]': picture('dependabot') })
      await Promise.resolve()
    })
    expect(result.current).toEqual({ 'person-57': picture('57'), 'dependabot[bot]': picture('dependabot') })

    await act(async () => {
      requests[0]?.answer({ 'person-00': picture('00') })
      await Promise.resolve()
    })
    expect(result.current).toEqual({
      'person-00': picture('00'),
      'person-57': picture('57'),
      'dependabot[bot]': picture('dependabot'),
    })
    expect(platform.cardPictures).toHaveBeenCalledTimes(3)
  })

  it('renders nothing new for an answer that brings nothing new', async () => {
    platform.cardPictures.mockResolvedValue({ 'octo-user': picture('octo-user') })
    let calls = 0
    const { result, rerender } = renderHook(
      ({ items }) => {
        calls += 1

        return useCardPictures(items, true)
      },
      { initialProps: { items: [person('octo-user')] } },
    )

    await waitFor(() => {
      expect(result.current).toEqual({ 'octo-user': picture('octo-user') })
    })
    const shown = result.current

    // Another author asks again, and only the same picture comes back.
    rerender({ items: [person('octo-user'), bot('dependabot')] })
    const rendered = calls
    // A timer's turn comes after every answer already given.
    await act(async () => {
      await new Promise((resolve) => {
        setTimeout(resolve, 0)
      })
    })

    expect(platform.cardPictures).toHaveBeenLastCalledWith(['octo-user'], [{ login: 'dependabot', url: botURL(1) }])
    expect(result.current).toBe(shown)
    expect(calls, 'no render for the answer').toBe(rendered)
  })

  it('asks again for different authors, cancelling the requests under way and never showing their answers', async () => {
    const requests = held()
    const { result, rerender } = renderHook(({ items }) => useCardPictures(items, true), {
      initialProps: { items: [person('amy-user')] },
    })

    await act(async () => {
      requests[0]?.answer({ 'amy-user': picture('amy') })
      await Promise.resolve()
    })

    // The same authors, in another order or another item, ask nothing more.
    rerender({ items: [person('amy-user'), person('amy-user')] })
    expect(requests).toHaveLength(1)

    rerender({ items: [person('amy-user'), person('zed-user')] })
    expect(requests).toHaveLength(2)
    expect(requests[1]?.people).toEqual(['amy-user', 'zed-user'])

    rerender({ items: [person('kim-user')] })
    expect(requests[1]?.cancel).toHaveBeenCalledOnce()
    await act(async () => {
      requests[1]?.answer({ 'zed-user': picture('zed') })
      requests[1]?.fail(new Error('cancelled'))
      await Promise.resolve()
    })

    // Until the new authors’ pictures come, those shown stay.
    expect(result.current).toEqual({ 'amy-user': picture('amy') })

    await act(async () => {
      requests[2]?.answer({ 'kim-user': picture('kim') })
      await Promise.resolve()
    })
    expect(result.current).toEqual({ 'amy-user': picture('amy'), 'kim-user': picture('kim') })
  })

  it('asks for nothing while off, and shows no pictures', async () => {
    platform.cardPictures.mockResolvedValue({ 'octo-user': picture('octo-user') })
    const { result, rerender } = renderHook(({ on }) => useCardPictures([person('octo-user')], on), {
      initialProps: { on: false },
    })

    await act(async () => {
      await Promise.resolve()
    })
    expect(platform.cardPictures).not.toHaveBeenCalled()
    expect(result.current).toEqual({})

    rerender({ on: true })
    await waitFor(() => {
      expect(result.current).toEqual({ 'octo-user': picture('octo-user') })
    })

    rerender({ on: false })
    expect(result.current).toEqual({})
    expect(platform.cardPictures).toHaveBeenCalledOnce()
  })

  it('asks for nothing without an author', () => {
    renderHook(() => useCardPictures([person(''), bot('dependabot', '')], true))

    expect(platform.cardPictures).not.toHaveBeenCalled()
  })

  it('logs a request that fails, leaving the others’ pictures, but not one cancelled as the cards go', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const requests = held()
    const { result, unmount } = renderHook(() => useCardPictures(many(60), true))
    const err = new Error('the database is locked')

    await act(async () => {
      requests[0]?.fail(err)
      requests[1]?.answer({ 'person-49': picture('49') })
      await Promise.resolve()
    })

    expect(result.current).toEqual({ 'person-49': picture('49') })
    expect(logged).toHaveBeenCalledExactlyOnceWith('Read the cards’ pictures', err)

    // A cancelled bound call rejects.
    unmount()
    expect(requests[2]?.cancel).toHaveBeenCalledOnce()
    await act(async () => {
      requests[2]?.fail(new Error('cancelled'))
      await Promise.resolve()
    })
    expect(logged).toHaveBeenCalledOnce()
  })
})
