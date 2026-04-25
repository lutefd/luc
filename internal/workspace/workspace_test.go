package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFindsGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := Detect(nested)
	if err != nil {
		t.Fatal(err)
	}

	if info.Root != nested {
		t.Fatalf("expected workspace root %q, got %q", nested, info.Root)
	}
	if !info.HasGit {
		t.Fatalf("expected HasGit true")
	}
	if info.GitRoot != root {
		t.Fatalf("expected git root %q, got %q", root, info.GitRoot)
	}
	if info.Branch != "main" {
		t.Fatalf("expected branch %q, got %q", "main", info.Branch)
	}
}

func TestDetectFallsBackToCWD(t *testing.T) {
	cwd := t.TempDir()
	info, err := Detect(cwd)
	if err != nil {
		t.Fatal(err)
	}

	if info.Root != cwd {
		t.Fatalf("expected root %q, got %q", cwd, info.Root)
	}
	if info.HasGit {
		t.Fatalf("expected HasGit false")
	}
	for _, dir := range []string{
		filepath.Join(cwd, ".luc", "history"),
		filepath.Join(cwd, ".luc", "logs"),
		filepath.Join(cwd, ".luc", "prompts"),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected state dir %q: %v", dir, err)
		}
	}
}

func TestDetectIgnoresInvalidParentGitDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "project")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := Detect(nested)
	if err != nil {
		t.Fatal(err)
	}

	if info.Root != nested {
		t.Fatalf("expected root %q, got %q", nested, info.Root)
	}
	if info.HasGit {
		t.Fatalf("expected HasGit false")
	}
}
