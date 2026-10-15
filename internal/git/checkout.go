package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot is a commit of a remote repository checked out without .git.
type Snapshot struct {
	Dir string
	Ref ResolvedRef
	// Trees maps each repo-relative directory, "." for the root, to its git
	// tree SHA.
	Trees map[string]string
}

// Checkout checks out ref of cloneURL into dir, fetching through a cache of
// the remote's objects shared across runs: a commit fetched before is
// checked out offline.
func Checkout(ctx context.Context, cloneURL string, ref ResolvedRef, dir string) (Snapshot, error) {
	gitDir, err := cacheRepo(cloneURL)
	if err != nil {
		return Snapshot{}, err
	}

	commit, tree, err := fetchCached(ctx, gitDir, cloneURL, ref)
	if err != nil {
		return Snapshot{}, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, err
	}

	// A private index keeps concurrent checkouts off the cache's index.lock.
	index := filepath.Clean(dir) + ".index"
	defer os.Remove(index)

	restore, err := gitCommand(ctx, "--git-dir", gitDir, "--work-tree", dir, "restore", "--source", commit, "--worktree", "--", ":/")
	if err != nil {
		return Snapshot{}, err
	}

	restore.Env = append(restore.Env, "GIT_INDEX_FILE="+index)
	if out, err := restore.CombinedOutput(); err != nil {
		return Snapshot{}, fmt.Errorf("git restore: %w\n%s", err, out)
	}

	trees, err := listTrees(ctx, gitDir, commit)
	if err != nil {
		return Snapshot{}, err
	}

	trees["."] = tree

	return Snapshot{Dir: dir, Ref: ref, Trees: trees}, nil
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

func listTrees(ctx context.Context, gitDir, commit string) (map[string]string, error) {
	out, err := runGit(ctx, "--git-dir", gitDir, "ls-tree", "-r", "-d", "-z", commit)
	if err != nil {
		return nil, err
	}

	trees := map[string]string{}

	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		info, path, ok := strings.Cut(entry, "\t")
		if fields := strings.Fields(info); ok && len(fields) == 3 && fields[1] == "tree" {
			trees[path] = fields[2]
		}
	}

	return trees, nil
}

// Tree returns the tree SHA of the repo-relative directory path.
func (s Snapshot) Tree(path string) (string, error) {
	tree, ok := s.Trees[path]
	if !ok {
		return "", fmt.Errorf("no directory %q at %s", path, s.Ref.Full)
	}

	return tree, nil
}
