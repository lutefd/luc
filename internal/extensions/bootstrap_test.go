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

	for _, path := range []string{
		filepath.Join(root, "skills", "runtime-extension-authoring", "luc.yaml"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "SKILL.md"),
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
			"`schema: luc.prompt/v1`",
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
	if !containsAll(string(capabilityTools), "schema: luc.tool/v1", "structured_io", "client_actions", "`modal.open`", "`command.run`", "`client_action` uses `action`") {
		t.Fatalf("expected capability tool reference to cover structured exec tools, got %q", string(capabilityTools))
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
	if !containsAll(string(runtimeActions), "modal.open", "confirm.request", "view.open", "view.refresh", "command.run", "Tools, providers, and hooks with `client_actions`", "command palette", "built-in dialog surface") {
		t.Fatalf("expected runtime UI actions reference to include host-owned action guidance, got %q", string(runtimeActions))
	}

	hookPatterns, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "hook-patterns.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(hookPatterns), "message.assistant.final", "tool.finished", "\"workspace\": {\"root\":", "`message` as a compatibility alias", "`done: true`", "`client_action`", "`view.refresh`", "`client_result`", "`modal.open`", "`command.run`") {
		t.Fatalf("expected hook patterns reference to include concrete hook protocol guidance, got %q", string(hookPatterns))
	}
}

func TestEnsureGlobalRuntimeDoesNotOverwriteExistingAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".luc", "skills", "skill-usage", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

func TestEnsureGlobalRuntimeMirrorsEmbeddedBootstrapAssetsExactly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(home, ".luc")
	expected := map[string]string{}
	err := fs.WalkDir(bootstrapAssets, bootstrapAssetRoot, func(assetPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if assetPath == bootstrapAssetRoot || d.IsDir() {
			return nil
		}
		relPath := filepath.FromSlash(strings.TrimPrefix(assetPath, bootstrapAssetRoot+"/"))
		data, err := fs.ReadFile(bootstrapAssets, assetPath)
		if err != nil {
			return err
		}
		expected[relPath] = string(data)
		return nil
	})
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
