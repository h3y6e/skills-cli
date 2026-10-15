package git_test

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

func fetch(t *testing.T, url, ref string) (git.Snapshot, error) {
	t.Helper()

	resolved, err := git.ResolveDefault(t.Context(), url, ref)
	if err != nil {
		return git.Snapshot{}, err
	}

	s, err := git.Fetch(t.Context(), url, resolved)
	if err == nil {
		t.Cleanup(func() { _ = s.Close() })
	}

	return s, err
}

func readSkill(t *testing.T, s git.Snapshot) string {
	t.Helper()

	data, err := fs.ReadFile(s.FS, "skills/my-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestFetch(t *testing.T) {
	t.Parallel()

	t.Run("when no ref is given, fetching reads the default branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		s, err := fetch(t, repo, "")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v2.0.0-rc.1\n" {
			t.Errorf("SKILL.md = %q, want the main branch content", got)
		}
	})

	t.Run("when the commit holds links and executables, the files keep their modes and links are never followed", func(t *testing.T) {
		t.Parallel()

		// Arrange
		work := testutil.InitWorkRepo(t)
		testutil.WriteFiles(t, work, map[string]string{"skills/a/SKILL.md": "# A\n", "skills/a/run.sh": "#!/bin/sh\n"})
		if err := os.Chmod(filepath.Join(work, "skills", "a", "run.sh"), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink("../../../etc", filepath.Join(work, "skills", "a", "escape")); err != nil {
			t.Fatal(err)
		}

		testutil.Git(t, work, "add", "-A")
		testutil.Git(t, work, "commit", "-m", "init")

		// Act
		s, err := fetch(t, "file://"+work, "")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		var walked []string

		_ = fs.WalkDir(s.FS, "skills", func(p string, d fs.DirEntry, err error) error {
			walked = append(walked, p+" "+d.Type().String())
			return err
		})

		want := []string{"skills d---------", "skills/a d---------", "skills/a/SKILL.md ----------", "skills/a/escape L---------", "skills/a/run.sh ----------"}
		if !slices.Equal(walked, want) {
			t.Errorf("walked %q, want %q", walked, want)
		}

		if info, err := fs.Stat(s.FS, "skills/a/run.sh"); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("run.sh mode = %v, %v, want 0755", info, err)
		}

		if _, err := s.FS.Open("skills/a/escape/passwd"); err == nil {
			t.Error("opened a path through a link")
		}
	})

	t.Run("when a branch, a lightweight tag or an annotated tag is given, fetching reads its content", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		for ref, want := range map[string]string{"feature/install": "# Feature\n", "v1.0.0": "# v1.0.0\n", "annotated": "# v1.9.0\n"} {
			// Act
			s, err := fetch(t, repo, ref)
			// Assert
			if err != nil {
				t.Fatalf("%s: %v", ref, err)
			}

			if got := readSkill(t, s); got != want {
				t.Errorf("%s: SKILL.md = %q, want %q", ref, got, want)
			}
		}
	})

	t.Run("when a name matches both a branch and a tag, fetching reads the branch", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		// Act
		s, err := fetch(t, repo, "collide")
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if s.Ref.Full != "refs/heads/collide" || readSkill(t, s) != "# Feature\n" {
			t.Errorf("ref %q, SKILL.md %q, want the collide branch", s.Ref.Full, readSkill(t, s))
		}
	})

	t.Run("when the ref is a commit SHA, fetching reads that commit", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		sha := strings.TrimSpace(testutil.Git(t, strings.TrimPrefix(repo, "file://"), "rev-parse", "v1.0.0"))

		// Act
		s, err := fetch(t, repo, sha)
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v1.0.0\n" {
			t.Errorf("SKILL.md = %q, want the v1.0.0 content", got)
		}
	})

	t.Run("when a commit was fetched before, fetching reads it without the remote", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)

		ref, err := git.ResolveDefault(t.Context(), repo, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}

		first, err := git.Fetch(t.Context(), repo, ref)
		if err != nil {
			t.Fatal(err)
		}

		_ = first.Close()

		if err := os.RemoveAll(strings.TrimPrefix(repo, "file://")); err != nil {
			t.Fatal(err)
		}

		// Act
		s, err := git.Fetch(t.Context(), repo, ref)
		t.Cleanup(func() { _ = s.Close() })
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if got := readSkill(t, s); got != "# v1.0.0\n" {
			t.Errorf("SKILL.md = %q, want the v1.0.0 content", got)
		}
	})

	t.Run("when the same remote is fetched concurrently, every fetch succeeds", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		refs := []string{"v1.0.0", "v1.9.0", "feature/install", "main", "collide", "v1.0.0"}
		errs := make([]error, len(refs))

		// Act
		var wg sync.WaitGroup
		for i, ref := range refs {
			wg.Go(func() { _, errs[i] = fetch(t, repo, ref) })
		}

		wg.Wait()

		// Assert
		for i, err := range errs {
			if err != nil {
				t.Errorf("%s: %v", refs[i], err)
			}
		}
	})

	t.Run("when fetched, Trees holds the tree SHA of the root and every directory", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		bare := strings.TrimPrefix(repo, "file://")

		// Act
		s, err := fetch(t, repo, "v1.0.0")
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

	t.Run("when the remote does not exist, fetching fails", func(t *testing.T) {
		t.Parallel()

		// Act
		_, err := fetch(t, "/nonexistent/repo", "")

		// Assert
		if err == nil {
			t.Fatal("fetch succeeded without a remote")
		}
	})

	t.Run("when the context is cancelled, fetching fails", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		ref := git.ResolvedRef{Full: "refs/heads/main", Object: "refs/heads/main"}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Act
		_, err := git.Fetch(ctx, repo, ref)

		// Assert
		if err == nil {
			t.Fatal("fetch succeeded with a cancelled context")
		}
	})
}

