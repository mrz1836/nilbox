package fence

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The files a fenced run keeps in its own folder.
const (
	// PolicyFile is the policy the fence hook reads, in the run's folder.
	PolicyFile = "fence.json"

	// ProofDir is the folder in the run's folder that the sandbox check writes its proof
	// into, the one place outside the worktree a sandboxed command may write.
	ProofDir = "sandbox"

	// proofName is the proof the check writes, and probeName the file it must fail to
	// write, beside the policy.
	proofName = "tried"
	probeName = "canary"

	// PolicyVersion is the policy file's format. A hook that reads another refuses every
	// call, so a run never goes on under a policy its hook misreads.
	PolicyVersion = 1

	// maxPolicyBytes caps the policy file the hook reads.
	maxPolicyBytes = 1 << 20

	// maxCommandRunes caps a routine's extra command.
	maxCommandRunes = 200
)

var (
	// ErrPreset is returned for a tools preset this build doesn't know.
	ErrPreset = errors.New("unknown tools preset")

	// ErrSpec is returned for a run the fence can't be worked out for: a folder that isn't
	// an absolute path, or one whose name the agent's rules can't hold.
	ErrSpec = errors.New("the fence can't be set up for this run")

	// ErrCrossRepository is returned for a push preset on a pull request whose branch is in
	// a fork: the exact push goes to the repository's own branch, which it isn't.
	ErrCrossRepository = errors.New("the pull request's branch is in a fork, which a run can't push to")

	// ErrNoBranch is returned for a push preset on an item with no head branch known.
	ErrNoBranch = errors.New("the pull request's head branch isn't known, so no push can be allowed")

	// ErrNotPlain is returned for a push to a branch or remote whose name isn't plain: one
	// every shell reads as itself, which git can't read as an option or another ref.
	ErrNotPlain = errors.New("isn't a plain name, so no push can be allowed")

	// ErrLoosens is returned for a routine's extra command that would let the agent past
	// one of the fence's hard lines.
	ErrLoosens = errors.New("would loosen the fence")
)

// Command is a command the agent's rules name: its first words, and whether any
// arguments may follow (a prefix) or none (exactly these words).
type Command struct {
	Words  string `json:"words"`
	Prefix bool   `json:"prefix"`
}

// Push is the one push a push preset allows: the pull request's own branch, on the
// clone's remote for the repository. Both names are plain.
type Push struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

// Commands returns the push's exact command lines: the branch named in full, as the
// prompts write it, and as its short name.
func (p Push) Commands() []string {
	return []string{
		"git push " + p.Remote + " HEAD:refs/heads/" + p.Branch,
		"git push " + p.Remote + " HEAD:" + p.Branch,
	}
}

// Canary is the check that a sandboxed run's commands run in the sandbox: one command
// writes Proof, which the sandbox allows, and then Probe, outside the worktree and the
// temporary folder, which it must refuse. Until Proof exists, the fence runs no other
// command; once Probe exists, the sandbox didn't hold, and the fence refuses every call.
type Canary struct {
	Proof string `json:"proof"`
	Probe string `json:"probe"`
}

// Command is the check's exact command line.
func (c Canary) Command() string {
	return "touch " + textutil.ShellWord(c.Proof) + " " + textutil.ShellWord(c.Probe)
}

