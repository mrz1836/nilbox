import type { GitHubStatus, GitHubStatusState } from '../platform'

// Tone is how loudly the window shows GitHub's status: a warning for a minor incident or
// maintenance, danger for an outage.
export type Tone = 'warning' | 'danger'

// troubled reports whether GitHub's status page says something is wrong, which the
// toolbar shows. With no reading, the check off, or every system operational, it shows
// nothing.
export function troubled(status: GitHubStatus | null): status is GitHubStatus {
  return status !== null && toneOf(status.state) !== null
}

// toneOf is how loudly to show state, or null for one the toolbar doesn't show.
export function toneOf(state: GitHubStatusState): Tone | null {
  switch (state) {
    case 'minor':
    case 'maintenance':
      return 'warning'
    case 'major':
    case 'critical':
      return 'danger'
    default:
      return null
  }
}

// toneText and toneFill color a tone's text and its dot.
export const toneText: Readonly<Record<Tone, string>> = { warning: 'text-warning', danger: 'text-danger' }
export const toneFill: Readonly<Record<Tone, string>> = { warning: 'bg-warning', danger: 'bg-danger' }

// stateWords are each state's own words, for a reading whose page gave none.
const stateWords: Readonly<Record<GitHubStatusState, string>> = {
  off: 'Not checked',
  unknown: 'Not checked yet',
  operational: 'All systems operational',
  minor: 'Minor service outage',
  major: 'Partial system outage',
  critical: 'Major system outage',
  maintenance: 'Under maintenance',
}

// headline is what GitHub's status page says, as a sentence's start: "Partial system
// outage", in the page's own words when it gave some.
export function headline(status: GitHubStatus): string {
  const said = status.description.trim()

  return said === '' ? stateWords[status.state] : sentenceCase(said)
}

// briefWords are each state's shortest words, for a hint beside a menu item.
const briefWords: Readonly<Record<GitHubStatusState, string>> = {
  off: '',
  unknown: '',
  operational: '',
  minor: 'Minor issue',
  major: 'Partial outage',
  critical: 'Major outage',
  maintenance: 'Maintenance',
}

// brief is a state in a word or two, "Partial outage", or '' for one the toolbar doesn't
// show.
export function brief(state: GitHubStatusState): string {
  return briefWords[state]
}

// statusLabel names the toolbar's button: "GitHub status: Partial system outage".
export function statusLabel(status: GitHubStatus): string {
  return `GitHub status: ${headline(status)}`
}

// phrase turns one of the page's snake_case words, such as partial_outage, into words:
// "Partial outage".
export function phrase(word: string): string {
  const words = word.replaceAll('_', ' ').trim()

  return words === '' ? '' : sentenceCase(words)
}

// sentenceCase keeps a phrase's first letter capital and lowers the rest, so the page's
// Title Case reads as the window's: "Partial System Outage" becomes "Partial system outage".
function sentenceCase(text: string): string {
  const lower = text.toLowerCase()

  return lower.charAt(0).toUpperCase() + lower.slice(1)
}
