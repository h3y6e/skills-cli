package git

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ErrNotCached reports that the cache lacks an object.
var ErrNotCached = errors.New("not in the cache")

// Snapshot is a commit of a remote repository, read from the cache.
type Snapshot struct {
	Ref ResolvedRef
	FS  fs.FS
	// TreeSHA returns the git tree SHA of a repo-relative directory, "." for
	// the root.
	TreeSHA func(dir string) (string, error)
}

// Cache holds the objects of remote repositories across runs, one bare
// repository per remote, so that a commit fetched before is read offline.
// It reads each repository through a single git process until Close.
type Cache struct {
	mu        sync.Mutex
	repos     map[string]func() (*repo, error)
	checkouts []*os.Root
}

// NewCache opens the cache.
func NewCache() *Cache {
	return &Cache{repos: map[string]func() (*repo, error){}}
}

// Close stops reading the cache.
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error

	for _, open := range c.repos {
		if r, err := open(); err == nil {
			errs = append(errs, r.close())
		}
	}

	for _, root := range c.checkouts {
		errs = append(errs, root.Close(), os.RemoveAll(root.Name()))
	}

	return errors.Join(errs...)
}

// Checkout reads ref of cloneURL as git checkout writes it, converting line
// endings and applying filters as .gitattributes and core.autocrlf direct,
// fetching ref unless the cache holds it.
func (c *Cache) Checkout(ctx context.Context, cloneURL string, ref ResolvedRef) (fs.FS, error) {
	r, err := c.repo(ctx, cloneURL)
	if err != nil {
		return nil, err
	}

	tree, err := r.fetch(ctx, cloneURL, ref)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "skolar-checkout-*")
	if err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}

	c.mu.Lock()
	c.checkouts = append(c.checkouts, root)
	c.mu.Unlock()

	// A private index keeps concurrent checkouts off the repository's index.lock.
	restore, err := gitCommand(ctx, "--git-dir", r.gitDir, "--work-tree", dir, "restore", "--source", tree, "--worktree", "--", ":/")
	if err != nil {
		return nil, err
	}

	restore.Env = append(restore.Env, "GIT_INDEX_FILE="+dir+".index")
	defer os.Remove(dir + ".index")

	if out, err := restore.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git restore: %w\n%s", err, out)
	}

	return root.FS(), nil
}

// Fetch reads ref of cloneURL, fetching it unless the cache holds it.
func (c *Cache) Fetch(ctx context.Context, cloneURL string, ref ResolvedRef) (Snapshot, error) {
	r, err := c.repo(ctx, cloneURL)
	if err != nil {
		return Snapshot{}, err
	}

	tree, err := r.fetch(ctx, cloneURL, ref)
	if err != nil {
		return Snapshot{}, err
	}

	t := r.tree(tree)

	return Snapshot{Ref: ref, FS: t, TreeSHA: t.treeSHA}, nil
}

// Prefetch fetches ref of cloneURL unless the cache holds it.
func (c *Cache) Prefetch(ctx context.Context, cloneURL string, ref ResolvedRef) error {
	r, err := c.repo(ctx, cloneURL)
	if err != nil {
		return err
	}

	_, err = r.fetch(ctx, cloneURL, ref)

	return err
}

// ReadTree reads the tree treeSHA of cloneURL from the cache, offline.
func (c *Cache) ReadTree(ctx context.Context, cloneURL, treeSHA string) (fs.FS, error) {
	if !isObjectID(treeSHA) {
		return nil, fmt.Errorf("invalid tree SHA %q", treeSHA)
	}

	r, err := c.repo(ctx, cloneURL)
	if err != nil {
		return nil, err
	}

	if typ, err := r.info(treeSHA); err != nil || typ != "tree" {
		return nil, fmt.Errorf("tree %s: %w", treeSHA, ErrNotCached)
	}

	return r.tree(treeSHA), nil
}

// repo opens the repository of cloneURL, keyed so that the URLs one remote
// is named by, with or without .git, share it.
func (c *Cache) repo(ctx context.Context, cloneURL string) (*repo, error) {
	key := strings.TrimSuffix(strings.TrimSuffix(cloneURL, "/"), ".git")

	c.mu.Lock()

	open, ok := c.repos[key]
	if !ok {
		open = sync.OnceValues(func() (*repo, error) { return openRepo(ctx, key) })
		c.repos[key] = open
	}

	c.mu.Unlock()

	return open()
}