// Policy is what the fence hook holds a run's tool calls to. Its JSON is the run folder's
// PolicyFile, versioned by Version.
type Policy struct {
	Version int    `json:"version"`
	Preset  Preset `json:"preset"`

	// Worktree is the run's worktree, with its links resolved: file tools write only
	// inside it. WorktreeAs is the path the run names it by, when its links make that
	// another, so an agent's rules name both.
	Worktree   string `json:"worktree"`
	WorktreeAs string `json:"worktreeAs,omitempty"`

	// GitDirs are the worktree's git folder and its clone's, which no file tool writes.
	GitDirs []string `json:"gitDirs"`

	// Home is the user's home folder, for a command's ~.
	Home string `json:"home"`

	// Repo and Number are the run's item, the only one GitHub tasks may write to.
	Repo   string `json:"repo"`
	Number int    `json:"number"`

	// Remote is the clone's remote for the item's repository when its name is plain, for
	// the exact fetch of a pull request's head, or "".
	Remote string `json:"remote"`

	// Push is the one push allowed, or nil for none.
	Push *Push `json:"push,omitempty"`

	// Tools are the repository's tools an edit preset's agent may run.
	Tools []string `json:"tools"`

	// Allow and Deny are the routine's extra commands, by their first words. Allow never
	// loosens a hard line; Deny adds to them.
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`

	// ReadDeny are the folders and files no tool call reads: the user's credentials, the
	// app's data, and the agent's own config.
	ReadDeny []string `json:"readDeny"`

	// Canary is the sandbox check, for a sandboxed preset; nil otherwise.
	Canary *Canary `json:"canary,omitempty"`

	// ShellReads reports an agent with no file tools of its own, which reads through its
	// shell (Codex): under Read only, its commands that only read run, held besides by
	// its own read-only sandbox.
	ShellReads bool `json:"shellReads,omitempty"`
}

// Sandbox is what the agent's own sandbox holds a sandboxed run's commands to.
type Sandbox struct {
	// Write are the folders beside the worktree a command may write: the worktree's git
	// folders, the tools' caches, and the sandbox check's proof.
	Write []string `json:"write"`

	// DenyWrite are paths inside those no command writes: the git folders' hooks and
	// config, and the worktree's own agent, workflow, and tooling files.
	DenyWrite []string `json:"denyWrite"`

	// DenyRead are the paths no command reads.
	DenyRead []string `json:"denyRead"`

	// Domains are the hosts a command may reach.
	Domains []string `json:"domains"`

	// Excluded are the commands that run outside the sandbox: the exact push and fetch,
	// which a sandbox cuts off from the user's SSH key, and gh's reads, which it cuts off
	// from gh's sign-in. The fence lets each through only as one plain command.
	Excluded []Command `json:"excluded"`
}

// Fence is a run's fence: the policy its hook enforces, and for a sandboxed preset, its
// sandbox.
type Fence struct {
	Policy  Policy   `json:"policy"`
	Sandbox *Sandbox `json:"sandbox,omitempty"`
}

// Spec is what a run's fence is worked out from. Every folder is an absolute path.
type Spec struct {
	Preset Preset

	// Worktree is the run's worktree; GitDir and CommonDir its git folder and its clone's,
	// or "" to read them from the worktree's .git file.
	Worktree  string
	GitDir    string
	CommonDir string

	// Repo ("owner/name") and Number are the run's item.
	Repo   string
	Number int

	// Branch is a pull request's head branch, as GitHub reported it, and Remote the
	// clone's remote for its repository; CrossRepository reports a branch in a fork.
	Branch          string
	Remote          string
	CrossRepository bool

	// Tools are the repository's tools (RepoTools).
	Tools []string

	// Allow and Deny are the routine's extra commands.
	Allow []string
	Deny  []string

	// RunDir is the run's own folder, Home the user's, DataDir the app's, and ConfigDir
	// the agent profile's config folder.
	RunDir    string
	Home      string
	DataDir   string
	ConfigDir string

	// Env is the run's environment, read for the tools' cache folders.
	Env map[string]string

	// ShellReads reports an agent that reads through its shell (Policy.ShellReads), as
	// its spec sets it.
	ShellReads bool
}

// Build works a run's fence out from its spec.
func Build(s Spec) (Fence, error) {
	if !s.Preset.Valid() {
		return Fence{}, fmt.Errorf("%w %q", ErrPreset, s.Preset)
	}

	if err := checkFolders(s); err != nil {
		return Fence{}, err
	}

	p, err := policyFor(s)
	if err != nil {
		return Fence{}, err
	}

	f := Fence{Policy: p}

	if s.Preset.Sandboxed() {
		f.Sandbox = sandboxFor(s, p)
	}

	return f, nil
}

// policyFor works a run's policy out from its spec.
func policyFor(s Spec) (Policy, error) {
	p := Policy{
		Version: PolicyVersion, Preset: s.Preset, Worktree: paths.Resolved(s.Worktree), Home: filepath.Clean(s.Home),
		Repo: s.Repo, Number: s.Number, GitDirs: gitDirsFor(s), ShellReads: s.ShellReads,
	}

	if given := filepath.Clean(s.Worktree); given != p.Worktree {
		p.WorktreeAs = given
	}

	if PlainName(s.Remote) {
		p.Remote = s.Remote
	}

	push, ok, err := pushFor(s)
	if err != nil {
		return Policy{}, err
	}

	if ok {
		p.Push = &push
	}

	if p.Tools, err = checkTools(s.Tools); err != nil {
		return Policy{}, err
	}

	if p.Deny, err = checkCommands(s.Deny, "deny"); err != nil {
		return Policy{}, err
	}

	p.ReadDeny = readDenied(s, p.Worktree)

	if s.Preset.Sandboxed() {
		p.Canary = &Canary{Proof: filepath.Join(s.RunDir, ProofDir, proofName), Probe: filepath.Join(s.RunDir, probeName)}
	}

	p.Allow, err = checkAllow(p, s.Allow)

	return p, err
}

// checkFolders refuses a spec whose folders aren't absolute, or hold what an agent's rules
// can't name: a glob, a parenthesis, a quote, or a control character.
func checkFolders(s Spec) error {
	for _, dir := range []struct{ name, path string }{
		{"the worktree", s.Worktree}, {"the run's folder", s.RunDir}, {"home", s.Home},
	} {
		if !filepath.IsAbs(dir.path) {
			return fmt.Errorf("%w: %s isn't an absolute path", ErrSpec, dir.name)
		}

		if !ruleSafe(dir.path) {
			return fmt.Errorf("%w: %s's path holds characters the agent's rules can't name", ErrSpec, dir.name)
		}
	}

	return nil
}

// ruleSafe reports a path or command an agent's rule can hold as it is.
func ruleSafe(s string) bool {
	return !strings.ContainsAny(s, "()*?[]{}'\"\\`") && !strings.ContainsFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f })
}

