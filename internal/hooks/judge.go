package hooks

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// hookLimit caps the hook a scratch repository is given to judge.
	hookLimit = 1 << 20

	// scratchPrefix starts each scratch repository's folder.
	scratchPrefix = "judge-"
)

// judge works out the state of a clone's pre-commit hook: missing, another tool's (named
// where it can be), or go-pre-commit's, executable or not, and current or not as the copy
// in use says of those bytes.
func (s *Service) judge(ctx context.Context, hook hookFile, tool ToolFacts) (State, string) {
	switch {
	case hook.err != nil:
		return StateUnreadable, ""
	case !hook.present:
		return StateMissing, ""
	case !hook.ours:
		return StateOther, hook.tool
	case !hook.executable:
		return StateNotExecutable, ""
	}

	current, known := s.verdict(ctx, hook, tool)

	switch {
	case !known:
		return StateUnknown, ""
	case current:
		return StateReady, ""
	default:
		return StateOutdated, ""
	}
}

// verdict returns whether go-pre-commit's hook with hook's bytes is current for the copy
// in use, and whether that is known: from what that copy said of the same bytes before, or
// else from its status in a scratch repository that holds them.
func (s *Service) verdict(ctx context.Context, hook hookFile, tool ToolFacts) (current, known bool) {
	current, known, _ = s.judgeBytes(ctx, hook, tool)

	return current, known
}

// judgeBytes is verdict, with the error of go-pre-commit's status when it ran and failed,
// which an action's Finish weighs against the app's quit.
func (s *Service) judgeBytes(ctx context.Context, hook hookFile, tool ToolFacts) (current, known bool, err error) {
	if !tool.Usable() || !hook.ours {
		return false, false, nil
	}

	if current, known = s.judged.get(tool.Key(), hook.sha); known {
		return current, true, nil
	}

	data, same := readJudged(hook)
	if !same {
		return false, false, nil
	}

	rep, err := s.statusOf(ctx, data, tool)
	if err != nil {
		s.logger.WarnContext(ctx, "go-pre-commit status failed", "error", err)

		return false, false, err
	}

	switch rep.hooks[hookPreCommit] {
	case statusReady:
		current = true
	case statusOutdated:
		current = false
	default:
		s.logger.WarnContext(ctx, "go-pre-commit status said nothing of the hook it was given")

		return false, false, nil
	}

	s.judged.set(tool.Key(), hook.sha, current)

	return current, true, nil
}

// readJudged reads hook's bytes for status to judge, and reports whether they are the bytes
// it was read with: a hook that changed since, or is too big to be one, isn't judged.
func readJudged(hook hookFile) ([]byte, bool) {
	data, err := paths.ReadHead(hook.path, hookLimit)
	sum := sha256.Sum256(data)

	return data, err == nil && hex.EncodeToString(sum[:]) == hook.sha
}

// statusOf runs `go-pre-commit status` on hook, and reads it: as JSON with a copy that has
// FeatureStatusJSON, else as text. It runs in a scratch repository of the app's own that
// holds hook as its pre-commit hook and an empty config, never in a clone: go-pre-commit
// loads a clone's .env files into its own environment, PATH included, before it runs git,
// and reading a clone runs no program the clone names. The bytes are all the verdict
// depends on.
func (s *Service) statusOf(ctx context.Context, hook []byte, tool ToolFacts) (statusReport, error) {
	dir, err := s.scratchRepo(hook)
	if err != nil {
		return statusReport{}, err
	}

	defer func() { _ = os.RemoveAll(dir) }()

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	base, _ := s.env()
	args := []string{"status", noColor}

	withJSON := tool.Has(integrations.FeatureStatusJSON)
	if withJSON {
		args = append(args, jsonFlag)
	}

	stdout, stderr, err := s.runner(runEnv(base, integrationprefs.RunsInApp), dir).Run(ctx, tool.Path, args...)

	if withJSON {
		if rep, jsonErr := readStatusJSON(stdout); jsonErr == nil || err == nil {
			return rep, jsonErr
		}
	} else if rep, textErr := readStatusText(append(stdout, stderr...)); textErr == nil {
		return rep, nil
	} else if err == nil {
		return statusReport{}, textErr
	}

	return statusReport{}, uierr.New("go-pre-commit status failed: "+execpath.FirstLine(stderr), err)
}

// scratchRepo makes a repository for go-pre-commit's status to judge hook in: the least git
// takes for one (HEAD, objects, and refs), its own core.hooksPath, so a global one doesn't
// send status elsewhere, hook as its pre-commit hook, and an empty modular config, which
// status needs. It returns the repository's folder, which the caller removes.
func (s *Service) scratchRepo(hook []byte) (string, error) {
	root := cmp.Or(s.scratch, os.TempDir())

	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("make the scratch folder: %w", err)
	}

	dir, err := os.MkdirTemp(root, scratchPrefix)
	if err != nil {
		return "", fmt.Errorf("make a scratch repository: %w", err)
	}

	git := filepath.Join(dir, ".git")
	hooks := filepath.Join(git, "hooks")

	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600},
		{filepath.Join(git, "config"), []byte("[core]\n\thooksPath = " + hooks + "\n"), 0o600},
		{filepath.Join(git, "objects", ".keep"), nil, 0o600},
		{filepath.Join(git, "refs", "heads", ".keep"), nil, 0o600},
		{filepath.Join(hooks, hookPreCommit), hook, 0o700},
		{filepath.Join(dir, filepath.FromSlash(envDir), "00-judge"+envExt), nil, 0o600},
	}

	for _, f := range files {
		err = os.MkdirAll(filepath.Dir(f.path), 0o700)
		if err == nil {
			err = os.WriteFile(f.path, f.data, f.mode)
		}

		if err != nil {
			_ = os.RemoveAll(dir)

			return "", fmt.Errorf("make a scratch repository: %w", err)
		}
	}

	return dir, nil
}

// verdicts keeps, per copy of go-pre-commit, which hook bytes it said are current. One
// copy writes one hook, byte for byte, in every clone and for every hook type, so once
// it called some bytes current, every other bytes are outdated.
type verdicts struct {
	mu      sync.Mutex
	current map[string]string
	stale   map[string]map[string]bool
}

// get returns whether the copy tool calls the hook with SHA-256 sha current, and whether
// it is known.
func (v *verdicts) get(tool, sha string) (current, known bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if cur, ok := v.current[tool]; ok {
		return sha == cur, true
	}

	if v.stale[tool][sha] {
		return false, true
	}

	return false, false
}

// set records what the copy tool said of the hook with SHA-256 sha.
func (v *verdicts) set(tool, sha string, current bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if current {
		if v.current == nil {
			v.current = map[string]string{}
		}

		v.current[tool] = sha

		return
	}

	if v.stale == nil {
		v.stale = map[string]map[string]bool{}
	}

	if v.stale[tool] == nil {
		v.stale[tool] = map[string]bool{}
	}

	v.stale[tool][sha] = true
}
