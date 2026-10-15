package git_test

import (
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/testutil"
)

// initTaggedRepo creates a bare repo on branch main with:
//   - tags v1.0.0, v1.9.0, v2.0.0-rc.1 (prerelease), note (non-semver),
//     annotated (an annotated tag)
//   - branches feature/install and collide (a tag named collide also exists)
func initTaggedRepo(t *testing.T) string {
	t.Helper()

	work := testutil.InitWorkRepo(t)
	skillFile := "skills/my-skill/SKILL.md"

	testutil.Commit(t, work, "v1.0.0", map[string]string{skillFile: "# v1.0.0\n"})
	testutil.Git(t, work, "tag", "v1.0.0")

	testutil.Commit(t, work, "v1.9.0", map[string]string{skillFile: "# v1.9.0\n"})
	testutil.Git(t, work, "tag", "v1.9.0")
	testutil.Git(t, work, "tag", "note")
	testutil.Git(t, work, "tag", "-a", "annotated", "-m", "annotated")

	testutil.Commit(t, work, "v2.0.0-rc.1", map[string]string{skillFile: "# v2.0.0-rc.1\n"})
	testutil.Git(t, work, "tag", "v2.0.0-rc.1")
	testutil.Git(t, work, "tag", "collide")

	testutil.Git(t, work, "checkout", "-b", "feature/install")
	testutil.Commit(t, work, "feature", map[string]string{skillFile: "# Feature\n"})

	testutil.Git(t, work, "checkout", "-b", "collide")
	testutil.Git(t, work, "checkout", "main")

	return testutil.BareClone(t, work)
}

func TestResolveLatest(t *testing.T) {
	t.Parallel()

	t.Run("when an explicit branch is given, resolving returns the branch ref", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		got, err := git.ResolveLatest(t.Context(), repo, "feature/install")
		// Assert
		if err != nil {
			t.Fatalf("ResolveLatest() error = %v", err)
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
		got, err := git.ResolveLatest(t.Context(), repo, "v1.0.0")
		// Assert
		if err != nil {
			t.Fatalf("ResolveLatest() error = %v", err)
		}

		if got.Full != "refs/tags/v1.0.0" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/tags/v1.0.0")
		}
	})

	t.Run("when an explicit ref matches nothing, resolving treats it as a commit SHA", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		sha := "329042b24ebc70240bb382a932a94bb90747622c"

		// Act
		got, err := git.ResolveLatest(t.Context(), repo, sha)
		// Assert
		if err != nil {
			t.Fatalf("ResolveLatest() error = %v", err)
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
		got, err := git.ResolveLatest(t.Context(), repo, "")
		// Assert
		if err != nil {
			t.Fatalf("ResolveLatest() error = %v", err)
		}

		if got.Full != "refs/tags/v1.9.0" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/tags/v1.9.0")
		}
	})

	t.Run("when no ref is given and the repo has no semver tags, resolving returns the default branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := testutil.BareRepo(t, map[string]string{"README.md": "hello\n"})

		// Act
		got, err := git.ResolveLatest(t.Context(), repo, "")
		// Assert
		if err != nil {
			t.Fatalf("ResolveLatest() error = %v", err)
		}

		if got.Full != "refs/heads/main" {
			t.Errorf("Full = %q, want %q", got.Full, "refs/heads/main")
		}
	})
}

func TestResolveLatestTie(t *testing.T) {
	t.Parallel()

	t.Run("when two tags rank as the same version, resolving always picks the same one", func(t *testing.T) {
		t.Parallel()

		// Arrange
		work := testutil.InitWorkRepo(t)
		testutil.Commit(t, work, "one", map[string]string{"a": "1\n"})
		testutil.Git(t, work, "tag", "1.0.0")
		testutil.Commit(t, work, "two", map[string]string{"a": "2\n"})
		testutil.Git(t, work, "tag", "v1.0.0")
		repo := testutil.BareClone(t, work)

		for range 10 {
			// Act
			got, err := git.ResolveLatest(t.Context(), repo, "")

			// Assert
			if err != nil || got.Full != "refs/tags/v1.0.0" {
				t.Fatalf("ResolveLatest() = %+v, %v, want refs/tags/v1.0.0", got, err)
			}
		}
	})
}

func TestResolveDefault(t *testing.T) {
	t.Parallel()

	t.Run("when no ref is given, resolving returns the default branch despite semver tags", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		got, err := git.ResolveDefault(t.Context(), repo, "")
		// Assert
		if err != nil || got.Full != "refs/heads/main" {
			t.Errorf("ResolveDefault() = %+v, %v, want refs/heads/main", got, err)
		}
	})
}
