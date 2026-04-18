package workspace

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Info struct {
	Root      string
	ProjectID string
	HasGit    bool
	StateDir  string
}

func Detect(cwd string) (Info, error) {
	root, hasGit, err := findRoot(cwd)
	if err != nil {
		return Info{}, err
	}

	sum := sha1.Sum([]byte(root))
	info := Info{
		Root:      root,
		ProjectID: hex.EncodeToString(sum[:8]),
		HasGit:    hasGit,
		StateDir:  filepath.Join(root, ".luc"),
	}

	return info, ensureStateDirs(info)
}

func findRoot(cwd string) (string, bool, error) {
	current := cwd
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, true, nil
		}

		next := filepath.Dir(current)
		if next == current {
			return cwd, false, nil
		}
		current = next
	}
}

func ensureStateDirs(info Info) error {
	dirs := []string{
		info.StateDir,
		filepath.Join(info.StateDir, "history"),
		filepath.Join(info.StateDir, "history", "sessions"),
		filepath.Join(info.StateDir, "logs"),
		filepath.Join(info.StateDir, "prompts"),
		filepath.Join(info.StateDir, "skills"),
		filepath.Join(info.StateDir, "themes"),
		filepath.Join(info.StateDir, "tools"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}
