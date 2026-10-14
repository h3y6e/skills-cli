package skill_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
)

func initGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	run := func(args ...string) string {
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

	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	writeFiles(t, dir, files)
	run("add", "-A")
	run("commit", "-m", "init")
	return dir
}

func installTracked(t *testing.T, repoRoot string, ref skill.SourceRef, resolved git.ResolvedRef, destDir string) []skill.DiscoveredSkill {
	t.Helper()

	discovered, err := skill.DiscoverSkills(repoRoot)
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}
	if err := skill.InstallTrackedSkills(t.Context(), discovered, repoRoot, ref, resolved, lock.NewLayout(destDir)); err != nil {
		t.Fatalf("InstallTrackedSkills() error = %v", err)
	}
	return discovered
}

func readInstalledMetadata(t *testing.T, destDir, name string) skill.SourceMetadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(destDir, name, "SKILL.md"))
	if err != nil {
		t.Fatalf("expected installed SKILL.md: %v", err)
	}
	meta, err := skill.ParseSourceMetadata(string(data))
	if err != nil {
		t.Fatalf("ParseSourceMetadata() error = %v", err)
	}
	return meta
}

func TestInstallTrackedSkills(t *testing.T) {
	t.Parallel()

	t.Run("when installing from a github source, SKILL.md carries injected metadata and no lockfile is written", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repoRoot := initGitRepo(t, map[string]string{
			"skills/foo/SKILL.md": "# Foo\n",
		})
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		ref := skill.SourceRef{SourceType: "github", CanonicalSource: "h3y6e/agent-skills"}
		resolved := git.ResolvedRef{Full: "refs/heads/main"}

		// Act
		installTracked(t, repoRoot, ref, resolved, destDir)

		// Assert
		meta := readInstalledMetadata(t, destDir, "foo")
		if meta.RepoURL != "https://github.com/h3y6e/agent-skills" {
			t.Errorf("RepoURL = %q", meta.RepoURL)
		}
		if meta.Ref != "refs/heads/main" {
			t.Errorf("Ref = %q", meta.Ref)
		}
		if meta.Path != "skills/foo" {
			t.Errorf("Path = %q", meta.Path)
		}
		if meta.Pinned != "" {
			t.Errorf("Pinned = %q, want empty", meta.Pinned)
		}
		wantTree := gitTreeSHA(t, repoRoot, "skills/foo")
		if meta.TreeSHA != wantTree {
			t.Errorf("TreeSHA = %q, want %q", meta.TreeSHA, wantTree)
		}
		if _, err := os.Stat(lock.FilePath(destDir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("lockfile must not be written in gh format, stat err = %v", err)
		}
	})

	t.Run("when the source has an explicit ref, the metadata records it as pinned", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repoRoot := initGitRepo(t, map[string]string{
			"skills/foo/SKILL.md": "# Foo\n",
		})
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		ref := skill.SourceRef{SourceType: "github", CanonicalSource: "h3y6e/agent-skills", Ref: "v1.0.0"}
		resolved := git.ResolvedRef{Full: "refs/tags/v1.0.0"}

		// Act
		installTracked(t, repoRoot, ref, resolved, destDir)

		// Assert
		meta := readInstalledMetadata(t, destDir, "foo")
		if meta.Pinned != "v1.0.0" {
			t.Errorf("Pinned = %q, want %q", meta.Pinned, "v1.0.0")
		}
		if meta.Ref != "refs/tags/v1.0.0" {
			t.Errorf("Ref = %q", meta.Ref)
		}
	})

	t.Run("when the source is not github, installing returns an error", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repoRoot := initGitRepo(t, map[string]string{
			"skills/foo/SKILL.md": "# Foo\n",
		})
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		ref := skill.SourceRef{SourceType: "gitlab", CanonicalSource: "h3y6e/agent-skills"}
		resolved := git.ResolvedRef{Full: "refs/heads/main"}
		discovered, err := skill.DiscoverSkills(repoRoot)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}

		// Act
		err = skill.InstallTrackedSkills(t.Context(), discovered, repoRoot, ref, resolved, lock.NewLayout(destDir))

		// Assert
		if err == nil {
			t.Fatal("InstallTrackedSkills() expected error, got nil")
		}
	})

	t.Run("when a skill with the same name exists, it is replaced", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repoRoot := initGitRepo(t, map[string]string{
			"skills/foo/SKILL.md": "# Foo v2\n",
		})
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, destDir, map[string]string{
			"foo/SKILL.md": "# Foo v1\n",
		})
		ref := skill.SourceRef{SourceType: "github", CanonicalSource: "h3y6e/agent-skills"}
		resolved := git.ResolvedRef{Full: "refs/heads/main"}

		// Act
		installTracked(t, repoRoot, ref, resolved, destDir)

		// Assert
		data, err := os.ReadFile(filepath.Join(destDir, "foo", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected installed SKILL.md: %v", err)
		}
		if got := string(data); !strings.Contains(got, "# Foo v2") {
			t.Errorf("installed content = %q, want it to contain %q", got, "# Foo v2")
		}
	})
}

func gitTreeSHA(t *testing.T, repoDir, path string) string {
	t.Helper()

	sha, err := git.TreeSHA(t.Context(), repoDir, path)
	if err != nil {
		t.Fatalf("TreeSHA() error = %v", err)
	}
	return sha
}
