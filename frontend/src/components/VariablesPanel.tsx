import { useId, useState } from 'react'

import type { PromptVariable } from '../platform'
import { SearchIcon } from './icons'

export interface VariablesPanelProps {
  variables: readonly PromptVariable[]
  // values holds each variable's value for the preview item, or null before a check.
  values: Readonly<Record<string, string | undefined>> | null
  // valuesFor names the preview item the values come from, as in "#141".
  valuesFor: string
  // forIssuesOnly dims the variables that are always empty for issues.
  forIssuesOnly: boolean
  onInsertVariable: (name: string) => void
  onInsertText: (text: string) => void
}

// The syntax a prompt can use, each with a snippet to insert.
const syntax: readonly { code: string; insert: string; meaning: string }[] = [
  { code: '{{.Title}}', insert: '{{.Title}}', meaning: 'Insert a value.' },
  {
    code: '{{if .InWorktree}}…{{end}}',
    insert: '{{if .InWorktree}}\n\n{{end}}',
    meaning: 'Text only when a value is set.',
  },
  {
    code: '{{if .IsPR}}…{{else}}…{{end}}',
    insert: '{{if .IsPR}}\n\n{{else}}\n\n{{end}}',
    meaning: 'One text or the other.',
  },
  {
    code: '{{if eq .Checks "failing"}}…{{end}}',
    insert: '{{if eq .Checks "failing"}}\n\n{{end}}',
    meaning: 'Compare with eq, ne, lt, gt; combine with and, or, not.',
  },
  {
    code: '{{with .Labels}}Labels: {{.}}{{end}}',
    insert: '{{with .Labels}}Labels: {{.}}{{end}}',
    meaning: 'Use a value only when it is set.',
  },
  { code: '{{/* note */}}', insert: '{{/*  */}}', meaning: 'A note that is never sent.' },
  { code: '{{- .Title -}}', insert: '{{- .Title -}}', meaning: 'Trim the spaces around it.' },
]

const groupOrder = ['Item', 'Repository', 'Pull request', 'Session']

// VariablesPanel is the prompt editor's cheat sheet: every variable by group, with its
// type, what it holds, and its value for the item being previewed, then the template
// syntax. Clicking a variable or a snippet inserts it at the editor's caret.
export function VariablesPanel({
  variables,
  values,
  valuesFor,
  forIssuesOnly,
  onInsertVariable,
  onInsertText,
}: VariablesPanelProps) {
  const searchId = useId()
  const [query, setQuery] = useState('')

  const q = query.trim().toLowerCase()
  const shown = variables.filter(
    (v) => q === '' || v.name.toLowerCase().includes(q) || v.description.toLowerCase().includes(q),
  )
  const groups = groupOrder
    .map((group) => ({ group, vars: shown.filter((v) => v.group === group) }))
    .filter((g) => g.vars.length > 0)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="px-4 pt-3 pb-2">
        <label htmlFor={searchId} className="sr-only">
          Search variables
        </label>
        <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60">
          <SearchIcon className="size-3.5 shrink-0 text-muted" />
          <input
            id={searchId}
            type="search"
            value={query}
            placeholder="Search variables"
            onChange={(e) => {
              setQuery(e.target.value)
            }}
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
          />
        </div>
        <p className="mt-2 text-xs text-muted">Click one to insert it. Values are for {valuesFor}.</p>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {groups.length === 0 && <p className="px-2 py-3 text-sm text-muted">No variable matches “{query}”.</p>}
        {groups.map(({ group, vars }) => (
          <section key={group} aria-label={group} className="mt-2 first:mt-0">
            <h3 className="px-2 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase">
              {group}
            </h3>
            <ul>
              {vars.map((v) => {
                const value = values?.[v.name]
                const dimmed = forIssuesOnly && v.prOnly

                return (
                  <li key={v.name}>
                    <button
                      type="button"
                      onClick={() => {
                        onInsertVariable(v.name)
                      }}
                      title={`Insert {{.${v.name}}}`}
                      className={`group flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-raised ${dimmed ? 'opacity-60' : ''}`}
                    >
                      <span className="flex w-full items-center gap-2">
                        <code className="rounded-sm bg-info/10 px-1 font-mono text-[0.75rem] text-info">{`{{.${v.name}}}`}</code>
                        <span className="text-[0.6875rem] text-muted">{v.type}</span>
                        {v.prOnly && (
                          <span className="ml-auto rounded-full border border-border px-1.5 text-[0.625rem] leading-4 text-muted">
                            PR only
                          </span>
                        )}
                      </span>
                      <span className="text-xs text-muted">{v.description}</span>
                      {value !== undefined && (
                        <span className="truncate font-mono text-[0.6875rem] text-text/80" title={value}>
                          {value === '' ? <span className="text-muted italic">empty</span> : value}
                        </span>
                      )}
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
        {query.trim() === '' && (
          <section aria-label="Syntax" className="mt-4">
            <h3 className="px-2 pt-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase">Syntax</h3>
            <ul>
              {syntax.map((s) => (
                <li key={s.code}>
                  <button
                    type="button"
                    onClick={() => {
                      onInsertText(s.insert)
                    }}
                    title="Insert this snippet"
                    className="flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-raised"
                  >
                    <code className="font-mono text-[0.75rem] break-all text-text">{s.code}</code>
                    <span className="text-xs text-muted">{s.meaning}</span>
                  </button>
                </li>
              ))}
            </ul>
            <p className="mt-3 px-2 text-xs leading-relaxed text-muted">
              Lines left blank by a condition collapse when the prompt is sent. An item&apos;s description and comments
              aren&apos;t variables: the agent reads them with <code className="font-mono">gh</code>, so their text
              stays information, never instructions.
            </p>
          </section>
        )}
      </div>
    </div>
  )
}
