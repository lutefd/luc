package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/workspace"
)

func TestRunDoctorAndReload(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "token")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer

	if err := Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "workspace:") {
		t.Fatalf("expected doctor output, got %q", string(out))
	}

	os.Stdout = oldStdout
	if err := Run(context.Background(), []string{"reload"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunRPCEntrypoints(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "token")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"rpc"}},
		{name: "mode-alias", args: []string{"--mode", "rpc"}},
		{name: "continue", args: []string{"rpc", "--continue"}},
		{name: "session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			args := append([]string(nil), tc.args...)
			if tc.name == "session" {
				sessionID := prepareSessionMeta(t, root)
				args = []string{"rpc", "--session", sessionID}
			}
			out, err := runWithEOF(t, root, args)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out) != "" {
				t.Fatalf("expected rpc mode to avoid human stdout, got %q", out)
			}
		})
	}
}

func TestRunRPCRejectsInvalidSelectionFlags(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "token")

	_, err := runWithEOF(t, root, []string{"rpc", "--continue", "--session", "sess_1"})
	if err == nil || !strings.Contains(err.Error(), "--session and --continue") {
		t.Fatalf("expected invalid selection error, got %v", err)
	}
}

func prepareSessionMeta(t *testing.T, root string) string {
	t.Helper()

	root = canonicalPath(t, root)
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()

	info, err := workspace.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	store := history.NewStore(info.StateDir)
	defer store.Close()

	meta := controller.Session()
	meta.UpdatedAt = time.Now().UTC()
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	return meta.SessionID
}

func runWithEOF(t *testing.T, root string, args []string) (string, error) {
	t.Helper()

	root = canonicalPath(t, root)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	inReader, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := inWriter.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = inReader

	outReader, outWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outWriter

	err = Run(context.Background(), args)
	if closeErr := outWriter.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	out, readErr := io.ReadAll(outReader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(out), err
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
