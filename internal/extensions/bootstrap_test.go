package extensions

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestEnsureGlobalRuntimeCreatesDirsAndSeedsAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(home, ".luc")
	for _, dir := range []string{
		root,
		filepath.Join(root, "tools"),
		filepath.Join(root, "providers"),
		filepath.Join(root, "ui"),
		filepath.Join(root, "hooks"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "themes"),
		filepath.Join(root, "prompts"),
		filepath.Join(root, "skills", "runtime-extension-authoring"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references"),
		filepath.Join(root, "skills", "skill-usage"),
		filepath.Join(root, "skills", "theme-creator"),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected %q to exist: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, bootstrapStateFile)); err != nil {
		t.Fatalf("expected bootstrap state file to exist: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, "skills", "runtime-extension-authoring", "luc.yaml"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "SKILL.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "extension-model.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "extension-host-protocol.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "capability-tools.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "provider-ui-composition.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "runtime-ui-views.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "runtime-ui-actions.md"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "references", "hook-patterns.md"),
		filepath.Join(root, "skills", "skill-usage", "luc.yaml"),
		filepath.Join(root, "skills", "skill-usage", "SKILL.md"),
		filepath.Join(root, "skills", "theme-creator", "luc.yaml"),
		filepath.Join(root, "skills", "theme-creator", "SKILL.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected seeded asset %q: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("expected seeded asset %q to be non-empty", path)
		}
	}

	runtimeAuthoring, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(runtimeAuthoring) == "" ||
		!containsAll(string(runtimeAuthoring),
			"`schema: luc.tool/v1`",
			"`schema: luc.tool/v2`",
			"`luc.extension/v1`",
			"`schema: luc.prompt/v1`",
			"`references/extension-model.md`",
			"`references/extension-host-protocol.md`",
			"`references/capability-tools.md`",
			"`references/provider-ui-composition.md`",
			"`references/runtime-ui-views.md`",
			"`references/runtime-ui-actions.md`",
			"`references/hook-patterns.md`",
			"`Overview`",
			"`inspector_tab`",
		) {
		t.Fatalf("expected runtime-extension-authoring skill to include capability guidance, got %q", string(runtimeAuthoring))
	}

	runtimeAuthoringManifest, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "luc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(runtimeAuthoringManifest), "triggers:", "runtime extension", "overview tab", "inspector view", "panel", "prompt extension", "prompt tuning") {
		t.Fatalf("expected runtime-extension-authoring manifest to include trigger hints, got %q", string(runtimeAuthoringManifest))
	}

	capabilityTools, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "capability-tools.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(capabilityTools), "schema: luc.tool/v1", "schema: luc.tool/v2", "structured_io", "client_actions", "tool_invoke", "tool_result", "`modal.open`", "`command.run`", "`client_action` uses `action`") {
		t.Fatalf("expected capability tool reference to cover structured exec tools, got %q", string(capabilityTools))
	}

	extensionModel, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "extension-model.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(extensionModel), "Which Surface To Use", "`luc.tool/v1`", "`luc.tool/v2`", "`luc.extension/v1`", "`luc.hook/v1`", "`luc.ui/v1`", "`luc.prompt/v1`") {
		t.Fatalf("expected extension model reference to include surface-selection guidance, got %q", string(extensionModel))
	}

	extensionProtocol, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "extension-host-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(extensionProtocol), "Startup Sequence", "Minimal Python Host", "Minimal Go Host", "`storage_snapshot`", "`tool_invoke`", "Hybrid Package Layout", "disabled for that session") {
		t.Fatalf("expected extension host protocol reference to include direct protocol guidance, got %q", string(extensionProtocol))
	}

	runtimeViews, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "runtime-ui-views.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(runtimeViews), "luc.ui/v1", "inspector_tab", "activity.summary", "built-in `Overview`") {
		t.Fatalf("expected runtime UI views reference to include concrete inspector-tab guidance, got %q", string(runtimeViews))
	}

	runtimeActions, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "runtime-ui-actions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(runtimeActions), "modal.open", "confirm.request", "view.open", "view.refresh", "command.run", "tool.run", "Tools, providers, hooks, and extension hosts", "command palette", "built-in dialog surface") {
		t.Fatalf("expected runtime UI actions reference to include host-owned action guidance, got %q", string(runtimeActions))
	}

	hookPatterns, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "hook-patterns.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(hookPatterns), "message.assistant.final", "tool.finished", "\"root\": \"/abs/workspace\"", "`message` as a compatibility alias", "`done: true`", "`client_action`", "`view.refresh`", "`client_result`", "`modal.open`", "`command.run`", "`tool.run`", "`input.transform`", "`tool.preflight`") {
		t.Fatalf("expected hook patterns reference to include concrete hook protocol guidance, got %q", string(hookPatterns))
	}
}

