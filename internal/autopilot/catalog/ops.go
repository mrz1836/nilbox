package catalog

import "slices"

// Type is the type of value a field holds, which decides the operators it takes.
type Type string

// The types of field.
const (
	// TypeText is one piece of text, compared ignoring case.
	TypeText Type = "text"

	// TypeList is a list of text, such as the labels or the failed checks' names.
	TypeList Type = "list"

	// TypeNumber is a whole number, such as the lines changed.
	TypeNumber Type = "number"

	// TypeBool is yes or no.
	TypeBool Type = "bool"

	// TypeChoice is one of the field's choices, such as the trust class.
	TypeChoice Type = "choice"

	// TypeDuration is how long ago something happened, such as the last activity.
	TypeDuration Type = "duration"
)

// Types returns every type of field.
func Types() []Type {
	return []Type{TypeText, TypeList, TypeNumber, TypeBool, TypeChoice, TypeDuration}
}

// Op is a condition's operator. Each belongs to one type of field.
type Op string

// The text operators. Text compares ignore case; a pattern takes * for any run of
// characters and ? for one; a regex is Go's RE2, so matching can't run away.
const (
	OpIs           Op = "is"
	OpIsNot        Op = "isNot"
	OpContains     Op = "contains"
	OpNotContains  Op = "notContains"
	OpStartsWith   Op = "startsWith"
	OpMatches      Op = "matches"
	OpMatchesRegex Op = "matchesRegex"
)

// The list operators. A field whose list holds one of its choices, such as the inbox
// lists, takes only the first three and the emptiness tests; one whose list holds paths
// matches them as .gitignore does.
const (
	OpHasAny       Op = "hasAny"
	OpHasAll       Op = "hasAll"
	OpHasNone      Op = "hasNone"
	OpAnyContains  Op = "anyContains"
	OpNoneContains Op = "noneContains"
	OpAnyMatches   Op = "anyMatches"
	OpAllMatch     Op = "allMatch"
	OpIsEmpty      Op = "isEmpty"
	OpNotEmpty     Op = "notEmpty"
)

// The number operators.
const (
	OpEq  Op = "eq"
	OpNe  Op = "ne"
	OpLt  Op = "lt"
	OpLte Op = "lte"
	OpGt  Op = "gt"
	OpGte Op = "gte"
)

// The yes-or-no operators.
const (
	OpIsYes Op = "isYes"
	OpIsNo  Op = "isNo"
)

// The choice operators.
const (
	OpOneOf  Op = "oneOf"
	OpNoneOf Op = "noneOf"
)

// The duration operators, against a length such as "30m", "2h", or "14d".
const (
	OpMoreThan Op = "moreThan"
	OpLessThan Op = "lessThan"
)

// Arity is how many values an operator compares against.
type Arity string

// The arities.
const (
	// ArityNone takes no value: is yes, is empty.
	ArityNone Arity = "none"

	// ArityOne takes one value, in the condition's "value".
	ArityOne Arity = "one"

	// ArityMany takes one or more, in the condition's "values".
	ArityMany Arity = "many"
)

// OpInfo describes an operator for the builder and the evaluator.
type OpInfo struct {
	ID Op `json:"id"`

	// Type is the type of field it applies to.
	Type Type `json:"type"`

	// Label is what the builder's menu shows: "contains any of".
	Label string `json:"label"`

	// Arity is how many values it takes.
	Arity Arity `json:"arity"`

	// Negation is the operator that says the opposite of this one about a known value,
	// or "" when none does. Either says nothing about a value that isn't known: both are
	// unknown then.
	Negation Op `json:"negation"`
}

