import { createContext, useContext } from 'react'

import { issueAvatars, type Issue, type Review, type TimelineEntry } from '../platform'
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
export function timelineLogins(entries: readonly TimelineEntry[]): string[] {
  return entries
    .filter((e) => (e.kind === 'comment' || e.kind === 'reviewed') && e.actor !== '' && !e.actorIsBot && !e.minimized)
    .map((e) => e.actor)
}

// sorted is logins sorted and each once, so the list stays the same while they do.
function sorted(logins: Iterable<string>): string[] {
  return [...new Set(logins)].sort()
}

// pictureLogins are the people the issue sheet shows a picture beside: who opened it, its
// assignees and participants, and the people its conversation shows (timelineLogins), but a
// bot.
export function pictureLogins(issue: Issue): string[] {
  const author = issue.author !== '' && !issue.isBot ? [issue.author] : []

  return sorted([
    ...author,
    ...issue.assignees.map((p) => p.login),
    ...issue.participants.map((p) => p.login),
    ...timelineLogins(issue.timeline.items),
  ])
}

// reviewPictureLogins are the people the review sheet shows a picture beside: who opened the
// pull request, the people its conversation shows, and its reviewers the conversation shows
// are people, but a bot. A reviewer's review in no page read is left out: the list of
// reviewers says nothing of who is a bot.
export function reviewPictureLogins(review: Review): string[] {
  const author = review.author !== '' && !review.isBot ? [review.author] : []
  const people = new Set(
    review.timeline.items.filter((e) => e.kind === 'reviewed' && e.actor !== '' && !e.actorIsBot).map((e) => e.actor),
  )

  return sorted([
    ...author,
    ...review.reviews.map((r) => r.author).filter((login) => people.has(login)),
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
