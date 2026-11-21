import { useState } from 'react'

import type { PromptAppliesTo, PromptVariable } from '../platform'
import { SearchBox } from './SearchBox'

export interface VariablesPanelProps {
  variables: readonly PromptVariable[]
  // values holds each variable's value for the preview item, or null before a check.
  values: Readonly<Record<string, string | undefined>> | null
  // valuesFor names what the values come from, as in "#141" or "the sample repository".
  valuesFor: string
  // appliesTo is what the prompt is for, which dims the variables always empty there.
  appliesTo: PromptAppliesTo
  onInsertVariable: (name: string) => void
  onInsertText: (text: string) => void
}

// alwaysEmpty reports a variable a prompt for appliesTo never sees set: one only a pull
// request fills in, in a prompt for issues, and one only an item fills in, in a prompt for
// repositories, whose launch has no item.
function alwaysEmpty(v: PromptVariable, appliesTo: PromptAppliesTo): boolean {
  return (appliesTo === 'issue' && v.prOnly) || (appliesTo === 'repo' && v.itemOnly)
}

// badgeOf is a variable's badge: PR only for one only a pull request fills in, and, in a
// prompt for repositories, Item only for the rest of those only an item fills in, which
// it shows only there, since an item's launch fills in every one; '' for none.
function badgeOf(v: PromptVariable, appliesTo: PromptAppliesTo): string {
  if (v.prOnly) {
    return 'PR only'
  }

  return appliesTo === 'repo' && v.itemOnly ? 'Item only' : ''
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
  {
    code: '`git switch {{shellquote .Branch}}`',
    insert: '{{shellquote .Branch}}',
    meaning: 'Put a value into a command as one word, quoted when it needs it.',
  },
  { code: '{{/* note */}}', insert: '{{/*  */}}', meaning: 'A note that is never sent.' },
  { code: '{{- .Title -}}', insert: '{{- .Title -}}', meaning: 'Trim the spaces around it.' },
]

const groupOrder = ['Item', 'Repository', 'Pull request', 'Session', 'Autopilot']

// VariablesPanel is the prompt editor's cheat sheet: every variable by group, with its
// type, what it holds, and its value for the item being previewed, then the template
// syntax. Clicking a variable or a snippet inserts it at the editor's caret. A variable
// always empty for what the prompt is for is dimmed, and its badge says why.
export function VariablesPanel({
  variables,
  values,
  valuesFor,
  appliesTo,
  onInsertVariable,
  onInsertText,
}: VariablesPanelProps) {
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
        <SearchBox
          value={query}
          onChange={setQuery}
          label="Search variables"
          placeholder="Search variables"
          dense={false}
        />
        <p className="mt-2 text-xs text-muted">Click one to insert it. Values are for {valuesFor}.</p>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {groups.length === 0 && <p className="px-2 py-3 text-sm text-muted">No variable matches “{query}”.</p>}
        {groups.map(({ group, vars }) => (
          <section key={group} aria-label={group} className="mt-2 first:mt-0">
            <h3 className="px-2 pt-2 pb-1 text-caption font-semibold tracking-wide text-muted uppercase">{group}</h3>
            <ul>
              {vars.map((v) => {
                const value = values?.[v.name]
                const dimmed = alwaysEmpty(v, appliesTo)
                const badge = badgeOf(v, appliesTo)

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
                        <code className="rounded-sm bg-info/10 px-1 font-mono text-small text-info">{`{{.${v.name}}}`}</code>
                        <span className="text-caption text-muted">{v.type}</span>
                        {badge !== '' && (
                          <span className="ml-auto rounded-full border border-border px-1.5 text-badge leading-4 text-muted">
                            {badge}
                          </span>
                        )}
                      </span>
                      <span className="text-xs text-muted">{v.description}</span>
                      {value !== undefined && (
                        <span className="truncate font-mono text-caption text-text/80" title={value}>
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
            <h3 className="px-2 pt-2 pb-1 text-caption font-semibold tracking-wide text-muted uppercase">Syntax</h3>
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
                    <code className="font-mono text-small break-all text-text">{s.code}</code>
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
