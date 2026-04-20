package extensions

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
	"golang.org/x/net/html"
)

var packageHTTPClient = http.DefaultClient

type installSource struct {
	Type           string
	Source         string
	RequestedRef   string
	ResolvedRef    string
	SourceRevision string
	Remote         bool
}

type stagedPackage struct {
	Root      string
	Source    installSource
	CleanupFn func() error
}

func (s stagedPackage) Cleanup() error {
	if s.CleanupFn == nil {
		return nil
	}
	return s.CleanupFn()
}

type goImportMeta struct {
	Prefix   string
	VCS      string
	RepoRoot string
}

func stagePackageSource(raw string) (stagedPackage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return stagedPackage{}, fmt.Errorf("package source is required")
	}

	parsed, err := parseInstallSource(raw)
	if err != nil {
		return stagedPackage{}, err
	}
	switch parsed.Type {
	case PackageSourceTypeLocalPath:
		return stagedPackage{
			Root: parsed.Source,
			Source: installSource{
				Type:   PackageSourceTypeLocalPath,
				Source: parsed.Source,
			},
		}, nil
	case PackageSourceTypeModulePath:
		repoURL, err := resolveModuleRepositoryURL(parsed.Source)
		if err != nil {
			return stagedPackage{}, err
		}
		resolvedRef := parsed.RequestedRef
		if strings.EqualFold(parsed.RequestedRef, "latest") {
			ref, err := resolveLatestGitTag(repoURL)
			if err != nil {
				return stagedPackage{}, err
			}
			resolvedRef = ref
		}
		root, cleanup, err := cloneGitRevision(repoURL, resolvedRef)
		if err != nil {
			return stagedPackage{}, err
		}
		return stagedPackage{
			Root: root,
			Source: installSource{
				Type:           PackageSourceTypeModulePath,
				Source:         parsed.Source,
				RequestedRef:   parsed.RequestedRef,
				ResolvedRef:    resolvedRef,
				SourceRevision: resolvedRef,
				Remote:         true,
			},
			CleanupFn: cleanup,
		}, nil
	case PackageSourceTypeGitURL:
		root, cleanup, err := cloneGitRevision(parsed.Source, parsed.RequestedRef)
		if err != nil {
			return stagedPackage{}, err
		}
		return stagedPackage{
			Root: root,
			Source: installSource{
				Type:           PackageSourceTypeGitURL,
				Source:         parsed.Source,
				RequestedRef:   parsed.RequestedRef,
				ResolvedRef:    parsed.RequestedRef,
				SourceRevision: parsed.RequestedRef,
				Remote:         true,
			},
			CleanupFn: cleanup,
		}, nil
	case PackageSourceTypeArchiveURL:
		root, cleanup, err := downloadAndExtractPackageArchive(parsed.Source)
		if err != nil {
			return stagedPackage{}, err
		}
		return stagedPackage{
			Root: root,
			Source: installSource{
				Type:   PackageSourceTypeArchiveURL,
				Source: parsed.Source,
				Remote: true,
			},
			CleanupFn: cleanup,
		}, nil
	default:
		return stagedPackage{}, fmt.Errorf("unsupported source type %q", parsed.Type)
	}
}

func parseInstallSource(raw string) (installSource, error) {
	switch {
	case strings.HasPrefix(raw, "git+https://"), strings.HasPrefix(raw, "git+http://"), strings.HasPrefix(raw, "git+file://"):
		base, ref := splitSourceRef(strings.TrimPrefix(raw, "git+"))
		if ref == "" {
			return installSource{}, fmt.Errorf("git sources require an exact tag or commit")
		}
		if !semver.IsValid(canonicalSemver(ref)) && !looksLikeCommitHash(ref) {
			return installSource{}, fmt.Errorf("git sources require a semver tag or commit hash, got %q", ref)
		}
		return installSource{Type: PackageSourceTypeGitURL, Source: base, RequestedRef: ref}, nil
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "http://"):
		return installSource{Type: PackageSourceTypeArchiveURL, Source: raw}, nil
	default:
		if localPath, ok, versioned := detectLocalPath(raw); ok {
			if versioned {
				return installSource{}, fmt.Errorf("local path installs do not accept an @version suffix")
			}
			return installSource{Type: PackageSourceTypeLocalPath, Source: localPath}, nil
		}
		module, ref := splitSourceRef(raw)
		if ref == "" {
			return installSource{}, fmt.Errorf("module-path installs require @<version> or @latest")
		}
		if !strings.EqualFold(ref, "latest") && !semver.IsValid(canonicalSemver(ref)) {
			return installSource{}, fmt.Errorf("module-path installs require a semver version or @latest, got %q", ref)
		}
		if err := validatePackageModule(module); err != nil {
			return installSource{}, err
		}
		return installSource{Type: PackageSourceTypeModulePath, Source: module, RequestedRef: ref}, nil
	}
}