// pushFor works out the one push a preset allows, and whether it allows one. A push
// preset on a fork's pull request, one whose branch isn't known, or one whose branch or
// remote isn't plain allows none: Edit, test, and push refuses to start; the profile's own
// permissions start with no push.
func pushFor(s Spec) (Push, bool, error) {
	if !s.Preset.MayPush() {
		return Push{}, false, nil
	}

	var err error

	switch {
	case s.CrossRepository:
		err = ErrCrossRepository
	case s.Branch == "" || s.Number <= 0:
		err = ErrNoBranch
	case !PlainBranch(s.Branch):
		err = fmt.Errorf("the pull request's head branch %w", ErrNotPlain)
	case !PlainName(s.Remote):
		err = fmt.Errorf("the clone's remote for the repository %w", ErrNotPlain)
	}

	switch {
	case err == nil:
		return Push{Remote: s.Remote, Branch: s.Branch}, true, nil
	case s.Preset == Profile:
		return Push{}, false, nil
	default:
		return Push{}, false, err
	}
}

// PlainName reports a name a command can use as it is: one every POSIX shell reads as
// itself (textutil.ShellSafe) that git can't read as an option, as the prompts' plain
// names are.
func PlainName(s string) bool {
	return textutil.ShellSafe(s) && !strings.HasPrefix(s, "-")
}

// PlainBranch reports a branch name a push can name as it is: a plain name that is a
// branch git would take, so HEAD:refs/heads/<branch> names that branch and no other ref.
func PlainBranch(s string) bool {
	return PlainName(s) && !strings.ContainsAny(s, ":") && !strings.Contains(s, "..") &&
		!strings.Contains(s, "@{") && !strings.Contains(s, "//") && !strings.HasPrefix(s, "/") &&
		!strings.HasSuffix(s, "/") && !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ".lock")
}

// checkTools keeps the repository's tools that are plain program names, refusing one the
// fence never runs.
func checkTools(tools []string) ([]string, error) {
	out := []string{}

	for _, tool := range tools {
		if !PlainName(tool) || strings.Contains(tool, "/") {
			return nil, fmt.Errorf("%w: the tool %q isn't a program name", ErrSpec, tool)
		}

		if hardProgram(tool) {
			return nil, fmt.Errorf("allowing %s %w", tool, ErrLoosens)
		}

		if !slices.Contains(out, tool) {
			out = append(out, tool)
		}
	}

	return out, nil
}

