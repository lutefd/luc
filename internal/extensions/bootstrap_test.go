package extensions

import (
	"os"
	"path/filepath"
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
	if !containsAll(string(runtimeAuthoringManifest), "triggers:", "runtime extension", "overview tab", "inspector view", "panel") {
		t.Fatalf("expected runtime-extension-authoring manifest to include trigger hints, got %q", string(runtimeAuthoringManifest))
	}

	capabilityTools, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "capability-tools.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(capabilityTools), "schema: luc.tool/v1", "structured_io", "client_actions") {
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
	if !containsAll(string(runtimeActions), "modal.open", "confirm.request", "view.open", "view.refresh", "command.run") {
		t.Fatalf("expected runtime UI actions reference to include host-owned action guidance, got %q", string(runtimeActions))
	}

	hookPatterns, err := os.ReadFile(filepath.Join(root, "skills", "runtime-extension-authoring", "references", "hook-patterns.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(hookPatterns), "message.assistant.final", "tool.finished", "\"workspace\": {\"root\":", "`message` as a compatibility alias", "`done: true`") {
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

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
