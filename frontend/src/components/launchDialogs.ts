import { lazy } from 'react'

// The dialogs a launch opens load on their own: the launch options (LaunchDialog, with the
// LaunchMenu it holds and the preview the menu shows), and the dialogs a held launch and a
// take-over ask with, which the sessions panel's Resume and Take over (SessionPanel) open
// too. No card needs them until it is clicked, so the inbox's first paint doesn't wait for
// them. Each is defined here once, for every control that opens it, so code one loaded is
// there for all. The first launch control to paint starts loading them all
// (preloadDialogs): the sessions panel opens from a card or a sheet, each with a launch
// control of its own, so a first opening rarely waits for its code.
const loadLaunchDialog = () => import('./LaunchDialog')
const loadLaunchMenu = () => import('./LaunchMenu')
const loadLaunchHoldDialog = () => import('./LaunchHoldDialog')
const loadTakeOverDialog = () => import('./autopilot/TakeOverDialog')

export const LaunchDialog = lazy(() => loadLaunchDialog().then((m) => ({ default: m.LaunchDialog })))
export const LaunchMenu = lazy(() => loadLaunchMenu().then((m) => ({ default: m.LaunchMenu })))
export const LaunchHoldDialog = lazy(() => loadLaunchHoldDialog().then((m) => ({ default: m.LaunchHoldDialog })))
export const TakeOverDialog = lazy(() => loadTakeOverDialog().then((m) => ({ default: m.TakeOverDialog })))

// preloading records that the dialogs' code was asked for, so the many launch controls of a
// list ask once between them.
let preloading = false

// preloadDialogs starts loading the dialogs' code, once. A load that fails is left to the
// dialog's own load on its first opening, which tries again.
export function preloadDialogs() {
  if (preloading) {
    return
  }

  preloading = true

  for (const load of [loadLaunchDialog, loadLaunchMenu, loadLaunchHoldDialog, loadTakeOverDialog]) {
    load().catch(() => undefined)
  }
}
