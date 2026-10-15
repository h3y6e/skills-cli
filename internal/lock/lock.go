package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the standard lockfile name (compatible with vercel-labs/skills).
const FileName = "skills-lock.json"

const DefaultDir = ".agents/skills"

// File represents the lockfile contents.
type File struct {
	Version int              `json:"version"`
	Skills  map[string]Entry `json:"skills"`
}

// Entry represents a single skill in the lockfile.
type Entry struct {
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath,omitempty"`
	ComputedHash string `json:"computedHash"`
}

func ReadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read lockfile %q: %w", path, err)
	}

	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("decode lockfile %q: %w", path, err)
	}

	if file.Skills == nil {
		file.Skills = map[string]Entry{}
	}

	return file, nil
}

// WriteFile atomically writes the lockfile.
func WriteFile(path string, file File) error {
	if file.Skills == nil {
		file.Skills = map[string]Entry{}
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lockfile %q: %w", path, err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create lockfile parent %q: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills-lock-*.json")
	if err != nil {
		return fmt.Errorf("create temp lockfile for %q: %w", path, err)
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp lockfile for %q: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp lockfile for %q: %w", path, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace lockfile %q: %w", path, err)
	}

	return nil
}

// Layout describes the directory structure of installed skills and the lockfile.
type Layout struct {
	Dir string
}

func NewLayout(skillsDir string) Layout {
	return Layout{Dir: filepath.Clean(skillsDir)}
}

func (l Layout) LockPath() string {
	if filepath.IsAbs(l.Dir) {
		return filepath.Join(filepath.Dir(filepath.Dir(l.Dir)), FileName)
	}
	return FileName
}

func (l Layout) SkillDir(name string) string {
	return filepath.Join(l.Dir, name)
}

func FilePath(skillsDir string) string {
	return NewLayout(skillsDir).LockPath()
}

// UsesLockfile reports whether the lockfile records this layout's
// directory, which holds only for DefaultDir beside the lockfile.
func (l Layout) UsesLockfile() bool {
	rel, err := filepath.Rel(filepath.Dir(l.LockPath()), filepath.Clean(l.Dir))
	return err == nil && rel == filepath.Clean(DefaultDir)
}

// Entries returns the lockfile's skills when the lockfile records this
// layout's directory, and none otherwise.
func (l Layout) Entries(f File) map[string]Entry {
	if !l.UsesLockfile() {
		return map[string]Entry{}
	}
	return f.Skills
}
