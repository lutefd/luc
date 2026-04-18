package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	UI       UIConfig       `yaml:"ui"`
}

type ProviderConfig struct {
	Kind        string  `yaml:"kind"`
	BaseURL     string  `yaml:"base_url"`
	Model       string  `yaml:"model"`
	APIKeyEnv   string  `yaml:"api_key_env"`
	Temperature float32 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

type UIConfig struct {
	InspectorPosition string `yaml:"inspector_position"`
	InspectorOpen     bool   `yaml:"inspector_open"`
	Theme             string `yaml:"theme"`
}

type partialConfig struct {
	Provider partialProviderConfig `yaml:"provider"`
	UI       partialUIConfig       `yaml:"ui"`
}

type partialProviderConfig struct {
	Kind        *string  `yaml:"kind"`
	BaseURL     *string  `yaml:"base_url"`
	Model       *string  `yaml:"model"`
	APIKeyEnv   *string  `yaml:"api_key_env"`
	Temperature *float32 `yaml:"temperature"`
	MaxTokens   *int     `yaml:"max_tokens"`
}

type partialUIConfig struct {
	InspectorPosition *string `yaml:"inspector_position"`
	InspectorOpen     *bool   `yaml:"inspector_open"`
	Theme             *string `yaml:"theme"`
}

func Default() Config {
	return Config{
		Provider: ProviderConfig{
			Kind:        "openai-compatible",
			BaseURL:     "https://api.openai.com/v1",
			Model:       "gpt-5.4",
			APIKeyEnv:   "OPENAI_API_KEY",
			Temperature: 0.2,
		},
		UI: UIConfig{
			InspectorPosition: "auto",
			InspectorOpen:     false,
			Theme:             "light",
		},
	}
}

func Load(workspaceRoot string) (Config, error) {
	cfg := Default()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, err
	}

	paths := []string{
		filepath.Join(home, ".config", "luc", "config.yaml"),
		filepath.Join(workspaceRoot, ".luc", "config.yaml"),
	}

	for _, path := range paths {
		if err := mergeFile(path, &cfg); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}

func mergeFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var partial partialConfig
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return err
	}

	if partial.Provider.Kind != nil {
		cfg.Provider.Kind = *partial.Provider.Kind
	}
	if partial.Provider.BaseURL != nil {
		cfg.Provider.BaseURL = *partial.Provider.BaseURL
	}
	if partial.Provider.Model != nil {
		cfg.Provider.Model = *partial.Provider.Model
	}
	if partial.Provider.APIKeyEnv != nil {
		cfg.Provider.APIKeyEnv = *partial.Provider.APIKeyEnv
	}
	if partial.Provider.Temperature != nil {
		cfg.Provider.Temperature = *partial.Provider.Temperature
	}
	if partial.Provider.MaxTokens != nil {
		cfg.Provider.MaxTokens = *partial.Provider.MaxTokens
	}
	if partial.UI.InspectorPosition != nil {
		cfg.UI.InspectorPosition = *partial.UI.InspectorPosition
	}
	if partial.UI.InspectorOpen != nil {
		cfg.UI.InspectorOpen = *partial.UI.InspectorOpen
	}
	if partial.UI.Theme != nil {
		cfg.UI.Theme = *partial.UI.Theme
	}

	return nil
}