// Ops returns every operator, grouped by type in Types' order.
func Ops() []OpInfo {
	return []OpInfo{
		{ID: OpIs, Type: TypeText, Label: "is", Arity: ArityOne, Negation: OpIsNot},
		{ID: OpIsNot, Type: TypeText, Label: "isn't", Arity: ArityOne, Negation: OpIs},
		{ID: OpContains, Type: TypeText, Label: "contains", Arity: ArityOne, Negation: OpNotContains},
		{ID: OpNotContains, Type: TypeText, Label: "doesn't contain", Arity: ArityOne, Negation: OpContains},
		{ID: OpStartsWith, Type: TypeText, Label: "starts with", Arity: ArityOne},
		{ID: OpMatches, Type: TypeText, Label: "matches the pattern", Arity: ArityOne},
		{ID: OpMatchesRegex, Type: TypeText, Label: "matches the regex", Arity: ArityOne},

		{ID: OpHasAny, Type: TypeList, Label: "has any of", Arity: ArityMany, Negation: OpHasNone},
		{ID: OpHasAll, Type: TypeList, Label: "has all of", Arity: ArityMany},
		{ID: OpHasNone, Type: TypeList, Label: "has none of", Arity: ArityMany, Negation: OpHasAny},
		{ID: OpAnyContains, Type: TypeList, Label: "contains any of", Arity: ArityMany, Negation: OpNoneContains},
		{ID: OpNoneContains, Type: TypeList, Label: "contains none of", Arity: ArityMany, Negation: OpAnyContains},
		{ID: OpAnyMatches, Type: TypeList, Label: "any matches", Arity: ArityMany},
		{ID: OpAllMatch, Type: TypeList, Label: "all match", Arity: ArityMany},
		{ID: OpIsEmpty, Type: TypeList, Label: "is empty", Arity: ArityNone, Negation: OpNotEmpty},
		{ID: OpNotEmpty, Type: TypeList, Label: "isn't empty", Arity: ArityNone, Negation: OpIsEmpty},

		{ID: OpEq, Type: TypeNumber, Label: "=", Arity: ArityOne, Negation: OpNe},
		{ID: OpNe, Type: TypeNumber, Label: "≠", Arity: ArityOne, Negation: OpEq},
		{ID: OpLt, Type: TypeNumber, Label: "<", Arity: ArityOne, Negation: OpGte},
		{ID: OpLte, Type: TypeNumber, Label: "≤", Arity: ArityOne, Negation: OpGt},
		{ID: OpGt, Type: TypeNumber, Label: ">", Arity: ArityOne, Negation: OpLte},
		{ID: OpGte, Type: TypeNumber, Label: "≥", Arity: ArityOne, Negation: OpLt},

		{ID: OpIsYes, Type: TypeBool, Label: "is yes", Arity: ArityNone, Negation: OpIsNo},
		{ID: OpIsNo, Type: TypeBool, Label: "is no", Arity: ArityNone, Negation: OpIsYes},

		{ID: OpOneOf, Type: TypeChoice, Label: "is any of", Arity: ArityMany, Negation: OpNoneOf},
		{ID: OpNoneOf, Type: TypeChoice, Label: "is none of", Arity: ArityMany, Negation: OpOneOf},

		{ID: OpMoreThan, Type: TypeDuration, Label: "more than", Arity: ArityOne},
		{ID: OpLessThan, Type: TypeDuration, Label: "less than", Arity: ArityOne},
	}
}

// choiceListOps are the list operators a field whose list holds its choices takes.
func choiceListOps() []Op {
	return []Op{OpHasAny, OpHasAll, OpHasNone, OpIsEmpty, OpNotEmpty}
}

// OpsFor returns the operators a field takes, in Ops' order.
func OpsFor(f Field) []Op {
	ops := Ops()
	out := make([]Op, 0, len(ops))

	for _, o := range ops {
		if Takes(f, o.ID) {
			out = append(out, o.ID)
		}
	}

	return out
}

// Takes reports whether a field takes an operator: one of its type's, and for a list of
// choices only the set operators.
func Takes(f Field, op Op) bool {
	o, ok := OpByID(op)
	if !ok || o.Type != f.Type {
		return false
	}

	return f.Type != TypeList || len(f.Choices) == 0 || slices.Contains(choiceListOps(), op)
}
