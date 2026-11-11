import { useId, useState } from 'react'

import { blocked, outcomeTones, ownerOf, replaceQuestion } from '../../lib/hooks'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import { getHookActionResult, replaceHook } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { ErrorText } from '../settings/rows'
import { ToneIcon, toneWords } from './ToneIcon'

export interface HookResultProps {
  // runId is the hooks action's run, ended.
  runId: number
  // onSelect shows another run, as Replace… starts one.
  onSelect: (id: number) => void
}

// HookResult is what a hooks action did, as the hook files said once go-pre-commit ended,
// never from its exit status alone: installed, updated, already current, replaced (and
// where the other tool's hook went), uninstalled (and what came back), or nothing
// installed because another tool's hook is in the way, with Replace…, which asks first and
// opens the run it starts. It shows nothing until the result is read, nor for a run that
// ended without one, whose status says why.
export function HookResult({ runId, onSelect }: HookResultProps) {
  const id = useId()
  const query = useQuery(() => getHookActionResult(runId), [runId])
  const [asking, setAsking] = useState(false)
  const replace = useMutation((dir: string) => replaceHook({ dir, confirm: '', runsIn: '' }))
  const result = query.data

  if (result === undefined) {
    return null
  }

  const tone = outcomeTones[result.outcome]

  const replaceIt = async () => {
    setAsking(false)

    const started = await replace.run(result.dir)

    if (started.ok) {
      onSelect(started.data)
    }
  }

  return (
    <section
      aria-labelledby={`${id}-heading`}
      className="flex shrink-0 flex-col gap-1.5 rounded-lg border border-border bg-surface px-3 py-2.5 text-sm"
    >
      <h3 id={`${id}-heading`} className="flex items-start gap-1.5 font-medium text-text">
        <ToneIcon tone={tone} className="mt-0.5 size-4 shrink-0" />
        <span>
          <span className="sr-only">{toneWords[tone]} </span>
          {result.message}
          {blocked(result) && (
            <>
              {' · '}
              <button
                type="button"
                aria-label={`Replace the hook in ${result.name}…`}
                disabled={replace.pending}
                onClick={() => {
                  setAsking(true)
                }}
                className="font-medium text-accent hover:underline disabled:opacity-50"
              >
                Replace…
              </button>
            </>
          )}
        </span>
      </h3>
      {result.hooksDir !== '' && (
        <p className="text-xs text-muted">
          Hooks folder: <span className="font-mono break-all">{result.hooksDir}</span>
        </p>
      )}
      <ErrorText error={replace.error} />
      {asking && (
        <ConfirmDialog
          title={`Replace ${ownerOf(result.otherTool)} hook?`}
          confirmLabel={`Replace ${ownerOf(result.otherTool)} hook`}
          onConfirm={() => {
            void replaceIt()
          }}
          onCancel={() => {
            setAsking(false)
          }}
        >
          {replaceQuestion(result)}
        </ConfirmDialog>
      )}
    </section>
  )
}
