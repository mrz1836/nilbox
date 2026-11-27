import { createContext, useContext, useEffect, useEffectEvent, useState } from 'react'

import {
  cardPictures,
  issueAvatars,
  type InboxItem,
  type Issue,
  type PictureBot,
  type Review,
  type TimelineEntry,
} from '../platform'
import { cancel, start } from './call'
import { useQuery } from './useQuery'

// Pictures maps a login to its profile picture, as a data: URL Go downloaded and kept, so
// the window loads nothing remote itself.
export type Pictures = Readonly<Partial<Record<string, string>>>

// none is no pictures, one object, so a sheet without any keeps the same value.
const none: Pictures = {}

// PicturesContext holds the pictures of the people the sheet shows, which PersonAvatar
// reads beside each name.
export const PicturesContext = createContext<Pictures>(none)

// timelineLogins are the people a conversation shows a picture beside: who wrote each
// comment and each review shown in full, but a bot, whose login GitHub has no picture for.
function timelineLogins(entries: readonly TimelineEntry[]): string[] {
  return entries
    .filter((e) => (e.kind === 'comment' || e.kind === 'reviewed') && e.actor !== '' && !e.actorIsBot && !e.minimized)
    .map((e) => e.actor)
}

// sorted is logins sorted and each once, so the list stays the same while they do.
function sorted(logins: Iterable<string>): string[] {
  return [...new Set(logins)].sort()
}

// pictureLogins are the people the issue sheet shows a picture beside: who opened it, its
// assignees and participants, its sub-issues' assignees, and the people its conversation
// shows (timelineLogins), but a bot.
export function pictureLogins(issue: Issue): string[] {
  const author = issue.author !== '' && !issue.isBot ? [issue.author] : []

  return sorted([
    ...author,
    ...issue.assignees.map((p) => p.login),
    ...issue.participants.map((p) => p.login),
    ...issue.subIssues.flatMap((s) => s.assignees.map((p) => p.login)),
    ...timelineLogins(issue.timeline.items),
  ])
}

// reviewPictureLogins are the people the review sheet shows a picture beside: who opened the
// pull request, the people its conversation shows, its reviewers the conversation shows are
// people, but a bot, the people it asks to review, but a team, and who it is assigned to. A
// reviewer's review in no page read is left out: a cache an older build kept says nothing of
// who is a bot.
export function reviewPictureLogins(review: Review): string[] {
  const author = review.author !== '' && !review.isBot ? [review.author] : []
  const people = new Set(
    review.timeline.items.filter((e) => e.kind === 'reviewed' && e.actor !== '' && !e.actorIsBot).map((e) => e.actor),
  )

  return sorted([
    ...author,
    ...review.reviews.filter((r) => !r.team && (r.requested || people.has(r.author))).map((r) => r.author),
    ...review.assignees.map((a) => a.login),
    ...timelineLogins(review.timeline.items),
  ])
}

// usePictures reads the profile pictures of logins, once for each different list, and keeps
// the last ones while a new list is read. A read that fails leaves the letters showing, and
// is only logged.
export function usePictures(logins: readonly string[]): Pictures {
  const key = logins.join('\n')
  const read = useQuery(() => issueAvatars(key === '' ? [] : key.split('\n')), [key], {
    enabled: key !== '',
    log: 'Read the profile pictures',
  })

  return read.data ?? none
}

// usePicture is login's picture from the sheet's pictures, or '' while there is none.
export function usePicture(login: string): string {
  return useContext(PicturesContext)[login] ?? ''
}

// cardBatch is the most authors one request for the cards' pictures asks for, so the first
// pictures show while the rest still download.
const cardBatch = 25

// CardPicturesContext holds the pictures of the inbox's authors, keyed as cardPictureKey
// says, which only CardAvatar reads. Only the inbox provides it: the Watching list's cards
// show initials and the bot mark alone.
export const CardPicturesContext = createContext<Pictures>(none)

// cardPictureKey is the key of an author's picture among the cards' pictures: a person's
// login, or a bot's with "[bot]" after it, as the avatars service answers it.
export function cardPictureKey(author: string, isBot: boolean): string {
  return isBot ? `${author}[bot]` : author
}

// CardAuthors are authors whose pictures the cards ask for: people by login, and bots by
// login and the URL of their picture.
export interface CardAuthors {
  people: string[]
  bots: PictureBot[]
}

const noAuthors: CardAuthors = { people: [], bots: [] }

// cardAuthors are the authors of items, each once, sorted: people by login, and bots by
// login and the picture's URL the inbox stored with their item. A bot no read brought a URL
// for yet has no picture to ask for, and neither has a deleted account (no login).
export function cardAuthors(items: readonly InboxItem[]): CardAuthors {
  const people = new Set<string>()
  const bots = new Map<string, string>()

  for (const i of items) {
    if (i.author !== '' && !i.authorIsBot) {
      people.add(i.author)
    } else if (i.author !== '' && i.authorAvatarUrl !== '') {
      bots.set(i.author, i.authorAvatarUrl)
    }
  }

  return {
    people: [...people].sort(),
    bots: [...bots].sort(([a], [b]) => (a < b ? -1 : 1)).map(([login, url]) => ({ login, url })),
  }
}

// cardBatches splits authors into requests of at most cardBatch authors each, the people
// first.
export function cardBatches({ people, bots }: CardAuthors): CardAuthors[] {
  const batches: CardAuthors[] = []

  for (let at = 0; at < people.length + bots.length; at += cardBatch) {
    batches.push({
      people: people.slice(at, at + cardBatch),
      bots: bots.slice(Math.max(0, at - people.length), Math.max(0, at + cardBatch - people.length)),
    })
  }

  return batches
}

// authorsKey is authors as one string, which stays the same while they do: a person's line
// is their login, and a bot's its login and URL, a tab between. Neither holds a tab or a
// line break.
function authorsKey({ people, bots }: CardAuthors): string {
  return [...people, ...bots.map((b) => `${b.login}\t${b.url}`)].join('\n')
}

// merged is have with got's pictures added, or have itself when got adds or changes none,
// so an answer with nothing new renders nothing.
function merged(have: Pictures, got: Pictures): Pictures {
  const fresh = Object.entries(got).filter(([key, picture]) => have[key] !== picture)

  return fresh.length === 0 ? have : { ...have, ...Object.fromEntries(fresh) }
}

// useCardPictures reads the pictures of the authors of items, while on, in requests of at
// most cardBatch authors each, all asked at once, and shows each request's pictures as it
// lands, so a slow download holds back only its own request's. Different authors ask
// again, cancelling the requests under way, whose answers are never shown, and keep the
// pictures shown until new ones come. A request that fails leaves initials and the bot mark
// showing, and is only logged. Off, it asks for nothing and gives no pictures.
export function useCardPictures(items: readonly InboxItem[], on: boolean): Pictures {
  const authors = on ? cardAuthors(items) : noAuthors
  const key = authorsKey(authors)
  const [pictures, setPictures] = useState(none)
  const batches = useEffectEvent(() => cardBatches(authors))

  useEffect(() => {
    if (key === '') {
      return undefined
    }

    let live = true
    const calls = batches().map(({ people, bots }) => {
      const call = start(() => cardPictures(people, bots))

      call.then(
        (got) => {
          if (live) {
            setPictures((have) => merged(have, got))
          }
        },
        (err: unknown) => {
          if (live) {
            console.error('Read the cards’ pictures', err)
          }
        },
      )

      return call
    })

    return () => {
      live = false

      for (const call of calls) {
        cancel(call)
      }
    }
  }, [key])

  return on ? pictures : none
}
