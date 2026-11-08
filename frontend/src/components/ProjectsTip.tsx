import { useId, useState } from 'react'

import { brand } from '../generated/brand'
import { grantProjectsCommand } from '../lib/projects'
import { CommandLine } from './CommandLine'
import { buttonClass } from './controls'
import { CircleCheckIcon, ProjectIcon, XIcon } from './icons'

export interface ProjectsTipProps {
  // on is set once a refresh found the token can read projects: the tip then says they
  // show, until it is closed.
  on: boolean
  // refreshing reports a refresh under way, the check the tip asks for among them.
  refreshing: boolean
  // onCheck refreshes the inbox, which finds out whether the token can read projects now.
  onCheck: () => void
  // onClose puts the tip away. While it offers, that is for good.
  onClose: () => void
}

// closeClass is the tip's close button, as on the app's other notices.
const closeClass =
  'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text'

// ProjectsTip offers, above the inbox lists, to show the GitHub project each issue is in
// and its status there, which needs the GitHub CLI's token to have the read:project scope.
// It shows the command that grants it and a button that checks again once the user has
// run it; a check that still finds no access says so. Once a refresh finds access, the
// tip says the projects show, until it is closed. Settings → Account keeps the command
// after the tip is put away.
export function ProjectsTip({ on, refreshing, onCheck, onClose }: ProjectsTipProps) {
  const headingId = useId()
  // checked records that the user asked for a check, so one that finds no access yet can
  // say so.
  const [checked, setChecked] = useState(false)

  if (on) {
    return (
      <section
        aria-labelledby={headingId}
        className="flex items-start gap-3 rounded-md border border-success py-2 pr-1.5 pl-3 text-sm"
      >
        <CircleCheckIcon className="mt-0.5 size-4 shrink-0 text-success" />
        <p className="min-w-0 flex-1 py-px">
          <span id={headingId} className="font-medium">
            Projects are on.
          </span>{' '}
          <span className="text-muted">Issue cards now show each project an issue is in, with its status there.</span>
        </p>
        <button type="button" aria-label="Close" title="Close" onClick={onClose} className={closeClass}>
          <XIcon className="size-3.5" />
        </button>
      </section>
    )
  }

  return (
    <section
      aria-labelledby={headingId}
      className="flex items-start gap-3 rounded-md border border-border bg-surface py-3 pr-1.5 pl-3 text-sm"
    >
      <ProjectIcon className="mt-0.5 size-4 shrink-0 text-accent" />
      <div className="min-w-0 flex-1">
        <h2 id={headingId} className="font-medium">
          See where your issues stand in their projects
        </h2>
        <p className="mt-0.5 text-muted">
          {brand.name} can show the GitHub project each issue is in and its status there, such as In progress. It needs
          read-only access to your projects: run this in a terminal, then check again.
        </p>
        <div className="mt-2.5 flex flex-wrap items-center gap-2">
          <CommandLine command={grantProjectsCommand} className="w-full max-w-sm" />
          <button
            type="button"
            disabled={refreshing}
            onClick={() => {
              setChecked(true)
              onCheck()
            }}
            className={buttonClass}
          >
            {refreshing && checked ? 'Checking…' : 'Check again'}
          </button>
        </div>
        <p role="status" className="text-xs text-muted">
          {checked && !refreshing && (
            <span className="mt-1.5 block">
              Still no access to your projects. Make sure the command finished in your terminal, then check again.
            </span>
          )}
        </p>
      </div>
      <button
        type="button"
        aria-label="Don’t show this again"
        title="Don’t show this again. Settings → Account keeps the command."
        onClick={onClose}
        className={closeClass}
      >
        <XIcon className="size-3.5" />
      </button>
    </section>
  )
}
