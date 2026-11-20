package spec

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// conditions checks the routine's conditions against the kinds the watch holds.
func (v *validator) conditions(c Conditions, kinds []item.Kind) {
	count := len(c.Of)

	for _, cl := range c.Of {
		if cl.Group != nil {
			count += len(cl.Group.Of)
		}
	}

	if count > MaxConditions {
		v.add(PartConditions, "of", "use at most %d conditions", MaxConditions)
	}

	v.list("", c, kinds, true)
}

// list checks one list of conditions; a group may hold no group itself.
func (v *validator) list(field string, c Conditions, kinds []item.Kind, top bool) {
	at := func(s string) string {
		if field == "" {
			return s
		}

		return field + "." + s
	}

	if !member(c.Match) {
		v.add(PartConditions, at("match"), "match %s of the conditions", choices[Match]())
	}

	if !top && len(c.Of) == 0 {
		v.add(PartConditions, at("of"), "add a condition to the group, or remove it")
	}

	for i, cl := range c.Of {
		v.member(at(fmt.Sprintf("of[%d]", i)), cl, kinds, top)
	}
}

// member checks one member of a list: a condition, or, in the top list only, a group.
func (v *validator) member(field string, cl Clause, kinds []item.Kind, top bool) {
	switch {
	case cl.Group != nil && (cl.Field != "" || cl.Op != "" || cl.Value != "" || cl.Values != nil):
		v.add(PartConditions, field, "a condition is a field or a group, not both")
	case cl.Group != nil && !top:
		v.add(PartConditions, field+".group", "groups go one level deep")
	case cl.Group != nil:
		v.list(field+".group", *cl.Group, kinds, false)
	default:
		v.clause(field, cl, kinds)
	}
}

// clause checks one condition: a field the catalog has, an operator it takes, and the
// values the operator needs.
func (v *validator) clause(field string, c Clause, kinds []item.Kind) {
	f, ok := catalog.FieldByID(c.Field)
	if !ok {
		v.add(PartConditions, field+".field", "%q isn't a field", c.Field)

		return
	}

	if !fits(kinds, f.Applies) {
		v.add(PartConditions, field+".field", "%s is only a pull request's, and this routine watches only %s",
			f.Label, kindWords(kinds))
	}

	op, ok := catalog.OpByID(c.Op)
	if !ok || !catalog.Takes(f, c.Op) {
		v.add(PartConditions, field+".op", "%s doesn't take %q", f.Label, c.Op)

		return
	}

	switch op.Arity {
	case catalog.ArityNone:
		if c.Value != "" || c.Values != nil {
			v.add(PartConditions, field+".value", "%q takes no value", op.Label)
		}
	case catalog.ArityOne:
		v.one(field, f, op, c)
	case catalog.ArityMany:
		v.many(field, f, op, c)
	}
}

// one checks an operator's one value.
func (v *validator) one(field string, f catalog.Field, op catalog.OpInfo, c Clause) {
	if c.Values != nil {
		v.add(PartConditions, field+".values", "%q takes one value, in \"value\"", op.Label)
	}

	if msg := checkText(c.Value, MaxTextRunes); msg != "" {
		v.add(PartConditions, field+".value", "%s", msg)

		return
	}

	if msg := checkOne(f.Type, op.ID, c.Value); msg != "" {
		v.add(PartConditions, field+".value", "%s", msg)
	}
}

// checkOne says what is wrong with an operator's one value for a field of type t, or "".
func checkOne(t catalog.Type, op catalog.Op, value string) string {
	switch {
	case t == catalog.TypeNumber:
		if n, err := strconv.Atoi(value); err != nil || n < 0 || n > maxNumber || strconv.Itoa(n) != value {
			return fmt.Sprintf("write a whole number from 0 to %d", maxNumber)
		}
	case t == catalog.TypeDuration:
		if _, err := catalog.ParseLength(value); err != nil {
			return err.Error()
		}
	case op == catalog.OpMatchesRegex:
		if _, err := regexp.Compile("(?i)" + value); err != nil {
			return "the regex doesn't compile: " + err.Error()
		}
	}

	return ""
}

// many checks an operator's values: some, not too many, each one a choice when the field
// has them, and each a pattern that compiles when the field's are paths.
func (v *validator) many(field string, f catalog.Field, op catalog.OpInfo, c Clause) {
	if c.Value != "" {
		v.add(PartConditions, field+".value", "%q takes values, in \"values\"", op.Label)
	}

	if len(c.Values) == 0 {
		v.add(PartConditions, field+".values", "give at least one value")
	}

	v.texts(PartConditions, field+".values", c.Values, MaxValues)

	for i, value := range c.Values {
		if msg := checkMany(f, op.ID, value); msg != "" {
			v.add(PartConditions, fmt.Sprintf("%s.values[%d]", field, i), "%s", msg)
		}
	}
}

// checkMany says what is wrong with one of an operator's values for field f, or "".
func checkMany(f catalog.Field, op catalog.Op, value string) string {
	if len(f.Choices) > 0 {
		if _, ok := f.Choice(value); !ok {
			return fmt.Sprintf("%q isn't one of %s's choices", value, f.Label)
		}

		return ""
	}

	matching := op == catalog.OpAnyMatches || op == catalog.OpAllMatch
	if matching && f.Patterns == catalog.PatternsGitignore {
		if _, err := sensitive.CompilePattern(value); err != nil {
			return err.Error()
		}
	}

	return ""
}
