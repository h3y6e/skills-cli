package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
)

// initTaggedRepo creates a bare repo on branch main with:
//   - tags v1.0.0, v1.9.0, v2.0.0-rc.1 (prerelease), note (non-semver)
//   - branches feature/install and collide (a tag named collide also exists)
func initTaggedRepo(t *testing.T) string {
	t.Helper()

	work := t.TempDir()

	gitInDir(t, work, "init", "-b", "main")
	gitInDir(t, work, "config", "user.email", "test@test.com")
	gitInDir(t, work, "config", "user.name", "Test")

	write := func(name, content string) {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("skills/my-skill/SKILL.md", "# v1.0.0\n")
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "v1.0.0")
	gitInDir(t, work, "tag", "v1.0.0")

	write("skills/my-skill/SKILL.md", "# v1.9.0\n")
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "v1.9.0")
	gitInDir(t, work, "tag", "v1.9.0")
	gitInDir(t, work, "tag", "note")

	write("skills/my-skill/SKILL.md", "# v2.0.0-rc.1\n")
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "v2.0.0-rc.1")
	gitInDir(t, work, "tag", "v2.0.0-rc.1")
	gitInDir(t, work, "tag", "collide")

	gitInDir(t, work, "checkout", "-b", "feature/install")
	write("skills/my-skill/SKILL.md", "# Feature\n")
	gitInDir(t, work, "add", "-A")
	gitInDir(t, work, "commit", "-m", "feature")

	gitInDir(t, work, "checkout", "-b", "collide")
	gitInDir(t, work, "checkout", "main")

	bare := t.TempDir()
	gitInDir(t, work, "clone", "--bare", work, bare)

	return "file://" + bare
}

func TestResolveRef(t *testing.T) {
	t.Parallel()

	t.Run("when an explicit branch is given, resolving returns the branch ref", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		got, err := git.ResolveRef(context.Background(), repo, "feature/install")

		// Assert
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if got.Full != "refs/heads/feature/install" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/heads/feature/install")
		}
	})

	t.Run("when an explicit tag is given, resolving returns the tag ref", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		got, err := git.ResolveRef(context.Background(), repo, "v1.0.0")

		// Assert
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if got.Full != "refs/tags/v1.0.0" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/tags/v1.0.0")
		}
	})

	t.Run("when a name matches both a branch and a tag, resolving and cloning use the branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		dest := t.TempDir()

		// Act
		got, err := git.ResolveRef(context.Background(), repo, "collide")
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if err := git.ShallowClone(context.Background(), repo, got.Full, dest); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		// Assert
		if got.Full != "refs/heads/collide" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/heads/collide")
		}
		data, err := os.ReadFile(filepath.Join(dest, "skills/my-skill/SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "# Feature\n" {
			t.Errorf("cloned SKILL.md = %q, want branch content", data)
		}
	})

	t.Run("when an explicit ref matches nothing, resolving treats it as a commit SHA", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		sha := "329042b24ebc70240bb382a932a94bb90747622c"

		// Act
		got, err := git.ResolveRef(context.Background(), repo, sha)

		// Assert
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if got.Full != sha {
			t.Errorf("got = %+v, want Full = %q", got, sha)
		}
	})

	t.Run("when no ref is given, resolving returns the latest semver tag", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		got, err := git.ResolveRef(context.Background(), repo, "")

		// Assert
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if got.Full != "refs/tags/v1.9.0" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/tags/v1.9.0")
		}
	})

	t.Run("when no ref is given and the repo has no semver tags, resolving returns the default branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initBareRepo(t, map[string]string{"README.md": "hello\n"})

		// Act
		got, err := git.ResolveRef(context.Background(), repo, "")

		// Assert
		if err != nil {
			t.Fatalf("ResolveRef() error = %v", err)
		}
		if got.Full != "refs/heads/main" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/heads/main")
		}
	})
}

func TestTreeSHA(t *testing.T) {
	t.Parallel()

	t.Run("when a directory exists at HEAD, TreeSHA returns its git tree SHA", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		clone := filepath.Join(t.TempDir(), "clone")
		if err := git.ShallowClone(context.Background(), repo, "v1.0.0", clone); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}
		want := strings.TrimSpace(gitInDir(t, clone, "rev-parse", "HEAD:skills/my-skill"))

		// Act
		got, err := git.TreeSHA(context.Background(), clone, "skills/my-skill")

		// Assert
		if err != nil {
			t.Fatalf("TreeSHA() error = %v", err)
		}
		if got != want {
			t.Errorf("TreeSHA() = %q, want %q", got, want)
		}
	})

	t.Run("when the path is the repository root, TreeSHA returns the root tree SHA", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		clone := filepath.Join(t.TempDir(), "clone")
		if err := git.ShallowClone(context.Background(), repo, "v1.0.0", clone); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}
		want := strings.TrimSpace(gitInDir(t, clone, "rev-parse", "HEAD^{tree}"))

		// Act
		got, err := git.TreeSHA(context.Background(), clone, ".")

		// Assert
		if err != nil || got != want {
			t.Errorf("TreeSHA() = %q, %v, want %q", got, err, want)
		}
	})

	t.Run("when the path does not exist at HEAD, TreeSHA returns an error", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		clone := filepath.Join(t.TempDir(), "clone")
		if err := git.ShallowClone(context.Background(), repo, "", clone); err != nil {
			t.Fatalf("ShallowClone() error = %v", err)
		}

		// Act
		_, err := git.TreeSHA(context.Background(), clone, "skills/no-such-skill")

		// Assert
		if err == nil {
			t.Fatal("TreeSHA() expected error, got nil")
		}
	})
}
