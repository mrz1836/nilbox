// What closing the app does to the runs going in it, as the dialogs that ask first say
// it: quitting (QuitDialog) and restarting to update (UpdateRestartDialog) stop them alike,
// but for the agents' runs that keep to a budget of their own, which the user can let finish
// on a quit, and which carry on through a restart.

import { brand } from '../generated/brand'

// carryOnText says what letting the agents finish does: they carry on, and the app picks
// them up when it opens again. n is how many there are.
export function carryOnText(n: number): string {
  return n === 1
    ? `It carries on, and ${brand.name} picks it up when it opens again.`
    : `They carry on, and ${brand.name} picks them up when it opens again.`
}

// tagRisk is what stopping a release partway can leave behind, said of the one release
// named before, and releaseTagRisk the same among several runs.
export const tagRisk = 'Stopping it now can leave a tag on this Mac but not on GitHub.'
export const releaseTagRisk = 'Stopping a release now can leave a tag on this Mac but not on GitHub.'

// heldReleaseText says what closing the app does to releases held back from stopping for
// now: it waits a few seconds for them, and one still held then goes on after the app
// closes, and is checked at the next start. one is a single release, named before.
export function heldReleaseText(one: boolean): string {
  return one
    ? `magex is switching branches or pulling, so stopping waits up to a few seconds for it to finish. If it doesn’t, the release goes on after ${brand.name} closes, and ${brand.name} checks how it ended when it opens again.`
    : `A release is switching branches or pulling, so stopping waits up to a few seconds for it. If it doesn’t finish, that release goes on after ${brand.name} closes, and ${brand.name} checks how it ended when it opens again.`
}

// heldRunText says the same of any other run held back, as a pull is while git writes the
// clone's files; doing names what closes the app, quitting (the default) or restarting.
export function heldRunText(one: boolean, doing: 'Quitting' | 'Restarting' = 'Quitting'): string {
  return one
    ? `${doing} waits up to a few seconds for it. If it hasn’t ended by then, it goes on after ${brand.name} closes.`
    : `A run that can’t be stopped right now is waited for up to a few seconds. If it doesn’t finish, it goes on after ${brand.name} closes.`
}
