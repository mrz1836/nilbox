import { useId } from 'react'

import { fieldsFor, lengthParts, omit, opsFor, problemsAt, problemsExactly } from '../../../lib/routineBuilder'
import type {
  AutopilotClause,
  AutopilotConditions,
  AutopilotField,
  AutopilotFieldID,
  AutopilotOpID,
  ItemKind,
} from '../../../platform'
import { dangerIconButtonClass, fieldClass, quietButtonClass, Select, type SelectOption } from '../../controls'
import { PlusIcon, TrashIcon } from '../../icons'
import { Block, Notes, type BuilderKit } from './parts'
import { TokenField, type TokenOption } from './TokenField'

export interface IfBlockProps {
  kit: BuilderKit
  // kinds are the kinds of item the routine acts on, whose fields a condition can read.
  kinds: readonly ItemKind[]
  conditions: AutopilotConditions
  onChange: (conditions: AutopilotConditions) => void
}

// firstClause is a new condition: the first field the routine can read, its first
// operator, and no value yet.
function firstClause(kit: BuilderKit, kinds: readonly ItemKind[]): AutopilotClause {
  const field = fieldsFor(kit.catalog, kinds)[0]
  const op = field === undefined ? undefined : opsFor(kit.catalog, field)[0]

  if (field === undefined || op === undefined) {
    return {}
  }

  return withOp({ field: field.id }, op.id, op.arity)
}

// withOp is clause comparing with op, of arity: its value or values kept where the
// operator takes them, and left out where it takes none.
function withOp(clause: AutopilotClause, op: AutopilotOpID, arity: string): AutopilotClause {
  const bare: AutopilotClause = { ...omit(omit(clause, 'value'), 'values'), op }

  switch (arity) {
    case 'many':
      return { ...bare, values: clause.values ?? [] }
    case 'one':
      return { ...bare, value: clause.value ?? '' }
    default:
      return bare
  }
}

// IfBlock is a routine's If: whether all or any of its conditions must hold, each one a
// field, an operator, and its values, and one level of groups.
export function IfBlock({ kit, kinds, conditions, onChange }: IfBlockProps) {
  return (
    <Block title="If" hint="Which of those count">
      <ConditionList kit={kit} kinds={kinds} conditions={conditions} onChange={onChange} path="" top />
      <Notes problems={problemsExactly(kit.problems, 'conditions', 'of')} />
    </Block>
  )
}

interface ConditionListProps extends IfBlockProps {
  // path is where the list is in the conditions: "" for the top, "of[2].group" for a
  // group.
  path: string
  top: boolean
  onRemove?: () => void
}

// ConditionList is one list of conditions, the top one or a group: All ▾ or Any ▾ of
// these, each condition, and + Add a condition (and, at the top, + Add a group), with what
// is wrong with the list itself: its match, and for a group, that it is empty, or isn't one
// a group can be (of[2] and of[2].group, which only an imported spec holds).
function ConditionList({ kit, kinds, conditions, onChange, path, top, onRemove }: ConditionListProps) {
  const id = useId()
  const at = (s: string) => (path === '' ? s : `${path}.${s}`)
  const own = top ? [at('match')] : [path.slice(0, -'.group'.length), path, at('match'), at('of')]
  const set = (i: number, cl: AutopilotClause) => {
    onChange({ ...conditions, of: conditions.of.map((c, j) => (j === i ? cl : c)) })
  }
  const remove = (i: number) => {
    onChange({ ...conditions, of: conditions.of.filter((_, j) => j !== i) })
  }

  return (
    <div className={`flex flex-col gap-1.5 ${top ? '' : 'rounded-md border border-border p-2'}`}>
      {(conditions.of.length > 0 || !top) && (
        <div className="flex items-center gap-1.5 text-sm">
          <label htmlFor={`${id}-match`} className="sr-only">
            {top ? 'Match' : 'Match in the group'}
          </label>
          <select
            id={`${id}-match`}
            value={conditions.match}
            onChange={(e) => {
              onChange({ ...conditions, match: e.target.value === 'any' ? 'any' : 'all' })
            }}
            className={`${fieldClass} py-1`}
          >
            <option value="all">All</option>
            <option value="any">Any</option>
          </select>
          <span className="text-muted">of these{top ? ':' : ', together:'}</span>
          {onRemove !== undefined && (
            <button
              type="button"
              aria-label="Remove the group"
              title="Remove the group"
              onClick={onRemove}
              className={`${dangerIconButtonClass} ml-auto`}
            >
              <TrashIcon className="size-3.5" />
            </button>
          )}
        </div>
      )}
      {conditions.of.length === 0 && top && (
        <p className="text-xs text-muted">No conditions: every item that fires counts.</p>
      )}
      {conditions.of.map((cl, i) => {
        const here = at(`of[${String(i)}]`)

        return cl.group !== undefined && cl.group !== null ? (
          <ConditionList
            // The conditions have no identity of their own; their order is theirs.
            key={i}
            kit={kit}
            kinds={kinds}
            conditions={cl.group}
            onChange={(group) => {
              set(i, { group })
            }}
            path={`${here}.group`}
            top={false}
            onRemove={() => {
              remove(i)
            }}
          />
        ) : (
          <Condition
            key={i}
            kit={kit}
            kinds={kinds}
            clause={cl}
            path={here}
            index={i}
            onChange={(next) => {
              set(i, next)
            }}
            onRemove={() => {
              remove(i)
            }}
          />
        )
      })}
      <Notes problems={own.flatMap((p) => problemsExactly(kit.problems, 'conditions', p))} />
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => {
            onChange({ ...conditions, of: [...conditions.of, firstClause(kit, kinds)] })
          }}
          className={quietButtonClass}
        >
          <PlusIcon className="size-3.5" />
          Add a condition
        </button>
        {top && (
          <button
            type="button"
            onClick={() => {
              onChange({
                ...conditions,
                of: [...conditions.of, { group: { match: 'any', of: [firstClause(kit, kinds)] } }],
              })
            }}
            className={quietButtonClass}
          >
            <PlusIcon className="size-3.5" />
            Add a group
          </button>
        )}
      </div>
    </div>
  )
}

