package workspace

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Info struct {
	Root      string
	ProjectID string
	HasGit    bool
	Branch    string
	GitRoot   string
	StateDir  string
}

func Detect(cwd string) (Info, error) {
	root := filepath.Clean(cwd)
	gitRoot, hasGit, err := findGitRoot(root)
	if err != nil {
		return Info{}, err
	}

	branch := ""
	if hasGit {
		branch, _ = currentBranch(gitRoot)
	}

	sum := sha1.Sum([]byte(root))
	info := Info{
		Root:      root,
		ProjectID: hex.EncodeToString(sum[:8]),
		HasGit:    hasGit,
		Branch:    branch,
		GitRoot:   gitRoot,
		StateDir:  filepath.Join(root, ".luc"),
	}

	return info, ensureStateDirs(info)
}

func findGitRoot(cwd string) (string, bool, error) {
	current := cwd
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil && isValidGitRoot(current) {
			return current, true, nil
		}

		next := filepath.Dir(current)
		if next == current {
			return "", false, nil
		}
		current = next
	}
}

func isValidGitRoot(root string) bool {
	gitDir, err := resolveGitDir(root)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil {
		return false
	}
	return true
}

func ensureStateDirs(info Info) error {
	dirs := []string{
		info.StateDir,
		filepath.Join(info.StateDir, "history"),
		filepath.Join(info.StateDir, "history", "sessions"),
		filepath.Join(info.StateDir, "logs"),
		filepath.Join(info.StateDir, "packages"),
		filepath.Join(info.StateDir, "extensions"),
		filepath.Join(info.StateDir, "extensions", "sessions"),
		filepath.Join(info.StateDir, "extensions", "workspace"),
		filepath.Join(info.StateDir, "prompts"),
		filepath.Join(info.StateDir, "providers"),
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

func currentBranch(root string) (string, error) {
	gitDir, err := resolveGitDir(root)
	if err != nil {
		return "", err
	}

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(head))
	if line == "" {
		return "", nil
	}
	if strings.HasPrefix(line, "ref: ") {
		ref := strings.TrimSpace(strings.TrimPrefix(line, "ref: "))
		return filepath.Base(ref), nil
	}
	if len(line) >= 12 {
		return "detached@" + line[:12], nil
	}
	return "detached", nil
}

func resolveGitDir(root string) (string, error) {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return gitPath, nil
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return "", fmt.Errorf("invalid gitdir pointer")
	}
	target := strings.TrimSpace(line[len(prefix):])
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	return filepath.Clean(target), nil
}
