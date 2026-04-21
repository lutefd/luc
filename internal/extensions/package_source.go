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
	urls := gitRemoteCandidates(repoURL)
	var (
		out     []byte
		lastErr error
	)
	for _, candidate := range urls {
		cmd := exec.Command("git", "ls-remote", "--tags", "--refs", candidate)
		output, err := cmd.Output()
		if err == nil {
			out = output
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", fmt.Errorf("resolve latest tag for %s: %w", repoURL, lastErr)
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
	urls := gitRemoteCandidates(repoURL)
	var lastErr error
	for _, candidate := range urls {
		tempDir, err := os.MkdirTemp("", "luc-pkg-git-*")
		if err != nil {
			return "", nil, err
		}
		cleanup := func() error { return os.RemoveAll(tempDir) }

		if semver.IsValid(canonicalSemver(revision)) {
			cmd := exec.Command("git", "clone", "--depth", "1", "--branch", revision, candidate, tempDir)
			if out, err := cmd.CombinedOutput(); err != nil {
				_ = cleanup()
				lastErr = fmt.Errorf("clone %s@%s: %v: %s", candidate, revision, err, strings.TrimSpace(string(out)))
				continue
			}
			return tempDir, cleanup, nil
		}

		cmd := exec.Command("git", "clone", candidate, tempDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = cleanup()
			lastErr = fmt.Errorf("clone %s: %v: %s", candidate, err, strings.TrimSpace(string(out)))
			continue
		}
		checkout := exec.Command("git", "-C", tempDir, "checkout", "--detach", revision)
		if out, err := checkout.CombinedOutput(); err != nil {
			_ = cleanup()
			lastErr = fmt.Errorf("checkout %s: %v: %s", revision, err, strings.TrimSpace(string(out)))
			continue
		}
		return tempDir, cleanup, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("clone %s: no candidate URLs", repoURL)
	}
	return "", nil, lastErr
}

// gitRemoteCandidates returns a list of git remote URLs to try for the given
// repository, preferring SSH (scp-like) over HTTPS when possible. Enterprise
// networks frequently block outbound HTTPS to public git hosts, so we try SSH
// first and fall back to HTTPS. Inputs that are already SSH, git://, file://
// or unrecognized schemes are returned as-is (with no fallback duplicates).
func gitRemoteCandidates(repoURL string) []string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil
	}

	// Already an SSH URL (scp-like, e.g. git@github.com:owner/repo.git) or
	// ssh:// scheme — try it first, fall back to derived HTTPS if we can.
	if strings.HasPrefix(repoURL, "ssh://") || isScpLikeGitURL(repoURL) {
		if https, ok := sshToHTTPS(repoURL); ok && https != repoURL {
			return []string{repoURL, https}
		}
		return []string{repoURL}
	}

	// HTTPS/HTTP — try SSH first, fall back to the original HTTPS URL.
	if strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://") {
		if ssh, ok := httpsToSSH(repoURL); ok {
			return []string{ssh, repoURL}
		}
		return []string{repoURL}
	}

	// git://, file://, or anything else — use as-is.
	return []string{repoURL}
}

// httpsToSSH converts an https://host/owner/repo(.git) URL to the scp-like
// SSH form git@host:owner/repo.git. Returns false if the URL cannot be
// meaningfully converted (missing host or path).
func httpsToSSH(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := u.Host
	if host == "" {
		return "", false
	}
	// Strip any user info / port — SSH expects just the hostname with the
	// default git user.
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if colon := strings.Index(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return "", false
	}
	if !strings.HasSuffix(path, ".git") {
		path += ".git"
	}
	return "git@" + host + ":" + path, true
}

// sshToHTTPS converts an SSH URL (either scp-like git@host:owner/repo.git or
// ssh://git@host/owner/repo.git) into its https://host/owner/repo equivalent.
func sshToHTTPS(raw string) (string, bool) {
	if strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.Path == "" {
			return "", false
		}
		host := u.Hostname()
		path := strings.TrimPrefix(u.Path, "/")
		if path == "" {
			return "", false
		}
		return "https://" + host + "/" + path, true
	}
	if isScpLikeGitURL(raw) {
		// git@host:owner/repo.git
		at := strings.Index(raw, "@")
		colon := strings.Index(raw, ":")
		if at < 0 || colon < 0 || colon <= at+1 {
			return "", false
		}
		host := raw[at+1 : colon]
		path := raw[colon+1:]
		if host == "" || path == "" {
			return "", false
		}
		return "https://" + host + "/" + path, true
	}
	return "", false
}

// isScpLikeGitURL reports whether raw looks like the scp-style SSH form
// git@host:owner/repo.git (no scheme, user@host:path).
func isScpLikeGitURL(raw string) bool {
	if strings.Contains(raw, "://") {
		return false
	}
	at := strings.Index(raw, "@")
	colon := strings.Index(raw, ":")
	if at < 0 || colon < 0 || colon <= at {
		return false
	}
	// Guard against Windows-style drive letters (C:\...) by requiring the
	// '@' to appear before the ':'.
	return at < colon
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