interface ConditionProps {
  kit: BuilderKit
  kinds: readonly ItemKind[]
  clause: AutopilotClause
  path: string
  index: number
  onChange: (clause: AutopilotClause) => void
  onRemove: () => void
}

// valueSuggestions are what a field's values are picked from: its choices, or what the
// stored items hold of it, such as the check names the app has seen.
function valueSuggestions(kit: BuilderKit, field: AutopilotField): TokenOption[] {
  if (field.choices.length > 0) {
    return field.choices.map((c) => ({ value: c.value, label: c.label }))
  }

  const seen: Partial<Record<AutopilotFieldID, readonly string[]>> = {
    'checks.failedNames': kit.suggestions.checks,
    'checks.names': kit.suggestions.checks,
    'item.labels': kit.suggestions.labels,
    'item.author': [...kit.suggestions.bots, ...kit.suggestions.authors],
    'repo.owner': kit.suggestions.owners,
  }

  return (seen[field.id] ?? []).map((v) => ({ value: v, label: v }))
}

// Condition is one condition: its field, its operator, and its value or values, with what
// is wrong with it.
function Condition({ kit, kinds, clause, path, index, onChange, onRemove }: ConditionProps) {
  const id = useId()
  const fields = fieldsFor(kit.catalog, kinds)
  const field = kit.catalog.fields.find((f) => f.id === clause.field)
  const ops = field === undefined ? [] : opsFor(kit.catalog, field)
  const op = kit.catalog.ops.find((o) => o.id === clause.op)
  const problems = problemsAt(kit.problems, 'conditions', path)
  const invalid = problems.length > 0
  const describedBy = invalid ? `${id}-notes` : undefined
  const groups = kit.catalog.groups.filter((g) => fields.some((f) => f.group === g.id))
  const name = `Condition ${String(index + 1)}`
  // A field the routine can't read on what it acts on, or one this version doesn't know, is
  // kept as an option, so the menu shows the field the condition holds, with its problem.
  const listed = fields.some((f) => f.id === clause.field)
  const kept: SelectOption<AutopilotFieldID | ''>[] = listed
    ? []
    : [{ value: clause.field ?? '', label: field?.label ?? clause.field ?? 'Pick a field' }]

  // A new field starts at its first operator, with no value: a value of another field's
  // means nothing to it.
  const pickField = (fieldID: AutopilotFieldID) => {
    const next = kit.catalog.fields.find((f) => f.id === fieldID)
    const first = next === undefined ? undefined : opsFor(kit.catalog, next)[0]

    onChange(first === undefined ? { field: fieldID } : withOp({ field: fieldID }, first.id, first.arity))
  }

  // A value of the old operator's arity is kept where the new one takes it.
  const pickOp = (opID: AutopilotOpID) => {
    onChange(withOp(clause, opID, kit.catalog.ops.find((o) => o.id === opID)?.arity ?? 'none'))
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-start gap-1.5">
        <label htmlFor={`${id}-field`} className="sr-only">
          {`${name}: field`}
        </label>
        <Select<AutopilotFieldID | ''>
          id={`${id}-field`}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          className={`${fieldClass} py-1`}
          value={clause.field ?? ''}
          options={[
            ...kept,
            ...groups.map((g) => ({
              group: g.label,
              options: fields.filter((f) => f.group === g.id).map((f) => ({ value: f.id, label: f.label })),
            })),
          ]}
          onChange={(v) => {
            if (v !== '') {
              pickField(v)
            }
          }}
        />
        <label htmlFor={`${id}-op`} className="sr-only">
          {`${name}: how it compares`}
        </label>
        <Select<AutopilotOpID | ''>
          id={`${id}-op`}
          title={op?.help}
          className={`${fieldClass} py-1`}
          value={clause.op ?? ''}
          options={ops.map((o) => ({ value: o.id, label: o.label }))}
          onChange={(v) => {
            if (v !== '') {
              pickOp(v)
            }
          }}
        />
        {field !== undefined && op?.arity === 'many' && (
          <div className="min-w-48 flex-1">
            <TokenField
              id={`${id}-values`}
              label={`${name}: values`}
              placeholder={field.choices.length > 0 ? 'Pick…' : 'Type, then Enter'}
              tokens={clause.values ?? []}
              onChange={(values) => {
                onChange({ ...clause, values })
              }}
              tokenLabel={(v) => field.choices.find((c) => c.value.toLowerCase() === v.toLowerCase())?.label ?? v}
              suggestions={valueSuggestions(kit, field)}
              choicesOnly={field.choices.length > 0}
              invalid={invalid}
              describedBy={describedBy}
            />
          </div>
        )}
        {field !== undefined && op?.arity === 'one' && (
          <OneValue
            id={`${id}-value`}
            name={name}
            field={field}
            clause={clause}
            onChange={onChange}
            invalid={invalid}
            describedBy={describedBy}
            kit={kit}
          />
        )}
        <button
          type="button"
          aria-label={`Remove condition ${String(index + 1)}`}
          title="Remove"
          onClick={onRemove}
          className={`${dangerIconButtonClass} ml-auto`}
        >
          <TrashIcon className="size-3.5" />
        </button>
      </div>
      {op?.help !== undefined && op.help !== '' && <p className="text-xs text-muted">{op.help}</p>}
      <Notes id={`${id}-notes`} problems={problems} />
    </div>
  )
}

