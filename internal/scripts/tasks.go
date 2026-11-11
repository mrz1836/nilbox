package scripts

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
)

// The task types the app runs.
const (
	typeShell   = "shell"
	typeProcess = "process"
)

// orderSequence is dependsOrder's value for dependencies that run one after another.
const orderSequence = "sequence"

// maxTasks bounds the tasks the app reads from a tasks.json, and maxChain the commands one
// task's dependencies may run, so a file can't make a listing slow.
const (
	maxTasks = 500
	maxChain = 64
)

// envName matches a variable name a program can take.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// tasksDoc is what the app reads of a tasks.json: its tasks and inputs, each read on its
// own so one the app can't read greys only itself, and the options every task starts from.
type tasksDoc struct {
	Tasks   []json.RawMessage `json:"tasks"`
	Inputs  []json.RawMessage `json:"inputs"`
	Options *optionsDoc       `json:"options"`
}

// taskDoc is the subset of a task the app reads. problemMatcher, presentation, group, and
// the rest are ignored.
type taskDoc struct {
	Label        string            `json:"label"`
	Type         string            `json:"type"`
	Command      json.RawMessage   `json:"command"`
	Args         []json.RawMessage `json:"args"`
	Detail       string            `json:"detail"`
	Options      *optionsDoc       `json:"options"`
	Osx          *platformDoc      `json:"osx"`
	DependsOn    json.RawMessage   `json:"dependsOn"`
	DependsOrder string            `json:"dependsOrder"`
	IsBackground bool              `json:"isBackground"`
}

// platformDoc is a task's osx section: what it changes on macOS, where the app runs.
type platformDoc struct {
	Command json.RawMessage   `json:"command"`
	Args    []json.RawMessage `json:"args"`
	Options *optionsDoc       `json:"options"`
}

// optionsDoc is a task's options: its folder, its variables, and its shell.
type optionsDoc struct {
	Cwd   *string           `json:"cwd"`
	Env   map[string]string `json:"env"`
	Shell *shellSpec        `json:"shell"`
}

// inputDoc is one of tasks.json's inputs.
type inputDoc struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Default     string            `json:"default"`
	Options     []json.RawMessage `json:"options"`
	Password    bool              `json:"password"`
}

