package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tree is a read-only fs.FS over a commit in the cache. It never follows
// symbolic links: opening one fails, and Stat and ReadDir report them as
// links. Submodules appear as irregular files.
type Tree struct {
	entries  map[string]treeEntry
	children map[string][]string

	mu     sync.Mutex
	blobs  *bufio.Reader
	in     io.WriteCloser
	closer func() error
}

type treeEntry struct {
	mode fs.FileMode
	oid  string
	size int64
}

func openTree(ctx context.Context, gitDir, commit string) (*Tree, error) {
	out, err := runGit(ctx, "--git-dir", gitDir, "ls-tree", "-r", "-t", "-l", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}

	t := &Tree{
		entries:  map[string]treeEntry{".": {mode: fs.ModeDir | 0o755}},
		children: map[string][]string{},
	}

	for record := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		info, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(info)

		if !ok || len(fields) != 4 {
			return nil, fmt.Errorf("git ls-tree: unexpected entry %q", record)
		}

		size, _ := strconv.ParseInt(fields[3], 10, 64)
		t.entries[name] = treeEntry{mode: fileMode(fields[0]), oid: fields[2], size: size}
		dir := path.Dir(name)
		t.children[dir] = append(t.children[dir], path.Base(name))
	}

	cmd, err := gitCommand(ctx, "--git-dir", gitDir, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}

	if t.in, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	t.blobs = bufio.NewReader(stdout)
	t.closer = sync.OnceValue(func() error {
		return errors.Join(t.in.Close(), cmd.Wait())
	})

	return t, nil
}

func fileMode(gitMode string) fs.FileMode {
	switch gitMode {
	case "040000":
		return fs.ModeDir | 0o755
	case "100755":
		return 0o755
	case "100644":
		return 0o644
	case "120000":
		return fs.ModeSymlink | 0o777
	default:
		return fs.ModeIrregular
	}
}

// Close stops reading the cache.
func (t *Tree) Close() error {
	return t.closer()
}

// Open opens the regular file or directory name.
func (t *Tree) Open(name string) (fs.File, error) {
	e, err := t.lookup("open", name)
	if err != nil {
		return nil, err
	}

	switch {
	case e.mode.IsDir():
		return &treeDir{info: t.info(name, e), entries: t.readDir(name)}, nil
	case e.mode.IsRegular():
		data, err := t.blob(e.oid)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}

		return &treeFile{info: t.info(name, e), Reader: bytes.NewReader(data)}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("not a regular file or directory")}
}

// ReadFile reads the regular file name.
func (t *Tree) ReadFile(name string) ([]byte, error) {
	e, err := t.lookup("read", name)
	if err != nil {
		return nil, err
	}

	if !e.mode.IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: name, Err: errors.New("not a regular file")}
	}

	return t.blob(e.oid)
}

// ReadDir lists the directory name, sorted by name.
func (t *Tree) ReadDir(name string) ([]fs.DirEntry, error) {
	e, err := t.lookup("readdir", name)
	if err != nil {
		return nil, err
	}

	if !e.mode.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}

	return t.readDir(name), nil
}

// Stat describes name without following a symbolic link.
func (t *Tree) Stat(name string) (fs.FileInfo, error) {
	e, err := t.lookup("stat", name)
	if err != nil {
		return nil, err
	}

	return t.info(name, e), nil
}

func (t *Tree) lookup(op, name string) (treeEntry, error) {
	if !fs.ValidPath(name) {
		return treeEntry{}, &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}

	e, ok := t.entries[name]
	if !ok {
		return treeEntry{}, &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}

	return e, nil
}

func (t *Tree) info(name string, e treeEntry) treeInfo {
	return treeInfo{name: path.Base(name), entry: e}
}

func (t *Tree) readDir(name string) []fs.DirEntry {
	names := slices.Sorted(slices.Values(t.children[name]))

	entries := make([]fs.DirEntry, len(names))
	for i, n := range names {
		entries[i] = fs.FileInfoToDirEntry(t.info(path.Join(name, n), t.entries[path.Join(name, n)]))
	}

	return entries
}

func (t *Tree) blob(oid string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := io.WriteString(t.in, oid+"\n"); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	header, err := t.blobs.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	fields := strings.Fields(header)
	if len(fields) != 3 {
		return nil, fmt.Errorf("git cat-file: %s", strings.TrimSpace(header))
	}

	size, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	data := make([]byte, size+1)
	if _, err := io.ReadFull(t.blobs, data); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}

	return data[:size], nil
}

type treeInfo struct {
	name  string
	entry treeEntry
}

func (i treeInfo) Name() string       { return i.name }
func (i treeInfo) Size() int64        { return i.entry.size }
func (i treeInfo) Mode() fs.FileMode  { return i.entry.mode }
func (i treeInfo) ModTime() time.Time { return time.Time{} }
func (i treeInfo) IsDir() bool        { return i.entry.mode.IsDir() }
func (i treeInfo) Sys() any           { return nil }

type treeFile struct {
	info treeInfo
	*bytes.Reader
}

func (f *treeFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *treeFile) Close() error               { return nil }

type treeDir struct {
	info    treeInfo
	entries []fs.DirEntry
}

func (d *treeDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *treeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errors.New("is a directory")}
}
func (d *treeDir) Close() error { return nil }

func (d *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		entries := d.entries
		d.entries = nil

		return entries, nil
	}

	if len(d.entries) == 0 {
		return nil, io.EOF
	}

	n = min(n, len(d.entries))
	entries := d.entries[:n]
	d.entries = d.entries[n:]

	return entries, nil
}
