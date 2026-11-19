import { createContext } from 'react'

import type { CardDensity } from '../platform'

// Card preferences: how the inbox's cards look, as Settings → Cards stores them
// (internal/settings' cards.* keys), and when they offer a nudge and how one starts, as
// Settings → Reviewing does (review.nudgeAfterHours, review.nudgeSnooze). Both lists'
// cards follow them.

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
  // nudgeAfterHours is Offer a nudge after: how many hours a pull request waiting on its
  // author has to be quiet before its card's primary becomes Nudge, 0 for never.
  nudgeAfterHours: number
  // nudgeSnooze is Snooze after a nudge: whether the Nudge dialog starts with its Then
  // snooze box ticked.
  nudgeSnooze: boolean
}

// defaultCardPrefs are a fresh install's, the backend's defaults.
export const defaultCardPrefs: CardPrefs = {
  avatars: true,
  density: 'comfortable',
  unread: true,
  nudgeAfterHours: 24,
  nudgeSnooze: false,
}

// CardPrefsContext gives the cards their settings. The window provides it from the
// settings, and a fresh install's until they load; without a provider, the cards have a
// fresh install's.
export const CardPrefsContext = createContext<CardPrefs>(defaultCardPrefs)
