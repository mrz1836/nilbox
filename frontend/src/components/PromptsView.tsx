import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import { brand } from '../generated/brand'
import { counted, errorMessage } from '../lib/format'
import { isFindKey } from '../lib/keys'
import { kindPlural, promptApplies, usageCount, usagePlaces } from '../lib/launch'
import { useMutation } from '../lib/useMutation'
import { useDebouncedQuery } from '../lib/useQuery'
import {
  checkPrompt,
  chooseFileToOpen,
  chooseFileToSave,
  createPrompt,
  deletePrompt,
  duplicatePrompt,
  exportPrompts,
  resetPrompt,
  setDefaultPrompt,
  updatePrompt,
  type AgentInfo,
  type DefaultPrompts,
  type Diagnostic,
  type ImportResult,
  type InboxItem,
  type PerAgent,
  type Prompt,
  type PromptInput,
  type PromptVariable,
} from '../platform'
import { ConfirmDialog, type ConfirmDialogProps } from './ConfirmDialog'
import { buttonClass, primaryButtonClass } from './controls'
import {
  CopyIcon,
  DocumentIcon,
  ExportIcon,
  HistoryIcon,
  ImportIcon,
  MoreIcon,
  PlusIcon,
  ResetIcon,
  StarIcon,
  TrashIcon,
} from './icons'
import { ImportPromptsDialog, type ImportStart } from './ImportPromptsDialog'
import { MenuButton, type MenuItem } from './Menu'
import { PromptEditor } from './PromptEditor'
import { PromptLibrary, type Selection } from './PromptLibrary'
import { PromptPreview } from './PromptPreview'
import { TabPanel, Tabs } from './Tabs'
import type { TemplateEditorHandle } from './TemplateEditor'
import { VariablesPanel } from './VariablesPanel'

export interface PromptsViewProps {
  // agents are the coding agents, whose models and flags a prompt can set.
  agents: readonly AgentInfo[]
  prompts: readonly Prompt[]
  defaults: DefaultPrompts | null
  variables: readonly PromptVariable[]
  // items are the inbox's items, offered to preview a prompt with.
  items: readonly InboxItem[]
  // onChanged reloads the prompts and everything that depends on them; it resolves
  // once they are loaded again.
  onChanged: () => Promise<void>
}

// Pending is a question the page is asking before it acts.
type Pending =
  | { kind: 'discard'; then: Selection }
  | { kind: 'delete'; prompt: Prompt }
  | { kind: 'in-use'; prompt: Prompt }
  | { kind: 'reset'; prompt: Prompt }

// Question is what the page asks before acting on a Pending: the confirm dialog's words,
// and what confirming does.
type Question = Omit<ConfirmDialogProps, 'onCancel'>

// sidePanels are the tabs beside the editor.
const sidePanels = [
  { value: 'preview', label: 'Preview' },
  { value: 'variables', label: 'Variables' },
] as const

// newPromptBody starts a new prompt with the item's address, so the variables show at
// work from the first keystroke.
const newPromptBody = 'Look at {{.Kind}} #{{.Number}} in {{.Repo}}: {{.Title}}\n{{.URL}}\n\n'

const blankInput: PromptInput = {
  name: '',
  nickname: '',
  appliesTo: 'pr',
  body: newPromptBody,
  models: {},
  effort: '',
  planMode: null,
  worktree: null,
  extraFlags: {},
}

// toInput is a stored prompt as the editor holds it.
function toInput(p: Prompt): PromptInput {
  return {
    name: p.name,
    nickname: p.nickname,
    appliesTo: p.appliesTo,
    body: p.body,
    models: { ...p.models },
    effort: p.effort,
    planMode: p.planMode,
    worktree: p.worktree,
    extraFlags: { ...p.extraFlags },
  }
}

// samePerAgent reports whether two per-agent settings are the same, a blank value and no
// entry being the same.
function samePerAgent(a: PerAgent<string>, b: PerAgent<string>): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]) as Set<keyof PerAgent<string>>

  return [...keys].every((k) => (a[k] ?? '').trim() === (b[k] ?? '').trim())
}

// sameInput reports whether two drafts hold the same prompt.
function sameInput(a: PromptInput, b: PromptInput): boolean {
  return (
    a.name === b.name &&
    a.nickname === b.nickname &&
    a.appliesTo === b.appliesTo &&
    a.body === b.body &&
    samePerAgent(a.models, b.models) &&
    a.effort === b.effort &&
    a.planMode === b.planMode &&
    a.worktree === b.worktree &&
    samePerAgent(a.extraFlags, b.extraFlags)
  )
}

// promptFiles is the type of file prompts are exported to and imported from. An import
// also reads a copy of the app's database, such as one restored from a backup.
const promptFiles = [{ name: `${brand.name} prompts`, pattern: '*.json' }]
const importFiles = [...promptFiles, { name: `${brand.name} database`, pattern: '*.db' }]