interface OneValueProps {
  id: string
  name: string
  field: AutopilotField
  clause: AutopilotClause
  onChange: (clause: AutopilotClause) => void
  invalid: boolean
  // describedBy names what says why it is invalid.
  describedBy: string | undefined
  kit: BuilderKit
}

// OneValue is an operator's one value: a number, a length in minutes, hours, or days, or a
// piece of text, offering what the stored items hold of it.
function OneValue({ id, name, field, clause, onChange, invalid, describedBy, kit }: OneValueProps) {
  const value = clause.value ?? ''

  if (field.type === 'duration') {
    const { n, unit } = lengthParts(value === '' ? '1d' : value)

    return (
      <span className="inline-flex items-center gap-1.5">
        <label htmlFor={id} className="sr-only">
          {`${name}: how long`}
        </label>
        <input
          id={id}
          type="number"
          inputMode="numeric"
          min={1}
          value={value === '' ? '' : n}
          placeholder="1"
          aria-invalid={invalid}
          aria-describedby={describedBy}
          onChange={(e) => {
            onChange({ ...clause, value: e.target.value === '' ? '' : `${e.target.value}${unit}` })
          }}
          className={`${fieldClass} w-16 py-1 text-right tabular-nums`}
        />
        <label htmlFor={`${id}-unit`} className="sr-only">
          {`${name}: unit`}
        </label>
        <select
          id={`${id}-unit`}
          value={unit}
          onChange={(e) => {
            onChange({ ...clause, value: `${n === '' ? '1' : n}${e.target.value}` })
          }}
          className={`${fieldClass} py-1`}
        >
          <option value="m">minutes</option>
          <option value="h">hours</option>
          <option value="d">days</option>
        </select>
      </span>
    )
  }

  const offered = valueSuggestions(kit, field)

  return (
    <>
      <label htmlFor={id} className="sr-only">
        {`${name}: value`}
      </label>
      <input
        id={id}
        type={field.type === 'number' ? 'number' : 'text'}
        inputMode={field.type === 'number' ? 'numeric' : undefined}
        list={offered.length > 0 ? `${id}-offered` : undefined}
        value={value}
        spellCheck={false}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        onChange={(e) => {
          onChange({ ...clause, value: e.target.value })
        }}
        className={`${fieldClass} min-w-32 flex-1 py-1 ${field.type === 'number' ? 'max-w-24 text-right tabular-nums' : ''}`}
      />
      {offered.length > 0 && (
        <datalist id={`${id}-offered`}>
          {offered.map((o) => (
            <option key={o.value} value={o.value} />
          ))}
        </datalist>
      )}
    </>
  )
}
