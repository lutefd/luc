package extensions

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const bootstrapAssetRoot = "bootstrap_assets"
const bootstrapStateFile = ".bootstrap-assets.sha256"

type bootstrapAssetFile struct {
	RelPath string
	Data    []byte
}

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

	assets, digest, err := bootstrapAssetManifest()
	if err != nil {
		return err
	}
	refreshExisting, err := shouldRefreshBootstrapAssets(root, assets, digest)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.RelPath))
		if !refreshExisting {
			if _, err := os.Stat(path); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := writeBootstrapAsset(path, asset.Data); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(root, bootstrapStateFile), []byte(digest), 0o644)
}

func bootstrapAssetManifest() ([]bootstrapAssetFile, string, error) {
	assets := []bootstrapAssetFile{}
	hash := sha256.New()
	err := fs.WalkDir(bootstrapAssets, bootstrapAssetRoot, func(assetPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if assetPath == bootstrapAssetRoot || d.IsDir() {
			return nil
		}

		data, err := fs.ReadFile(bootstrapAssets, assetPath)
		if err != nil {
			return err
		}
		relPath := strings.TrimPrefix(assetPath, bootstrapAssetRoot+"/")
		assets = append(assets, bootstrapAssetFile{
			RelPath: relPath,
			Data:    data,
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].RelPath < assets[j].RelPath
	})
	for _, asset := range assets {
		if _, err := hash.Write([]byte(asset.RelPath)); err != nil {
			return nil, "", err
		}
		if _, err := hash.Write([]byte{0}); err != nil {
			return nil, "", err
		}
		if _, err := hash.Write(asset.Data); err != nil {
			return nil, "", err
		}
		if _, err := hash.Write([]byte{0}); err != nil {
			return nil, "", err
		}
	}
	return assets, hex.EncodeToString(hash.Sum(nil)), nil
}

func shouldRefreshBootstrapAssets(root string, assets []bootstrapAssetFile, currentDigest string) (bool, error) {
	statePath := filepath.Join(root, bootstrapStateFile)
	data, err := os.ReadFile(statePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		for _, asset := range assets {
			path := filepath.Join(root, filepath.FromSlash(asset.RelPath))
			if _, statErr := os.Stat(path); statErr == nil {
				return true, nil
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return false, statErr
			}
		}
		return false, nil
	case err != nil:
		return false, err
	default:
		return strings.TrimSpace(string(data)) != currentDigest, nil
	}
}

func writeBootstrapAsset(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == string(data) {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