// repo is a bare repository in the cache, read through one
// git cat-file --batch-command process.
type repo struct {
	gitDir string
	mu     sync.Mutex
	in     io.WriteCloser
	out    *bufio.Reader
	close  func() error
}

func openRepo(ctx context.Context, key string) (*repo, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locate cache: %w", err)
	}

	// Private repositories land here, so only the owner may enter.
	root := filepath.Join(base, "skolar")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}

	sum := sha256.Sum256([]byte(key))
	r := &repo{gitDir: filepath.Join(root, "repos", hex.EncodeToString(sum[:12])+".git")}

	if err := r.init(ctx); err != nil {
		return nil, err
	}

	// The reader outlives the call that opens it; Close stops it.
	cmd, err := gitCommand(context.WithoutCancel(ctx), "--git-dir", r.gitDir, "cat-file", "--batch-command")
	if err != nil {
		return nil, err
	}

	if r.in, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	r.out = bufio.NewReader(stdout)
	r.close = sync.OnceValue(func() error { return errors.Join(r.in.Close(), cmd.Wait()) })

	return r, nil
}

func (r *repo) init(ctx context.Context) error {
	unlock, err := lock(ctx, r.gitDir+".lock")
	if err != nil {
		return err
	}
	defer unlock()

	if _, err := os.Stat(filepath.Join(r.gitDir, "HEAD")); !errors.Is(err, os.ErrNotExist) {
		return err
	}

	_, err = runGit(ctx, "init", "--quiet", "--bare", "--", r.gitDir)

	return err
}

// fetch returns the root tree of ref, fetching ref unless the repository
// holds it.
func (r *repo) fetch(ctx context.Context, cloneURL string, ref ResolvedRef) (string, error) {
	unlock, err := lock(ctx, r.gitDir+".lock")
	if err != nil {
		return "", err
	}
	defer unlock()

	// Only an object ID names what the remote holds now; a name the remote
	// no longer advertises may still name a stale ref in the cache.
	if isObjectID(ref.Object) {
		if tree, err := r.peelTree(ref.Object); err == nil {
			return tree, nil
		}
	}

	// A ref per fetched version keeps its objects from gc for verify; a flat
	// name keeps it from clashing with another, even on a case-insensitive
	// file system.
	local := ref.Object
	if !isObjectID(local) {
		sum := sha256.Sum256([]byte(ref.Full))
		local = hex.EncodeToString(sum[:12])
	}

	local = "refs/skolar/" + local

	// With every fetch in one pack, which git writes whole, an interrupted
	// fetch leaves no objects behind, so a cached commit is complete.
	if _, err := runGit(ctx, "-C", r.gitDir, "-c", "fetch.unpackLimit=1", "-c", "gc.autoDetach=false",
		"fetch", "--quiet", "--depth", "1", "--no-tags", "--no-write-fetch-head", "--", cloneURL, "+"+ref.Full+":"+local); err != nil {
		return "", err
	}

	return r.peelTree(local)
}

// peelTree returns the root tree of the commit object names.
func (r *repo) peelTree(object string) (string, error) {
	if _, err := r.info(object + "^{commit}"); err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	oid, _, err := r.request("info", object+"^{tree}")

	return oid, err
}

func (r *repo) info(object string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, typ, err := r.request("info", object)

	return typ, err
}

func (r *repo) contents(oid string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, size, err := r.request("contents", oid)
	if err != nil {
		return nil, err
	}

	n, err := strconv.Atoi(size)
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	data := make([]byte, n+1)
	if _, err := io.ReadFull(r.out, data); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	return data[:n], nil
}

// request sends a cat-file command and returns the object ID with the type
// for info, or the size for contents. The caller holds r.mu.
func (r *repo) request(command, object string) (string, string, error) {
	if strings.ContainsAny(object, "\n\r") {
		return "", "", fmt.Errorf("invalid object name %q", object)
	}

	if _, err := fmt.Fprintf(r.in, "%s %s\n", command, object); err != nil {
		return "", "", fmt.Errorf("git cat-file: %w", err)
	}

	header, err := r.out.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("git cat-file: %w", err)
	}

	fields := strings.Fields(header)
	if len(fields) != 3 {
		return "", "", fmt.Errorf("%s: %w", strings.TrimSpace(header), ErrNotCached)
	}

	if command == "info" {
		return fields[0], fields[1], nil
	}

	return fields[0], fields[2], nil
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
