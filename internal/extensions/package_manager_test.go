package extensions

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidatePackagePathAndPackRoundTrip(t *testing.T) {
	pkgRoot := writeTestPackage(t, t.TempDir(), PackageManifest{
		Schema:     "luc.pkg/v1",
		Module:     "github.com/acme/luc-theme-sunrise",
		Version:    "v1.2.0",
		LucVersion: ">=0.1.0",
		Name:       "luc-theme-sunrise",
	}, map[string]string{
		"themes/sunrise.yaml":   "name: sunrise\ninherits: light\n",
		"README.md":             "# Sunrise\n",
		"README.pt-BR.md":       "# Sunrise\n",
		"read.me.pt-br":         "# Sunrise\n",
		"CHANGELOG.md":          "# Changelog\n",
		".gitignore":            "dist/\n",
		"docs/usage.md":         "# Usage\n",
		"examples/basic.yaml":   "name: basic\n",
		"tests/package_test.sh": "#!/bin/sh\n",
	})

	validation, err := ValidatePackagePath(pkgRoot)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Manifest.Module != "github.com/acme/luc-theme-sunrise" {
		t.Fatalf("unexpected manifest %#v", validation.Manifest)
	}
	if len(validation.Categories) != 1 || validation.Categories[0] != "themes" {
		t.Fatalf("expected themes category, got %#v", validation.Categories)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	outDir := t.TempDir()
	if err := os.Chdir(outDir); err != nil {
		t.Fatal(err)
	}

	archivePath, packedValidation, err := PackPackage(pkgRoot)
	if err != nil {
		t.Fatal(err)
	}
	if packedValidation.Manifest.Version != "v1.2.0" {
		t.Fatalf("unexpected packed manifest %#v", packedValidation.Manifest)
	}
	stageRoot, cleanup, err := extractPackageArchive(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	archiveRoot, err := detectPackageRoot(stageRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.pt-BR.md", "read.me.pt-br", "CHANGELOG.md", ".gitignore", filepath.Join("docs", "usage.md"), filepath.Join("examples", "basic.yaml"), filepath.Join("tests", "package_test.sh")} {
		if _, err := os.Stat(filepath.Join(archiveRoot, name)); err != nil {
			t.Fatalf("expected packed archive to include %s: %v", name, err)
		}
	}

	archiveValidation, err := ValidatePackagePath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if archiveValidation.Manifest.Module != validation.Manifest.Module {
		t.Fatalf("expected archive validation to match original, got %#v", archiveValidation.Manifest)
	}
}

func TestValidatePackagePathRejectsUnsupportedTopLevelEntries(t *testing.T) {
	root := t.TempDir()
	writeTestPackage(t, root, PackageManifest{
		Schema:     "luc.pkg/v1",
		Module:     "github.com/acme/luc-bad",
		Version:    "v0.1.0",
		LucVersion: ">=0.1.0",
	}, map[string]string{
		"scripts/setup.sh": "echo nope\n",
	})

	_, err := ValidatePackagePath(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported top-level directory") {
		t.Fatalf("expected unsupported top-level directory error, got %v", err)
	}
}

func TestInstallListInspectAndRemovePackagesByScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := t.TempDir()
	pkgRoot := writeTestPackage(t, t.TempDir(), PackageManifest{
		Schema:      "luc.pkg/v1",
		Module:      "github.com/acme/luc-sunset",
		Version:     "v1.0.0",
		LucVersion:  ">=0.1.0",
		Name:        "luc-sunset",
		Description: "A sunset theme bundle.",
	}, map[string]string{
		"themes/sunset.yaml": "name: sunset\ninherits: dark\n",
		"prompts/voice.yaml": "schema: luc.prompt/v1\nid: voice\nprompt: Keep it terse.\n",
	})

	userInstall, err := InstallPackage(workspaceRoot, pkgRoot, InstallOptions{Scope: PackageScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	if userInstall.Record.Scope != PackageScopeUser {
		t.Fatalf("expected user scope, got %#v", userInstall.Record)
	}
	if !strings.HasPrefix(userInstall.Record.PackageDir, filepath.Join(home, ".luc", "packages")) {
		t.Fatalf("expected user install under home package store, got %q", userInstall.Record.PackageDir)
	}

	projectInstall, err := InstallPackage(workspaceRoot, pkgRoot, InstallOptions{Scope: PackageScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if projectInstall.Record.Scope != PackageScopeProject {
		t.Fatalf("expected project scope, got %#v", projectInstall.Record)
	}
	if !strings.HasPrefix(projectInstall.Record.PackageDir, filepath.Join(workspaceRoot, ".luc", "packages")) {
		t.Fatalf("expected project install under workspace package store, got %q", projectInstall.Record.PackageDir)
	}

	packages, err := ListInstalledPackages(workspaceRoot, PackageScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 {
		t.Fatalf("expected two installed packages, got %#v", packages)
	}

	inspected, err := InspectInstalledPackages(workspaceRoot, "github.com/acme/luc-sunset", PackageScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspected) != 2 {
		t.Fatalf("expected both scopes during inspect, got %#v", inspected)
	}
	if len(inspected[0].Categories) != 2 || inspected[0].Categories[0] != "prompts" || inspected[0].Categories[1] != "themes" {
		t.Fatalf("expected prompts/themes categories, got %#v", inspected[0].Categories)
	}

	removed, ok, err := RemoveInstalledPackage(workspaceRoot, "github.com/acme/luc-sunset", PackageScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || removed.Scope != PackageScopeProject {
		t.Fatalf("expected project package removal, got %#v ok=%v", removed, ok)
	}

	remaining, err := ListInstalledPackages(workspaceRoot, PackageScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Record.Scope != PackageScopeUser {
		t.Fatalf("expected only user install to remain, got %#v", remaining)
	}
}

func TestInstallPackageSupportsGitAndArchiveSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := t.TempDir()
	repoRoot := writeTestPackage(t, t.TempDir(), PackageManifest{
		Schema:      "luc.pkg/v1",
		Module:      "github.com/acme/luc-sky",
		Version:     "v1.3.0",
		LucVersion:  ">=0.1.0",
		Name:        "luc-sky",
		Description: "A sky theme bundle.",
	}, map[string]string{
		"themes/sky.yaml": "name: sky\ninherits: light\n",
	})

	initGitRepo(t, repoRoot, "v1.3.0")

	gitSource := "git+file://" + repoRoot + "@v1.3.0"
	installed, err := InstallPackage(workspaceRoot, gitSource, InstallOptions{Scope: PackageScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Record.SourceType != PackageSourceTypeGitURL || installed.Record.SourceRevision != "v1.3.0" {
		t.Fatalf("expected git install metadata, got %#v", installed.Record)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	packDir := t.TempDir()
	if err := os.Chdir(packDir); err != nil {
		t.Fatal(err)
	}
	archivePath, _, err := PackPackage(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	server := newTestArchiveServer(t, archivePath)
	defer server.Close()

	projectInstall, err := InstallPackage(workspaceRoot, server.URL+"/luc-sky.tar.gz", InstallOptions{Scope: PackageScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if projectInstall.Record.SourceType != PackageSourceTypeArchiveURL {
		t.Fatalf("expected archive install metadata, got %#v", projectInstall.Record)
	}
}

func TestInstallPackageSupportsVanityModulePathSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := t.TempDir()
	repoRoot := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("go-get") != "1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><meta name="go-import" content="` + r.Host + `/acme/luc-sky git file://` + repoRoot + `"></head></html>`))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	modulePath := serverURL.Host + "/acme/luc-sky"
	repoRoot = writeTestPackage(t, t.TempDir(), PackageManifest{
		Schema:      "luc.pkg/v1",
		Module:      modulePath,
		Version:     "v1.3.0",
		LucVersion:  ">=0.1.0",
		Name:        "luc-sky",
		Description: "A sky theme bundle.",
	}, map[string]string{
		"themes/sky.yaml": "name: sky\ninherits: light\n",
	})
	initGitRepo(t, repoRoot, "v1.3.0")

	prevClient := packageHTTPClient
	packageHTTPClient = server.Client()
	defer func() { packageHTTPClient = prevClient }()

	installed, err := InstallPackage(workspaceRoot, modulePath+"@latest", InstallOptions{Scope: PackageScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Record.SourceType != PackageSourceTypeModulePath {
		t.Fatalf("expected module-path install metadata, got %#v", installed.Record)
	}
	if installed.Record.Source != modulePath || installed.Record.SourceRevision != "v1.3.0" {
		t.Fatalf("expected module-path source metadata, got %#v", installed.Record)
	}
}

func TestInstallPackageRemoteExecutableRequiresConfirmation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := t.TempDir()
	repoRoot := writeTestPackage(t, t.TempDir(), PackageManifest{
		Schema:     "luc.pkg/v1",
		Module:     "github.com/acme/luc-toolkit",
		Version:    "v0.4.0",
		LucVersion: ">=0.1.0",
		Name:       "luc-toolkit",
	}, map[string]string{
		"tools/status.yaml": "name: status\ndescription: Show status.\ncommand: printf ok\nschema:\n  type: object\n  properties: {}\n",
	})

	initGitRepo(t, repoRoot, "v0.4.0")

	var stdout bytes.Buffer
	_, err := InstallPackage(workspaceRoot, "git+file://"+repoRoot+"@v0.4.0", InstallOptions{
		Scope:  PackageScopeUser,
		Stdin:  strings.NewReader("n\n"),
		Stdout: &stdout,
	})
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("expected aborted install, got %v", err)
	}
	if !strings.Contains(stdout.String(), "contains executable assets") {
		t.Fatalf("expected trust warning prompt, got %q", stdout.String())
	}

	packages, err := ListInstalledPackages(workspaceRoot, PackageScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 0 {
		t.Fatalf("expected no installed packages after abort, got %#v", packages)
	}
}

func writeTestPackage(t *testing.T, root string, manifest PackageManifest, files map[string]string) string {
	t.Helper()

	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "luc.pkg.yaml"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func initGitRepo(t *testing.T, root, tag string) {
	t.Helper()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run("init")
	run("config", "user.name", "luc tests")
	run("config", "user.email", "luc@example.com")
	run("add", ".")
	run("commit", "-m", "package")
	run("tag", tag)
}

func newTestArchiveServer(t *testing.T, archivePath string) *httptest.Server {
	t.Helper()

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(data)
	}))
}
