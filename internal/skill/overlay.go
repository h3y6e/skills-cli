package skill

import (
	"bytes"
	"fmt"
	"io/fs"
	"time"
)

// withMetadata returns skill with gh source metadata merged into its
// SKILL.md, so that the installed result stays updatable by gh skill.
func withMetadata(skill fs.FS, meta SourceMetadata) (fs.FS, error) {
	data, err := fs.ReadFile(skill, "SKILL.md")
	if err != nil {
		return nil, err
	}

	injected, err := InjectSourceMetadata(string(data), meta)
	if err != nil {
		return nil, fmt.Errorf("SKILL.md: %w", err)
	}

	return overlayFS{FS: skill, skillMD: []byte(injected)}, nil
}

// overlayFS replaces the content of the SKILL.md at the root of FS.
type overlayFS struct {
	fs.FS
	skillMD []byte
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if name != "SKILL.md" {
		return o.FS.Open(name)
	}

	return &memFile{Reader: bytes.NewReader(o.skillMD), size: int64(len(o.skillMD))}, nil
}

type memFile struct {
	*bytes.Reader
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f, nil }
func (f *memFile) Close() error               { return nil }
func (f *memFile) Name() string               { return "SKILL.md" }
func (f *memFile) Size() int64                { return f.size }
func (f *memFile) Mode() fs.FileMode          { return 0o644 }
func (f *memFile) ModTime() time.Time         { return time.Time{} }
func (f *memFile) IsDir() bool                { return false }
func (f *memFile) Sys() any                   { return nil }
