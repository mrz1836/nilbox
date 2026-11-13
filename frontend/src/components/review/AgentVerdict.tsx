import { useState } from 'react'

import { attempt } from '../../lib/call'
import { agentName } from '../../lib/launch'
import { sinceWhen, stateLabel } from '../../lib/sessions'
import { formatAgo } from '../../lib/time'
import { sessionLastWords, type AgentInfo, type AgentSession } from '../../platform'
import { quietButtonClass } from '../controls'
import { Markdown } from '../Markdown'
import { RelativeTime } from '../RelativeTime'

export interface AgentVerdictProps {
  // sessions are the item's agent sessions, newest first.
  sessions: readonly AgentSession[] | undefined
  agents: readonly AgentInfo[]
  // onUse puts the verdict into the sheet's text, and useLabel names the button that does.
  onUse: (text: string) => void
  useLabel?: string
}

// AgentVerdict shows what the latest agent session on the item said: the agent, what it
// is doing, and its last message. Show all reads the whole message from the agent's own
// files on this Mac, and Use as review comment (or as useLabel says) puts it into the
// sheet's text, for the user to edit and send. An agent never reviews or comments on its
// own.
export function AgentVerdict({ sessions, agents, onUse, useLabel = 'Use as review comment' }: AgentVerdictProps) {
  const latest = sessions?.[0]
  const [full, setFull] = useState<{ id: string; text: string; partial: boolean } | null>(null)
  const [error, setError] = useState('')

  if (latest === undefined) {
    return null
  }

  const shown = full?.id === latest.id ? full : null
  const text = shown?.text ?? latest.lastMessage

  return (
    <section aria-label="Agent verdict" className="space-y-1.5 rounded-md border border-border p-3">
      <p className="text-xs text-muted">
        <span className="font-medium text-text">{agentName(agents, latest.agent)}</span> · {stateLabel(latest.state)} ·{' '}
        <RelativeTime iso={sinceWhen(latest)} show={formatAgo} />
      </p>
      {text === '' ? <p className="text-xs text-muted">It hasn’t said anything yet.</p> : <Markdown text={text} />}
      {shown?.partial === true && <p className="text-xs text-muted">Only the start of the message could be read.</p>}
      {error !== '' && <p className="text-xs text-danger">{error}</p>}
      {text !== '' && (
        <div className="flex gap-2">
          {shown === null && (
            <button
              type="button"
              onClick={() => {
                void attempt(sessionLastWords(latest.id)).then((result) => {
                  if (result.ok) {
                    setError('')
                    setFull({ id: latest.id, ...result.data })
                  } else {
                    setError(result.error)
                  }
                })
              }}
              className={quietButtonClass}
            >
              Show all
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              onUse(text)
            }}
            className={quietButtonClass}
          >
            {useLabel}
          </button>
        </div>
      )}
    </section>
  )
}
