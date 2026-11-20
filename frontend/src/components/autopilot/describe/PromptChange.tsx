import { useId, useState } from 'react'

import { attempt } from '../../../lib/call'
import { promptInput, promptKindWords, type PromptPart } from '../../../lib/describeit'
import { createPrompt } from '../../../platform'
import { quietButtonClass } from '../../controls'
import { ChevronDownIcon, ChevronRightIcon } from '../../icons'
import { OutcomeLine, Problems, Risks, RowActions, rowClass, RowTitle, useAct, type RowProps } from './parts'

// PromptChange is a card's new prompt: its nickname and name, what it is for, its text on
// request, its problems and risks, and Add, which adds it to the prompts with the library's
// defaults for its models, effort, plan mode, worktree, and flags, or Discard.
export function PromptChange({ p, outcome, held, onSettle, kit }: RowProps<PromptPart>) {
  const id = useId()
  const [shown, setShown] = useState(false)
  const act = useAct(p, onSettle)
  const prompt = p.checked.prompt
  const nickname = prompt.nickname === '' ? prompt.name : prompt.nickname

  return (
    <div role="group" aria-labelledby={`${id}-title`} className={rowClass}>
      <RowTitle id={`${id}-title`} title={nickname} kind="New prompt" />
      <p className="text-xs text-muted">{`${prompt.name} · ${promptKindWords(prompt)}`}</p>
      <button
        type="button"
        aria-expanded={shown}
        aria-controls={`${id}-body`}
        onClick={() => {
          setShown(!shown)
        }}
        className={`${quietButtonClass} self-start`}
      >
        {shown ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
        Show the prompt
      </button>
      {shown && (
        <pre
          id={`${id}-body`}
          className="max-h-64 overflow-auto rounded-md border border-border bg-background px-2.5 py-2 font-mono text-xs whitespace-pre-wrap text-text"
        >
          {prompt.body}
        </pre>
      )}
      <Problems problems={p.checked.problems} />
      <Risks risks={p.checked.risks} />
      {outcome !== undefined ? (
        <OutcomeLine part="prompt" outcome={outcome} onShowRoutine={kit.onShowRoutine} />
      ) : (
        <RowActions
          label="Add"
          blocked={p.checked.problems.length > 0}
          busy={held || act.busy}
          error={act.error}
          onApply={() => {
            void act.act('added', async () => {
              const made = await attempt(createPrompt(promptInput(prompt)))

              return made.ok ? { ok: true, data: 0 } : made
            })
          }}
          onDiscard={() => {
            void act.act('discarded')
          }}
        />
      )}
    </div>
  )
}
