// Provider registry — lets plugins/extensions declare additional providers
// and models beyond the built-ins. The TUI model-selection modal reads from
// a Registry; extensions can call Register() at startup to contribute more.
package provider

import (
	"sort"
	"sync"

	"github.com/lutefd/luc/internal/config"
)

// ModelDef describes a single model a user can pick.
type ModelDef struct {
	ID          string // API identifier, e.g. "gpt-5", "o3-mini"
	Name        string // Display name, e.g. "GPT-5"
	Description string // One-line blurb shown in the modal
	ContextK    int    // Context window in thousands (e.g. 128 for 128k)
	Provider    string // ProviderDef.ID this model belongs to
	Reasoning   bool   // True for "thinking" models (o-series etc.)
}

// Factory builds a Provider from a config. The config's Model field is
// filled with the selected ModelDef.ID at call-time.
type Factory func(cfg config.ProviderConfig) (Provider, error)

// ProviderDef declares a provider and the models it offers.
type ProviderDef struct {
	ID      string // e.g. "openai", "anthropic", "my-plugin"
	Name    string // Display name
	Factory Factory
	Models  []ModelDef
}

// Registry holds the global set of providers. It's concurrency-safe so
// extensions may register from init functions without ordering fuss.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]ProviderDef
	order     []string
}

// NewRegistry returns an empty registry. Use DefaultRegistry for the
// process-wide registry that ships with built-in OpenAI models.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]ProviderDef{}}
}

// Register adds or replaces a provider. Later calls with the same ID
// overwrite the previous definition (useful for extensions that
// customize a built-in provider's model list).
func (r *Registry) Register(def ProviderDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[def.ID]; !ok {
		r.order = append(r.order, def.ID)
	}
	r.providers[def.ID] = def
}

// Providers returns all registered providers in insertion order.
func (r *Registry) Providers() []ProviderDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderDef, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.providers[id])
	}
	return out
}

// Provider looks up a provider by ID.
func (r *Registry) Provider(id string) (ProviderDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.providers[id]
	return def, ok
}

// AllModels returns every model across all providers, sorted by provider
// order then by model ID. Handy for a flat modal listing.
func (r *Registry) AllModels() []ModelDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ModelDef
	for _, id := range r.order {
		models := append([]ModelDef(nil), r.providers[id].Models...)
		sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
		out = append(out, models...)
	}
	return out
}

// FindModel returns the model and its provider definition by model ID.
// When providerID is non-empty it is used as a hint: if the named provider
// has a matching model it is returned first, otherwise the search falls back
// to all providers. This ensures that two providers with the same model ID
// (e.g. a gateway and a direct provider both offering claude-opus-4-7) can
// be distinguished by the caller.
func (r *Registry) FindModel(providerID, modelID string) (ModelDef, ProviderDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if providerID != "" {
		if p, ok := r.providers[providerID]; ok {
			for _, m := range p.Models {
				if m.ID == modelID {
					return m, p, true
				}
			}
		}
	}
	for _, pid := range r.order {
		p := r.providers[pid]
		for _, m := range p.Models {
			if m.ID == modelID {
				return m, p, true
			}
		}
	}
	return ModelDef{}, ProviderDef{}, false
}

// defaultRegistry is the process-wide registry. Seed it from a package
// that imports concrete provider implementations (e.g. the kernel) via
// SetDefaultRegistry, then extensions can contribute more via
// DefaultRegistry().Register(...).
var (
	defaultRegistryMu sync.Mutex
	defaultRegistry   *Registry
)

// DefaultRegistry returns the process-wide registry. Returns an empty
// registry if SetDefaultRegistry has not been called yet.
func DefaultRegistry() *Registry {
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if defaultRegistry == nil {
		defaultRegistry = NewRegistry()
	}
	return defaultRegistry
}

// SetDefaultRegistry installs r as the process-wide registry. Useful for
// the kernel bootstrap to seed built-ins before extensions load.
func SetDefaultRegistry(r *Registry) {
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	defaultRegistry = r
}
