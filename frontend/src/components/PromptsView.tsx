import { useEffect, useEffectEvent, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react'

import { kindPlural, promptApplies, usageCount, usagePlaces } from '../lib/launch'
import { useMutation } from '../lib/useMutation'
import { useDebouncedQuery } from '../lib/useQuery'
import {
  checkPrompt,
  createPrompt,
  deletePrompt,
  duplicatePrompt,
  resetPrompt,
  setDefaultPrompt,
  updatePrompt,
  type DefaultPrompts,
  type Diagnostic,
  type InboxItem,
  type Prompt,
  type PromptAppliesTo,
  type PromptInput,
  type PromptVariable,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { buttonClass, primaryButtonClass, quietButtonClass } from './controls'
import { CopyIcon, DocumentIcon, MoreIcon, PlusIcon, ResetIcon, SearchIcon, StarIcon, TrashIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'
import { PromptEditor } from './PromptEditor'
import { PromptPreview } from './PromptPreview'
import type { TemplateEditorHandle } from './TemplateEditor'
import { VariablesPanel } from './VariablesPanel'

export interface PromptsViewProps {
  prompts: readonly Prompt[]
  defaults: DefaultPrompts | null
  variables: readonly PromptVariable[]
  // items are the inbox's items, offered to preview a prompt with.
  items: readonly InboxItem[]
  // active reports whether the page is showing, so ⌘S only saves here.
  active: boolean
  // onChanged reloads the prompts and everything that depends on them; it resolves
  // once they are loaded again.
  onChanged: () => Promise<void>
}

// Selection is the prompt being edited: a stored one by ID, or a new one.
type Selection = { kind: 'prompt'; id: number } | { kind: 'new' }

// Pending is a question the page is asking before it acts.
type Pending =
  | { kind: 'discard'; then: Selection }
  | { kind: 'delete'; prompt: Prompt }
  | { kind: 'in-use'; prompt: Prompt }
  | { kind: 'reset'; prompt: Prompt }

// newPromptBody starts a new prompt with the item's address, so the variables show at
// work from the first keystroke.
const newPromptBody = 'Look at {{.Kind}} #{{.Number}} in {{.Repo}}: {{.Title}}\n{{.URL}}\n\n'

const blankInput: PromptInput = {
  name: '',
  nickname: '',
  appliesTo: 'pr',
  body: newPromptBody,
  model: '',
  effort: '',
  planMode: null,
  worktree: null,
  extraFlags: '',
}

// toInput is a stored prompt as the editor holds it.
function toInput(p: Prompt): PromptInput {
  return {
    name: p.name,
    nickname: p.nickname,
    appliesTo: p.appliesTo,
    body: p.body,
    model: p.model,
    effort: p.effort,
    planMode: p.planMode,
    worktree: p.worktree,
    extraFlags: p.extraFlags,
  }
}

// sameInput reports whether two drafts hold the same prompt.
function sameInput(a: PromptInput, b: PromptInput): boolean {
  return (
    a.name === b.name &&
    a.nickname === b.nickname &&
    a.appliesTo === b.appliesTo &&
    a.body === b.body &&
    a.model === b.model &&
    a.effort === b.effort &&
    a.planMode === b.planMode &&
    a.worktree === b.worktree &&
    a.extraFlags === b.extraFlags
  )
}

const groups: readonly { appliesTo: PromptAppliesTo; title: string }[] = [
  { appliesTo: 'pr', title: 'Pull requests' },
  { appliesTo: 'issue', title: 'Issues' },
  { appliesTo: 'any', title: 'Both' },
]

// touchedField reports whether a field of the draft was edited away from its start.
function touchedField(field: string, draft: PromptInput, start: PromptInput): boolean {
  const key = field as keyof PromptInput

  return key in draft && draft[key] !== start[key]
}

// isAppDefault reports whether a prompt is the app's default for a kind.
function isAppDefault(p: Prompt): boolean {
  return p.usage.defaults.length > 0
}

// PromptsView is the Prompts page: the prompt library on the left, the selected prompt's
// editor in the middle, and its live preview and the variables cheat sheet on the right.
// Edits are checked by the backend as you type and saved with Save (or ⌘S). Leaving a
// prompt with unsaved changes asks first.
export function PromptsView({ prompts, defaults, variables, items, active, onChanged }: PromptsViewProps) {
  const id = useId()
  const editorRef = useRef<TemplateEditorHandle>(null)
  const [selection, setSelection] = useState<Selection | null>(null)
  const [baseline, setBaseline] = useState<PromptInput>(blankInput)
  const [draft, setDraft] = useState<PromptInput>(blankInput)
  // opened counts the prompts opened in the editor, so a check made for one never shows
  // on the next.
  const [opened, setOpened] = useState(0)
  // pickedItem is the preview item picked in the panel, or null to pick automatically.
  const [pickedItem, setPickedItem] = useState<number | null>(null)
  const [inWorktree, setInWorktree] = useState(true)
  const [atPrHead, setAtPrHead] = useState(true)
  const [panel, setPanel] = useState<'preview' | 'variables'>('preview')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<{ tone: 'error' | 'success'; text: string } | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)

  // applicableItems are the inbox items the draft applies to, which it can be previewed
  // with.
  const applicableItems = useMemo(
    () => items.filter((item) => promptApplies({ appliesTo: draft.appliesTo }, item.kind)),
    [items, draft.appliesTo],
  )

  // previewItem is the item chosen to preview with: the one picked while the prompt
  // applies to it (0 for the sample), else the first item it applies to.
  const previewItem =
    pickedItem !== null && (pickedItem === 0 || applicableItems.some((item) => item.id === pickedItem))
      ? pickedItem
      : (applicableItems[0]?.id ?? 0)

  // The draft is checked, once typing rests, whenever it or the preview changes.
  const checked = useDebouncedQuery(
    () =>
      checkPrompt({
        id: selection?.kind === 'prompt' ? selection.id : 0,
        input: draft,
        itemId: previewItem,
        inWorktree,
        atPrHead,
      }).then((result) => ({ opened, result })),
    [selection, draft, previewItem, inWorktree, atPrHead, opened],
    150,
    { enabled: selection !== null },
  )
  const check = checked.data?.opened === opened ? checked.data.result : null
  const checkError = !checked.pending && checked.error !== '' ? `Could not check the prompt: ${checked.error}` : ''

  const saved = selection?.kind === 'prompt' ? (prompts.find((p) => p.id === selection.id) ?? null) : null
  const dirty = selection !== null && !sameInput(draft, baseline)
  // A new prompt's empty fields are not problems to point at until they are edited.
  const shownFields = useMemo(
    () => (check?.fields ?? []).filter((f) => selection?.kind !== 'new' || touchedField(f.field, draft, blankInput)),
    [check, selection, draft],
  )
  const fieldErrors = useMemo(
    () => Object.fromEntries(shownFields.map((f) => [f.field, f.message])) as Record<string, string>,
    [shownFields],
  )
  const diagnostics = check?.diagnostics ?? []
  const hiddenProblems = (check?.fields.length ?? 0) - shownFields.length
  const problemCount = shownFields.length + diagnostics.filter((d: Diagnostic) => d.severity === 'error').length

  // open shows a prompt from list (or a new one) in the editor, and reports whether it
  // could: a stored prompt must be in the list.
  const open = (list: readonly Prompt[], next: Selection): boolean => {
    const stored = next.kind === 'prompt' ? list.find((p) => p.id === next.id) : undefined

    if (next.kind === 'prompt' && stored === undefined) {
      return false
    }

    const input = stored === undefined ? blankInput : toInput(stored)

    setSelection(next)
    setBaseline(input)
    setDraft(input)
    setOpened((n) => n + 1)

    return true
  }

  const select = (next: Selection) => {
    open(prompts, next)
  }

  // afterReload is the prompt an action asked to open once the library reloads.
  const [afterReload, setAfterReload] = useState<Selection | null>(null)
  // library is the prompt list last seen, to notice when a reload brings a new one.
  const [library, setLibrary] = useState<readonly Prompt[] | null>(null)

  // Whenever the library loads, open the prompt an action asked for, or else the first
  // prompt when none is open or the open one was deleted. This adjusts state while
  // rendering, as React recommends for reacting to a changed prop.
  if (library !== prompts) {
    setLibrary(prompts)

    const wanted = afterReload !== null && open(prompts, afterReload)

    if (wanted) {
      setAfterReload(null)
    } else if (selection === null || (selection.kind === 'prompt' && !prompts.some((p) => p.id === selection.id))) {
      const first = prompts[0]

      if (first !== undefined) {
        open(prompts, { kind: 'prompt', id: first.id })
      }
    }
  }

  // ask switches prompts, asking first when the current one has unsaved changes.
  const ask = (next: Selection) => {
    if (dirty) {
      setPending({ kind: 'discard', then: next })
    } else {
      select(next)
    }
  }

  // saver stores the draft as a new prompt or over the open one, then reloads the library.
  const saver = useMutation(async (at: Selection, input: PromptInput) => {
    const stored = await (at.kind === 'new' ? createPrompt(input) : updatePrompt(at.id, input))

    await onChanged()

    return stored
  })
  // runner performs an action on the library and reloads it, opening the prompt the
  // action names, if any, once the reload brings it in.
  const runner = useMutation(async (action: () => Promise<Selection | null>) => {
    setAfterReload(await action())
    await onChanged()
  })
  const busy = saver.pending || runner.pending

  const save = async () => {
    if (selection === null || busy) {
      return
    }

    setStatus(null)

    // submitted is what is being saved; edits typed while it saves are kept.
    const submitted = draft
    const result = await saver.run(selection, submitted)

    if (!result.ok) {
      setStatus({ tone: 'error', text: result.error })

      return
    }

    const stored = result.data

    setSelection({ kind: 'prompt', id: stored.id })
    setBaseline(toInput(stored))
    setDraft((current) => (sameInput(current, submitted) ? toInput(stored) : current))
    setStatus({ tone: 'success', text: selection.kind === 'new' ? 'Prompt created.' : 'Saved.' })
  }

  const canSave = dirty && problemCount === 0 && hiddenProblems === 0 && !busy

  // ⌘S saves while the page is showing.
  const saveOnKey = useEffectEvent((e: globalThis.KeyboardEvent) => {
    if (e.metaKey && e.key === 's') {
      e.preventDefault()

      if (canSave) {
        void save()
      }
    }
  })

  useEffect(() => {
    if (!active) {
      return undefined
    }

    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      saveOnKey(e)
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [active])

  // run performs an action on the library and says how it went.
  const run = async (action: () => Promise<Selection | null>, done: string) => {
    setStatus(null)

    const result = await runner.run(action)

    if (result.ok) {
      setStatus({ tone: 'success', text: done })
    } else {
      setAfterReload(null)
      setStatus({ tone: 'error', text: result.error })
    }
  }

  // shown is the status the footer shows: a failed action, then a failed check, then how
  // the last action went.
  const shown = status?.tone === 'error' || checkError === '' ? status : { tone: 'error' as const, text: checkError }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()

    return q === ''
      ? prompts
      : prompts.filter((p) => p.nickname.toLowerCase().includes(q) || p.name.toLowerCase().includes(q))
  }, [prompts, query])

  const menuItems = (p: Prompt): MenuItem[] => {
    const items: MenuItem[] = []

    for (const kind of ['pr', 'issue'] as const) {
      const isDefault = (kind === 'pr' ? defaults?.pr : defaults?.issue) === p.id

      if (promptApplies(p, kind) && !isDefault) {
        items.push({
          label: `Make the default for ${kindPlural(kind)}`,
          icon: <StarIcon className="size-4 text-muted" />,
          onSelect: () => {
            void run(
              async () => {
                await setDefaultPrompt(kind, p.id)

                return null
              },
              `“${p.nickname}” is now the default for ${kindPlural(kind)}.`,
            )
          },
        })
      }
    }

    items.push({
      label: 'Duplicate',
      icon: <CopyIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: dirty ? 'Save or discard your changes first.' : undefined,
      onSelect: () => {
        void run(async () => {
          const copy = await duplicatePrompt(p.id)

          return { kind: 'prompt', id: copy.id }
        }, `Duplicated “${p.nickname}”.`)
      },
    })

    if (p.modified) {
      items.push({
        label: 'Reset to original',
        icon: <ResetIcon className="size-4 text-muted" />,
        onSelect: () => {
          setPending({ kind: 'reset', prompt: p })
        },
      })
    }

    items.push({
      label: 'Delete…',
      icon: <TrashIcon className="size-4" />,
      danger: true,
      onSelect: () => {
        setPending(usageCount(p.usage) > 0 ? { kind: 'in-use', prompt: p } : { kind: 'delete', prompt: p })
      },
    })

    return items
  }

  const onPanelKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      setPanel((p) => (p === 'preview' ? 'variables' : 'preview'))
    }
  }

  const previewFor = (() => {
    const item = items.find((i) => i.id === previewItem)

    return item === undefined ? 'the sample' : `#${String(item.number)}`
  })()

  const forPRs = (() => {
    const item = items.find((i) => i.id === previewItem)

    return item === undefined ? draft.appliesTo !== 'issue' : item.kind === 'pr'
  })()

  return (
    <div className="flex h-full min-h-0">
      <aside aria-label="Prompt library" className="flex w-60 shrink-0 flex-col border-r border-border">
        <div className="flex items-center justify-between gap-2 px-4 pt-4 pb-2">
          <h2 className="text-sm font-semibold">Prompts</h2>
          <button
            type="button"
            onClick={() => {
              ask({ kind: 'new' })
            }}
            className={`${quietButtonClass} text-text`}
          >
            <PlusIcon className="size-3.5" />
            New
          </button>
        </div>
        <div className="px-3 pb-2">
          <label htmlFor={`${id}-search`} className="sr-only">
            Search prompts
          </label>
          <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60">
            <SearchIcon className="size-3.5 shrink-0 text-muted" />
            <input
              id={`${id}-search`}
              type="search"
              value={query}
              placeholder="Search"
              onChange={(e) => {
                setQuery(e.target.value)
              }}
              className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
            />
          </div>
        </div>
        <nav aria-label="Prompts" className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
          {selection?.kind === 'new' && (
            <div className="mb-1 rounded-md bg-raised px-2.5 py-2">
              <p className="text-sm font-medium italic">
                {draft.nickname.trim() === '' ? 'New prompt' : draft.nickname}
              </p>
              <p className="text-xs text-muted">Not saved yet</p>
            </div>
          )}
          {filtered.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">No prompt matches “{query}”.</p>}
          {groups.map((g) => {
            const list = filtered.filter((p) => p.appliesTo === g.appliesTo)

            if (list.length === 0) {
              return null
            }

            return (
              <section key={g.appliesTo} aria-label={g.title} className="mt-2 first:mt-0">
                <h3 className="px-2.5 pt-2 pb-1 text-[11px] font-semibold tracking-wide text-muted uppercase">
                  {g.title}
                </h3>
                <ul>
                  {list.map((p) => {
                    const current = selection?.kind === 'prompt' && selection.id === p.id
                    const places = p.usage.orgs.length + p.usage.repos.length

                    return (
                      <li key={p.id}>
                        <button
                          type="button"
                          aria-current={current ? 'true' : undefined}
                          onClick={() => {
                            if (!current) {
                              ask({ kind: 'prompt', id: p.id })
                            }
                          }}
                          title={usagePlaces(p.usage).join('\n') || undefined}
                          className="flex w-full flex-col rounded-md px-2.5 py-1.5 text-left transition-colors hover:bg-raised aria-[current=true]:bg-raised"
                        >
                          <span className="flex w-full items-center gap-1.5">
                            <span className="truncate text-sm font-medium">{p.nickname}</span>
                            {isAppDefault(p) && <StarIcon className="size-3 shrink-0 text-accent" />}
                            {current && dirty && (
                              <span
                                aria-label="Unsaved changes"
                                className="ml-auto size-1.5 shrink-0 rounded-full bg-warning"
                              />
                            )}
                          </span>
                          <span className="truncate text-xs text-muted">{p.name}</span>
                          {places > 0 && (
                            <span className="text-[11px] text-muted">
                              Default in {places} {places === 1 ? 'place' : 'places'}
                            </span>
                          )}
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </section>
            )
          })}
        </nav>
      </aside>

      <section aria-label="Prompt editor" className="flex min-w-0 flex-1 flex-col">
        {selection === null ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
            <DocumentIcon className="size-8 text-muted" />
            <p className="text-sm text-muted">No prompts yet.</p>
            <button
              type="button"
              onClick={() => {
                select({ kind: 'new' })
              }}
              className={primaryButtonClass}
            >
              <PlusIcon className="size-4" />
              New prompt
            </button>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-end gap-2 px-6 pt-4">
              {saved?.starterKey !== '' && saved !== null && (
                <span className="mr-auto text-xs text-muted">
                  {saved.modified ? 'Starter prompt · changed' : 'Starter prompt'}
                </span>
              )}
              {saved !== null && (
                <MenuButton
                  label={`More actions for ${saved.nickname}`}
                  title="More actions"
                  items={menuItems(saved)}
                  className="flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
                >
                  <MoreIcon className="size-4" />
                </MenuButton>
              )}
            </div>
            <PromptEditor
              draft={draft}
              onChange={(next) => {
                setDraft(next)
                setStatus(null)
              }}
              saved={saved}
              fieldErrors={fieldErrors}
              diagnostics={diagnostics}
              variables={variables}
              editorRef={editorRef}
              checked={check !== null}
              onReveal={(d) => {
                editorRef.current?.reveal(d.line, d.column, d.length)
              }}
            />
            <footer className="flex items-center justify-end gap-2 border-t border-border px-6 py-3">
              <p
                role={shown?.tone === 'error' ? 'alert' : 'status'}
                className={`mr-auto min-w-0 truncate text-xs ${shown?.tone === 'error' ? 'text-danger' : shown?.tone === 'success' && !dirty ? 'text-success' : 'text-muted'}`}
                title={shown?.text}
              >
                {shown?.tone === 'error'
                  ? shown.text
                  : busy
                    ? 'Saving…'
                    : dirty && problemCount > 0
                      ? `Fix ${String(problemCount)} ${problemCount === 1 ? 'problem' : 'problems'} to save.`
                      : hiddenProblems > 0
                        ? 'Give the prompt a title and a nickname to create it.'
                        : dirty
                          ? 'Unsaved changes'
                          : (shown?.text ?? '')}
              </p>
              {dirty && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    if (selection.kind === 'new') {
                      const first = prompts[0]

                      if (first !== undefined) {
                        select({ kind: 'prompt', id: first.id })
                      }
                    } else {
                      setDraft(baseline)
                    }
                  }}
                  className={buttonClass}
                >
                  {selection.kind === 'new' ? 'Cancel' : 'Discard'}
                </button>
              )}
              <button
                type="button"
                disabled={!canSave}
                onClick={() => {
                  void save()
                }}
                title="Save (⌘S)"
                className={primaryButtonClass}
              >
                {selection.kind === 'new' ? 'Create prompt' : 'Save'}
              </button>
            </footer>
          </>
        )}
      </section>

      {selection !== null && (
        <aside
          aria-label="Preview and variables"
          className="hidden w-80 shrink-0 flex-col border-l border-border lg:flex"
        >
          <div
            role="tablist"
            aria-label="Side panel"
            tabIndex={-1}
            onKeyDown={onPanelKeyDown}
            className="flex gap-1 border-b border-border px-3 pt-3"
          >
            {(['preview', 'variables'] as const).map((tab) => (
              <button
                key={tab}
                id={`${id}-tab-${tab}`}
                type="button"
                role="tab"
                aria-selected={panel === tab}
                aria-controls={`${id}-panel`}
                tabIndex={panel === tab ? 0 : -1}
                onClick={() => {
                  setPanel(tab)
                }}
                className="-mb-px border-b-2 border-transparent px-2.5 pb-2 text-sm font-medium text-muted transition-colors hover:text-text aria-selected:border-accent aria-selected:text-text"
              >
                {tab === 'preview' ? 'Preview' : 'Variables'}
              </button>
            ))}
          </div>
          <div
            id={`${id}-panel`}
            role="tabpanel"
            aria-labelledby={`${id}-tab-${panel}`}
            className="flex min-h-0 flex-1 flex-col"
          >
            {panel === 'preview' ? (
              <PromptPreview
                items={applicableItems}
                itemId={previewItem}
                onItemChange={setPickedItem}
                inWorktree={inWorktree}
                onInWorktreeChange={setInWorktree}
                atPrHead={atPrHead}
                onAtPrHeadChange={setAtPrHead}
                forPRs={forPRs}
                result={check}
                hasErrors={diagnostics.some((d) => d.severity === 'error')}
              />
            ) : (
              <VariablesPanel
                variables={variables}
                values={check?.values ?? null}
                valuesFor={previewFor}
                forIssuesOnly={draft.appliesTo === 'issue'}
                onInsertVariable={(name) => {
                  editorRef.current?.insertVariable(name)
                }}
                onInsertText={(text) => {
                  editorRef.current?.insertText(text)
                }}
              />
            )}
          </div>
        </aside>
      )}

      {pending?.kind === 'discard' && (
        <ConfirmDialog
          title="Discard your changes?"
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            const next = pending.then

            setPending(null)
            select(next)
          }}
        >
          {selection?.kind === 'new'
            ? 'The new prompt hasn’t been created yet.'
            : `“${baseline.nickname}” has changes that aren’t saved.`}
        </ConfirmDialog>
      )}
      {pending?.kind === 'delete' && (
        <ConfirmDialog
          title={`Delete “${pending.prompt.nickname}”?`}
          confirmLabel="Delete prompt"
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            const doomed = pending.prompt

            setPending(null)
            void run(async () => {
              await deletePrompt(doomed.id)

              return null
            }, `Deleted “${doomed.nickname}”.`)
          }}
        >
          {pending.prompt.starterKey !== ''
            ? 'It’s a starter prompt, and it won’t be added back.'
            : 'This can’t be undone.'}
        </ConfirmDialog>
      )}
      {pending?.kind === 'in-use' && (
        <ConfirmDialog
          title={`“${pending.prompt.nickname}” is still in use`}
          confirmLabel="Got it"
          tone="primary"
          cancelLabel={null}
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            setPending(null)
          }}
        >
          <p>A prompt can be deleted once nothing uses it as a default. Choose another default here first:</p>
          <ul className="mt-2 list-disc pl-5 text-text">
            {usagePlaces(pending.prompt.usage).map((place) => (
              <li key={place}>{place}</li>
            ))}
          </ul>
          <p className="mt-2">
            App defaults are in Settings → Launching; org and repo defaults in Settings → Repositories.
          </p>
        </ConfirmDialog>
      )}
      {pending?.kind === 'reset' && (
        <ConfirmDialog
          title={`Reset “${pending.prompt.nickname}”?`}
          confirmLabel="Reset to original"
          tone="primary"
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            const target = pending.prompt

            setPending(null)
            void run(async () => {
              await resetPrompt(target.id)

              return { kind: 'prompt', id: target.id }
            }, `Reset “${target.nickname}” to its original text and settings.`)
          }}
        >
          Its title, text, and launch settings go back to how the app ships them.
          {dirty ? ' Your unsaved changes are discarded too.' : ''}
        </ConfirmDialog>
      )}
    </div>
  )
}