func TestReadTree(t *testing.T) {
	t.Parallel()

	t.Run("when a fetched commit holds the tree, reading it lists the tree's files", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		s, err := fetch(t, repo, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}

		// Act
		tree, err := git.ReadTree(t.Context(), repo, s.Trees["skills/my-skill"])
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = tree.Close() })

		if data, err := fs.ReadFile(tree.FS, "SKILL.md"); string(data) != "# v1.0.0\n" {
			t.Errorf("SKILL.md = %q, %v, want the v1.0.0 content", data, err)
		}
	})

	t.Run("when nothing fetched holds the tree, reading it reports it is not cached", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		if _, err := fetch(t, repo, "v1.0.0"); err != nil {
			t.Fatal(err)
		}

		bare := strings.TrimPrefix(repo, "file://")
		feature := strings.TrimSpace(testutil.Git(t, bare, "rev-parse", "feature/install:skills/my-skill"))

		// Act
		_, err := git.ReadTree(t.Context(), repo, feature)

		// Assert
		if !errors.Is(err, git.ErrNotCached) {
			t.Errorf("ReadTree() error = %v, want ErrNotCached", err)
		}
	})

	t.Run("when the tree SHA is a ref name, reading it fails without resolving the name", func(t *testing.T) {
		t.Parallel()

		// Arrange
		repo := initTaggedRepo(t)
		if _, err := fetch(t, repo, "v1.0.0"); err != nil {
			t.Fatal(err)
		}

		// Act
		_, err := git.ReadTree(t.Context(), repo, "refs/tags/v1.0.0")

		// Assert
		if err == nil || errors.Is(err, git.ErrNotCached) {
			t.Errorf("ReadTree() error = %v, want an invalid tree SHA", err)
		}
	})
}

func TestFetchUntrustedInput(t *testing.T) {
	t.Parallel()

	t.Run("when the ref looks like an option, git does not run it as one", func(t *testing.T) {
		t.Parallel()
		// Arrange
		bare := testutil.BareRepo(t, map[string]string{"SKILL.md": "# S\n"})
		marker := filepath.Join(t.TempDir(), "pwned")

		// Act
		_, _ = fetch(t, bare, "--upload-pack=touch "+marker)

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
		_, err := fetch(t, srv.URL+"/o/r.git", "")

		// Assert
		if err == nil || !strings.Contains(err.Error(), "terminal prompts disabled") {
			t.Errorf("fetch error = %v, want terminal prompts disabled", err)
		}
	})
}

func TestFetchIgnoresAskpass(t *testing.T) {
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
		_, err := fetch(t, srv.URL+"/o/r.git", "")

		// Assert
		if err == nil {
			t.Fatal("fetch succeeded against a remote that rejects every request")
		}

		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Errorf("askpass ran: stat %s = %v", marker, statErr)
		}
	})
}