// hardProgram reports a program a tool or an extra command can never allow by name:
// git and gh, whose subcommands the fence decides, and every program no preset runs.
func hardProgram(name string) bool {
	return name == "git" || name == "gh" || slices.Contains(networkTools(), name) ||
		slices.Contains(shells(), name) || slices.Contains(otherUsers(), name) || name == "eval"
}

// checkCommands checks a routine's extra commands: each is one plain command of words,
// which the agent's rules can hold.
func checkCommands(cmds []string, kind string) ([]string, error) {
	out := []string{}

	for _, raw := range cmds {
		c, err := checkCommand(raw, kind)
		if err != nil {
			return nil, err
		}

		if c != "" && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}

	return out, nil
}

// checkCommand checks one extra command and returns it with its spaces collapsed, or ""
// for a blank one.
func checkCommand(raw, kind string) (string, error) {
	c := strings.Join(strings.Fields(raw), " ")
	if c == "" {
		return "", nil
	}

	if len([]rune(c)) > maxCommandRunes || !ruleSafe(c) || strings.Contains(c, ":") && kind == "allow" {
		return "", fmt.Errorf("%w: the command to %s %q can't be named in a rule", ErrSpec, kind, textutil.Truncate(c, 60))
	}

	s, err := readShell(c)
	if err != nil || s.compound || len(s.commands) != 1 || !plainCommand(s.commands[0]) {
		return "", fmt.Errorf("%w: the command to %s %q isn't one plain command", ErrSpec, kind, textutil.Truncate(c, 60))
	}

	return c, nil
}

// plainCommand reports a simple command of static words alone.
func plainCommand(c simple) bool {
	return len(c.assigns) == 0 && len(c.redirects) == 0 && !anyDynamic(c.words)
}

// checkAllow checks the routine's extra commands to allow: each must pass the fence's
// policy as it stands, and none may name git or gh short of a subcommand, so allowing it
// can never let a hard line's command through the agent's own rules.
func checkAllow(p Policy, allow []string) ([]string, error) {
	cmds, err := checkCommands(allow, "allow")
	if err != nil {
		return nil, err
	}

	for _, c := range cmds {
		first := strings.Fields(c)[0]

		if (first == "git" || first == "gh") && !allowedGitOrGH(c) {
			return nil, fmt.Errorf("allowing %q %w: name the %s commands themselves", c, ErrLoosens, first)
		}

		if v := p.decideCommand(c, p.Worktree, noFS{}); v.Deny {
			return nil, fmt.Errorf("allowing %q %w: %s", c, ErrLoosens, v.Reason)
		}
	}

	return cmds, nil
}

// allowedGitOrGH reports an extra git or gh command named down to its subcommand (and a
// gh group's action), which the policy then decides like any other.
func allowedGitOrGH(c string) bool {
	words := strings.Fields(c)

	switch words[0] {
	case "git":
		return len(words) >= 2 && !strings.HasPrefix(words[1], "-")
	case "gh":
		return len(words) >= 3 && !strings.HasPrefix(words[1], "-") && !strings.HasPrefix(words[2], "-")
	default:
		return false
	}
}

// anyDynamic reports a word the shell changes at run time.
func anyDynamic(words []word) bool {
	return slices.ContainsFunc(words, func(w word) bool { return w.dynamic })
}

// gitDirsFor returns the worktree's git folder and its clone's, as the spec names them or
// as the worktree's .git file does, with links resolved, each once.
func gitDirsFor(s Spec) []string {
	gitDir, common := s.GitDir, s.CommonDir
	if gitDir == "" || common == "" {
		readGit, readCommon := GitDirs(s.Worktree)
		gitDir, common = cmp.Or(gitDir, readGit), cmp.Or(common, readCommon)
	}

	out := []string{}

	for _, dir := range []string{gitDir, common} {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}

		if r := paths.Resolved(dir); !slices.Contains(out, r) {
			out = append(out, r)
		}
	}

	return out
}

