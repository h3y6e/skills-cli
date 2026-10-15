package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/testutil"
)

func TestLock(t *testing.T) {
	t.Parallel()

	t.Run("when another holder keeps the lock, waiting for it ends with the context", func(t *testing.T) {
		t.Parallel()

		// Arrange
		path := filepath.Join(t.TempDir(), "x.lock")

		unlock, err := lock(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(unlock)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Act
		_, err = lock(ctx, path)

		// Assert
		if !errors.Is(err, context.Canceled) {
			t.Errorf("lock() error = %v, want context.Canceled", err)
		}
	})
}

func TestCacheRepo(t *testing.T) {
	t.Parallel()

	t.Run("when a repository is opened, only the owner may enter the cache", func(t *testing.T) {
		t.Parallel()

		// Arrange
		c := NewCache()

		t.Cleanup(func() { _ = c.Close() })

		// Act
		r, err := c.repo(t.Context(), testutil.BareRepo(t, map[string]string{"a": "a\n"}))
		// Assert
		if err != nil {
			t.Fatal(err)
		}

		if info, err := os.Stat(filepath.Dir(filepath.Dir(r.gitDir))); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("cache root = %v, %v, want drwx------", info, err)
		}
	})

	t.Run("when a branch moves on, gc keeps the tree fetched before", func(t *testing.T) {
		t.Parallel()

		// Arrange
		work := testutil.InitWorkRepo(t)
		testutil.Commit(t, work, "v1", map[string]string{"SKILL.md": "v1\n"})
		url := testutil.BareClone(t, work)
		bare := strings.TrimPrefix(url, "file://")

		c := NewCache()

		t.Cleanup(func() { _ = c.Close() })

		first, err := ResolveDefault(t.Context(), url, "")
		if err != nil {
			t.Fatal(err)
		}

		old, err := c.Fetch(t.Context(), url, first)
		if err != nil {
			t.Fatal(err)
		}

		oldTree, err := old.TreeSHA(".")
		if err != nil {
			t.Fatal(err)
		}

		// Two commits leave the shallow fetch of the new tip unconnected to the old one.
		testutil.Commit(t, work, "v2", map[string]string{"SKILL.md": "v2\n"})
		testutil.Commit(t, work, "v3", map[string]string{"SKILL.md": "v3\n"})
		testutil.Git(t, work, "push", "-q", bare, "main")

		second, err := ResolveDefault(t.Context(), url, "")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := c.Fetch(t.Context(), url, second); err != nil {
			t.Fatal(err)
		}

		r, err := c.repo(t.Context(), url)
		if err != nil {
			t.Fatal(err)
		}

		// Act
		testutil.Git(t, r.gitDir, "gc", "-q", "--prune=now")

		// Assert
		fresh := NewCache()

		t.Cleanup(func() { _ = fresh.Close() })

		if _, err := fresh.ReadTree(t.Context(), url, oldTree); err != nil {
			t.Errorf("ReadTree(old) error = %v, want the tree fetched before", err)
		}
	})
}

func TestCacheRoot(t *testing.T) {
	// Not parallel: sets XDG_CACHE_HOME.
	t.Run("when XDG_CACHE_HOME is absolute, the cache lives there", func(t *testing.T) {
		// Arrange
		want := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", want)

		// Act
		got, err := cacheRoot()

		// Assert
		if err != nil || got != want {
			t.Errorf("cacheRoot() = %q, %v, want %q", got, err, want)
		}
	})

	t.Run("when XDG_CACHE_HOME is relative, it is ignored", func(t *testing.T) {
		// Arrange
		t.Setenv("XDG_CACHE_HOME", "relative")

		// Act
		got, err := cacheRoot()

		// Assert
		if err != nil || !filepath.IsAbs(got) {
			t.Errorf("cacheRoot() = %q, %v, want the absolute platform cache directory", got, err)
		}
	})
}