func detectLocalPath(raw string) (string, bool, bool) {
	if raw == "" {
		return "", false, false
	}
	base, ref := splitSourceRef(raw)
	candidates := []string{raw}
	if ref != "" {
		candidates = append(candidates, base)
	}
	for idx, candidate := range candidates {
		expanded := expandUserPath(candidate)
		if !filepath.IsAbs(expanded) && !strings.HasPrefix(expanded, "."+string(os.PathSeparator)) && !strings.HasPrefix(expanded, ".."+string(os.PathSeparator)) && !strings.HasPrefix(candidate, "~") {
			if _, err := os.Stat(expanded); err != nil {
				continue
			}
		}
		if _, err := os.Stat(expanded); err == nil {
			absPath, absErr := filepath.Abs(expanded)
			if absErr != nil {
				return expanded, true, idx == 1
			}
			return absPath, true, idx == 1
		}
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasPrefix(raw, "~") {
		absPath, err := filepath.Abs(expandUserPath(raw))
		if err != nil {
			return raw, true, false
		}
		return absPath, true, false
	}
	return "", false, false
}

func splitSourceRef(raw string) (string, string) {
	idx := strings.LastIndex(raw, "@")
	if idx <= 0 {
		return raw, ""
	}
	return raw[:idx], raw[idx+1:]
}

func resolveModuleRepositoryURL(module string) (string, error) {
	meta, found, err := fetchGoImportMeta(module)
	if err == nil && found {
		if !strings.EqualFold(strings.TrimSpace(meta.VCS), "git") {
			return "", fmt.Errorf("module %s resolves to unsupported vcs %q", module, meta.VCS)
		}
		if strings.TrimSpace(meta.RepoRoot) == "" {
			return "", fmt.Errorf("module %s resolved go-import metadata without repo root", module)
		}
		return strings.TrimSpace(meta.RepoRoot), nil
	}
	return "https://" + module, nil
}

func fetchGoImportMeta(module string) (goImportMeta, bool, error) {
	urlStr, err := moduleMetaURL(module)
	if err != nil {
		return goImportMeta{}, false, err
	}
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return goImportMeta{}, false, err
	}
	resp, err := packageHTTPClient.Do(req)
	if err != nil {
		return goImportMeta{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return goImportMeta{}, false, fmt.Errorf("fetch %s: unexpected status %s", urlStr, resp.Status)
	}
	meta, found, err := parseGoImportMeta(resp.Body, module)
	if err != nil {
		return goImportMeta{}, false, err
	}
	return meta, found, nil
}

func moduleMetaURL(module string) (string, error) {
	host, suffix, ok := strings.Cut(module, "/")
	if !ok {
		return "", fmt.Errorf("module %q must look like host/owner/name", module)
	}
	return (&url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     "/" + suffix,
		RawQuery: "go-get=1",
	}).String(), nil
}

func parseGoImportMeta(r io.Reader, module string) (goImportMeta, bool, error) {
	z := html.NewTokenizer(r)
	best := goImportMeta{}
	bestLen := -1
	for {
		switch z.Next() {
		case html.ErrorToken:
			if err := z.Err(); err != nil && err != io.EOF {
				return goImportMeta{}, false, err
			}
			if bestLen >= 0 {
				return best, true, nil
			}
			return goImportMeta{}, false, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "meta" || !hasAttr {
				continue
			}
			metaName := ""
			content := ""
			for hasAttr {
				key, value, more := z.TagAttr()
				switch string(key) {
				case "name":
					metaName = string(value)
				case "content":
					content = string(value)
				}
				hasAttr = more
			}
			if !strings.EqualFold(strings.TrimSpace(metaName), "go-import") {
				continue
			}
			fields := strings.Fields(content)
			if len(fields) != 3 {
				continue
			}
			meta := goImportMeta{
				Prefix:   strings.TrimSpace(fields[0]),
				VCS:      strings.TrimSpace(fields[1]),
				RepoRoot: strings.TrimSpace(fields[2]),
			}
			if !moduleHasPrefix(module, meta.Prefix) {
				continue
			}
			if l := len(meta.Prefix); l > bestLen {
				best = meta
				bestLen = l
			}
		}
	}
}

func moduleHasPrefix(module, prefix string) bool {
	module = strings.Trim(strings.TrimSpace(module), "/")
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	return module == prefix || strings.HasPrefix(module, prefix+"/")
}

func resolveLatestGitTag(repoURL string) (string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", "--refs", repoURL)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve latest tag for %s: %w", repoURL, err)
	}
	var tags []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		tag := strings.TrimPrefix(fields[1], "refs/tags/")
		tag = canonicalSemver(tag)
		if semver.IsValid(tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("%s: no semver tags found", repoURL)
	}
	sort.Slice(tags, func(i, j int) bool { return semver.Compare(tags[i], tags[j]) > 0 })
	return tags[0], nil
}

func cloneGitRevision(repoURL, revision string) (string, func() error, error) {
	tempDir, err := os.MkdirTemp("", "luc-pkg-git-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error { return os.RemoveAll(tempDir) }

	if semver.IsValid(canonicalSemver(revision)) {
		cmd := exec.Command("git", "clone", "--depth", "1", "--branch", revision, repoURL, tempDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = cleanup()
			return "", nil, fmt.Errorf("clone %s@%s: %v: %s", repoURL, revision, err, strings.TrimSpace(string(out)))
		}
		return tempDir, cleanup, nil
	}

	cmd := exec.Command("git", "clone", repoURL, tempDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("clone %s: %v: %s", repoURL, err, strings.TrimSpace(string(out)))
	}
	checkout := exec.Command("git", "-C", tempDir, "checkout", "--detach", revision)
	if out, err := checkout.CombinedOutput(); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("checkout %s: %v: %s", revision, err, strings.TrimSpace(string(out)))
	}
	return tempDir, cleanup, nil
}

func downloadAndExtractPackageArchive(url string) (string, func() error, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := packageHTTPClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	file, err := os.CreateTemp("", "luc-pkg-archive-*.tar.gz")
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", nil, err
	}
	root, cleanup, err := extractPackageArchive(file.Name())
	if err != nil {
		_ = os.Remove(file.Name())
		return "", nil, err
	}
	return root, func() error {
		_ = os.Remove(file.Name())
		return cleanup()
	}, nil
}