// taskForm is a task's effective form, after the osx section and the file's options, with
// every ${…} as written, whose hash its trust is. A dependency is its own form's hash.
type taskForm struct {
	Source    Source      `json:"source"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Command   string      `json:"command"`
	Args      []string    `json:"args"`
	Cwd       string      `json:"cwd"`
	Env       [][2]string `json:"env"`
	Shell     *shellSpec  `json:"shell"`
	Inputs    []Input     `json:"inputs"`
	Order     string      `json:"order"`
	DependsOn []string    `json:"dependsOn"`
}

// taskDef is one task as read, before its dependencies are followed.
type taskDef struct {
	label, detail string

	// own is its own command, if it has one, and form its effective form, its
	// dependencies' hashes still to come.
	own  *part
	form taskForm

	// deps are the labels it depends on, and sequence reports they run one after
	// another in VS Code too.
	deps     []string
	sequence bool

	background bool

	// inputs are the inputs its own command uses, and why says why it can't run on its
	// own account, or "".
	inputs map[string]Input
	why    string
}

// chained is a task with its dependencies followed: every part it runs, each with the
// task it comes from, its hash, and the inputs every part asks for.
type chained struct {
	parts  []labeled
	hash   string
	inputs map[string]Input
	why    string
}

// labeled is a part and the task it comes from, which runs once in a chain.
type labeled struct {
	label string
	part  part
}

// tasksFileReader follows a tasks.json's tasks and their dependencies.
type tasksFileReader struct {
	defs   []*taskDef
	byName map[string]*taskDef
	done   map[string]*chained
	going  map[string]bool
}

// readTasks reads the tasks in the .vscode/tasks.json of the clone at dir, in its order.
// found reports the file is there; an error says why it couldn't be read.
func readTasks(dir string) (out []script, found bool, err error) {
	data, found, err := readSource(dir, tasksFile)
	if err != nil || !found {
		return nil, found, err
	}

	var doc tasksDoc
	if err = decodeJSONC(data, &doc); err != nil {
		return nil, true, invalidJSON(tasksFile, err)
	}

	inputs := readInputs(doc.Inputs)
	r := &tasksFileReader{byName: map[string]*taskDef{}, done: map[string]*chained{}, going: map[string]bool{}}

	for _, raw := range doc.Tasks[:min(len(doc.Tasks), maxTasks)] {
		def := readTask(raw, doc.Options, inputs)
		if def == nil {
			continue
		}

		if _, dup := r.byName[def.label]; dup {
			def.why = "Another task in " + tasksFile + " has this label."
		} else {
			r.byName[def.label] = def
		}

		r.defs = append(r.defs, def)
	}

	for _, def := range r.defs {
		out = append(out, r.script(def))
	}

	return out, true, nil
}

// script is the page's script for def: its chain, worked out.
func (r *tasksFileReader) script(def *taskDef) script {
	c := r.follow(def)
	why := cmp.Or(def.why, c.why)

	parts := make([]part, 0, len(c.parts))
	for _, p := range c.parts {
		parts = append(parts, p.part)
	}

	shown := parts
	if why != "" && def.own != nil {
		shown = []part{*def.own}
	}

	sc := script{
		entry: Entry{
			Ref: ref(SourceTasks, def.label), Source: SourceTasks, Name: def.label, Command: displayParts(shown),
			Detail: def.detail, Runnable: why == "", Why: why, Background: def.background,
			Inputs: sortedInputs(c.inputs),
		},
		name: def.label, hash: c.hash, parts: parts, inputs: c.inputs,
	}

	sc.entry.Release = bumps([]string{sc.entry.Command}, nil)

	if len(def.deps) > 1 && !def.sequence {
		sc.entry.Note = "Runs its dependencies one after another, where VS Code runs them side by side."
	}

	return sc
}

// follow works def's chain out: each dependency's chain in order, a task that already
// ran in the chain left out, then def's own command.
func (r *tasksFileReader) follow(def *taskDef) *chained {
	if c, ok := r.done[def.label]; ok && r.byName[def.label] == def {
		return c
	}

	c := &chained{inputs: maps.Clone(def.inputs)}
	form := def.form
	form.DependsOn = []string{}

	r.going[def.label] = true

	for _, label := range def.deps {
		r.depend(c, &form, label)
	}

	delete(r.going, def.label)

	if def.own != nil {
		c.parts = append(c.parts, labeled{label: def.label, part: *def.own})
	}

	switch {
	case len(c.parts) > maxChain:
		c.why = cmp.Or(c.why, fmt.Sprintf("Its dependencies run more than %d commands.", maxChain))
	case len(c.parts) == 0 && c.why == "":
		c.why = "Has no command to run."
	}

	c.hash = hashOf(form)

	if r.byName[def.label] == def {
		r.done[def.label] = c
	}

	return c
}

// depend follows the dependency called label for c: its chain's parts c doesn't run yet,
// its inputs, and its hash into form, or why c can't run because of it.
func (r *tasksFileReader) depend(c *chained, form *taskForm, label string) {
	dep, ok := r.byName[label]

	switch {
	case !ok:
		c.why = cmp.Or(c.why, fmt.Sprintf("Depends on %q, which isn't in %s.", label, tasksFile))

		return
	case r.going[label]:
		c.why = cmp.Or(c.why, "Its dependencies loop back to it.")

		return
	}

	got := r.follow(dep)

	switch {
	case got.why != "" || dep.why != "":
		c.why = cmp.Or(c.why, fmt.Sprintf("Depends on %q: %s", label, cmp.Or(dep.why, got.why)))
	case dep.background:
		c.why = cmp.Or(c.why, fmt.Sprintf("Depends on %q, which runs in the background and doesn't end.", label))
	}

	form.DependsOn = append(form.DependsOn, got.hash)
	maps.Copy(c.inputs, got.inputs)

	for _, p := range got.parts {
		if len(c.parts) >= maxChain {
			c.why = cmp.Or(c.why, fmt.Sprintf("Its dependencies run more than %d commands.", maxChain))

			return
		}

		if !slices.ContainsFunc(c.parts, func(have labeled) bool { return have.label == p.label }) {
			c.parts = append(c.parts, p)
		}
	}
}

// readTask reads one task, with the file's options under its own and its osx section over
// both; nil for one with no label, which nothing can name.
func readTask(raw json.RawMessage, global *optionsDoc, inputs map[string]inputDoc) *taskDef {
	var doc taskDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return unreadableTask(raw)
	}

	def := &taskDef{
		label: strings.TrimSpace(doc.Label), detail: doc.Detail, background: doc.IsBackground,
		sequence: doc.DependsOrder == orderSequence, inputs: map[string]Input{},
	}
	if def.label == "" {
		return nil
	}

	command, args, opts := effective(doc, global)
	words, wordsOK := commandWords(command)
	list, argsOK := argWords(args)
	deps, depsOK := dependsOn(doc.DependsOn)

	def.deps = deps
	def.form = formOf(def.label, doc, words, list, opts)

	switch {
	case !wordsOK || !argsOK:
		def.why = "Has a command or an argument written in a way " + brand.Name + " can't read."
	case !depsOK:
		def.why = "Names a dependency in a way " + brand.Name + " can't follow."
	default:
		def.why = def.check(inputs)
	}

	def.form.Inputs = sortedInputs(def.inputs)
	def.own = ownPart(def.form)

	return def
}

// unreadableTask is a task that doesn't decode as one: greyed, by its label, or nil when
// it has none.
func unreadableTask(raw json.RawMessage) *taskDef {
	var named struct {
		Label string `json:"label"`
	}

	if json.Unmarshal(raw, &named) != nil || strings.TrimSpace(named.Label) == "" {
		return nil
	}

	return &taskDef{
		label: strings.TrimSpace(named.Label), inputs: map[string]Input{},
		why: "Is written in a way " + brand.Name + " can't read.",
	}
}

// formOf is a task's effective form, its inputs and dependencies still to come.
func formOf(label string, doc taskDoc, command string, args []string, opts optionsDoc) taskForm {
	f := taskForm{
		Source: SourceTasks, Name: label, Type: doc.Type, Command: command, Args: args, Env: sortedEnv(opts.Env),
		Shell: opts.Shell, Order: cmp.Or(doc.DependsOrder, "parallel"),
	}

	if opts.Cwd != nil {
		f.Cwd = *opts.Cwd
	}

	if doc.Type == typeProcess {
		f.Shell = nil // a process runs with no shell
	}

	return f
}

// ownPart is the part a task's own command runs, or nil for a task with none.
func ownPart(f taskForm) *part {
	if strings.TrimSpace(f.Command) == "" {
		return nil
	}

	p := &part{kind: partShell, command: f.Command, args: f.Args, cwd: f.Cwd, env: f.Env, shell: f.Shell}
	if f.Type == typeProcess {
		p.kind = partProcess
	}

	return p
}

// check returns why def can't run on its own account, or "": a type the app doesn't run,
// a variable name a program can't take, a variable it can't fill in, or an input it can't
// ask for. It collects the inputs def's command uses.
func (def *taskDef) check(inputs map[string]inputDoc) string {
	if why := formProblem(def.form); why != "" {
		return why
	}

	for _, w := range def.form.words() {
		for _, m := range variable.FindAllStringSubmatch(w, -1) {
			if why := def.use(m[1], inputs); why != "" {
				return why
			}
		}
	}

	return ""
}

// formProblem says why a task's form can't run, or "": a type the app doesn't run, or a
// variable name a program can't take.
func formProblem(f taskForm) string {
	switch {
	case f.Type == "" && strings.TrimSpace(f.Command) != "":
		return "Has no type: shell or process."
	case f.Type != "" && f.Type != typeShell && f.Type != typeProcess:
		return fmt.Sprintf("A task of type %q, which only VS Code runs.", f.Type)
	}

	for _, kv := range f.Env {
		if !envName.MatchString(kv[0]) {
			return fmt.Sprintf("Sets a variable named %q, which a program can't take.", kv[0])
		}
	}

	return runProblem(f)
}

// runProblem says why the app can't run a task's form as it is written, or "": a
// character that hides what it runs, a program env would take for a variable, or
// arguments for a shell whose quoting differs from the POSIX shells'.
func runProblem(f taskForm) string {
	shell := ""
	if f.Shell != nil {
		shell = f.Shell.Executable
	}

	switch {
	case anyHidden(f.words()...):
		return hiddenWhy
	case f.Type == typeProcess && len(f.Env) > 0 && strings.ContainsRune(f.Command, '='):
		return envProgramWhy
	case shell != "" && !strings.Contains(shell, "${") && !posixShell(shell) && len(f.Args) > 0:
		return shellWhy(shell)
	default:
		return ""
	}
}

// words returns every word of a task's form that may hold a variable.
func (f taskForm) words() []string {
	words := append([]string{f.Command, f.Cwd}, f.Args...)
	for _, kv := range f.Env {
		words = append(words, kv[1])
	}

	if f.Shell != nil {
		words = append(append(words, f.Shell.Executable), f.Shell.Args...)
	}

	return words
}

// use checks the variable called name, collecting the input it names, and says why the app
// can't fill it in, or "".
func (def *taskDef) use(name string, inputs map[string]inputDoc) string {
	if why := variableProblem(name); why != "" {
		return why
	}

	id, ok := strings.CutPrefix(name, "input:")
	if !ok {
		return ""
	}

	in, ok := inputs[id]

	switch {
	case !ok:
		return "Uses ${" + name + "}, which " + tasksFile + " doesn't define."
	case in.Type == string(InputPrompt) && in.Password:
		return "Asks for a password (${" + name + "}), which " + brand.Name + " doesn't take."
	case in.Type != string(InputPick) && in.Type != string(InputPrompt):
		return fmt.Sprintf("Uses ${%s}, an input of type %q, which %s.", name, in.Type, editorWord)
	}

	input, why := inputOf(in)
	if why != "" {
		return why
	}

	def.inputs[id] = input

	return ""
}

// effective returns a task's command, arguments, and options on macOS: its osx section's
// over its own, and its own options over the file's, variables merged by name.
func effective(doc taskDoc, global *optionsDoc) (command json.RawMessage, args []json.RawMessage, opts optionsDoc) {
	command, args = doc.Command, doc.Args
	layers := []*optionsDoc{global, doc.Options}

	if osx := doc.Osx; osx != nil {
		if len(osx.Command) > 0 {
			command = osx.Command
		}

		if osx.Args != nil {
			args = osx.Args
		}

		layers = append(layers, osx.Options)
	}

	return command, args, merged(layers)
}

// merged is layers of options, each over the ones before it: its folder and its shell, and
// its variables by name.
func merged(layers []*optionsDoc) optionsDoc {
	out := optionsDoc{Env: map[string]string{}}

	for _, o := range layers {
		if o != nil {
			out.Cwd, out.Shell = cmp.Or(o.Cwd, out.Cwd), cmp.Or(o.Shell, out.Shell)
			maps.Copy(out.Env, o.Env)
		}
	}

	return out
}

// commandWords reads a task's command: a string, a list of words VS Code joins with
// spaces, or {"value": …}; none is "". ok is false for anything else.
func commandWords(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}

	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, " "), true
	}

	return word(raw)
}

// argWords reads a task's arguments, each a string or {"value": …}.
func argWords(raw []json.RawMessage) ([]string, bool) {
	out := make([]string, 0, len(raw))

	for _, r := range raw {
		w, ok := word(r)
		if !ok {
			return nil, false
		}

		out = append(out, w)
	}

	return out, true
}

// word reads a string, or VS Code's quoted form {"value": …, "quoting": …}, whose value
// the app quotes as a POSIX shell needs.
func word(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}

	var q struct {
		Value *string `json:"value"`
	}

	if json.Unmarshal(raw, &q) == nil && q.Value != nil {
		return *q.Value, true
	}

	return "", false
}

// dependsOn reads a task's dependencies: one label or a list of them. ok is false for a
// dependency named another way, as by its type.
func dependsOn(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, true
	}

	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{strings.TrimSpace(one)}, true
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return []string{}, false
	}

	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}

	return list, true
}

// readInputs reads tasks.json's inputs by ID; one that can't be read is left out, and a
// task that uses it says so.
func readInputs(raw []json.RawMessage) map[string]inputDoc {
	out := map[string]inputDoc{}

	for _, r := range raw {
		var in inputDoc
		if json.Unmarshal(r, &in) == nil && in.ID != "" {
			if _, dup := out[in.ID]; !dup {
				out[in.ID] = in
			}
		}
	}

	return out
}

// inputOf turns a pickString or a promptString into the form's Input, or says why it
// can't ask for it.
func inputOf(in inputDoc) (Input, string) {
	out := Input{ID: in.ID, Kind: InputKind(in.Type), Description: in.Description, Default: in.Default, Options: []Option{}}

	if out.Kind == InputPick {
		options, why := pickOptions(in)
		if why != "" {
			return out, why
		}

		out.Options = options
	}

	if anyHidden(out.Default) || slices.ContainsFunc(out.Options, func(o Option) bool { return anyHidden(o.Label, o.Value) }) {
		return out, hiddenWhy
	}

	return out, ""
}

// pickOptions reads a pickString's choices, each a string or {label, value}, or says why it
// can't.
func pickOptions(in inputDoc) ([]Option, string) {
	out := make([]Option, 0, len(in.Options))

	for _, raw := range in.Options {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out = append(out, optionOf(s, s))

			continue
		}

		var o struct {
			Label string `json:"label"`
			Value string `json:"value"`
		}

		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, "Its input " + in.ID + " has a choice " + brand.Name + " can't read."
		}

		out = append(out, optionOf(cmp.Or(o.Label, o.Value), o.Value))
	}

	if len(out) == 0 {
		return nil, "Its input " + in.ID + " has no choices."
	}

	return out, ""
}

// sortedInputs returns inputs by ID.
func sortedInputs(inputs map[string]Input) []Input {
	out := make([]Input, 0, len(inputs))
	for _, id := range slices.Sorted(maps.Keys(inputs)) {
		out = append(out, inputs[id])
	}

	return out
}

// sortedEnv returns a task's variables by name.
func sortedEnv(env map[string]string) [][2]string {
	out := make([][2]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, [2]string{k, env[k]})
	}

	return out
}
