import { createContext } from 'react'

import type { CardDensity } from '../platform'

// Card preferences: how the inbox's cards look, as Settings → Cards stores them
// (internal/settings' cards.* keys). Both lists' cards follow them.

// CardPrefs are the cards' settings, each a primitive, so App builds the value from them
// alone and a save of the settings that leaves them as they were makes no new value, and
// renders no card.
export interface CardPrefs {
  // avatars is Author pictures: each author's picture on their card, or initials and the
  // bot mark alone.
  avatars: boolean
  // density is Density: each card on two lines (comfortable), or on one (compact).
  density: CardDensity
  // unread is Unread dots: a dot on a card with activity the user hasn't seen, and Mark
  // as read and Mark as unread in its menu and its peek. Off, the window still marks what
  // the user sees; it only shows none of it.
  unread: boolean
}

// defaultCardPrefs are a fresh install's, the backend's defaults.
export const defaultCardPrefs: CardPrefs = { avatars: true, density: 'comfortable', unread: true }

// CardPrefsContext gives the cards their settings. The window provides it from the
// settings, and a fresh install's until they load; without a provider, the cards have a
// fresh install's.
export const CardPrefsContext = createContext<CardPrefs>(defaultCardPrefs)
