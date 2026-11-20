import type { Settings } from '../platform'
import { counted, joinList } from './format'

// The words for where the app looks for clones: the clone root, the primary, and the
// other repository locations the user added in Settings → Repositories.

// whereClones names the locations, the clone root first, as a page says where it looked:
// "under ~/projects" for the clone root alone, or "in ~/projects and 2 other locations".
export function whereClones(roots: readonly string[]): string {
  const [primary = '', ...others] = roots

  return others.length === 0
    ? `under ${primary}`
    : `in ${primary} and ${counted(others.length, 'other location', 'other locations')}`
}

// cloneWhere names the locations the settings hold (see whereClones).
export function cloneWhere(settings: Pick<Settings, 'cloneRoot' | 'cloneLocations'>): string {
  return whereClones([settings.cloneRoot, ...settings.cloneLocations.map((location) => location.path)])
}

// unreadWords says which other locations a listing couldn't read, as on a drive that is
// unplugged, or '' when it read them all.
export function unreadWords(unread: readonly string[]): string {
  if (unread.length === 0) {
    return ''
  }

  return `Couldn’t read ${joinList(unread)}, so ${unread.length === 1 ? 'its' : 'their'} clones aren’t listed. Check ${unread.length === 1 ? 'it' : 'them'} in Settings → Repositories.`
}