// GitDirs reads a worktree's git folder and its clone's from its .git file and that
// folder's commondir, as git does; a worktree whose .git is a folder is its own clone.
// It reads only regular files, at most a few bytes each, and returns "" for what it
// can't read.
func GitDirs(worktree string) (gitDir, common string) {
	dotGit := filepath.Join(worktree, ".git")

	data, err := paths.ReadHead(dotGit, 4096)
	if errors.Is(err, paths.ErrNotRegular) {
		return dotGit, dotGit
	}

	if err != nil {
		return "", ""
	}

	line, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", ""
	}

	gitDir = strings.TrimSpace(line)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}

	gitDir = filepath.Clean(gitDir)

	commonData, err := paths.ReadHead(filepath.Join(gitDir, "commondir"), 4096)
	if err != nil {
		return gitDir, gitDir
	}

	common = strings.TrimSpace(string(commonData))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}

	return gitDir, filepath.Clean(common)
}

// readDenied returns the paths no tool call reads: the user's credentials under home, the
// app's data, and the agent's config, leaving out any that holds the worktree.
func readDenied(s Spec, worktree string) []string {
	var out []string

	add := func(p string) {
		if p == "" || !filepath.IsAbs(p) {
			return
		}

		p = filepath.Clean(p)
		if within(worktree, p) || slices.Contains(out, p) {
			return
		}

		out = append(out, p)
	}

	for _, rel := range homeSecrets() {
		add(filepath.Join(s.Home, filepath.FromSlash(rel)))
	}

	add(s.DataDir)
	add(s.ConfigDir)

	if out == nil {
		return []string{}
	}

	return out
}

// sandboxFor works out a sandboxed preset's sandbox.
func sandboxFor(s Spec, p Policy) *Sandbox {
	sb := &Sandbox{
		Write: []string{p.Worktree}, DenyWrite: []string{},
		// The run's folder, which the sandbox check writes its proof into, stays readable:
		// the hook still refuses a command that names the app's data.
		DenyRead: slices.DeleteFunc(slices.Clone(p.ReadDeny), func(d string) bool { return within(d, s.RunDir) }),
		Domains:  Domains(), Excluded: excludedFor(p),
	}

	sb.Write = append(sb.Write, p.GitDirs...)

	for _, dir := range p.GitDirs {
		sb.DenyWrite = append(sb.DenyWrite, filepath.Join(dir, "hooks"), filepath.Join(dir, "config"),
			filepath.Join(dir, "config.worktree"), filepath.Join(dir, "info"))
	}

	for _, rel := range homeCaches() {
		sb.Write = appendNew(sb.Write, filepath.Join(s.Home, filepath.FromSlash(rel)))
	}

	for _, dir := range envCaches(s.Env, s.Home, p) {
		sb.Write = appendNew(sb.Write, dir)
	}

	sb.Write = appendNew(sb.Write, filepath.Join(s.RunDir, ProofDir))

	for _, rel := range rootDenied() {
		sb.DenyWrite = appendNew(sb.DenyWrite, filepath.Join(p.Worktree, filepath.FromSlash(rel)))
	}

	return sb
}

// appendNew appends v unless list holds it.
func appendNew(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}

	return append(list, v)
}

// excludedFor returns the commands a sandboxed run runs outside its sandbox: the exact
// push, the exact fetch of the pull request's head, its checkout through gh, and gh's
// reads.
func excludedFor(p Policy) []Command {
	var out []Command

	if p.Push != nil {
		for _, c := range p.Push.Commands() {
			out = append(out, Command{Words: c})
		}
	}

	if f := exactFetch(p); f != "" {
		out = append(out, Command{Words: f})
	}

	if p.Number > 0 {
		out = append(out, Command{Words: "gh pr checkout " + strconv.Itoa(p.Number)})
	}

	for _, c := range ghReads() {
		out = append(out, Command{Words: c.words(), Prefix: true})
	}

	return out
}

// exactFetch is the fetch of a pull request's head from the clone's remote for its
// repository, as the prompts write it, or "" without a plain remote.
func exactFetch(p Policy) string {
	if p.Remote == "" || p.Number <= 0 || !p.Preset.Edits() {
		return ""
	}

	return "git fetch " + p.Remote + " refs/pull/" + strconv.Itoa(p.Number) + "/head"
}