func TestEnsureGlobalRuntimeDoesNotOverwriteManagedAssetsWithinSameBootstrapVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".luc", "skills", "skill-usage", "SKILL.md")
	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom" {
		t.Fatalf("expected existing asset to be preserved, got %q", string(data))
	}
}

func TestEnsureGlobalRuntimeRefreshesManagedAssetsWhenBootstrapDigestChanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(home, ".luc")
	path := filepath.Join(root, "skills", "skill-usage", "SKILL.md")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bootstrapStateFile), []byte("outdated-digest"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assetMap, digest, err := expectedBootstrapAssets()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != assetMap[filepath.Join("skills", "skill-usage", "SKILL.md")] {
		t.Fatalf("expected managed asset to refresh to embedded content, got %q", string(data))
	}
	state, err := os.ReadFile(filepath.Join(root, bootstrapStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(state)) != digest {
		t.Fatalf("expected bootstrap digest %q, got %q", digest, strings.TrimSpace(string(state)))
	}
}

func TestEnsureGlobalRuntimeRefreshesLegacyManagedAssetsWithoutTouchingUserSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := filepath.Join(home, ".luc")
	managedPath := filepath.Join(root, "skills", "skill-usage", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPath, []byte("legacy-managed"), 0o644); err != nil {
		t.Fatal(err)
	}

	userSkillPath := filepath.Join(root, "skills", "custom-user-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userSkillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSkillPath, []byte("user-skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	assetMap, _, err := expectedBootstrapAssets()
	if err != nil {
		t.Fatal(err)
	}
	managedData, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(managedData) != assetMap[filepath.Join("skills", "skill-usage", "SKILL.md")] {
		t.Fatalf("expected legacy managed asset to refresh to embedded content, got %q", string(managedData))
	}
	userData, err := os.ReadFile(userSkillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(userData) != "user-skill" {
		t.Fatalf("expected user skill to remain untouched, got %q", string(userData))
	}
}

func TestEnsureGlobalRuntimeMirrorsEmbeddedBootstrapAssetsExactly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(home, ".luc")
	expected, _, err := expectedBootstrapAssets()
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relPath == bootstrapStateFile {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got[relPath] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(expected) {
		t.Fatalf("expected %d bootstrapped files, got %d\nexpected=%v\ngot=%v", len(expected), len(got), sortedKeys(expected), sortedKeys(got))
	}
	for relPath, want := range expected {
		gotContent, ok := got[relPath]
		if !ok {
			t.Fatalf("expected bootstrapped file %q to exist; got=%v", relPath, sortedKeys(got))
		}
		if gotContent != want {
			t.Fatalf("expected bootstrapped file %q to match embedded asset", relPath)
		}
	}
}

func expectedBootstrapAssets() (map[string]string, string, error) {
	assets, digest, err := bootstrapAssetManifest()
	if err != nil {
		return nil, "", err
	}
	expected := make(map[string]string, len(assets))
	for _, asset := range assets {
		expected[filepath.FromSlash(asset.RelPath)] = string(asset.Data)
	}
	return expected, digest, nil
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
