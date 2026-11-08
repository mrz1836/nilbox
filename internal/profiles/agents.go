package profiles

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
)

// maxModelsCacheBytes caps the size of an agent's models cache the app reads.
const maxModelsCacheBytes = 16 << 20

// AgentInfo is a coding agent as Settings and the launch menu show it: what it is, and
// whether this machine has its program. Every field is a plain value.
type AgentInfo struct {
	agent.Agent

	// Installed reports that the agent's program was found.
	Installed bool `json:"installed"`

	// Path is where the program was found, or "".
	Path string `json:"path"`
}

// Agents returns every coding agent, the default first, with whether its program is
// installed. An agent that keeps a cache of the models it offers (Codex's
// models_cache.json) has its Models read from there, from its default profile's directory
// or else the first of its other profiles' that has one; reading it asks no server.
func (s *Service) Agents(ctx context.Context) ([]AgentInfo, error) {
	stored, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	all := agent.All()
	out := make([]AgentInfo, 0, len(all))

	for _, a := range all {
		out = append(out, s.agentInfo(a, stored))
	}

	return out, nil
}

// agentInfo describes one agent: whether its program is installed, and the models its
// cache lists, when it keeps one.
func (s *Service) agentInfo(a agent.Agent, stored []Profile) AgentInfo {
	info := AgentInfo{Agent: a}

	if s.finder != nil {
		if path, err := s.finder.Find(a.Command); err == nil {
			info.Installed, info.Path = true, path
		}
	}

	if models := s.cachedModels(a, stored); len(models) > 0 {
		info.Models = models
	}

	return info
}

// cachedModels returns the models in the first readable models cache of the agent's
// profiles, the default profile's directory first, or nil.
func (s *Service) cachedModels(a agent.Agent, stored []Profile) []agent.Choice {
	if a.ModelsCache == "" {
		return nil
	}

	dirs := []string{filepath.Join(s.home, a.ConfigDir)}

	for _, p := range stored {
		if p.Agent == a.ID && p.ConfigDir != "" && !slices.Contains(dirs, p.ConfigDir) {
			dirs = append(dirs, p.ConfigDir)
		}
	}

	for _, dir := range dirs {
		if models, ok := s.models.read(filepath.Join(dir, a.ModelsCache)); ok {
			return models
		}
	}

	return nil
}

// modelsCache keeps the models read from each cache file until the file changes.
type modelsCache struct {
	mu    sync.Mutex
	files map[string]cachedModels
}

// cachedModels is one cache file's models, as of its size and modification time.
type cachedModels struct {
	size    int64
	modTime time.Time
	models  []agent.Choice
}

// newModelsCache returns an empty cache.
func newModelsCache() *modelsCache {
	return &modelsCache{files: map[string]cachedModels{}}
}

// read returns the models listed in the cache file at path, and false when it holds none
// or cannot be read. A file read before is read again only once it changes.
func (c *modelsCache) read(path string) ([]agent.Choice, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxModelsCacheBytes {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if hit, ok := c.files[path]; ok && hit.size == info.Size() && hit.modTime.Equal(info.ModTime()) {
		return slices.Clone(hit.models), len(hit.models) > 0
	}

	models, err := parseModelsCache(path)
	if err != nil {
		return nil, false
	}

	c.files[path] = cachedModels{size: info.Size(), modTime: info.ModTime(), models: models}

	return slices.Clone(models), len(models) > 0
}

// errModelsCache is returned for a models cache that cannot be read.
var errModelsCache = errors.New("unreadable models cache")

// cachedModel is one model in an agent's models cache, with the fields the app reads.
type cachedModel struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility"`
	Priority    int    `json:"priority"`
}

// parseModelsCache reads the models an agent lists for people in its cache: those whose
// visibility is "list" and whose name the agent can take, by their priority, each named by
// its display name.
func parseModelsCache(path string) ([]agent.Choice, error) {
	f, err := os.Open(path) //nolint:gosec // a file in the agent's own config directory
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	var doc struct {
		Models []cachedModel `json:"models"`
	}

	if err = json.NewDecoder(io.LimitReader(f, maxModelsCacheBytes)).Decode(&doc); err != nil {
		return nil, errors.Join(errModelsCache, err)
	}

	listed := slices.DeleteFunc(doc.Models, func(m cachedModel) bool {
		return m.Visibility != "list" || m.Slug == "" || agent.ValidateModel(m.Slug) != nil
	})

	slices.SortStableFunc(listed, func(a, b cachedModel) int { return cmp.Compare(a.Priority, b.Priority) })

	out := make([]agent.Choice, 0, len(listed))
	seen := map[string]bool{}

	for _, m := range listed {
		if !seen[m.Slug] {
			seen[m.Slug] = true
			out = append(out, agent.Choice{Value: m.Slug, Label: cmp.Or(m.DisplayName, m.Slug)})
		}
	}

	return out, nil
}
