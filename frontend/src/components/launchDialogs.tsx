import { lazy, useState, type ComponentProps } from 'react'

import type { TakeOverDialog as TakeOverDialogCode } from './autopilot/TakeOverDialog'
import type { LaunchDialog as LaunchDialogCode } from './LaunchDialog'
import type { LaunchHoldDialog as LaunchHoldDialogCode } from './LaunchHoldDialog'
import type { LaunchMenu as LaunchMenuCode } from './LaunchMenu'

// The dialogs a launch opens load on their own: the launch options (LaunchDialog, with the
// LaunchMenu it holds and the preview the menu shows), and the dialogs a held launch and a
// take-over ask with, which the sessions panel's Resume and Take over (SessionPanel) open
// too. No card needs them until it is clicked, so the inbox's first paint doesn't wait for
// them. Each is defined here once, for every control that opens it, so code one loaded is
// there for all. The first launch control to paint starts loading them all
// (preloadDialogs): the sessions panel opens from a card or a sheet, each with a launch
// control of its own, so a first opening rarely waits for its code.
//
// A dialog whose code is in is drawn as its own component, never through React.lazy:
// a lazy component's first render suspends even when its code has come, so its boundary
// would show its empty fallback for a moment (React holds a fallback for some 300 ms), and
// one already showing, such as the launch options' as the menu replaces Loading, would
// hide the open dialog and give its focus back. Each loader keeps the component it
// brought in (loadedLaunchDialog and the rest), and each dialog below chooses, once as it
// mounts, the component itself when it is in, else the lazy one, which waits for it under
// the caller's boundary. A mounted dialog never changes component, so nothing it holds is
// lost when its code comes.
let loadedLaunchDialog: typeof LaunchDialogCode | null = null
let loadedLaunchMenu: typeof LaunchMenuCode | null = null
let loadedLaunchHoldDialog: typeof LaunchHoldDialogCode | null = null
let loadedTakeOverDialog: typeof TakeOverDialogCode | null = null

const loadLaunchMenu = () =>
  import('./LaunchMenu').then((m) => {
    loadedLaunchMenu = m.LaunchMenu

    return m.LaunchMenu
  })

// LaunchDialog's load waits for its menu's code too, so the dialog never shows before the
// menu is in: the dialog opens on Loading while the launch settings load, and the menu
// takes its place once they come, drawn at once as its code is in.
const loadLaunchDialog = () =>
  Promise.all([import('./LaunchDialog'), loadLaunchMenu()]).then(([m]) => {
    loadedLaunchDialog = m.LaunchDialog

    return m.LaunchDialog
  })

const loadLaunchHoldDialog = () =>
  import('./LaunchHoldDialog').then((m) => {
    loadedLaunchHoldDialog = m.LaunchHoldDialog

    return m.LaunchHoldDialog
  })

const loadTakeOverDialog = () =>
  import('./autopilot/TakeOverDialog').then((m) => {
    loadedTakeOverDialog = m.TakeOverDialog

    return m.TakeOverDialog
  })

const LazyLaunchDialog = lazy(() => loadLaunchDialog().then((c) => ({ default: c })))
const LazyLaunchMenu = lazy(() => loadLaunchMenu().then((c) => ({ default: c })))
const LazyLaunchHoldDialog = lazy(() => loadLaunchHoldDialog().then((c) => ({ default: c })))
const LazyTakeOverDialog = lazy(() => loadTakeOverDialog().then((c) => ({ default: c })))

export function LaunchDialog(props: ComponentProps<typeof LaunchDialogCode>) {
  const [Shown] = useState(() => loadedLaunchDialog ?? LazyLaunchDialog)

  return <Shown {...props} />
}

export function LaunchMenu(props: ComponentProps<typeof LaunchMenuCode>) {
  const [Shown] = useState(() => loadedLaunchMenu ?? LazyLaunchMenu)

  return <Shown {...props} />
}

export function LaunchHoldDialog(props: ComponentProps<typeof LaunchHoldDialogCode>) {
  const [Shown] = useState(() => loadedLaunchHoldDialog ?? LazyLaunchHoldDialog)

  return <Shown {...props} />
}

export function TakeOverDialog(props: ComponentProps<typeof TakeOverDialogCode>) {
  const [Shown] = useState(() => loadedTakeOverDialog ?? LazyTakeOverDialog)

  return <Shown {...props} />
}

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
