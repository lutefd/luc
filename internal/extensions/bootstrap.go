package extensions

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const bootstrapAssetRoot = "bootstrap_assets"

func EnsureGlobalRuntime() error {
	root, err := configRoot()
	if err != nil {
		return err
	}

	dirs := []string{
		root,
		filepath.Join(root, "packages"),
		filepath.Join(root, "tools"),
		filepath.Join(root, "providers"),
		filepath.Join(root, "ui"),
		filepath.Join(root, "hooks"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "themes"),
		filepath.Join(root, "prompts"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return fs.WalkDir(bootstrapAssets, bootstrapAssetRoot, func(assetPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if assetPath == bootstrapAssetRoot || d.IsDir() {
			return nil
		}

		relPath := strings.TrimPrefix(assetPath, bootstrapAssetRoot+"/")
		path := filepath.Join(root, filepath.FromSlash(relPath))
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		data, err := fs.ReadFile(bootstrapAssets, assetPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	})
}
