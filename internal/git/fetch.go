package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot is a commit of a remote repository, read from the cache.
type Snapshot struct {
	Ref ResolvedRef
	// FS holds the files of the commit; Fetch makes it a *Tree.
	FS fs.FS
	// Trees maps each repo-relative directory, "." for the root, to its git
	// tree SHA.
	Trees map[string]string
}

// Fetch reads ref of cloneURL through a cache of the remote's objects shared
// across runs: a commit fetched before is read offline. Close the snapshot
// when done.
func Fetch(ctx context.Context, cloneURL string, ref ResolvedRef) (Snapshot, error) {
	gitDir, err := cacheRepo(cloneURL)
	if err != nil {
		return Snapshot{}, err
	}

	commit, tree, err := fetchCached(ctx, gitDir, cloneURL, ref)
	if err != nil {
		return Snapshot{}, err
	}

	t, err := openTree(ctx, gitDir, commit)
	if err != nil {
		return Snapshot{}, err
	}

	trees := map[string]string{".": tree}

	for name, e := range t.entries {
		if e.mode.IsDir() && name != "." {
			trees[name] = e.oid
		}
	}

	return Snapshot{Ref: ref, FS: t, Trees: trees}, nil
}

// ErrNotCached reports that the cache lacks an object.
var ErrNotCached = errors.New("not in the cache")

// ReadTree reads the tree treeSHA of cloneURL from the cache, offline.
// Close the snapshot when done.
func ReadTree(ctx context.Context, cloneURL, treeSHA string) (Snapshot, error) {
	gitDir, err := cacheRepo(cloneURL)
	if err != nil {
		return Snapshot{}, err
	}

	if !isObjectID(treeSHA) {
		return Snapshot{}, fmt.Errorf("invalid tree SHA %q", treeSHA)
	}

	if !hasTree(ctx, gitDir, treeSHA) {
		return Snapshot{}, fmt.Errorf("tree %s: %w", treeSHA, ErrNotCached)
	}

	t, err := openTree(ctx, gitDir, treeSHA)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{FS: t, Trees: map[string]string{".": treeSHA}}, nil
}

// Prefetch fetches ref of cloneURL into the cache unless it is there.
func Prefetch(ctx context.Context, cloneURL string, ref ResolvedRef) error {
	gitDir, err := cacheRepo(cloneURL)
	if err != nil {
		return err
	}

	_, _, err = fetchCached(ctx, gitDir, cloneURL, ref)

	return err
}

func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}

	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}

	return true
}

func hasTree(ctx context.Context, gitDir, treeSHA string) bool {
	cmd, err := gitCommand(ctx, "--git-dir", gitDir, "cat-file", "--batch-check")
	if err != nil {
		return false
	}

	cmd.Stdin = strings.NewReader(treeSHA + "\n")
	out, err := cmd.Output()

	return err == nil && strings.Fields(string(out))[1] == "tree"
}

// Close releases the snapshot's files.
func (s Snapshot) Close() error {
	if c, ok := s.FS.(io.Closer); ok {
		return c.Close()
	}

	return nil
}

// cacheDir returns the directory of the object caches. Replaceable for testing.
var cacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "skolar", "repos"), nil
}

func cacheRepo(cloneURL string) (string, error) {
	root, err := cacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache: %w", err)
	}

	sum := sha256.Sum256([]byte(cloneURL))

	return filepath.Join(root, hex.EncodeToString(sum[:12])+".git"), nil
}

// fetchCached returns the commit and root tree ref names in gitDir, fetching
// them from cloneURL unless gitDir already holds them.
func fetchCached(ctx context.Context, gitDir, cloneURL string, ref ResolvedRef) (string, string, error) {
	unlock, err := lock(gitDir + ".lock")
	if err != nil {
		return "", "", err
	}
	defer unlock()

	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if _, err := runGit(ctx, "init", "--quiet", "--bare", "--", gitDir); err != nil {
			return "", "", err
		}
	}

	// Only an object ID names what the remote holds now; a name the remote
	// no longer advertises may still name a stale ref in the cache.
	if isObjectID(ref.Object) {
		if commit, tree, err := peel(ctx, gitDir, ref.Object); err == nil {
			return commit, tree, nil
		}
	}

	// A ref per fetched ref keeps its objects from gc; a flat name keeps it
	// from clashing with another, even on a case-insensitive file system.
	sum := sha256.Sum256([]byte(ref.Full))
	local := "refs/skolar/" + hex.EncodeToString(sum[:12])

	// With every fetch in one pack, which git writes whole, an interrupted
	// fetch leaves no objects behind, so a cached commit is complete.
	if _, err := runGit(ctx, "-C", gitDir, "-c", "fetch.unpackLimit=1", "-c", "gc.autoDetach=false",
		"fetch", "--quiet", "--depth", "1", "--no-tags", "--no-write-fetch-head", "--", cloneURL, "+"+ref.Full+":"+local); err != nil {
		return "", "", err
	}

	return peel(ctx, gitDir, local)
}

// peel returns the commit and root tree object names gitDir holds for object.
func peel(ctx context.Context, gitDir, object string) (string, string, error) {
	cmd, err := gitCommand(ctx, "--git-dir", gitDir, "cat-file", "--batch-check")
	if err != nil {
		return "", "", err
	}

	cmd.Stdin = strings.NewReader(object + "^{commit}\n" + object + "^{tree}\n")

	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git cat-file: %w", err)
	}

	var names []string

	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return "", "", fmt.Errorf("%s: %s", gitDir, strings.TrimSpace(line))
		}

		names = append(names, fields[0])
	}

	return names[0], names[1], nil
}

// Tree returns the tree SHA of the repo-relative directory path.
func (s Snapshot) Tree(path string) (string, error) {
	tree, ok := s.Trees[path]
	if !ok {
		return "", fmt.Errorf("no directory %q at %s", path, s.Ref.Full)
	}

	return tree, nil
}
