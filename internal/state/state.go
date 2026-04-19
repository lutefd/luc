// Package state persists a user-level preference file that survives across
// luc launches. It holds the globally last-used theme and provider/model so
// that new sessions start from the user's last choice rather than the
// config-file default.
//
// The file lives at ~/.luc/state.yaml and is written opportunistically by
// the controller whenever the user changes theme or model. It is intentionally
// kept small and separate from config.yaml so the user's hand-authored config
// never gets clobbered by runtime updates.
package state

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State is the schema for ~/.luc/state.yaml. All fields are optional; empty
// values mean "no override, fall back to config.yaml".
type State struct {
	Theme        string `yaml:"theme,omitempty"`
	ProviderKind string `yaml:"provider_kind,omitempty"`
	Model        string `yaml:"model,omitempty"`
}

// Load reads the state file. A missing file returns an empty State and no
// error — first-run is a normal condition. Malformed YAML returns an error
// so callers can log and fall back to config defaults rather than silently
// ignore a broken file.
func Load() (State, error) {
	path, err := filePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Save writes the state atomically: serialize to a temp file next to the
// target, then rename. Rename-on-same-filesystem is atomic on POSIX, so a
// crash during write leaves either the old file or the new file — never a
// half-written one.
func Save(s State) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Update loads, mutates via the provided function, and writes back — the
// convenience form used by callers that only want to change one field
// without worrying about preserving the others.
func Update(mutate func(*State)) error {
	s, err := Load()
	if err != nil {
		// Load errors are non-fatal for updates: if the file is malformed
		// we overwrite with the caller's intended value rather than
		// refusing to write. This is the pragmatic choice for a
		// user-preference file — we'd rather recover state than preserve
		// a broken one.
		s = State{}
	}
	mutate(&s)
	return Save(s)
}

// filePath resolves the state file location. It honors the LUC_STATE_DIR
// environment variable so tests (and users who want to sandbox luc) can
// redirect state away from ~/.luc without touching the process's HOME.
func filePath() (string, error) {
	if dir := os.Getenv("LUC_STATE_DIR"); dir != "" {
		return filepath.Join(dir, "state.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".luc", "state.yaml"), nil
}
