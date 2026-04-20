package extensions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lutefd/luc/internal/workspace"
	"golang.org/x/mod/semver"
)

func ParsePackageScope(raw string, allowAll bool) (PackageScope, error) {
	switch PackageScope(strings.ToLower(strings.TrimSpace(raw))) {
	case "", PackageScopeUser:
		return PackageScopeUser, nil
	case PackageScopeProject:
		return PackageScopeProject, nil
	case PackageScopeAll:
		if allowAll {
			return PackageScopeAll, nil
		}
	}
	if allowAll {
		return "", fmt.Errorf("invalid scope %q (expected user, project, or all)", raw)
	}
	return "", fmt.Errorf("invalid scope %q (expected user or project)", raw)
}

func InstallPackage(workspaceRoot, rawSource string, opts InstallOptions) (InstallResult, error) {
	scope := opts.Scope
	if scope == "" {
		scope = PackageScopeUser
	}
	if scope != PackageScopeUser && scope != PackageScopeProject {
		return InstallResult{}, fmt.Errorf("install requires user or project scope")
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}

	staged, err := stagePackageSource(rawSource)
	if err != nil {
		return InstallResult{}, err
	}
	defer func() { _ = staged.Cleanup() }()

	validation, err := validatePackageDir(staged.Root)
	if err != nil {
		return InstallResult{}, err
	}
	if err := validateResolvedSource(validation.Manifest, staged.Source); err != nil {
		return InstallResult{}, err
	}
	if staged.Source.Remote && len(validation.ExecutableCategories) > 0 && !opts.Yes {
		if err := confirmRemoteExecutableInstall(opts.Stdin, opts.Stdout, validation.Manifest, validation.ExecutableCategories); err != nil {
			return InstallResult{}, err
		}
	}

	storeRoot, err := packageStoreDir(workspaceRoot, scope)
	if err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return InstallResult{}, err
	}

	records, err := loadInstalledPackageRecords(storeRoot)
	if err != nil {
		return InstallResult{}, err
	}
	targetDir := filepath.Join(storeRoot, normalizedPackageDirName(validation.Manifest.Module, validation.Manifest.Version))
	existing, existingIdx := findInstalledRecord(records, validation.Manifest.Module)
	if existingIdx >= 0 && strings.EqualFold(existing.Version, validation.Manifest.Version) {
		if _, err := os.Stat(existing.PackageDir); err == nil {
			return InstallResult{
				Record:           existing,
				Manifest:         validation.Manifest,
				Categories:       validation.Categories,
				AlreadyInstalled: true,
				ReloadRequired:   true,
			}, nil
		}
	}

	tempInstallDir := targetDir + ".installing"
	_ = os.RemoveAll(tempInstallDir)
	_ = os.RemoveAll(targetDir)
	if err := copyPackagePayload(validation.Root, tempInstallDir); err != nil {
		_ = os.RemoveAll(tempInstallDir)
		return InstallResult{}, err
	}

	backupDir := ""
	if existingIdx >= 0 && existing.PackageDir != "" && existing.PackageDir != targetDir {
		if _, err := os.Stat(existing.PackageDir); err == nil {
			backupDir = existing.PackageDir + ".backup"
			_ = os.RemoveAll(backupDir)
			if err := os.Rename(existing.PackageDir, backupDir); err != nil {
				_ = os.RemoveAll(tempInstallDir)
				return InstallResult{}, err
			}
		}
	}

	if err := os.Rename(tempInstallDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, existing.PackageDir)
		}
		_ = os.RemoveAll(tempInstallDir)
		return InstallResult{}, err
	}

	record := InstalledPackageRecord{
		Module:         validation.Manifest.Module,
		Version:        validation.Manifest.Version,
		Scope:          scope,
		SourceType:     staged.Source.Type,
		Source:         staged.Source.Source,
		SourceRevision: firstNonEmpty(staged.Source.SourceRevision, staged.Source.ResolvedRef),
		InstalledAt:    time.Now().UTC().Format(time.RFC3339),
		PackageDir:     targetDir,
		ManifestDigest: validation.ManifestDigest,
	}

	next := append([]InstalledPackageRecord(nil), records...)
	if existingIdx >= 0 {
		next[existingIdx] = record
	} else {
		next = append(next, record)
	}
	if err := saveInstalledPackageRecords(storeRoot, next); err != nil {
		_ = os.RemoveAll(targetDir)
		if backupDir != "" {
			_ = os.Rename(backupDir, existing.PackageDir)
		}
		return InstallResult{}, err
	}
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}

	return InstallResult{
		Record:         record,
		Manifest:       validation.Manifest,
		Categories:     validation.Categories,
		ReloadRequired: true,
	}, nil
}

