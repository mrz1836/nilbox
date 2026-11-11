import { useId } from 'react'

import type { ReleaseMajorOffer } from '../../platform'
import { fieldClass } from '../controls'
import { CheckIcon, XIcon } from '../icons'

export interface MajorConfirmProps {
  major: ReleaseMajorOffer
  // repo is the repository, "owner/name", the release is of.
  repo: string
  // typed is what the user typed to confirm, and allowModulePath whether they ticked the
  // box to release a major go.mod doesn't name.
  typed: string
  onTyped: (typed: string) => void
  allowModulePath: boolean
  onAllowModulePath: (allow: boolean) => void
  disabled?: boolean
}

// MajorConfirm is the careful form a major release takes: what a major means for the
// people who use the repository (or, from v0.x, that it is the first stable release),
// go.mod's module path when Go couldn't install the new version from it, with the only
// way past it, and the version to type, which starts empty every time. The release
// starts only once the field matches the version exactly; the service checks it too.
export function MajorConfirm({
  major,
  repo,
  typed,
  onTyped,
  allowModulePath,
  onAllowModulePath,
  disabled = false,
}: MajorConfirmProps) {
  const id = useId()
  const matches = typed.trim() === major.version
  const heading = major.firstStable
    ? `${major.version} is your first stable release`
    : 'A major release is a breaking change'

  return (
    <section
      aria-labelledby={`${id}-heading`}
      className="flex flex-col gap-3 rounded-lg border border-danger/45 bg-danger/5 px-4 py-3"
    >
      <h3 id={`${id}-heading`} className="text-sm font-semibold">
        {heading}
      </h3>
      <p className="text-sm text-muted">
        {major.firstStable
          ? `From here on, a change that can break the code of everyone who uses ${repo} needs a v2.`
          : `${major.version} tells everyone who uses ${repo} that this release can break their code.`}{' '}
        Once it’s pushed and fetched, a tag is as good as permanent.
      </p>
      {major.moduleText !== '' && (
        <div className="flex flex-col gap-1.5">
          <p className="flex items-start gap-2 text-sm text-text">
            <XIcon className="mt-px size-4 shrink-0 text-danger" />
            <span>
              <span className="sr-only">Blocks the release: </span>
              {major.moduleText}
            </span>
          </p>
          <label className="flex items-center gap-2 pl-6 text-sm">
            <input
              type="checkbox"
              checked={allowModulePath}
              disabled={disabled}
              onChange={(e) => {
                onAllowModulePath(e.target.checked)
              }}
              className="checkbox"
            />
            Release anyway: this module isn’t meant to be imported
          </label>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <label htmlFor={`${id}-typed`} className="text-sm font-medium">
          Type {major.version} to confirm
        </label>
        <input
          id={`${id}-typed`}
          type="text"
          value={typed}
          disabled={disabled}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          aria-describedby={`${id}-match`}
          onChange={(e) => {
            onTyped(e.target.value)
          }}
          className={`${fieldClass} w-40 font-mono`}
        />
        <span id={`${id}-match`} className="flex items-center gap-1 text-xs text-muted">
          {matches ? (
            <>
              <CheckIcon className="size-3.5 text-success" />
              Matches
            </>
          ) : (
            typed.trim() !== '' && (
              <>
                <XIcon className="size-3.5 text-danger" />
                Doesn’t match {major.version} yet
              </>
            )
          )}
        </span>
      </div>
    </section>
  )
}
