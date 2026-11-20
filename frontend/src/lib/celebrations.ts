import { createContext, use } from 'react'

import type { Celebrations } from '../platform'

// Celebrations: how the window celebrates, as Settings → Appearance stores it
// (internal/settings' ui.celebrations).
//
//   - milestones: the inbox's party throws confetti on its own for an achievement, at most
//     once a day, and plays a small flourish otherwise; the setup's end and a release on
//     GitHub each throw a burst.
//   - quiet: the party keeps its flourishes and achievements, and throws confetti only when
//     the user runs it again; nothing else throws any.
//   - off: the inbox at nil is a still strip, and nothing throws confetti.

// defaultCelebrations is a fresh install's, the backend's default.
export const defaultCelebrations: Celebrations = 'milestones'

// CelebrationsContext gives the window the setting. App provides it from the settings, and a
// fresh install's until they load; without a provider, it is a fresh install's.
export const CelebrationsContext = createContext<Celebrations>(defaultCelebrations)

// useCelebrations is how the window celebrates.
export function useCelebrations(): Celebrations {
  return use(CelebrationsContext)
}

// throwsBursts reports whether the one-off bursts, the setup's end and a release on GitHub,
// throw confetti.
export function throwsBursts(mode: Celebrations): boolean {
  return mode === 'milestones'
}
