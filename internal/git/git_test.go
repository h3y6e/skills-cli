package git_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
)

func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	os.Exit(m.Run())
}

func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func initBareRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	work := t.TempDir()

	gitInDir(t, work, "init", "-b", "main")
	gitInDir(t, work, "config", "user.email", "test@test.com")
	gitInDir(t, work, "config", "user.name", "Test")

	base := filepath.Join(work, ".gitkeep")
	if err := os.WriteFile(base, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "base")

	for name, content := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "add files")

	bare := t.TempDir()
	cmd := exec.Command("git", "clone", "--bare", work, bare)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --bare failed: %v\n%s", err, out)
	}

	return "file://" + bare
}

func initBareRepoWithRefs(t *testing.T) string {
	t.Helper()

	work := t.TempDir()

	gitInDir(t, work, "init", "-b", "main")
	gitInDir(t, work, "config", "user.email", "test@test.com")
	gitInDir(t, work, "config", "user.name", "Test")

	base := filepath.Join(work, "skills", "my-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("# Main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "main")
	gitInDir(t, work, "tag", "v1.0.0")

	gitInDir(t, work, "checkout", "-b", "feature/install")
	if err := os.WriteFile(base, []byte("# Feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "feature")
	gitInDir(t, work, "checkout", "main")

	bare := t.TempDir()
	cmd := exec.Command("git", "clone", "--bare", work, bare)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --bare failed: %v\n%s", err, out)
	}

	return "file://" + bare
}

func TestShallowClone(t *testing.T) {
	t.Parallel()

	bareRepo := initBareRepo(t, map[string]string{
		"skills/my-skill/SKILL.md": "# My Skill\n",
		"README.md":                "hello\n",
	})

	t.Run("clones repo to dest", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "clone-dest")

		err := git.ShallowClone(context.Background(), bareRepo, "", dest)
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

		if err := git.ShallowClone(context.Background(), bareRepo, "", dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		out := gitInDir(t, dest, "rev-list", "--count", "HEAD")
		count := strings.TrimSpace(out)
		if count != "1" {
			t.Errorf("expected 1 commit in shallow clone, got %s", count)
		}
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "bad-clone")

		err := git.ShallowClone(context.Background(), "/nonexistent/repo", "", dest)
		if err == nil {
			t.Fatal("expected error for invalid clone URL, got nil")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()
		dest := filepath.Join(t.TempDir(), "cancelled")

		ctx, cancel := context.WithCancel(context.Background())
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

		if err := git.ShallowClone(context.Background(), bareWithRefs, "feature/install", dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dest, "skills", "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected cloned branch file: %v", err)
		}
		if string(data) != "# Feature\n" {
			t.Errorf("branch clone content = %q, want %q", string(data), "# Feature\n")
		}
		out := gitInDir(t, dest, "rev-list", "--count", "HEAD")
		if strings.TrimSpace(out) != "1" {
			t.Errorf("expected shallow branch clone, got %s commits", strings.TrimSpace(out))
		}
	})

	t.Run("clones requested tag ref", func(t *testing.T) {
		t.Parallel()

		bareWithRefs := initBareRepoWithRefs(t)
		dest := filepath.Join(t.TempDir(), "tag-clone")

		if err := git.ShallowClone(context.Background(), bareWithRefs, "v1.0.0", dest); err != nil {
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
		os.MkdirAll(a, 0o755)
		os.MkdirAll(b, 0o755)
		os.WriteFile(filepath.Join(a, "file.txt"), []byte("same\n"), 0o644)
		os.WriteFile(filepath.Join(b, "file.txt"), []byte("same\n"), 0o644)

		output, hasDiff, err := git.DiffNoIndex(context.Background(), a, b)
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
		os.MkdirAll(a, 0o755)
		os.MkdirAll(b, 0o755)
		os.WriteFile(filepath.Join(a, "file.txt"), []byte("old\n"), 0o644)
		os.WriteFile(filepath.Join(b, "file.txt"), []byte("new\n"), 0o644)

		output, hasDiff, err := git.DiffNoIndex(context.Background(), a, b)
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
		os.MkdirAll(a, 0o755)
		os.MkdirAll(b, 0o755)
		os.WriteFile(filepath.Join(b, "new-file.txt"), []byte("added\n"), 0o644)

		output, hasDiff, err := git.DiffNoIndex(context.Background(), a, b)
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
		os.MkdirAll(a, 0o755)
		os.MkdirAll(b, 0o755)

		ctx, cancel := context.WithCancel(context.Background())
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
		bare := initBareRepo(t, map[string]string{"SKILL.md": "# S\n"})
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
