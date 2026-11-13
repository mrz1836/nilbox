import { createContext, useContext } from 'react'

import { issueAvatars, type Issue } from '../platform'
import { useQuery } from './useQuery'

// Pictures maps a login to its profile picture, as a data: URL Go downloaded and kept, so
// the window loads nothing remote itself.
export type Pictures = Readonly<Partial<Record<string, string>>>

// none is no pictures, one object, so a sheet without any keeps the same value.
const none: Pictures = {}

// PicturesContext holds the pictures of the people the sheet shows, which PersonAvatar
// reads beside each name.
export const PicturesContext = createContext<Pictures>(none)

// pictureLogins are the people the issue sheet shows a picture beside: who opened it, its
// assignees and participants, and who wrote each comment shown in full, but a bot, whose
// login GitHub has no picture for, sorted and each once, so the list stays the same while
// they do.
export function pictureLogins(issue: Issue): string[] {
  const logins = new Set<string>()

  if (issue.author !== '' && !issue.isBot) {
    logins.add(issue.author)
  }

  for (const p of [...issue.assignees, ...issue.participants]) {
    logins.add(p.login)
  }

  for (const it of issue.timeline.items) {
    if (it.kind === 'comment' && it.actor !== '' && !it.actorIsBot && !it.minimized) {
      logins.add(it.actor)
    }
  }

  return [...logins].sort()
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
