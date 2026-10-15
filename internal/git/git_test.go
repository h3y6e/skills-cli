package git_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/testutil"
)

func TestMain(m *testing.M) {
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(name, "/dev/null"); err != nil {
			panic(err)
		}
	}

	os.Exit(m.Run())
}

func initBareRepoWithRefs(t *testing.T) string {
	t.Helper()

	work := testutil.InitWorkRepo(t)
	skillFile := "skills/my-skill/SKILL.md"
	testutil.Commit(t, work, "main", map[string]string{skillFile: "# Main\n"})
	testutil.Git(t, work, "tag", "v1.0.0")

	testutil.Git(t, work, "checkout", "-b", "feature/install")
	testutil.Commit(t, work, "feature", map[string]string{skillFile: "# Feature\n"})
	testutil.Git(t, work, "checkout", "main")

	return testutil.BareClone(t, work)
}

func TestShallowClone(t *testing.T) {
	t.Parallel()

	bareRepo := testutil.BareRepo(t, map[string]string{
		"skills/my-skill/SKILL.md": "# My Skill\n",
		"README.md":                "hello\n",
	})

	t.Run("clones repo to dest", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "clone-dest")

		err := git.ShallowClone(t.Context(), bareRepo, "", dest)
		if err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		skillPath := filepath.Join(dest, "skills", "my-skill", "SKILL.md")

		data, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("expected skill file to exist: %v", err)
		}

		if string(data) != "# My Skill\n" {
			t.Errorf("skill file content = %q, want %q", string(data), "# My Skill\n")
		}
	})

	t.Run("is shallow (depth 1)", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "shallow-check")

		if err := git.ShallowClone(t.Context(), bareRepo, "", dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		out := testutil.Git(t, dest, "rev-list", "--count", "HEAD")

		count := strings.TrimSpace(out)
		if count != "1" {
			t.Errorf("expected 1 commit in shallow clone, got %s", count)
		}
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "bad-clone")

		err := git.ShallowClone(t.Context(), "/nonexistent/repo", "", dest)
		if err == nil {
			t.Fatal("expected error for invalid clone URL, got nil")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "cancelled")

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := git.ShallowClone(ctx, bareRepo, "", dest)
		if err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}
	})

	t.Run("clones requested branch ref", func(t *testing.T) {
		t.Parallel()

		bareWithRefs := initBareRepoWithRefs(t)
		dest := filepath.Join(t.TempDir(), "branch-clone")

		if err := git.ShallowClone(t.Context(), bareWithRefs, "feature/install", dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dest, "skills", "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected cloned branch file: %v", err)
		}

		if string(data) != "# Feature\n" {
			t.Errorf("branch clone content = %q, want %q", string(data), "# Feature\n")
		}

		out := testutil.Git(t, dest, "rev-list", "--count", "HEAD")
		if strings.TrimSpace(out) != "1" {
			t.Errorf("expected shallow branch clone, got %s commits", strings.TrimSpace(out))
		}
	})

	t.Run("clones requested tag ref", func(t *testing.T) {
		t.Parallel()

		bareWithRefs := initBareRepoWithRefs(t)
		dest := filepath.Join(t.TempDir(), "tag-clone")

		if err := git.ShallowClone(t.Context(), bareWithRefs, "v1.0.0", dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dest, "skills", "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected cloned tag file: %v", err)
		}

		if string(data) != "# Main\n" {
			t.Errorf("tag clone content = %q, want %q", string(data), "# Main\n")
		}
	})
}

func TestDiffNoIndex(t *testing.T) {
	t.Parallel()

	t.Run("no diff when directories are identical", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")
		testutil.WriteFiles(t, dir, map[string]string{"a/file.txt": "same\n", "b/file.txt": "same\n"})

		output, hasDiff, err := git.DiffNoIndex(t.Context(), a, b)
		if err != nil {
			t.Fatalf("DiffNoIndex() error = %v", err)
		}

		if hasDiff {
			t.Errorf("expected hasDiff=false for identical dirs, got true; output:\n%s", output)
		}
	})

	t.Run("reports diff when files differ", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")
		testutil.WriteFiles(t, dir, map[string]string{"a/file.txt": "old\n", "b/file.txt": "new\n"})

		output, hasDiff, err := git.DiffNoIndex(t.Context(), a, b)
		if err != nil {
			t.Fatalf("DiffNoIndex() error = %v", err)
		}

		if !hasDiff {
			t.Error("expected hasDiff=true for differing files")
		}

		if len(output) == 0 {
			t.Error("expected non-empty diff output")
		}
	})

	t.Run("reports diff for added file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")

		if err := os.MkdirAll(a, 0o755); err != nil {
			t.Fatal(err)
		}

		testutil.WriteFiles(t, dir, map[string]string{"b/new-file.txt": "added\n"})

		output, hasDiff, err := git.DiffNoIndex(t.Context(), a, b)
		if err != nil {
			t.Fatalf("DiffNoIndex() error = %v", err)
		}

		if !hasDiff {
			t.Error("expected hasDiff=true for added file")
		}

		if len(output) == 0 {
			t.Error("expected non-empty diff output for added file")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")

		if err := os.MkdirAll(a, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.MkdirAll(b, 0o755); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, _, err := git.DiffNoIndex(ctx, a, b)
		if err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}
	})
}

func TestShallowCloneUntrustedInput(t *testing.T) {
	t.Parallel()

	t.Run("when the ref looks like an option, git does not run it as one", func(t *testing.T) {
		t.Parallel()
		// Arrange
		bare := testutil.BareRepo(t, map[string]string{"SKILL.md": "# S\n"})
		marker := filepath.Join(t.TempDir(), "pwned")

		// Act
		_ = git.ShallowClone(t.Context(), bare, "--upload-pack=touch "+marker, filepath.Join(t.TempDir(), "dest"))

		// Assert
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("--upload-pack ran: stat %s = %v", marker, err)
		}
	})

	t.Run("when the remote asks for credentials, cloning fails instead of prompting", func(t *testing.T) {
		t.Parallel()
		// Arrange
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(srv.Close)

		// Act
		err := git.ShallowClone(t.Context(), srv.URL+"/o/r.git", "", filepath.Join(t.TempDir(), "dest"))

		// Assert
		if err == nil || !strings.Contains(err.Error(), "terminal prompts disabled") {
			t.Errorf("ShallowClone() error = %v, want terminal prompts disabled", err)
		}
	})
}

func TestShallowCloneIgnoresAskpass(t *testing.T) {
	t.Run("when an askpass program is configured, cloning fails without running it", func(t *testing.T) {
		// Arrange
		marker := filepath.Join(t.TempDir(), "asked")
		askpass := filepath.Join(t.TempDir(), "askpass")
		if err := os.WriteFile(askpass, []byte("#!/bin/sh\ntouch "+marker+"\necho x\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_ASKPASS", askpass)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(srv.Close)

		// Act
		err := git.ShallowClone(t.Context(), srv.URL+"/o/r.git", "", filepath.Join(t.TempDir(), "dest"))

		// Assert
		if err == nil {
			t.Fatal("ShallowClone() succeeded against a remote that rejects every request")
		}
		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Errorf("askpass ran: stat %s = %v", marker, statErr)
		}
	})
}