// words is the command's words as one line.
func (c ghCommand) words() string {
	return strings.TrimSpace("gh " + c.group + " " + c.action)
}

// envCaches returns the cache folders the run's environment names (GOCACHE, GOMODCACHE,
// GOPATH's pkg, XDG_CACHE_HOME), each kept only when it is a folder under home, at least
// two levels down, that holds no secret, the worktree, or the app's data.
func envCaches(env map[string]string, home string, p Policy) []string {
	var out []string

	candidates := []string{env["GOCACHE"], env["GOMODCACHE"], env["XDG_CACHE_HOME"]}
	if gopath, _, _ := strings.Cut(env["GOPATH"], string(filepath.ListSeparator)); gopath != "" {
		candidates = append(candidates, filepath.Join(gopath, "pkg"))
	}

	for _, dir := range candidates {
		if safeCache(dir, home, p) {
			out = append(out, filepath.Clean(dir))
		}
	}

	return out
}

// safeCache reports a cache folder a sandboxed command may write.
func safeCache(dir, home string, p Policy) bool {
	if dir == "" || !filepath.IsAbs(dir) || !ruleSafe(dir) {
		return false
	}

	dir = filepath.Clean(dir)

	rel, err := filepath.Rel(home, dir)
	if err != nil || strings.HasPrefix(rel, "..") || len(strings.Split(filepath.ToSlash(rel), "/")) < 2 {
		return false
	}

	for _, denied := range append(slices.Clone(p.ReadDeny), p.Worktree) {
		if within(dir, denied) || within(denied, dir) {
			return false
		}
	}

	return true
}

// within reports whether path is dir or inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Worktrees returns the worktree's paths: resolved, then as the run names it when that
// differs.
func (p Policy) Worktrees() []string {
	if p.WorktreeAs == "" {
		return []string{p.Worktree}
	}

	return []string{p.Worktree, p.WorktreeAs}
}

// Encode writes the policy as its file's JSON.
func (p Policy) Encode() ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the fence's policy: %w", err)
	}

	return append(data, '\n'), nil
}

// ErrPolicy is returned for a policy file the fence can't read: missing, too large, not
// its JSON, of another version, or without the folders it needs.
var ErrPolicy = errors.New("the fence's policy can't be read")

// DecodePolicy reads a policy file's JSON, refusing one of another version, with fields
// this build doesn't know, or without a preset and an absolute worktree.
func DecodePolicy(data []byte) (Policy, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("%w: %w", ErrPolicy, err)
	}

	switch {
	case p.Version != PolicyVersion:
		return Policy{}, fmt.Errorf("%w: it is version %d, and this build reads %d", ErrPolicy, p.Version, PolicyVersion)
	case !p.Preset.Valid():
		return Policy{}, fmt.Errorf("%w: %w %q", ErrPolicy, ErrPreset, p.Preset)
	case !filepath.IsAbs(p.Worktree) || !filepath.IsAbs(p.Home):
		return Policy{}, fmt.Errorf("%w: its folders aren't absolute paths", ErrPolicy)
	case p.Canary != nil && (!filepath.IsAbs(p.Canary.Proof) || !filepath.IsAbs(p.Canary.Probe)):
		return Policy{}, fmt.Errorf("%w: its sandbox check's files aren't absolute paths", ErrPolicy)
	}

	return p, nil
}

// ReadPolicy reads the policy file in a run's folder, a regular file of at most 1 MiB.
func ReadPolicy(runDir string) (Policy, error) {
	if !filepath.IsAbs(runDir) {
		return Policy{}, fmt.Errorf("%w: the run's folder isn't an absolute path", ErrPolicy)
	}

	data, err := paths.ReadHead(filepath.Join(runDir, PolicyFile), maxPolicyBytes+1)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: %w", ErrPolicy, err)
	}

	if len(data) > maxPolicyBytes {
		return Policy{}, fmt.Errorf("%w: the file is too large", ErrPolicy)
	}

	return DecodePolicy(data)
}
