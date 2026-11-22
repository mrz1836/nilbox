import { canMark, depsText, markTitle, moduleTitle, uncountedTitle, versionText } from '../../lib/manifests'
import { formatAgo, formatFull } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import {
  openManifestsTerminal,
  revealManifest,
  type ManifestKind,
  type ManifestsClone,
  type ManifestsModule,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, CircleCheckIcon, FolderIcon, TerminalIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { drawerClass, drawerFooterClass, toolButtonClass } from '../worktrees/layout'

// The drawer under a Deps row: a line for each of the clone's go.mod and package.json
// files, those that don't count toward its age marked so, its workspace files, and its
// actions in words. It shows what the page's read holds, and reads nothing more.

// moduleListClass is the drawer's list of modules, the grid of their seven columns that
// each line is a subgrid of: the kind, the manifest's path, its Go version or package
// manager, its dependencies, its last change's subject, that change's age, and Reveal in
// Finder. In a drawer under 42rem the subject moves to a second line, under the path.
const moduleListClass =
  'grid grid-cols-[auto_fit-content(20rem)_auto_auto_minmax(0,1fr)_auto_auto] items-center gap-x-3 px-3 py-1.5'

const moduleLineClass = 'col-span-full grid grid-cols-subgrid items-center gap-y-0.5 py-0.5'

// kindBadges mark each kind of module, in its own tint, spelled out in full so Tailwind
// finds them.
const kindBadges: Readonly<Record<ManifestKind, { className: string; title: string }>> = {
  go: { className: 'border-info/45 bg-info/10 text-info', title: 'A Go module' },
  js: { className: 'border-warning/45 bg-warning/10 text-warning', title: 'A JavaScript package' },
}

// upgradeButtonClass is the drawer's Upgrade dependencies…, its main action: tinted with
// the accent, and as tall as a Worktrees drawer's verbs.
const upgradeButtonClass =
  'ml-1 inline-flex h-6 items-center gap-1.25 rounded-md border border-accent/45 bg-accent/14 px-2.25 text-xs font-semibold whitespace-nowrap text-text transition-colors hover:border-accent/60 hover:bg-accent/22'

interface ModuleLineProps {
  module: ManifestsModule
  // opening reports Terminal or Finder being opened on the clone, and onReveal shows the
  // module's manifest in Finder.
  opening: boolean
  onReveal: () => void
}

// ModuleLine is one module on one line: its kind, its manifest's path (what it is in its
// tooltip: moduleTitle), "not counted" after it when it doesn't count toward the clone's
// age (why in its tooltip: uncountedTitle), its Go version or package manager, how many
// dependencies it names, its last change's subject and age, and Reveal in Finder; under
// them, in amber, why it couldn't be read in full, when it couldn't.
function ModuleLine({ module: m, opening, onReveal }: ModuleLineProps) {
  const badge = kindBadges[m.kind]

  return (
    <li className={moduleLineClass}>
      <span
        title={badge.title}
        className={`col-start-1 row-start-1 inline-flex cursor-help justify-center rounded-sm border px-1 font-mono text-marker leading-4 uppercase ${badge.className}`}
      >
        {m.kind}
      </span>
      <span className="col-start-2 row-start-1 flex min-w-0 items-center gap-1.5">
        <span title={moduleTitle(m)} className="min-w-0 truncate font-mono text-xs">
          {m.path}
        </span>
        {m.uncounted !== '' && (
          <span title={uncountedTitle(m)} className="shrink-0 cursor-help text-xs whitespace-nowrap text-muted">
            not counted
          </span>
        )}
      </span>
      <span className="col-start-3 row-start-1 text-xs whitespace-nowrap text-muted">{versionText(m)}</span>
      <span className="col-start-4 row-start-1 text-xs whitespace-nowrap text-muted tabular-nums">{depsText(m)}</span>
      <span
        title={m.subject}
        className="col-start-5 row-start-1 min-w-0 truncate text-xs text-muted @max-2xl:col-[2/-1] @max-2xl:row-start-2"
      >
        {m.subject}
      </span>
      {m.changedAt !== '' && (
        <span
          title={`Last changed ${formatFull(m.changedAt)}`}
          className="col-start-6 row-start-1 cursor-help text-right text-xs whitespace-nowrap text-muted tabular-nums"
        >
          <RelativeTime iso={m.changedAt} show={formatAgo} />
        </span>
      )}
      <span className="col-start-7 row-start-1 flex">
        <button
          type="button"
          title="Reveal in Finder"
          aria-label={`Reveal ${m.path} in Finder`}
          disabled={opening}
          onClick={onReveal}
          className={toolButtonClass}
        >
          <FolderIcon className="size-3.5" />
        </button>
      </span>
      {m.problem !== '' && (
        <p className="col-[2/-1] flex items-start gap-1.5 text-xs text-warning">
          <AlertIcon className="mt-0.5 size-3 shrink-0" />
          {m.problem}
        </p>
      )}
    </li>
  )
}

export interface DepsDrawerProps {
  // id is the drawer's, which its row's chevron controls.
  id: string
  clone: ManifestsClone
  // onUpgrade opens the clone's upgrade sheet.
  onUpgrade: () => void
  // onMark marks the clone's repository up to date (checked) or unmarks it, and marking
  // reports a mark under way; without onMark the drawer offers none.
  onMark?: ((checked: boolean) => void) | undefined
  marking?: boolean
}

// DepsDrawer is a row's drawer: a line for each module (ModuleLine), stalest first, as the
// page lists them, or why none was read; the clone's workspace files; why Terminal or
// Finder couldn't open, until the next try; and its actions in words: Open in Terminal,
// in the clone, Mark up to date (or Unmark up to date, while it is marked), and Upgrade
// dependencies…, which opens the clone's upgrade sheet. Each module reveals its own
// manifest in Finder, so the file Finder shows is the one its line names. None runs
// anything in the clone.
export function DepsDrawer({ id, clone, onUpgrade, onMark, marking = false }: DepsDrawerProps) {
  // opener opens the clone in the terminal (a path of null) or reveals a manifest in
  // Finder, one at a time.
  const opener = useMutation((path: string | null) =>
    path === null ? openManifestsTerminal(clone.dir) : revealManifest(clone.dir, path),
  )

  return (
    <li id={id} aria-label={`About ${clone.repo}`} className={drawerClass}>
      {clone.modules.length > 0 ? (
        <ul aria-label={`The modules of ${clone.repo}`} className={moduleListClass}>
          {clone.modules.map((m) => (
            <ModuleLine
              key={m.path}
              module={m}
              opening={opener.pending}
              onReveal={() => {
                void opener.run(m.path)
              }}
            />
          ))}
        </ul>
      ) : (
        <p className="flex items-start gap-1.5 px-3 py-2 text-xs text-warning">
          <AlertIcon className="mt-0.5 size-3 shrink-0" />
          {clone.error === '' ? 'No go.mod or package.json was read.' : clone.error}
        </p>
      )}
      {clone.workspaces.length > 0 && (
        <p className="border-t border-border/70 px-3 py-1.5 text-xs text-muted">
          Workspaces: <span className="font-mono text-text">{clone.workspaces.join(' · ')}</span>
        </p>
      )}
      {opener.error !== '' && (
        <p role="alert" className="border-t border-border/70 px-3 py-1.5 text-xs text-danger">
          {opener.error}
        </p>
      )}
      <div className={drawerFooterClass}>
        <button
          type="button"
          disabled={opener.pending}
          onClick={() => {
            void opener.run(null)
          }}
          className={quietButtonClass}
        >
          <TerminalIcon className="size-3.5" />
          Open in Terminal
        </button>
        {onMark !== undefined && canMark(clone) && (
          <button
            type="button"
            title={markTitle(clone)}
            disabled={marking}
            onClick={() => {
              onMark(clone.checkedAt === '')
            }}
            className={quietButtonClass}
          >
            <CircleCheckIcon className="size-3.5" />
            {clone.checkedAt === '' ? 'Mark up to date' : 'Unmark up to date'}
          </button>
        )}
        <span className="flex-1" />
        <button type="button" onClick={onUpgrade} className={upgradeButtonClass}>
          Upgrade dependencies…
        </button>
      </div>
    </li>
  )
}
