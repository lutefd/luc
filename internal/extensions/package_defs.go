package extensions

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const CurrentPackageAPIVersion = "0.1.0"

type PackageScope string

const (
	PackageScopeUser    PackageScope = "user"
	PackageScopeProject PackageScope = "project"
	PackageScopeAll     PackageScope = "all"
)

const (
	PackageSourceTypeLocalPath  = "local_path"
	PackageSourceTypeModulePath = "module_path"
	PackageSourceTypeGitURL     = "git_url"
	PackageSourceTypeArchiveURL = "archive_url"
)

var (
	packageCategoryOrder = []string{"tools", "providers", "ui", "hooks", "prompts", "skills", "themes", "extensions"}
	executableCategories = map[string]struct{}{
		"tools":      {},
		"hooks":      {},
		"providers":  {},
		"extensions": {},
	}
	allowedTopLevelFiles = map[string]struct{}{
		"luc.pkg.yaml": {},
		".gitignore":   {},
	}
)

func isAllowedTopLevelPackageFile(name string) bool {
	if _, ok := allowedTopLevelFiles[name]; ok {
		return true
	}
	base := strings.ToLower(strings.TrimSpace(name))
	compact := strings.NewReplacer(".", "", "-", "", "_", "").Replace(base)
	for _, prefix := range []string{"readme", "changelog", "changes", "history", "license", "copying", "notice"} {
		if compact == prefix || strings.HasPrefix(compact, prefix) {
			return true
		}
	}
	return false
}

type PackageManifest struct {
	Schema      string   `yaml:"schema" json:"schema"`
	Module      string   `yaml:"module" json:"module"`
	Version     string   `yaml:"version" json:"version"`
	LucVersion  string   `yaml:"luc_version" json:"luc_version"`
	Name        string   `yaml:"name,omitempty" json:"name,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	License     string   `yaml:"license,omitempty" json:"license,omitempty"`
	Homepage    string   `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Repository  string   `yaml:"repository,omitempty" json:"repository,omitempty"`
	Keywords    []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
}

type PackageValidation struct {
	Root                 string
	Manifest             PackageManifest
	ManifestDigest       string
	Categories           []string
	ExecutableCategories []string
}

type InstalledPackageRecord struct {
	Module         string       `json:"module"`
	Version        string       `json:"version"`
	Scope          PackageScope `json:"scope"`
	SourceType     string       `json:"source_type"`
	Source         string       `json:"source"`
	SourceRevision string       `json:"source_revision,omitempty"`
	InstalledAt    string       `json:"installed_at"`
	PackageDir     string       `json:"package_dir"`
	ManifestDigest string       `json:"manifest_digest"`
}

type InstalledPackage struct {
	Record               InstalledPackageRecord
	Manifest             PackageManifest
	Categories           []string
	ExecutableCategories []string
}

type InstallOptions struct {
	Scope  PackageScope
	Yes    bool
	Stdin  io.Reader
	Stdout io.Writer
}

type InstallResult struct {
	Record           InstalledPackageRecord
	Manifest         PackageManifest
	Categories       []string
	AlreadyInstalled bool
	ReloadRequired   bool
}

func normalizedPackageDirName(module, version string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(module) {
		switch {
		case r == '/', r == '\\', r == ':':
			builder.WriteByte('_')
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String() + "@" + version
}

func packageArchiveBaseName(manifest PackageManifest) string {
	name := strings.TrimSpace(firstNonEmpty(manifest.Name, filepath.Base(manifest.Module)))
	if name == "" {
		name = "package"
	}
	return name + "-" + manifest.Version
}

func orderedCategories(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, category := range packageCategoryOrder {
		for _, value := range values {
			if value != category {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func isAllowedPackageCategory(name string) bool {
	for _, category := range packageCategoryOrder {
		if name == category {
			return true
		}
	}
	return false
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func expandUserPath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func looksLikeTarball(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".tar.gz") || strings.HasSuffix(strings.ToLower(path), ".tgz")
}
