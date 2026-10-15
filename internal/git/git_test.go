package git_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

func checkout(t *testing.T, url, ref string) (git.Snapshot, error) {
	t.Helper()

	resolved, err := git.ResolveDefault(t.Context(), url, ref)
	if err != nil {
		return git.Snapshot{}, err
	}

	return git.Checkout(t.Context(), url, resolved, filepath.Join(t.TempDir(), "dest"))
}

func readSkill(t *testing.T, s git.Snapshot) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(s.Dir, "skills", "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestCheckout(t *testing.T) {
	t.Parallel()

	t.Run("when no ref is given, checkout writes the default branch without .git", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		s, err := checkout(t, repo, "")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v2.0.0-rc.1\n" {
			t.Errorf("SKILL.md = %q, want the main branch content", got)
		}

		if _, err := os.Stat(filepath.Join(s.Dir, ".git")); !os.IsNotExist(err) {
			t.Errorf("stat .git = %v, want not exist", err)
		}
	})

	t.Run("when a branch, a lightweight tag or an annotated tag is given, checkout writes its content", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		for ref, want := range map[string]string{"feature/install": "# Feature\n", "v1.0.0": "# v1.0.0\n", "annotated": "# v1.9.0\n"} {
			// Act
			s, err := checkout(t, repo, ref)
			// Assert
			if err != nil {
				t.Fatalf("%s: %v", ref, err)
			}

			if got := readSkill(t, s); got != want {
				t.Errorf("%s: SKILL.md = %q, want %q", ref, got, want)
			}
		}
	})

	t.Run("when a name matches both a branch and a tag, checkout writes the branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		s, err := checkout(t, repo, "collide")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if s.Ref.Full != "refs/heads/collide" || readSkill(t, s) != "# Feature\n" {
			t.Errorf("ref %q, SKILL.md %q, want the collide branch", s.Ref.Full, readSkill(t, s))
		}
	})

	t.Run("when the ref is a commit SHA, checkout writes that commit", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		sha := strings.TrimSpace(testutil.Git(t, strings.TrimPrefix(repo, "file://"), "rev-parse", "v1.0.0"))

		// Act
		s, err := checkout(t, repo, sha)
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v1.0.0\n" {
			t.Errorf("SKILL.md = %q, want the v1.0.0 content", got)
		}
	})

	t.Run("when a commit was fetched before, checkout writes it without the remote", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		ref, err := git.ResolveDefault(t.Context(), repo, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := git.Checkout(t.Context(), repo, ref, filepath.Join(t.TempDir(), "first")); err != nil {
			t.Fatal(err)
		}

		if err := os.RemoveAll(strings.TrimPrefix(repo, "file://")); err != nil {
			t.Fatal(err)
		}

		// Act
		s, err := git.Checkout(t.Context(), repo, ref, filepath.Join(t.TempDir(), "second"))
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v1.0.0\n" {
			t.Errorf("SKILL.md = %q, want the v1.0.0 content", got)
		}
	})

	t.Run("when the same remote is checked out concurrently, every checkout succeeds", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		refs := []string{"v1.0.0", "v1.9.0", "feature/install", "main", "collide", "v1.0.0"}
		errs := make([]error, len(refs))

		// Act
		var wg sync.WaitGroup
		for i, ref := range refs {
			wg.Go(func() { _, errs[i] = checkout(t, repo, ref) })
		}

		wg.Wait()

		// Assert
		for i, err := range errs {
			if err != nil {
				t.Errorf("%s: %v", refs[i], err)
			}
		}
	})

	t.Run("when checked out, Trees holds the tree SHA of the root and every directory", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		bare := strings.TrimPrefix(repo, "file://")

		// Act
		s, err := checkout(t, repo, "v1.0.0")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		for path, rev := range map[string]string{".": "v1.0.0^{tree}", "skills/my-skill": "v1.0.0:skills/my-skill"} {
			if want := strings.TrimSpace(testutil.Git(t, bare, "rev-parse", rev)); s.Trees[path] != want {
				t.Errorf("Trees[%q] = %q, want %q", path, s.Trees[path], want)
			}
		}
	})

	t.Run("when the remote does not exist, checkout fails", func(t *testing.T) {
		t.Parallel()

		// Act
		_, err := checkout(t, "/nonexistent/repo", "")

		// Assert
		if err == nil {
			t.Fatal("checkout succeeded without a remote")
		}
	})

	t.Run("when the context is cancelled, checkout fails", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		ref := git.ResolvedRef{Full: "refs/heads/main", Object: "refs/heads/main"}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Act
		_, err := git.Checkout(ctx, repo, ref, t.TempDir())

		// Assert
		if err == nil {
			t.Fatal("checkout succeeded with a cancelled context")
		}
	})
}

func TestNumStat(t *testing.T) {
	t.Parallel()

	t.Run("when text and binary files change, only text lines are counted", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := t.TempDir()
		testutil.WriteFiles(t, dir, map[string]string{
			"a/file.txt": "one\ntwo\n",
			"b/file.txt": "one\nthree\nfour\n",
			"b/new.md":   "added\n",
			"a/bin":      "\x00a",
			"b/bin":      "\x00b",
		})

		// Act
		added, deleted, err := git.NumStat(t.Context(), filepath.Join(dir, "a"), filepath.Join(dir, "b"))

		// Assert
		if err != nil || added != 3 || deleted != 1 {
			t.Errorf("NumStat() = +%d -%d, %v, want +3 -1", added, deleted, err)
		}
	})
}

func TestCheckoutUntrustedInput(t *testing.T) {
	t.Parallel()

	t.Run("when the ref looks like an option, git does not run it as one", func(t *testing.T) {
		t.Parallel()
		// Arrange
		bare := testutil.BareRepo(t, map[string]string{"SKILL.md": "# S\n"})
		marker := filepath.Join(t.TempDir(), "pwned")

		// Act
		_, _ = checkout(t, bare, "--upload-pack=touch "+marker)

		// Assert
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("--upload-pack ran: stat %s = %v", marker, err)
		}
	})

	t.Run("when the remote asks for credentials, resolving fails instead of prompting", func(t *testing.T) {
		t.Parallel()
		// Arrange
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(srv.Close)

		// Act
		_, err := checkout(t, srv.URL+"/o/r.git", "")

		// Assert
		if err == nil || !strings.Contains(err.Error(), "terminal prompts disabled") {
			t.Errorf("checkout error = %v, want terminal prompts disabled", err)
		}
	})
}

func TestCheckoutIgnoresAskpass(t *testing.T) {
	t.Run("when an askpass program is configured, resolving fails without running it", func(t *testing.T) {
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
		_, err := checkout(t, srv.URL+"/o/r.git", "")

		// Assert
		if err == nil {
			t.Fatal("checkout succeeded against a remote that rejects every request")
		}

		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Errorf("askpass ran: stat %s = %v", marker, statErr)
		}
	})
}
