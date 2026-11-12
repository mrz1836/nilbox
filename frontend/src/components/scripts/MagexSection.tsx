import { useId, useState } from 'react'

import { formatCount } from '../../lib/format'
import { argsOf, magexTargets, matchTargets } from '../../lib/repoScripts'
import type { ScriptAsk } from '../../lib/useScriptRun'
import type { RunsIn, ScriptEntry, ScriptSection } from '../../platform'
import { chipClass, fieldClass } from '../controls'
import { AlertIcon, ChevronDownIcon, ChevronRightIcon, InfoIcon, TagIcon } from '../icons'
import { MenuButton } from '../Menu'
import { GroupHeading } from './GroupHeading'
import { RepoSearch } from './RepoSearch'
import { argsFor, releaseWords, scriptMenu, ScriptRow, type ScriptMenuProps } from './ScriptRow'

// List is one of the section's folded lists of targets.
type List = 'custom' | 'builtins'

// chipButtonClass draws a common target as a chip that can be pressed.
const chipButtonClass = `${chipClass} inline-flex items-center gap-1 font-mono transition-colors hover:bg-raised hover:text-text disabled:opacity-50 disabled:hover:bg-transparent`

// Chip is one common target: a button that runs it at once; greyed, with why, when it
// can't run; or, for one that bumps a version, a menu that says Release does it, opens
// Release, and runs it anyway, as its row's menu does.
function Chip(props: ScriptMenuProps) {
  const { entry, args, pending, onRun } = props

  if (entry.runnable && entry.release) {
    return (
      <MenuButton
        label={`magex ${entry.name}: ${releaseWords}`}
        title={`${releaseWords}. Open Release, or run it anyway.`}
        heading={releaseWords}
        items={() => scriptMenu(props, true)}
        className={chipButtonClass}
      >
        <TagIcon className="size-3 text-accent" />
        {entry.name}
      </MenuButton>
    )
  }

  return (
    <button
      type="button"
      aria-label={`Run magex ${entry.name}`}
      title={entry.runnable ? entry.detail : entry.why}
      disabled={pending || !entry.runnable}
      onClick={() => {
        onRun({ entry, runsIn: '', anyway: false, args: argsFor(entry, args) })
      }}
      className={chipButtonClass}
    >
      {entry.name}
    </button>
  )
}

export interface MagexSectionProps {
  section: ScriptSection
  // headingId is the section's heading's.
  headingId: string
  // open shows the targets under the heading, and onToggle opens or folds them.
  open: boolean
  onToggle: () => void
  // other is the place runs don't go, as set, and otherName its name.
  other: RunsIn
  otherName: string
  pending: boolean
  onRun: (ask: ScriptAsk) => void
  onOpenRelease: (() => void) | null
}

// MagexSection is magex's targets in the clone: the common ones as chips, each running
// its target at once, the repository's own targets, and every built-in, both folded and
// searchable together, with an Arguments field for the built-ins' key=value pairs, and the
// note that magex targets run the repository's own code and its .github/env settings.
// While magex can't run, it says why in place of the targets. Its heading folds it all.
export function MagexSection({ section, headingId, open: shown, onToggle, ...place }: MagexSectionProps) {
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2">
      <GroupHeading
        id={headingId}
        title={section.title}
        count={section.entries.length}
        open={shown}
        disabled={false}
        onToggle={onToggle}
      />
      {shown && <MagexBody section={section} {...place} />}
    </section>
  )
}

// MagexBody is the magex section's targets under its heading, while it is open.
function MagexBody({ section, ...place }: Omit<MagexSectionProps, 'headingId' | 'open' | 'onToggle'>) {
  const id = useId()
  const [argsText, setArgsText] = useState('')
  const [search, setSearch] = useState('')
  const [open, setOpen] = useState<Readonly<Record<List, boolean>>>({ custom: false, builtins: false })
  const { common, custom, builtins } = magexTargets(section)
  const args = argsOf(argsText)
  const searching = search.trim() !== ''
  const lists: readonly { id: List; title: string; entries: ScriptEntry[] }[] = [
    { id: 'custom', title: 'This repository’s targets', entries: matchTargets(custom, search) },
    { id: 'builtins', title: 'All magex commands', entries: matchTargets(builtins, search) },
  ]
  const listed = section.entries.length > 0

  return (
    <>
      {section.note !== '' && (
        <p className="flex items-start gap-1.5 px-1 text-xs text-muted">
          <InfoIcon className="mt-px size-3.5 shrink-0 text-info" />
          {section.note}
        </p>
      )}
      {section.error !== '' && (
        <p className="flex items-start gap-1.5 px-1 text-xs text-warning">
          <AlertIcon className="mt-px size-3.5 shrink-0" />
          {section.error}
        </p>
      )}
      {common.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 px-1">
          <span className="text-xs text-muted">Common:</span>
          <ul aria-label="Common magex targets" className="contents">
            {common.map((entry) => (
              <li key={entry.ref}>
                <Chip entry={entry} args={args} {...place} />
              </li>
            ))}
          </ul>
        </div>
      )}
      {listed && (
        <div className="flex flex-wrap items-start gap-3 px-1">
          <div className="flex min-w-48 flex-1 flex-col gap-1">
            <label htmlFor={`${id}-args`} className="text-xs font-medium">
              Arguments
            </label>
            <input
              id={`${id}-args`}
              type="text"
              value={argsText}
              placeholder="bump=minor"
              spellCheck={false}
              aria-describedby={`${id}-args-help`}
              onChange={(e) => {
                setArgsText(e.target.value)
              }}
              className={`${fieldClass} font-mono text-[0.8125rem]`}
            />
            <p id={`${id}-args-help`} className="text-xs text-muted">
              key=value pairs, separated by spaces, for magex’s own targets.
            </p>
          </div>
          {/* The search lines up with the field, below the field's label. */}
          <div className="flex min-w-48 flex-1 flex-col pt-5">
            <RepoSearch
              value={search}
              onChange={setSearch}
              label="Search magex targets"
              placeholder="Search magex targets…"
            />
          </div>
        </div>
      )}
      {searching && lists.every((l) => l.entries.length === 0) && (
        <p className="px-1 text-sm text-muted">No magex target matching “{search.trim()}”.</p>
      )}
      {lists.map((list) => {
        const shown = searching || open[list.id]
        const listId = `${id}-${list.id}`

        if (list.entries.length === 0) {
          return null
        }

        return (
          <div key={list.id} className="flex flex-col gap-1">
            <button
              type="button"
              aria-expanded={shown}
              // The list it opens is there only while it is open.
              aria-controls={shown ? listId : undefined}
              disabled={searching}
              onClick={() => {
                setOpen({ ...open, [list.id]: !open[list.id] })
              }}
              className="flex items-center gap-1 self-start rounded-md px-1 py-0.5 text-xs font-medium text-muted hover:text-text disabled:hover:text-muted"
            >
              {shown ? <ChevronDownIcon className="size-3.5" /> : <ChevronRightIcon className="size-3.5" />}
              {list.title} ({formatCount(list.entries.length)})
            </button>
            {shown && (
              <ul id={listId} aria-label={list.title} className="flex flex-col">
                {list.entries.map((entry) => (
                  <ScriptRow key={entry.ref} entry={entry} args={args} command={false} {...place} />
                ))}
              </ul>
            )}
          </div>
        )
      })}
    </>
  )
}