func RemoveInstalledPackage(workspaceRoot, module string, scope PackageScope) (InstalledPackageRecord, bool, error) {
	if scope != PackageScopeUser && scope != PackageScopeProject {
		return InstalledPackageRecord{}, false, fmt.Errorf("remove requires user or project scope")
	}
	storeRoot, err := packageStoreDir(workspaceRoot, scope)
	if err != nil {
		return InstalledPackageRecord{}, false, err
	}
	records, err := loadInstalledPackageRecords(storeRoot)
	if err != nil {
		return InstalledPackageRecord{}, false, err
	}
	record, idx := findInstalledRecord(records, module)
	if idx < 0 {
		return InstalledPackageRecord{}, false, nil
	}
	if err := os.RemoveAll(record.PackageDir); err != nil {
		return InstalledPackageRecord{}, false, err
	}
	next := append([]InstalledPackageRecord(nil), records[:idx]...)
	next = append(next, records[idx+1:]...)
	if err := saveInstalledPackageRecords(storeRoot, next); err != nil {
		return InstalledPackageRecord{}, false, err
	}
	return record, true, nil
}

func ListInstalledPackages(workspaceRoot string, scope PackageScope) ([]InstalledPackage, error) {
	scopes, err := scopesForQuery(scope)
	if err != nil {
		return nil, err
	}
	var out []InstalledPackage
	for _, oneScope := range scopes {
		storeRoot, err := packageStoreDir(workspaceRoot, oneScope)
		if err != nil {
			return nil, err
		}
		records, err := loadInstalledPackageRecords(storeRoot)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			installed, err := hydrateInstalledPackage(record)
			if err != nil {
				return nil, err
			}
			out = append(out, installed)
		}
	}
	sortInstalledPackages(out)
	return out, nil
}

func InspectInstalledPackages(workspaceRoot, module string, scope PackageScope) ([]InstalledPackage, error) {
	module = strings.TrimSpace(module)
	if module == "" {
		return nil, fmt.Errorf("module is required")
	}
	packages, err := ListInstalledPackages(workspaceRoot, scope)
	if err != nil {
		return nil, err
	}
	var out []InstalledPackage
	for _, pkg := range packages {
		if strings.EqualFold(strings.TrimSpace(pkg.Record.Module), module) {
			out = append(out, pkg)
		}
	}
	return out, nil
}

func packageStoreDir(workspaceRoot string, scope PackageScope) (string, error) {
	switch scope {
	case PackageScopeUser:
		root, err := configRoot()
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "packages"), nil
	case PackageScopeProject:
		info, err := workspace.Detect(workspaceRoot)
		if err != nil {
			return "", err
		}
		return filepath.Join(info.StateDir, "packages"), nil
	default:
		return "", fmt.Errorf("unsupported package scope %q", scope)
	}
}

func scopesForQuery(scope PackageScope) ([]PackageScope, error) {
	switch scope {
	case "", PackageScopeAll:
		return []PackageScope{PackageScopeUser, PackageScopeProject}, nil
	case PackageScopeUser:
		return []PackageScope{PackageScopeUser}, nil
	case PackageScopeProject:
		return []PackageScope{PackageScopeProject}, nil
	default:
		return nil, fmt.Errorf("unsupported package scope %q", scope)
	}
}

func confirmRemoteExecutableInstall(stdin io.Reader, stdout io.Writer, manifest PackageManifest, categories []string) error {
	if _, err := fmt.Fprintf(stdout, "Package %s@%s contains executable assets (%s).\nInstalling it may run local code with your user permissions.\nContinue? [y/N]: ", manifest.Module, manifest.Version, strings.Join(categories, ", ")); err != nil {
		return err
	}
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil {
		if err == io.EOF {
			return fmt.Errorf("installation aborted")
		}
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("installation aborted")
	}
}

func validateResolvedSource(manifest PackageManifest, source installSource) error {
	switch source.Type {
	case PackageSourceTypeModulePath:
		if !strings.EqualFold(strings.TrimSpace(source.Source), manifest.Module) {
			return fmt.Errorf("package manifest module %q does not match requested module %q", manifest.Module, source.Source)
		}
		if source.ResolvedRef != "" && canonicalSemver(source.ResolvedRef) != manifest.Version {
			return fmt.Errorf("package manifest version %q does not match requested version %q", manifest.Version, source.ResolvedRef)
		}
	case PackageSourceTypeGitURL:
		if source.ResolvedRef != "" && semver.IsValid(canonicalSemver(source.ResolvedRef)) && canonicalSemver(source.ResolvedRef) != manifest.Version {
			return fmt.Errorf("package manifest version %q does not match requested tag %q", manifest.Version, source.ResolvedRef)
		}
	}
	return nil
}