// datePart is a date as a file name carries it: 2026-11-08.
function datePart(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')

  return `${String(now.getFullYear())}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

// fileSafe is text with the characters a file name cannot hold replaced.
function fileSafe(text: string): string {
  return text.replace(/[/\\:*?"<>|]+/g, '-').trim()
}

// baseName is the last part of a path.
function baseName(path: string): string {
  return path.split(/[\\/]/).at(-1) ?? path
}

// importedText says what an import, or a restore from a backup, did.
function importedText(result: ImportResult, restored: boolean): string {
  const done = `${restored ? 'Restored' : 'Imported'} ${counted(result.added + result.replaced, 'prompt', 'prompts')}.`

  if (result.skipped === 0) {
    return done
  }

  return `${done} ${result.skipped === 1 ? 'One was' : `${String(result.skipped)} were`} already in your library.`
}

// touchedField reports whether a field of the draft was edited away from its start.
function touchedField(field: string, draft: PromptInput, start: PromptInput): boolean {
  const key = field as keyof PromptInput

  return key in draft && draft[key] !== start[key]
}

// PromptsView is the Prompts page: the prompt library on the left, the selected prompt's
// editor in the middle, and its live preview and the variables cheat sheet on the right.
// Edits are checked by the backend as you type and saved with Save (or ⌘S). Leaving a
// prompt with unsaved changes asks first.
export function PromptsView({ agents, prompts, defaults, variables, items, onChanged }: PromptsViewProps) {
  const id = useId()
  const editorRef = useRef<TemplateEditorHandle>(null)
  // searchRef is the library's search field, which ⌘F focuses.
  const searchRef = useRef<HTMLInputElement>(null)
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
  const [status, setStatus] = useState<{ tone: 'error' | 'success'; text: string } | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  // importing is the import under way in its dialog, or null.
  const [importing, setImporting] = useState<ImportStart | null>(null)

  // applicableItems are the inbox items the draft applies to, which it can be previewed
  // with.
  const applicableItems = items.filter((item) => promptApplies({ appliesTo: draft.appliesTo }, item.kind))

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
  const shownFields = (check?.fields ?? []).filter(
    (f) => selection?.kind !== 'new' || touchedField(f.field, draft, blankInput),
  )
  const fieldErrors = Object.fromEntries(shownFields.map((f) => [f.field, f.message])) as Record<string, string>
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

  // ⌘S saves while the page is showing, and ⌘F finds a prompt in the library: a page
  // hidden behind another runs no effects, so it stops listening.
  const saveOnKey = useEffectEvent((e: globalThis.KeyboardEvent) => {
    if (e.metaKey && e.key === 's') {
      e.preventDefault()

      if (canSave) {
        void save()
      }
    } else if (isFindKey(e)) {
      e.preventDefault()
      searchRef.current?.focus()
      searchRef.current?.select()
    }
  })

  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      saveOnKey(e)
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

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

  const exporter = useMutation((path: string, ids: number[]) => exportPrompts(path, ids))

  // exportTo asks where to save the prompts with ids (every prompt when ids is empty),
  // offering filename, then writes them there.
  const exportTo = async (ids: number[], filename: string) => {
    setStatus(null)

    let path: string

    try {
      path = await chooseFileToSave({ title: 'Export prompts', button: 'Export', filters: promptFiles, filename })
    } catch (err) {
      setStatus({ tone: 'error', text: `Couldn’t open the save dialog: ${errorMessage(err)}` })

      return
    }

    if (path === '') {
      return
    }

    const result = await exporter.run(path, ids)

    setStatus(
      result.ok
        ? { tone: 'success', text: `Exported ${counted(result.data, 'prompt', 'prompts')} to “${baseName(path)}”.` }
        : { tone: 'error', text: result.error },
    )
  }

  // importFile asks for a prompt file, then shows what importing it would do.
  const importFile = async () => {
    setStatus(null)

    try {
      const path = await chooseFileToOpen({ title: 'Import prompts', button: 'Import', filters: importFiles })

      if (path !== '') {
        setImporting({ kind: 'file', path })
      }
    } catch (err) {
      setStatus({ tone: 'error', text: `Couldn’t open the file dialog: ${errorMessage(err)}` })
    }
  }

  // An import opens the prompt it brought in, so unsaved changes are dealt with first.
  const unsavedTitle = dirty ? 'Save or discard your changes first.' : undefined

  // libraryActions are what can be done with the whole library.
  const libraryActions: MenuItem[] = [
    {
      label: 'Import from a file…',
      icon: <ImportIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: unsavedTitle,
      onSelect: () => {
        void importFile()
      },
    },
    {
      label: 'Restore from a backup…',
      icon: <HistoryIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: unsavedTitle,
      onSelect: () => {
        setStatus(null)
        setImporting({ kind: 'backups' })
      },
    },
    {
      label: 'Export all prompts…',
      icon: <ExportIcon className="size-4 text-muted" />,
      divided: true,
      disabled: prompts.length === 0,
      onSelect: () => {
        void exportTo([], `${brand.name} prompts ${datePart(new Date())}.json`)
      },
    },
  ]

  // shown is the status the footer shows: a failed action, then a failed check, then how
  // the last action went.
  const shown = status?.tone === 'error' || checkError === '' ? status : { tone: 'error' as const, text: checkError }

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

    items.push({
      label: 'Export…',
      icon: <ExportIcon className="size-4 text-muted" />,
      title: dirty ? 'Exports it as last saved.' : undefined,
      onSelect: () => {
        void exportTo([p.id], `${fileSafe(p.nickname)}.json`)
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

  // question words the dialog for a pending question, and what confirming it does.
  const question = (asked: Pending): Question => {
    switch (asked.kind) {
      case 'discard':
        return {
          title: 'Discard your changes?',
          confirmLabel: 'Discard changes',
          cancelLabel: 'Keep editing',
          children:
            selection?.kind === 'new'
              ? 'The new prompt hasn’t been created yet.'
              : `“${baseline.nickname}” has changes that aren’t saved.`,
          onConfirm: () => {
            select(asked.then)
          },
        }
      case 'delete':
        return {
          title: `Delete “${asked.prompt.nickname}”?`,
          confirmLabel: 'Delete prompt',
          children:
            asked.prompt.starterKey !== ''
              ? 'It’s a starter prompt, and it won’t be added back.'
              : 'This can’t be undone.',
          onConfirm: () => {
            void run(async () => {
              await deletePrompt(asked.prompt.id)

              return null
            }, `Deleted “${asked.prompt.nickname}”.`)
          },
        }
      case 'in-use':
        return {
          title: `“${asked.prompt.nickname}” is still in use`,
          confirmLabel: 'Got it',
          tone: 'primary',
          cancelLabel: null,
          children: (
            <>
              <p>A prompt can be deleted once nothing uses it as a default. Choose another default here first:</p>
              <ul className="mt-2 list-disc pl-5 text-text">
                {usagePlaces(asked.prompt.usage).map((place) => (
                  <li key={place}>{place}</li>
                ))}
              </ul>
              <p className="mt-2">
                App defaults are in Settings → Launching; org and repo defaults in Settings → Repositories.
              </p>
            </>
          ),
          onConfirm: () => {
            // Acknowledging is all there is to do.
          },
        }
      default:
        return {
          title: `Reset “${asked.prompt.nickname}”?`,
          confirmLabel: 'Reset to original',
          tone: 'primary',
          children: `Its title, text, and launch settings go back to how the app ships them.${dirty ? ' Your unsaved changes are discarded too.' : ''}`,
          onConfirm: () => {
            void run(async () => {
              await resetPrompt(asked.prompt.id)

              return { kind: 'prompt', id: asked.prompt.id }
            }, `Reset “${asked.prompt.nickname}” to its original text and settings.`)
          },
        }
    }
  }

  const asking = pending === null ? null : question(pending)

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
      <PromptLibrary
        prompts={prompts}
        selection={selection}
        newNickname={draft.nickname}
        dirty={dirty}
        onAsk={ask}
        actions={libraryActions}
        searchRef={searchRef}
      />

      <section aria-label="Prompt editor" className="flex min-w-0 flex-1 flex-col">
        {selection === null ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
            <DocumentIcon className="size-8 text-muted" />
            <p className="text-sm text-muted">No prompts yet.</p>
            <div className="flex gap-2">
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
              <button
                type="button"
                onClick={() => {
                  setImporting({ kind: 'backups' })
                }}
                className={buttonClass}
              >
                <HistoryIcon className="size-4" />
                Restore from a backup
              </button>
            </div>
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
              agents={agents}
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
          <Tabs
            baseId={id}
            label="Side panel"
            tabs={sidePanels}
            value={panel}
            onChange={setPanel}
            className="flex gap-1 border-b border-border px-3 pt-3"
          />
          <TabPanel baseId={id} value={panel} className="flex min-h-0 flex-1 flex-col">
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
          </TabPanel>
        </aside>
      )}

      {importing !== null && (
        <ImportPromptsDialog
          start={importing}
          agents={agents}
          onCancel={() => {
            setImporting(null)
          }}
          onImported={(result) => {
            const restored = importing.kind === 'backups'

            setImporting(null)
            void run(
              () => Promise.resolve(result.first === 0 ? null : { kind: 'prompt' as const, id: result.first }),
              importedText(result, restored),
            )
          }}
        />
      )}

      {asking !== null && (
        <ConfirmDialog
          {...asking}
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            setPending(null)
            asking.onConfirm()
          }}
        />
      )}
    </div>
  )
}
