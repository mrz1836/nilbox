import { useId } from 'react'

import { plainText } from '../../lib/ansi'
import {
  changedHeading,
  changedNote,
  checkSuffix,
  checksHeading,
  checksTone,
  checksTook,
  checkTones,
} from '../../lib/hooks'
import { useQuery } from '../../lib/useQuery'
import { getChecksResult, type HookCheckResult } from '../../platform'
import { ToneIcon, toneWords } from './ToneIcon'

// worthSaying reports a check with something to say beyond its mark: one that failed or
// was skipped, with what went wrong, how to fix it, or its own output.
function worthSaying(check: HookCheckResult): boolean {
  return (
    (check.state === 'failed' || check.state === 'skipped') &&
    (check.error !== '' || check.suggestion !== '' || check.detail.length > 0)
  )
}

export interface ChecksResultProps {
  // runId is the checks run, ended.
  runId: number
}

// ChecksResult is how a checks run came out: which checks, in which clone, on which files,
// and how long they took; the sum in a sentence; each check with its mark (passed, failed,
// skipped, off in the repository, or not run); for each that failed or was skipped, what
// go-pre-commit said went wrong, how to fix it, and its output; every file the checks
// changed or staged, one that already had uncommitted changes included; the tools
// go-pre-commit installed for them; and its own errors. It shows nothing until the result
// is read, nor for a run that ended without one, whose status says why.
export function ChecksResult({ runId }: ChecksResultProps) {
  const id = useId()
  const query = useQuery(() => getChecksResult(runId), [runId])
  const result = query.data

  if (result === undefined) {
    return null
  }

  const tone = checksTone(result)
  const took = checksTook(result.durationMs)
  const said = result.checks.filter(worthSaying)

  return (
    <section
      aria-labelledby={`${id}-heading`}
      className="flex max-h-80 shrink-0 flex-col gap-2 overflow-y-auto rounded-lg border border-border bg-surface px-3 py-2.5 text-xs"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h3 id={`${id}-heading`} className="text-sm font-semibold">
          {checksHeading(result)}
        </h3>
        {took !== '' && <span className="shrink-0 text-muted tabular-nums">{took}</span>}
      </div>
      <p className="flex items-start gap-1.5 text-sm text-text">
        <ToneIcon tone={tone} className="mt-0.5 size-4 shrink-0" />
        <span>
          <span className="sr-only">{toneWords[tone]} </span>
          {result.message}
        </span>
      </p>
      <ul aria-label="Checks" className="flex flex-wrap gap-x-3 gap-y-1">
        {result.checks.map((check) => (
          <li key={check.name} className="flex items-center gap-1 text-text">
            <ToneIcon tone={checkTones[check.state]} />
            <span className="sr-only">{toneWords[checkTones[check.state]]} </span>
            {check.name}
            {checkSuffix[check.state] !== '' && <span className="text-muted">{checkSuffix[check.state]}</span>}
          </li>
        ))}
      </ul>
      {said.map((check) => (
        <div key={check.name} className="flex flex-col gap-1">
          {check.error !== '' && (
            <p className="text-text">
              <span className="font-medium">{check.name}:</span> {plainText(check.error)}
            </p>
          )}
          {check.suggestion !== '' && <p className="text-muted">To fix: {plainText(check.suggestion)}</p>}
          {check.detail.length > 0 && (
            <pre
              aria-label={`${check.name}’s output`}
              className="rounded-md bg-background px-2 py-1.5 font-mono leading-[1.35rem] break-all whitespace-pre-wrap text-text"
            >
              {check.detail.map((line) => plainText(line)).join('\n')}
            </pre>
          )}
        </div>
      ))}
      {result.changedKnown ? (
        result.changed.length > 0 && (
          <div className="flex flex-col gap-0.5">
            <p className="text-text">{changedHeading(result.changed)}</p>
            <ul aria-label="Changed files" className="flex flex-col gap-0.5 pl-3">
              {result.changed.map((file) => {
                const note = changedNote(file)

                return (
                  <li key={file.path} className="text-text">
                    <span className="font-mono break-all">{file.path}</span>
                    {note !== '' && <span className="text-muted"> ({note})</span>}
                  </li>
                )
              })}
            </ul>
          </div>
        )
      ) : (
        <p className="flex items-start gap-1.5 text-warning">
          <ToneIcon tone="warn" className="mt-px size-3.5 shrink-0" />
          <span>
            Couldn’t tell which files the checks changed
            {result.changedError === '' ? '.' : `: ${result.changedError}`}
          </span>
        </p>
      )}
      {result.installs.length > 0 && (
        <p className="flex items-start gap-1.5 text-muted">
          <ToneIcon tone="info" className="mt-px size-3.5 shrink-0" />
          <span>go-pre-commit installed {result.installs.join(', ')} for these checks.</span>
        </p>
      )}
      {result.errors.length > 0 && (
        <ul aria-label="go-pre-commit’s errors" className="flex flex-col gap-1">
          {result.errors.map((error) => (
            <li key={error} className="flex items-start gap-1.5 text-text">
              <ToneIcon tone="bad" className="mt-px size-3.5 shrink-0" />
              <span className="min-w-0 font-mono break-all whitespace-pre-wrap">{plainText(error)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
