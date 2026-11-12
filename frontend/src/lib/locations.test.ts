import { describe, expect, it } from 'vitest'

import { settings } from '../test/fixtures'
import { cloneWhere, unreadWords, whereClones } from './locations'

describe('whereClones', () => {
  it('names the clone root alone as it always did, and counts the other locations', () => {
    expect(whereClones(['~/projects'])).toBe('under ~/projects')
    expect(whereClones(['~/projects', '~/work'])).toBe('in ~/projects and 1 other location')
    expect(whereClones(['~/projects', '~/work', '~/Desktop/exp'])).toBe('in ~/projects and 2 other locations')
  })

  it('names the locations the settings hold', () => {
    expect(cloneWhere(settings())).toBe('under ~/projects')
    expect(
      cloneWhere(
        settings({
          cloneRoot: '~/code',
          cloneLocations: [
            { path: '~/work', kind: 'folder' },
            { path: '~/Desktop/exp', kind: 'clone' },
          ],
        }),
      ),
    ).toBe('in ~/code and 2 other locations')
  })
})

describe('unreadWords', () => {
  it('says which locations a listing couldn’t read', () => {
    expect(unreadWords([])).toBe('')
    expect(unreadWords(['/Volumes/ext/src'])).toBe(
      'Couldn’t read /Volumes/ext/src, so its clones aren’t listed. Check it in Settings → Repositories.',
    )
    expect(unreadWords(['~/a', '~/b', '~/c'])).toBe(
      'Couldn’t read ~/a, ~/b and ~/c, so their clones aren’t listed. Check them in Settings → Repositories.',
    )
  })
})
