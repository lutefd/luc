package kernel

import (
	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/provider/openai"
)

// seedDefaultRegistry populates the process-wide provider.Registry with the
// shipped built-in providers. Safe to call multiple times; Register()
// replaces existing entries with the same ID, so the built-in set remains
// authoritative even after an extension attempts to override it here.
func seedDefaultRegistry() {
	reg := provider.DefaultRegistry()
	openaiFactory := func(cfg config.ProviderConfig) (provider.Provider, error) {
		return openai.New(cfg)
	}

	reg.Register(provider.ProviderDef{
		ID:      "openai",
		Name:    "OpenAI",
		Factory: openaiFactory,
		Models: []provider.ModelDef{
			// GPT-5 family
			{ID: "gpt-5", Name: "GPT-5", Description: "Flagship general-purpose model", ContextK: 400, Provider: "openai"},
			{ID: "gpt-5-mini", Name: "GPT-5 mini", Description: "Faster, cheaper GPT-5 variant", ContextK: 400, Provider: "openai"},
			{ID: "gpt-5-nano", Name: "GPT-5 nano", Description: "Lowest-latency GPT-5 tier", ContextK: 400, Provider: "openai"},
			{ID: "gpt-5-thinking", Name: "GPT-5 thinking", Description: "Deep reasoning GPT-5", ContextK: 400, Provider: "openai", Reasoning: true},
			{ID: "gpt-5.4", Name: "GPT-5.4", Description: "Incremental GPT-5 refresh", ContextK: 400, Provider: "openai"},
			// GPT-4.1 family
			{ID: "gpt-4.1", Name: "GPT-4.1", Description: "Long-context 4.x flagship", ContextK: 1000, Provider: "openai"},
			{ID: "gpt-4.1-mini", Name: "GPT-4.1 mini", Description: "Fast, cheap 4.1 variant", ContextK: 1000, Provider: "openai"},
			{ID: "gpt-4.1-nano", Name: "GPT-4.1 nano", Description: "Smallest 4.1 variant", ContextK: 1000, Provider: "openai"},
			// GPT-4o family
			{ID: "gpt-4o", Name: "GPT-4o", Description: "Omni model (text + vision + audio)", ContextK: 128, Provider: "openai"},
			{ID: "gpt-4o-mini", Name: "GPT-4o mini", Description: "Lightweight 4o", ContextK: 128, Provider: "openai"},
			// o-series reasoning
			{ID: "o3", Name: "o3", Description: "Reasoning model — strongest", ContextK: 200, Provider: "openai", Reasoning: true},
			{ID: "o3-mini", Name: "o3 mini", Description: "Reasoning — faster/cheaper", ContextK: 200, Provider: "openai", Reasoning: true},
			{ID: "o4-mini", Name: "o4 mini", Description: "Next-gen reasoning, compact", ContextK: 200, Provider: "openai", Reasoning: true},
			// Legacy, still widely used
			{ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Description: "Legacy turbo series", ContextK: 128, Provider: "openai"},
			{ID: "gpt-3.5-turbo", Name: "GPT-3.5 Turbo", Description: "Cheapest legacy option", ContextK: 16, Provider: "openai"},
		},
	})
}
