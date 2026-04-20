package extensions

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

func ValidatePackagePath(path string) (PackageValidation, error) {
	absPath, err := filepath.Abs(expandUserPath(path))
	if err != nil {
		return PackageValidation{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return PackageValidation{}, err
	}

	if info.IsDir() {
		return validatePackageDir(absPath)
	}
	if !looksLikeTarball(absPath) {
		return PackageValidation{}, fmt.Errorf("%s: expected a package directory or .tar.gz archive", absPath)
	}

	stageRoot, cleanup, err := extractPackageArchive(absPath)
	if err != nil {
		return PackageValidation{}, err
	}
	defer func() { _ = cleanup() }()

	root, err := detectPackageRoot(stageRoot)
	if err != nil {
		return PackageValidation{}, err
	}
	return validatePackageDir(root)
}

func validatePackageDir(root string) (PackageValidation, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return PackageValidation{}, err
	}
	manifestPath := filepath.Join(root, "luc.pkg.yaml")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PackageValidation{}, fmt.Errorf("%s: missing luc.pkg.yaml", root)
		}
		return PackageValidation{}, err
	}

	var manifest PackageManifest
	if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
		return PackageValidation{}, fmt.Errorf("%s: %w", manifestPath, err)
	}
	if strings.TrimSpace(manifest.Schema) != "luc.pkg/v1" {
		return PackageValidation{}, fmt.Errorf("%s: unsupported package schema %q", manifestPath, manifest.Schema)
	}
	manifest.Module = strings.TrimSpace(manifest.Module)
	if err := validatePackageModule(manifest.Module); err != nil {
		return PackageValidation{}, fmt.Errorf("%s: %w", manifestPath, err)
	}
	manifest.Version = canonicalSemver(manifest.Version)
	if !semver.IsValid(manifest.Version) {
		return PackageValidation{}, fmt.Errorf("%s: version must be valid semver, got %q", manifestPath, manifest.Version)
	}
	manifest.LucVersion = strings.TrimSpace(manifest.LucVersion)
	if manifest.LucVersion == "" {
		return PackageValidation{}, fmt.Errorf("%s: luc_version is required", manifestPath)
	}
	ok, err := matchesVersionConstraint(CurrentPackageAPIVersion, manifest.LucVersion)
	if err != nil {
		return PackageValidation{}, fmt.Errorf("%s: invalid luc_version constraint %q: %w", manifestPath, manifest.LucVersion, err)
	}
	if !ok {
		return PackageValidation{}, fmt.Errorf("%s: luc_version %q is incompatible with luc %s", manifestPath, manifest.LucVersion, CurrentPackageAPIVersion)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return PackageValidation{}, err
	}

	categories := make([]string, 0, len(packageCategoryOrder))
	execCats := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			if !isAllowedPackageCategory(name) {
				return PackageValidation{}, fmt.Errorf("%s: unsupported top-level directory %q", root, name)
			}
			if err := validatePackageCategory(filepath.Join(root, name), name); err != nil {
				return PackageValidation{}, err
			}
			hasFiles, err := dirHasVisibleFiles(filepath.Join(root, name))
			if err != nil {
				return PackageValidation{}, err
			}
			if hasFiles {
				categories = append(categories, name)
				if _, ok := executableCategories[name]; ok {
					execCats = append(execCats, name)
				}
			}
			continue
		}
		if _, ok := allowedTopLevelFiles[name]; ok {
			continue
		}
		return PackageValidation{}, fmt.Errorf("%s: unsupported top-level file %q", root, name)
	}

	digest := sha256.Sum256(manifestBytes)
	return PackageValidation{
		Root:                 root,
		Manifest:             manifest,
		ManifestDigest:       hex.EncodeToString(digest[:]),
		Categories:           orderedCategories(categories),
		ExecutableCategories: orderedCategories(execCats),
	}, nil
}

func validatePackageCategory(dir, category string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: symlinks are not supported in packages", dir)
	}
	switch category {
	case "tools":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parseToolDef(path); err != nil {
				return err
			}
		}
	case "providers":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parseProviderDef(path); err != nil {
				return err
			}
		}
	case "ui":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parseUIManifest(path); err != nil {
				return err
			}
		}
	case "hooks":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parseHookManifest(path); err != nil {
				return err
			}
		}
	case "prompts":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parsePromptExtension(path); err != nil {
				return err
			}
		}
	case "skills":
		sources, err := listSkillSources(dir)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if _, err := parseSkillSource(source); err != nil {
				return err
			}
		}
	case "themes":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := validateThemeFile(path); err != nil {
				return err
			}
		}
	case "extensions":
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if _, err := parseExtensionManifest(path); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: unsupported package category %q", dir, category)
	}
	return walkPackageCategoryFiles(dir)
}

func walkPackageCategoryFiles(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not supported in packages", path)
		}
		return nil
	})
}

func validateThemeFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw struct {
		Name     string      `yaml:"name" json:"name"`
		Inherits string      `yaml:"inherits" json:"inherits"`
		Colors   ThemeColors `yaml:"colors" json:"colors"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func validatePackageModule(module string) error {
	module = strings.TrimSpace(module)
	if module == "" {
		return fmt.Errorf("module is required")
	}
	if strings.Contains(module, " ") || strings.Contains(module, "@") {
		return fmt.Errorf("module %q is invalid", module)
	}
	parts := strings.Split(module, "/")
	if len(parts) < 3 {
		return fmt.Errorf("module %q must look like host/owner/name", module)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || strings.Contains(part, "\\") {
			return fmt.Errorf("module %q is invalid", module)
		}
	}
	return nil
}

func matchesVersionConstraint(current, constraint string) (bool, error) {
	current = canonicalSemver(current)
	if !semver.IsValid(current) {
		return false, fmt.Errorf("invalid current version %q", current)
	}
	normalized := strings.NewReplacer(",", " ", "||", " ").Replace(strings.TrimSpace(constraint))
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return false, fmt.Errorf("empty constraint")
	}
	for _, token := range tokens {
		op := ""
		version := token
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(token, candidate) {
				op = candidate
				version = strings.TrimSpace(strings.TrimPrefix(token, candidate))
				break
			}
		}
		version = canonicalSemver(version)
		if !semver.IsValid(version) {
			return false, fmt.Errorf("invalid version %q", token)
		}
		cmp := semver.Compare(current, version)
		switch op {
		case "", "=":
			if cmp != 0 {
				return false, nil
			}
		case ">":
			if cmp <= 0 {
				return false, nil
			}
		case ">=":
			if cmp < 0 {
				return false, nil
			}
		case "<":
			if cmp >= 0 {
				return false, nil
			}
		case "<=":
			if cmp > 0 {
				return false, nil
			}
		default:
			return false, fmt.Errorf("unsupported operator in %q", token)
		}
	}
	return true, nil
}

func canonicalSemver(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "v") {
		return value
	}
	if value[0] >= '0' && value[0] <= '9' {
		return "v" + value
	}
	return value
}

func looksLikeCommitHash(value string) bool {
	if len(value) < 7 || len(value) > 40 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func dirHasVisibleFiles(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		found = true
		return io.EOF
	})
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return found, err
}
