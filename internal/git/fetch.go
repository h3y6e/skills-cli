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

	if commit, tree, err := peel(ctx, gitDir, ref.Object); err == nil {
		return commit, tree, nil
	}

	// Each fetched ref keeps its own name, so the cache can hold many refs.
	local := ref.Full
	if !strings.HasPrefix(local, "refs/") {
		local = "refs/commits/" + local
	}

	if _, err := runGit(ctx, "-C", gitDir, "fetch", "--quiet", "--no-auto-gc", "--depth", "1", "--no-tags",
		"--no-write-fetch-head", "--", cloneURL, "+"+ref.Full+":"+local); err != nil {
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
